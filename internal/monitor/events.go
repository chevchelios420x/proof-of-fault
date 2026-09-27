package monitor

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chevchelios420x/proof-of-fault/internal/path"
	"github.com/chevchelios420x/proof-of-fault/internal/store"
)

// EvLog is the UI event carrying one new event-log entry.
const EvLog = "event"

// Event kinds of the event log.
const (
	KindSession     = "session"
	KindPath        = "path"
	KindPathChange  = "path_change"
	KindZones       = "zones"
	KindLoss        = "loss"        // loss from a hop on up to the target: real
	KindLossHop     = "loss_hop"    // loss only at an intermediate hop: harmless
	KindSpike       = "spike"       // latency spike from a hop on up to the target
	KindLossDevice  = "loss_device" // user-defined measuring point did not answer
	KindOutageStart = "outage_start"
	KindOutageEnd   = "outage_end"
)

const (
	targetTTL  = 1 << 10 // sort key of the target (behind every hop)
	episodeGap = 2       // ticks without the condition that close an episode
	baselineN  = 60      // successful samples forming a series' baseline
)

// series describes one probed line (zone measuring point or watched hop).
type series struct {
	key   string
	ttl   int
	zone  path.Zone // evaluation role (LAN, ISP_EDGE, WAN, none)
	group string    // display zone (user zone ID), "" = same as zone
	addr  string
	label string
}

// tick collects the results of all probes sent at the same moment, so that
// losses and spikes can be judged along the path.
type tick struct {
	seq    int
	at     time.Time
	order  []series // sorted by TTL, target last
	custom []series // user-defined measuring points (not on the path)
	result map[string]time.Duration
}

type episode struct {
	kind      string
	attr      series
	start     time.Time
	last      time.Time
	lastSeq   int
	ticks     int
	lost      map[string]int // lost probes per series key
	peak      time.Duration
	baseline  time.Duration
	before    string // last series before attr that was fine
	harmlessK []string
}

// baseline keeps the last successful RTTs of a series.
type baseline struct {
	rtts []time.Duration
}

func (b *baseline) add(d time.Duration) {
	b.rtts = append(b.rtts, d)
	if len(b.rtts) > baselineN {
		b.rtts = b.rtts[len(b.rtts)-baselineN:]
	}
}

func (b *baseline) median() time.Duration {
	if len(b.rtts) < 10 {
		return 0 // not enough history yet
	}
	c := append([]time.Duration(nil), b.rtts...)
	sort.Slice(c, func(i, j int) bool { return c[i] < c[j] })
	return c[len(c)/2]
}

// seriesOrder snapshots the probed series in path order.
func (r *runner) seriesOrder() []series {
	names := r.names
	label := func(ttl int, addr string, z path.Zone) string {
		n := addr
		if names[addr] != "" {
			n = names[addr] + " (" + addr + ")"
		}
		if ttl >= targetTTL {
			return "Ziel " + n
		}
		if z != "" {
			return fmt.Sprintf("Hop %d %s [%s]", ttl, n, z)
		}
		return fmt.Sprintf("Hop %d %s", ttl, n)
	}
	var out []series
	isRep := map[string]bool{}
	for _, rep := range r.reps {
		ttl := rep.TTL
		if rep.Zone == path.WAN || ttl == 0 {
			ttl = targetTTL
		}
		isRep[rep.IP] = true
		out = append(out, series{key: string(rep.Zone), ttl: ttl, zone: rep.Zone, addr: rep.IP, label: label(ttl, rep.IP, rep.Zone)})
	}
	for addr, h := range r.hopMode {
		if isRep[addr] {
			continue
		}
		out = append(out, series{key: HopKey(addr), ttl: h.ttl, zone: r.zoneOf(addr), addr: addr, label: label(h.ttl, addr, "")})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ttl < out[j].ttl })
	return out
}

func (r *runner) zoneOf(addr string) path.Zone {
	for _, h := range r.hops {
		if h.Addr == addr {
			return h.Zone
		}
	}
	return ""
}

