// Package path discovers the hops towards a target (TTL-limited probes) and
// splits them into the three zones LAN, ISP_EDGE and WAN.
package path

import (
	"context"
	"net/netip"
	"sync"
	"time"

	"github.com/chevchelios420x/proof-of-fault/internal/probe"
)

// Zone is one of the three analysis zones.
type Zone string

const (
	LAN     Zone = "LAN"
	ISPEdge Zone = "ISP_EDGE"
	WAN     Zone = "WAN"
)

// Zones in path order; a fault is attributed to the first failing zone.
var Zones = []Zone{LAN, ISPEdge, WAN}

// Hop is one TTL step on the path.
type Hop struct {
	TTL        int     `json:"ttl"`
	Addr       string  `json:"addr"` // empty when the hop never answered
	Responsive bool    `json:"responsive"`
	RTTMs      float64 `json:"rttMs"`
	Zone       Zone    `json:"zone"`
	Manual     bool    `json:"manual"` // zone set by the user
}

// ValidZone reports whether z is one of the three zones.
func ValidZone(z Zone) bool { return z == LAN || z == ISPEdge || z == WAN }

const (
	maxTTL        = 30
	probesPerHop  = 3
	probeTimeout  = 2 * time.Second
	parallelLimit = 10
)

// Discover traces the path to dst. Silent hops (ICMP filtered) are kept with
// Responsive=false; they are not treated as failures. overrides maps hop
// addresses to user-chosen zones.
func Discover(ctx context.Context, p probe.Prober, dst netip.Addr, overrides map[string]Zone) ([]Hop, error) {
	hops := make([]Hop, maxTTL)
	sem := make(chan struct{}, parallelLimit)
	var wg sync.WaitGroup
	for ttl := 1; ttl <= maxTTL; ttl++ {
		wg.Add(1)
		go func(ttl int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			h := Hop{TTL: ttl}
			for i := 0; i < probesPerHop && ctx.Err() == nil; i++ {
				r, err := p.Probe(ctx, probe.Request{Dst: dst, TTL: ttl, Timeout: probeTimeout})
				if err != nil || r.Kind == probe.Timeout || !r.From.IsValid() {
					continue
				}
				h.Addr = r.From.String()
				h.Responsive = true
				h.RTTMs = float64(r.RTT) / float64(time.Millisecond)
				if r.Kind == probe.EchoReply || r.Kind == probe.Unreachable {
					break
				}
			}
			hops[ttl-1] = h
		}(ttl)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Cut the path after the first hop that is the destination itself.
	for i, h := range hops {
		if h.Addr == dst.String() {
			hops = hops[:i+1]
			break
		}
	}
	// Otherwise drop trailing silent hops (target filters ICMP).
	for len(hops) > 0 && !hops[len(hops)-1].Responsive {
		hops = hops[:len(hops)-1]
	}
	Classify(hops, overrides)
	return hops, nil
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// isLocal reports whether addr belongs to a home network. CGNAT space
// (100.64/10) is deliberately excluded: it is operated by the provider.
func isLocal(a netip.Addr) bool {
	return a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLoopback()
}

// Classify assigns zones in place:
//   - LAN: the leading run of private/link-local hops (router, cascaded
//     routers, cable modem in router mode). Hop 1 is always LAN.
//   - ISP_EDGE: the first hop after LAN (including CGNAT/provider transfer
//     nets) up to and including the first public address.
//   - WAN: everything behind that.
//
// Silent hops inherit the zone of the region they are in. Afterwards user
// overrides (by hop address) replace the automatic zone.
func Classify(hops []Hop, overrides map[string]Zone) {
	zone := LAN
	sawEdgeHop := false
	for i := range hops {
		h := &hops[i]
		a, err := netip.ParseAddr(h.Addr)
		switch {
		case i == 0:
			// Default gateway.
		case zone == LAN && err == nil && !isLocal(a):
			zone = ISPEdge
		case zone == ISPEdge && sawEdgeHop:
			zone = WAN
		}
		h.Zone = zone
		if zone == ISPEdge && err == nil && !cgnat.Contains(a) && !isLocal(a) {
			// First public provider address closes the edge zone.
			sawEdgeHop = true
		}
	}
	for i := range hops {
		h := &hops[i]
		h.Manual = false
		if z, ok := overrides[h.Addr]; ok && h.Addr != "" && ValidZone(z) {
			h.Zone, h.Manual = z, true
		}
	}
}

// Representative is the address that is pinged continuously for a zone.
type Representative struct {
	Zone Zone       `json:"zone"`
	Addr netip.Addr `json:"-"`
	IP   string     `json:"ip"`
	TTL  int        `json:"ttl"`
	// Direct is true if the hop answers echo requests itself; otherwise it is
	// measured with TTL-limited probes towards the target.
	Direct bool `json:"direct"`
}

// Representatives picks one hop per zone: the last responsive LAN hop (covers
// the whole home network), the first responsive ISP_EDGE hop and the target.
func Representatives(ctx context.Context, p probe.Prober, hops []Hop, target netip.Addr) []Representative {
	var reps []Representative
	var lan, edge *Hop
	for i := range hops {
		h := &hops[i]
		if !h.Responsive {
			continue
		}
		switch h.Zone {
		case LAN:
			lan = h
		case ISPEdge:
			if edge == nil {
				edge = h
			}
		}
	}
	for _, c := range []struct {
		zone Zone
		hop  *Hop
	}{{LAN, lan}, {ISPEdge, edge}} {
		if c.hop == nil {
			continue
		}
		a, err := netip.ParseAddr(c.hop.Addr)
		if err != nil || a == target {
			continue
		}
		reps = append(reps, Representative{
			Zone: c.zone, Addr: a, IP: a.String(), TTL: c.hop.TTL,
			Direct: AnswersEcho(ctx, p, a),
		})
	}
	reps = append(reps, Representative{Zone: WAN, Addr: target, IP: target.String(), Direct: true})
	return reps
}

// AnswersEcho reports whether a answers direct echo requests.
func AnswersEcho(ctx context.Context, p probe.Prober, a netip.Addr) bool {
	for i := 0; i < 3; i++ {
		r, err := p.Probe(ctx, probe.Request{Dst: a, Timeout: time.Second})
		if err == nil && r.Kind == probe.EchoReply {
			return true
		}
	}
	return false
}
