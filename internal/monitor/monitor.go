// Package monitor runs a measurement session: path discovery, one probe per
// zone and second, fault attribution and persistence.
package monitor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/chevchelios420x/proof-of-fault/internal/metrics"
	"github.com/chevchelios420x/proof-of-fault/internal/path"
	"github.com/chevchelios420x/proof-of-fault/internal/probe"
	"github.com/chevchelios420x/proof-of-fault/internal/store"
)

const (
	interval       = time.Second
	probeTimeout   = 3 * time.Second
	outageAfter    = 3 // consecutive losses
	rediscoverEach = 5 * time.Minute
)

// Emitter delivers events to the UI.
type Emitter func(event string, data any)

// Event names.
const (
	EvStatus  = "status"
	EvPath    = "path"
	EvSamples = "samples"
	EvStats   = "stats"
	EvOutage  = "outage"
)

// Status is sent whenever the state of the monitor changes.
type Status struct {
	State     string                `json:"state"` // idle | resolving | discovering | running | error
	Message   string                `json:"message"`
	SessionID int64                 `json:"sessionId"`
	Target    string                `json:"target"`
	TargetIP  string                `json:"targetIp"`
	Reps      []path.Representative `json:"reps"`
	Hops      []path.Hop            `json:"hops"`
}

// LiveSample is sent to the UI in batches once per second.
type LiveSample struct {
	Zone  path.Zone `json:"zone"`
	T     int64     `json:"t"`     // unix ms
	RTTMs float64   `json:"rttMs"` // -1 = loss
}

// OutageEvent is sent when the attributed fault zone changes.
type OutageEvent struct {
	Zone   path.Zone `json:"zone"` // empty = recovered
	Since  int64     `json:"since"`
	Active bool      `json:"active"`
}

// Monitor owns at most one running session.
type Monitor struct {
	prober probe.Prober
	store  *store.Store
	emit   Emitter

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	status Status
}

// New creates a monitor.
func New(p probe.Prober, s *store.Store, emit Emitter) *Monitor {
	return &Monitor{prober: p, store: s, emit: emit, status: Status{State: "idle"}}
}

// Status returns the current status.
func (m *Monitor) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *Monitor) setStatus(f func(*Status)) {
	m.mu.Lock()
	f(&m.status)
	st := m.status
	m.mu.Unlock()
	m.emit(EvStatus, st)
}

// Start begins monitoring target (IP or host name).
func (m *Monitor) Start(target string) error {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return errors.New("Überwachung läuft bereits")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.done = make(chan struct{})
	m.status = Status{State: "resolving", Target: target}
	m.mu.Unlock()

	go func() {
		defer close(m.done)
		err := m.run(ctx, target)
		if err != nil && !errors.Is(err, context.Canceled) {
			m.setStatus(func(s *Status) { s.State, s.Message = "error", err.Error() })
		} else {
			m.setStatus(func(s *Status) { s.State, s.Message = "idle", "" })
		}
		m.mu.Lock()
		m.cancel = nil
		m.mu.Unlock()
	}()
	return nil
}