// finishTick judges one tick and extends or opens episodes.
func (r *runner) finishTick(t *tick) {
	n := len(t.order)
	if n == 0 {
		return
	}
	lost := make([]bool, n)
	spiked := make([]bool, n)
	base := make([]time.Duration, n)
	anyLost := false
	for i, s := range t.order {
		rtt, ok := t.result[s.key]
		if !ok || rtt < 0 {
			lost[i], anyLost = true, true
			continue
		}
		b := r.base[s.key]
		if b == nil {
			b = &baseline{}
			r.base[s.key] = b
		}
		m := b.median()
		base[i] = m
		if r.cfg.IsSpike(string(s.zone), rtt, m) {
			spiked[i] = true
		}
		b.add(rtt)
	}

	// suffixFrom returns the first index from which on every series matches.
	suffixFrom := func(match func(i int) bool) int {
		i := n
		for i > 0 && match(i-1) {
			i--
		}
		return i
	}

	lostFrom := n
	if anyLost {
		from := suffixFrom(func(i int) bool { return lost[i] })
		lostFrom = from
		if from < n {
			r.touch(t, KindLoss, from, 0, 0, lost)
			for i := from; i < n; i++ {
				r.realLost[t.order[i].key]++
			}
		}
		// Losses before the lost suffix: the path behind answered.
		for i := 0; i < from; i++ {
			if lost[i] {
				r.touch(t, KindLossHop, i, 0, 0, lost)
			}
		}
	}
	// Spike: target slow (or lost after a spike) and a run of slow series
	// reaching it; the first slow series is where it starts.
	from := suffixFrom(func(i int) bool { return spiked[i] || lost[i] })
	for from < n && !spiked[from] {
		from++
	}
	if from < n && spiked[n-1] {
		var peak time.Duration
		for i := from; i < n; i++ {
			if d := t.result[t.order[i].key]; d > peak {
				peak = d
			}
		}
		r.touch(t, KindSpike, from, peak, base[from], lost)
	}
	r.checkCustom(t, lost)

	st := map[string]byte{}
	for i, s := range t.order {
		switch {
		case lost[i]:
			st[s.key] = 'x'
		case spiked[i]:
			st[s.key] = 's'
		default:
			st[s.key] = '.'
		}
	}
	r.customStates(t, st)
	ts := &tickState{t: t, state: st}
	ts.class, ts.origin = classifyTick(t, st, lostFrom)
	r.trackIncident(ts)

	r.closeEpisodes(t.seq, false)
}

// checkCustom logs unanswered user-defined measuring points together with
// the state of the path at the same moment.
func (r *runner) checkCustom(t *tick, pathLost []bool) {
	for _, c := range t.custom {
		if c.zone == ZoneNone {
			continue // recorded, but not evaluated
		}
		if rtt, ok := t.result[c.key]; ok && rtt >= 0 {
			continue
		}
		if otherProtocolAnswered(t, c.key) {
			continue // maybe false positive: the other protocol reached the host
		}
		ctx := "Der Heimrouter antwortete zur selben Zeit."
		for i, s := range t.order {
			if s.zone == path.LAN && pathLost[i] {
				ctx = "Zur selben Zeit antwortete auch der Heimrouter nicht."
			}
		}
		if n := len(t.order); n > 0 && pathLost[n-1] {
			ctx += " Das Ziel war ebenfalls nicht erreichbar."
		}
		key := KindLossDevice + "|" + c.key
		ep := r.episodes[key]
		if ep == nil {
			ep = &episode{kind: KindLossDevice, attr: c, start: t.at, lost: map[string]int{}, before: ctx}
			r.episodes[key] = ep
		}
		ep.last, ep.lastSeq = t.at, t.seq
		ep.ticks++
	}
}

// touch extends the running episode of the same kind and origin or opens a new one.
func (r *runner) touch(t *tick, kind string, idx int, peak, base time.Duration, lost []bool) {
	s := t.order[idx]
	key := kind + "|" + s.key
	ep := r.episodes[key]
	if ep == nil {
		ep = &episode{kind: kind, attr: s, start: t.at, lost: map[string]int{}, baseline: base}
		for i := idx - 1; i >= 0; i-- {
			if !lost[i] {
				ep.before = t.order[i].label
				break
			}
		}
		r.episodes[key] = ep
	}
	ep.last, ep.lastSeq = t.at, t.seq
	ep.ticks++
	if peak > ep.peak {
		ep.peak = peak
	}
	for i, l := range lost {
		if l {
			ep.lost[t.order[i].key]++
		}
	}
}