// Stop ends the running session and waits for it to finish.
func (m *Monitor) Stop() {
	m.mu.Lock()
	cancel, done := m.cancel, m.done
	m.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

func resolve(ctx context.Context, target string) (netip.Addr, error) {
	if a, err := netip.ParseAddr(target); err == nil {
		if !a.Is4() {
			return netip.Addr{}, errors.New("IPv6 wird noch nicht unterstützt")
		}
		return a, nil
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", target)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("DNS-Auflösung von %q fehlgeschlagen: %w", target, err)
	}
	if len(addrs) == 0 {
		return netip.Addr{}, fmt.Errorf("keine IPv4-Adresse für %q", target)
	}
	return addrs[0].Unmap(), nil
}

func (m *Monitor) run(ctx context.Context, target string) error {
	dst, err := resolve(ctx, target)
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	sid, err := m.store.CreateSession(target, dst.String(), map[string]string{
		"hostname": host, "os": runtime.GOOS, "arch": runtime.GOARCH,
	})
	if err != nil {
		return err
	}
	defer m.store.EndSession(sid)

	m.setStatus(func(s *Status) {
		s.State, s.SessionID, s.TargetIP = "discovering", sid, dst.String()
		s.Message = "Ermittle Route (Traceroute) …"
	})
	hops, err := path.Discover(ctx, m.prober, dst)
	if err != nil {
		return err
	}
	if len(hops) == 0 {
		return errors.New("kein einziger Hop hat geantwortet – ist die Netzwerkverbindung aktiv?")
	}
	reps := path.Representatives(ctx, m.prober, hops, dst)
	m.store.SavePath(sid, time.Now(), hops)
	m.setStatus(func(s *Status) {
		s.State, s.Message, s.Hops, s.Reps = "running", "", hops, reps
	})

	r := newRunner(m, sid, dst, hops, reps)
	return r.loop(ctx)
}

// runner holds the state of one running session.
type runner struct {
	m    *Monitor
	sid  int64
	dst  netip.Addr
	hops []path.Hop
	reps []path.Representative

	results chan result
	zones   map[path.Zone]*zoneState
	fault   path.Zone // currently attributed outage zone, "" = none
	pending []LiveSample
}

type result struct {
	zone path.Zone
	at   time.Time
	rtt  time.Duration // <0 = loss
}

type zoneState struct {
	rtts        []time.Duration
	streak      int
	streakStart time.Time
}

func newRunner(m *Monitor, sid int64, dst netip.Addr, hops []path.Hop, reps []path.Representative) *runner {
	r := &runner{m: m, sid: sid, dst: dst, hops: hops, reps: reps,
		results: make(chan result, 64), zones: map[path.Zone]*zoneState{}}
	for _, z := range path.Zones {
		r.zones[z] = &zoneState{}
	}
	return r
}

func (r *runner) loop(ctx context.Context) error {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	flush := time.NewTicker(time.Second)
	defer flush.Stop()
	rediscover := time.NewTicker(rediscoverEach)
	defer rediscover.Stop()
	pathUpdates := make(chan []path.Hop, 1)

	var wg sync.WaitGroup
	defer wg.Wait()

	r.fire(ctx, &wg)
	for {
		select {
		case <-ctx.Done():
			r.closeFault(time.Now())
			return ctx.Err()
		case <-tick.C:
			r.fire(ctx, &wg)
		case res := <-r.results:
			r.handle(res)
		case <-flush.C:
			r.flushLive()
		case <-rediscover.C:
			wg.Add(1)
			go func() {
				defer wg.Done()
				if hops, err := path.Discover(ctx, r.m.prober, r.dst); err == nil && len(hops) > 0 {
					select {
					case pathUpdates <- hops:
					default:
					}
				}
			}()
		case hops := <-pathUpdates:
			r.updatePath(ctx, hops)
		}
	}
}

// fire sends one probe per representative without blocking the loop.
func (r *runner) fire(ctx context.Context, wg *sync.WaitGroup) {
	for _, rep := range r.reps {
		wg.Add(1)
		go func(rep path.Representative) {
			defer wg.Done()
			req := probe.Request{Dst: rep.Addr, Timeout: probeTimeout}
			want := probe.EchoReply
			if !rep.Direct {
				req = probe.Request{Dst: r.dst, TTL: rep.TTL, Timeout: probeTimeout}
				want = probe.TimeExceeded
			}
			res, err := r.m.prober.Probe(ctx, req)
			if ctx.Err() != nil {
				return
			}
			out := result{zone: rep.Zone, at: res.Sent, rtt: -1}
			if out.at.IsZero() {
				out.at = time.Now()
			}
			if err == nil && res.Kind == want {
				out.rtt = res.RTT
			}
			select {
			case r.results <- out:
			case <-ctx.Done():
			}
		}(rep)
	}
}

func (r *runner) handle(res result) {
	z := r.zones[res.zone]
	z.rtts = append(z.rtts, res.rtt)
	if res.rtt < 0 {
		if z.streak == 0 {
			z.streakStart = res.at
		}
		z.streak++
	} else {
		z.streak = 0
	}
	r.m.store.AddSample(store.Sample{SessionID: r.sid, Zone: string(res.zone), At: res.at, RTT: res.rtt})
	ms := -1.0
	if res.rtt >= 0 {
		ms = float64(res.rtt) / float64(time.Millisecond)
	}
	r.pending = append(r.pending, LiveSample{Zone: res.zone, T: res.at.UnixMilli(), RTTMs: ms})
	r.evaluate(res.at)
}

// evaluate attributes an outage to the first zone from which on every
// measured zone (up to the target) is down. Losses at an intermediate zone
// while the target still answers are ICMP rate limiting / silent hops and
// are not counted as outage.
func (r *runner) evaluate(now time.Time) {
	var measured []path.Zone
	for _, rep := range r.reps {
		measured = append(measured, rep.Zone)
	}
	fault := path.Zone("")
	var since time.Time
	for i := range measured {
		allDown := true
		for _, z := range measured[i:] {
			if r.zones[z].streak < outageAfter {
				allDown = false
				break
			}
		}
		if allDown {
			fault, since = measured[i], r.zones[measured[i]].streakStart
			break
		}
	}
	if fault == r.fault {
		return
	}
	r.closeFault(now)
	if fault != "" {
		r.fault = fault
		r.m.store.StartOutage(r.sid, string(fault), since)
		r.m.emit(EvOutage, OutageEvent{Zone: fault, Since: since.UnixMilli(), Active: true})
	}
}

func (r *runner) closeFault(now time.Time) {
	if r.fault == "" {
		return
	}
	r.m.store.EndOutage(r.sid, string(r.fault), now)
	r.m.emit(EvOutage, OutageEvent{Zone: r.fault, Since: now.UnixMilli(), Active: false})
	r.fault = ""
}

func (r *runner) flushLive() {
	if len(r.pending) > 0 {
		r.m.emit(EvSamples, r.pending)
		r.pending = nil
	}
	stats := map[path.Zone]metrics.Summary{}
	for _, rep := range r.reps {
		stats[rep.Zone] = metrics.Summarize(r.zones[rep.Zone].rtts)
	}
	r.m.emit(EvStats, stats)
}

func (r *runner) updatePath(ctx context.Context, hops []path.Hop) {
	if samePath(r.hops, hops) {
		return
	}
	r.hops = hops
	r.reps = path.Representatives(ctx, r.m.prober, hops, r.dst)
	r.m.store.SavePath(r.sid, time.Now(), hops)
	r.m.setStatus(func(s *Status) { s.Hops, s.Reps = hops, r.reps })
	r.m.emit(EvPath, hops)
}

func samePath(a, b []path.Hop) bool {
	var ra, rb []string
	for _, h := range a {
		if h.Responsive {
			ra = append(ra, h.Addr)
		}
	}
	for _, h := range b {
		if h.Responsive {
			rb = append(rb, h.Addr)
		}
	}
	if len(ra) != len(rb) {
		return false
	}
	for i := range ra {
		if ra[i] != rb[i] {
			return false
		}
	}
	return true
}