// closeEpisodes writes episodes that have not been extended recently (or
// all, when the session ends) to the event log.
func (r *runner) closeEpisodes(seq int, all bool) {
	for key, ep := range r.episodes {
		if !all && seq-ep.lastSeq <= episodeGap {
			continue
		}
		delete(r.episodes, key)
		r.logEvent(ep.toEvent())
	}
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func (ep *episode) toEvent() store.Event {
	dur := ep.last.Sub(ep.start) + interval
	e := store.Event{
		T: ep.start.UnixMilli(), End: ep.last.Add(interval).UnixMilli(),
		Kind: ep.kind, Zone: string(ep.attr.zone), TTL: ep.attr.ttl, Addr: ep.attr.addr, Count: ep.ticks,
	}
	if e.TTL >= targetTTL {
		e.TTL = 0
	}
	secs := fmt.Sprintf("%d s", int(dur.Round(time.Second)/time.Second))
	before := ""
	if ep.before != "" {
		before = " Davor antwortete " + ep.before + " normal."
	}
	switch ep.kind {
	case KindLoss:
		e.Severity = "warn"
		e.Title = fmt.Sprintf("Paketverlust ab %s", ep.attr.label)
		e.Detail = fmt.Sprintf("%d Sekunde(n) betroffen (%s). Ab %s bis zum Ziel kam keine Antwort.%s Ursache liegt damit im Bereich %s.",
			ep.ticks, secs, ep.attr.label, before, zoneText(ep.attr.zone))
	case KindLossHop:
		e.Severity = "info"
		e.Title = fmt.Sprintf("Verlust nur an %s (harmlos)", ep.attr.label)
		e.Detail = fmt.Sprintf("%d Probe(s) ohne Antwort, aber spätere Hops und das Ziel antworteten. Typische ICMP-Drosselung dieses Routers – kein Verbindungsproblem.", ep.ticks)
	case KindLossDevice:
		e.Severity = "warn"
		e.Title = fmt.Sprintf("Keine Antwort von %s", ep.attr.label)
		e.Detail = fmt.Sprintf("%d Sekunde(n) ohne Antwort (%s). %s", ep.ticks, secs, ep.before)
	case KindSpike:
		e.Severity = "warn"
		e.ValueMs = ms(ep.peak)
		e.Title = fmt.Sprintf("Latenzspitze ab %s: bis %.1f ms", ep.attr.label, ms(ep.peak))
		e.Detail = fmt.Sprintf("%d Sekunde(n) erhöht (%s), normal sind hier ca. %.1f ms. Ab %s bis zum Ziel war die Antwortzeit deutlich erhöht.%s Ursache liegt damit im Bereich %s.",
			ep.ticks, secs, ms(ep.baseline), ep.attr.label, before, zoneText(ep.attr.zone))
	}
	return e
}

func zoneText(z path.Zone) string {
	switch z {
	case path.LAN:
		return "LAN (eigenes Heimnetz: Router/WLAN/Kabel)"
	case path.ISPEdge:
		return "ISP_EDGE (Anschluss / Netz des Internetanbieters)"
	case path.WAN:
		return "WAN (Internet hinter dem Anbieter / Zielserver)"
	}
	return "unbekannt"
}

// logEvent stores an event and sends it to the UI.
func (r *runner) logEvent(e store.Event) {
	if e.T == 0 {
		e.T = time.Now().UnixMilli()
	}
	e, _ = r.m.store.AddEvent(r.sid, e)
	r.m.emit(EvLog, e)
}

// describePath renders hops as "1 192.168.0.1 [LAN] → 2 * → …".
func describePath(hops []path.Hop, names map[string]string) string {
	var parts []string
	for _, h := range hops {
		if !h.Responsive {
			parts = append(parts, fmt.Sprintf("%d *", h.TTL))
			continue
		}
		n := h.Addr
		if names[h.Addr] != "" {
			n = names[h.Addr] + " " + h.Addr
		}
		parts = append(parts, fmt.Sprintf("%d %s [%s]", h.TTL, n, h.Zone))
	}
	return strings.Join(parts, " → ")
}

// diffPath lists the per-TTL differences between two paths.
func diffPath(old, cur []path.Hop) string {
	at := func(hs []path.Hop, ttl int) string {
		for _, h := range hs {
			if h.TTL == ttl {
				if !h.Responsive {
					return "*"
				}
				return h.Addr
			}
		}
		return "–"
	}
	maxTTL := 0
	for _, h := range append(append([]path.Hop(nil), old...), cur...) {
		maxTTL = max(maxTTL, h.TTL)
	}
	var lines []string
	for ttl := 1; ttl <= maxTTL; ttl++ {
		a, b := at(old, ttl), at(cur, ttl)
		if a != b {
			lines = append(lines, fmt.Sprintf("Hop %d: %s → %s", ttl, a, b))
		}
	}
	if len(lines) == 0 {
		return "keine Adressänderung"
	}
	return strings.Join(lines, "; ")
}

func repsText(reps []path.Representative) string {
	var parts []string
	for _, r := range reps {
		parts = append(parts, fmt.Sprintf("%s=%s", r.Zone, r.IP))
	}
	return strings.Join(parts, ", ")
}
