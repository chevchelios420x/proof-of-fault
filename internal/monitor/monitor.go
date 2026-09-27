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
	"strings"
	"sync"
	"time"

	"github.com/chevchelios420x/proof-of-fault/internal/config"
	"github.com/chevchelios420x/proof-of-fault/internal/metrics"
	"github.com/chevchelios420x/proof-of-fault/internal/path"
	"github.com/chevchelios420x/proof-of-fault/internal/power"
	"github.com/chevchelios420x/proof-of-fault/internal/probe"
	"github.com/chevchelios420x/proof-of-fault/internal/store"
)

const (
	interval = time.Second
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
	Watched   []string              `json:"watched"` // hop addresses probed individually
	Names     map[string]string     `json:"names"`   // user labels by hop address
	Custom    []CustomStatus        `json:"custom"`  // user-defined measuring points
}

// HopKey is the series key used for individually watched hops.
func HopKey(addr string) string { return "hop:" + addr }

// DevKey is the series key of a user-defined measuring point.
func DevKey(host string) string { return "dev:" + host }

// CustomStatus is a user-defined measuring point as shown in the UI.
type CustomStatus struct {
	store.CustomPoint
	IP    string `json:"ip"`    // resolved address ("" = not resolved)
	Error string `json:"error"` // resolution problem
}

// LiveSample is sent to the UI in batches once per second.
type LiveSample struct {
	Zone  string  `json:"zone"`  // zone or HopKey(addr)
	T     int64   `json:"t"`     // unix ms
	RTTMs float64 `json:"rttMs"` // -1 = loss
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

	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	cmds    chan func(*runner)
	status  Status
	watched map[string]bool
	cfg     config.Settings
}

// Settings returns the current settings.
func (m *Monitor) Settings() config.Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// SaveSettings stores new settings and applies them immediately (also to a
// running measurement).
func (m *Monitor) SaveSettings(cfg config.Settings) error {
	cfg = cfg.Normalize()
	if err := m.store.SaveSettings(cfg); err != nil {
		return err
	}
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	custom := m.Status().Custom
	m.send(func(r *runner) {
		r.cfg = cfg
		r.setCustom(custom) // zone roles may have changed
	})
	return nil
}

// New creates a monitor.
func New(p probe.Prober, s *store.Store, emit Emitter) *Monitor {
	m := &Monitor{prober: p, store: s, emit: emit, watched: map[string]bool{}, cfg: s.Settings()}
	prefs, _ := s.HopPrefs()
	for a, p := range prefs {
		if p.Watched {
			m.watched[a] = true
		}
	}
	m.status = Status{State: "idle", Watched: m.watchedList(), Names: s.HopNames()}
	go m.applyCustom() // resolving host names must not delay the start
	return m
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
	m.cmds = make(chan func(*runner), 16)
	m.status = Status{State: "resolving", Target: target, Watched: m.watchedList(), Names: m.store.HopNames(), Custom: m.status.Custom}
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

// send runs f inside the session loop; it reports false if no session runs.
func (m *Monitor) send(f func(*runner)) bool {
	m.mu.Lock()
	cmds, done, running := m.cmds, m.done, m.cancel != nil
	m.mu.Unlock()
	if !running {
		return false
	}
	select {
	case cmds <- f:
		return true
	case <-done:
		return false
	}
}

func (m *Monitor) watchedList() []string {
	out := make([]string, 0, len(m.watched))
	for a := range m.watched {
		out = append(out, a)
	}
	return out
}

func (m *Monitor) overrides() map[string]path.Zone {
	raw, _ := m.store.HopZones()
	out := make(map[string]path.Zone, len(raw))
	for a, z := range raw {
		out[a] = path.Zone(z)
	}
	return out
}

// SetHopWatched adds or removes a hop from individual probing (chart line).
func (m *Monitor) SetHopWatched(addr string, on bool) {
	m.store.SetHopWatched(addr, on)
	m.mu.Lock()
	if on {
		m.watched[addr] = true
	} else {
		delete(m.watched, addr)
	}
	m.status.Watched = m.watchedList()
	m.mu.Unlock()
	m.send(func(r *runner) { r.syncWatched() })
	m.setStatus(func(*Status) {})
}

// customStatus resolves all user-defined measuring points.
func (m *Monitor) customStatus(ctx context.Context) []CustomStatus {
	pts, _ := m.store.CustomPoints()
	out := make([]CustomStatus, 0, len(pts))
	for _, p := range pts {
		cs := CustomStatus{CustomPoint: p}
		rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		a, err := resolve(rctx, p.Host)
		cancel()
		if err != nil {
			cs.Error = err.Error()
		} else {
			cs.IP = a.String()
		}
		out = append(out, cs)
	}
	return out
}

// SaveCustomPoint adds or updates a user-defined measuring point. An empty
// zone is derived from the address (private → LAN, otherwise WAN).
func (m *Monitor) SaveCustomPoint(p store.CustomPoint) error {
	p.Host = strings.TrimSpace(p.Host)
	p.Name = strings.TrimSpace(p.Name)
	if p.Host == "" {
		return errors.New("bitte IP-Adresse oder Hostnamen angeben")
	}
	if p.Zone != "" && m.Settings().Role(p.Zone) == "" {
		return fmt.Errorf("ungültige Zone %q", p.Zone)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	a, err := resolve(ctx, p.Host)
	cancel()
	if err != nil {
		return err
	}
	if p.Zone == "" {
		p.Zone = string(path.WAN)
		if a.IsPrivate() || a.IsLinkLocalUnicast() {
			p.Zone = string(path.LAN)
		}
	}
	if err := m.store.SaveCustomPoint(p); err != nil {
		return err
	}
	m.applyCustom()
	return nil
}

// DeleteCustomPoint removes a user-defined measuring point.
func (m *Monitor) DeleteCustomPoint(host string) error {
	if err := m.store.DeleteCustomPoint(host); err != nil {
		return err
	}
	m.applyCustom()
	return nil
}

func (m *Monitor) applyCustom() {
	cs := m.customStatus(context.Background())
	m.send(func(r *runner) { r.setCustom(cs) })
	m.setStatus(func(s *Status) { s.Custom = cs })
}

// SetHopName stores a user label for a hop ("" removes it).
func (m *Monitor) SetHopName(addr, name string) error {
	if err := m.store.SetHopName(addr, strings.TrimSpace(name)); err != nil {
		return err
	}
	names := m.store.HopNames()
	m.send(func(r *runner) { r.names = names })
	m.setStatus(func(s *Status) { s.Names = names })
	return nil
}

// SetHopZone stores a user zone for a hop address (remembered permanently;
// "" restores automatic classification) and applies it immediately.
func (m *Monitor) SetHopZone(addr string, zone path.Zone) error {
	if zone != "" && !path.ValidZone(zone) {
		return fmt.Errorf("ungültige Zone %q", zone)
	}
	if err := m.store.SetHopZone(addr, string(zone)); err != nil {
		return err
	}
	ov := m.overrides()
	if m.send(func(r *runner) { r.reclassify(ov) }) {
		return nil
	}
	m.setStatus(func(s *Status) {
		hops := append([]path.Hop(nil), s.Hops...)
		path.Classify(hops, ov)
		s.Hops = hops
	})
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
	cfg := m.Settings()
	cfg.LastTarget = target
	m.store.SaveSettings(cfg)
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	if cfg.PreventSleep {
		defer power.KeepAwake().Release()
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
	hops, err := path.Discover(ctx, m.prober, dst, m.overrides())
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
	r.setCustom(m.Status().Custom)
	r.logEvent(store.Event{Kind: KindSession, Severity: "info",
		Title:  fmt.Sprintf("Messung gestartet: %s (%s)", target, dst),
		Detail: "Route: " + describePath(hops, r.names) + ". Messpunkte: " + repsText(reps) + "."})
	return r.loop(ctx)
}

// runner holds the state of one running session.
type runner struct {
	m    *Monitor
	sid  int64
	dst  netip.Addr
	hops []path.Hop
	reps []path.Representative

	results    chan result
	zones      map[path.Zone]*zoneState
	hopMode    map[string]*hopProbe // individually watched hops by address
	fault      path.Zone            // currently attributed outage zone, "" = none
	faultSince time.Time
	pending    []LiveSample

	// event analysis
	seq         int
	nextSeq     int
	ticks       map[int]*tick
	done        map[int]bool
	base        map[string]*baseline
	episodes    map[string]*episode
	names       map[string]string
	pathUpdates chan []path.Hop
	custom      []customPoint
	recent      []*tickState
	inc         *incBuf
	realLost    map[string]int // losses that reached the target, per series
	cfg         config.Settings

	ctx context.Context
	wg  *sync.WaitGroup
}

type hopProbe struct {
	ttl    int
	direct bool // answers echo itself; otherwise TTL-limited towards target
}

type result struct {
	seq  int
	zone string // zone or HopKey(addr)
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
		results: make(chan result, 256), zones: map[path.Zone]*zoneState{}, hopMode: map[string]*hopProbe{},
		seq: 0, nextSeq: 1, ticks: map[int]*tick{}, done: map[int]bool{}, base: map[string]*baseline{},
		episodes: map[string]*episode{}, realLost: map[string]int{}, cfg: m.Settings(), names: m.store.HopNames(), pathUpdates: make(chan []path.Hop, 1)}
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
	rediscoverEvery := time.Duration(r.cfg.RediscoverMin) * time.Minute
	rediscover := time.NewTicker(rediscoverEvery)
	defer rediscover.Stop()

	var wg sync.WaitGroup
	defer wg.Wait()
	r.ctx, r.wg = ctx, &wg
	r.syncWatched()

	r.fire(ctx, &wg)
	for {
		select {
		case <-ctx.Done():
			r.closeFault(time.Now())
			r.drainTicks(true)
			r.closeIncident()
			r.closeEpisodes(r.seq, true)
			r.logEvent(store.Event{Kind: KindSession, Severity: "info", Title: "Messung beendet"})
			return ctx.Err()
		case <-tick.C:
			r.fire(ctx, &wg)
		case res := <-r.results:
			r.handle(res)
		case <-flush.C:
			r.flushLive()
		case <-rediscover.C:
			r.rediscover()
		case hops := <-r.pathUpdates:
			r.updatePath(ctx, hops)
		case f := <-r.m.cmds:
			f(r)
		}
	}
}

// rediscover traces the path again in the background; a changed path is
// applied (and logged) by the loop.
func (r *runner) rediscover() {
	ctx := r.ctx
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		if hops, err := path.Discover(ctx, r.m.prober, r.dst, nil); err == nil && len(hops) > 0 {
			select {
			case r.pathUpdates <- hops:
			default:
			}
		}
	}()
}

// fire sends one probe per series without blocking the loop. All probes of
// one call form a tick that is judged as a whole.
func (r *runner) fire(ctx context.Context, wg *sync.WaitGroup) {
	r.seq++
	t := &tick{seq: r.seq, at: time.Now(), order: r.seriesOrder(), result: map[string]time.Duration{}}
	for _, c := range r.custom {
		t.custom = append(t.custom, c.s)
	}
	r.ticks[t.seq] = t
	r.drainTicks(false)
	for _, rep := range r.reps {
		r.probeOne(ctx, wg, t.seq, string(rep.Zone), rep.Addr, rep.TTL, rep.Direct)
	}
	isRep := map[netip.Addr]bool{}
	for _, rep := range r.reps {
		isRep[rep.Addr] = true
	}
	for addr, h := range r.hopMode {
		a, err := netip.ParseAddr(addr)
		if err != nil || isRep[a] {
			continue // zone representatives are measured already
		}
		r.probeOne(ctx, wg, t.seq, HopKey(addr), a, h.ttl, h.direct)
	}
	for _, c := range r.custom {
		r.probeOne(ctx, wg, t.seq, c.s.key, c.addr, 0, true)
	}
}

// customPoint is an enabled, resolved user-defined measuring point.
type customPoint struct {
	s    series
	addr netip.Addr
}

func (r *runner) setCustom(cs []CustomStatus) {
	r.custom = nil
	for _, c := range cs {
		a, err := netip.ParseAddr(c.IP)
		if !c.Enabled || err != nil {
			continue
		}
		label := c.Host
		if c.Name != "" {
			label = c.Name + " (" + c.Host + ")"
		}
		role := r.cfg.Role(c.Zone)
		if role == "" {
			role = ZoneNone // zone was deleted
		}
		if role == ZoneNone {
			label += " – nicht gewertet"
		}
		r.custom = append(r.custom, customPoint{addr: a,
			s: series{key: DevKey(c.Host), zone: path.Zone(role), group: c.Zone, addr: c.IP, label: "Gerät " + label}})
	}
}

// drainTicks judges completed ticks strictly in order. Ticks that are
// overdue (or all, when stopping) count missing results as loss.
func (r *runner) drainTicks(all bool) {
	for {
		t, ok := r.ticks[r.nextSeq]
		if !ok {
			return
		}
		complete := len(t.result) >= len(t.order)+len(t.custom)
		overdue := r.seq-t.seq > int(r.cfg.ProbeTimeout()/interval)+2
		if !complete && !overdue && !all {
			return
		}
		delete(r.ticks, r.nextSeq)
		r.nextSeq++
		r.finishTick(t)
	}
}

func (r *runner) probeOne(ctx context.Context, wg *sync.WaitGroup, seq int, key string, addr netip.Addr, ttl int, direct bool) {
	timeout := r.cfg.ProbeTimeout()
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := probe.Request{Dst: addr, Timeout: timeout}
		want := probe.EchoReply
		if !direct {
			req = probe.Request{Dst: r.dst, TTL: ttl, Timeout: timeout}
			want = probe.TimeExceeded
		}
		res, err := r.m.prober.Probe(ctx, req)
		if ctx.Err() != nil {
			return
		}
		out := result{seq: seq, zone: key, at: res.Sent, rtt: -1}
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
	}()
}

// syncWatched aligns the individually probed hops with the watch list and
// the current path. New hops start TTL-limited and switch to direct echo
// once they are known to answer it.
func (r *runner) syncWatched() {
	r.m.mu.Lock()
	watched := make(map[string]bool, len(r.m.watched))
	for a := range r.m.watched {
		watched[a] = true
	}
	r.m.mu.Unlock()

	ttls := map[string]int{}
	for _, h := range r.hops {
		if h.Responsive && h.Addr != r.dst.String() {
			if _, ok := ttls[h.Addr]; !ok {
				ttls[h.Addr] = h.TTL
			}
		}
	}
	for addr := range r.hopMode {
		if _, ok := ttls[addr]; !ok || !watched[addr] {
			delete(r.hopMode, addr)
		}
	}
	for addr := range watched {
		ttl, ok := ttls[addr]
		if !ok {
			continue
		}
		if h, ok := r.hopMode[addr]; ok {
			h.ttl = ttl
			continue
		}
		r.hopMode[addr] = &hopProbe{ttl: ttl}
		a, _ := netip.ParseAddr(addr)
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			if path.AnswersEcho(r.ctx, r.m.prober, a) {
				r.m.send(func(r *runner) {
					if h, ok := r.hopMode[addr]; ok {
						h.direct = true
					}
				})
			}
		}()
	}
}

// reclassify applies changed zone overrides to the current path.
func (r *runner) reclassify(ov map[string]path.Zone) {
	path.Classify(r.hops, ov)
	oldReps := r.reps
	r.reps = path.Representatives(r.ctx, r.m.prober, r.hops, r.dst)
	r.logEvent(store.Event{Kind: KindZones, Severity: "info", Title: "Zonen vom Benutzer geändert",
		Detail: "Route: " + describePath(r.hops, r.names) + ". Messpunkte: " + repsText(oldReps) + " → " + repsText(r.reps) + "."})
	for _, z := range path.Zones {
		r.zones[z].streak = 0
	}
	r.closeFault(time.Now())
	r.m.store.SavePath(r.sid, time.Now(), r.hops)
	hops := append([]path.Hop(nil), r.hops...)
	r.m.setStatus(func(s *Status) { s.Hops, s.Reps = hops, r.reps })
}

func (r *runner) handle(res result) {
	ms := -1.0
	if res.rtt >= 0 {
		ms = float64(res.rtt) / float64(time.Millisecond)
	}
	r.m.store.AddSample(store.Sample{SessionID: r.sid, Zone: res.zone, At: res.at, RTT: res.rtt})
	r.pending = append(r.pending, LiveSample{Zone: res.zone, T: res.at.UnixMilli(), RTTMs: ms})
	if t := r.ticks[res.seq]; t != nil {
		t.result[res.zone] = res.rtt
		r.drainTicks(false)
	}
	if strings.HasPrefix(res.zone, "hop:") || strings.HasPrefix(res.zone, "dev:") {
		return
	}
	z := r.zones[path.Zone(res.zone)]
	z.rtts = append(z.rtts, res.rtt)
	if res.rtt < 0 {
		if z.streak == 0 {
			z.streakStart = res.at
		}
		z.streak++
	} else {
		z.streak = 0
	}
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
			if r.zones[z].streak < r.cfg.OutageAfter {
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
		r.fault, r.faultSince = fault, since
		r.m.store.StartOutage(r.sid, string(fault), since)
		r.logEvent(store.Event{T: since.UnixMilli(), Kind: KindOutageStart, Severity: "crit", Zone: string(fault),
			Title:  fmt.Sprintf("AUSFALL: keine Verbindung ab Zone %s", fault),
			Detail: fmt.Sprintf("Mindestens %d Sekunden in Folge keine Antwort ab %s bis zum Ziel. Ursache im Bereich %s. Route wird zur Kontrolle neu ermittelt.", r.cfg.OutageAfter, fault, zoneText(fault))})
		if r.ctx != nil {
			r.rediscover()
		}
		r.m.emit(EvOutage, OutageEvent{Zone: fault, Since: since.UnixMilli(), Active: true})
	}
}

func (r *runner) closeFault(now time.Time) {
	if r.fault == "" {
		return
	}
	r.m.store.EndOutage(r.sid, string(r.fault), now)
	d := now.Sub(r.faultSince).Round(time.Second)
	r.logEvent(store.Event{T: now.UnixMilli(), Kind: KindOutageEnd, Severity: "ok", Zone: string(r.fault), Count: int(d / time.Second),
		Title:  fmt.Sprintf("Ausfall beendet (Zone %s, Dauer %s)", r.fault, d),
		Detail: fmt.Sprintf("Verbindung wieder da. Ausfall von %s bis %s.", r.faultSince.Format("15:04:05"), now.Format("15:04:05"))})
	if r.ctx != nil {
		r.rediscover()
	}
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
	real := make(map[string]int, len(r.realLost))
	for k, v := range r.realLost {
		real[k] = v
	}
	r.m.emit(EvRealLoss, real)
}

// EvRealLoss carries per series the number of losses that reached the target
// (as opposed to ICMP rate limiting at a single hop).
const EvRealLoss = "realloss"

func (r *runner) updatePath(ctx context.Context, hops []path.Hop) {
	if samePath(r.hops, hops) {
		return
	}
	path.Classify(hops, r.m.overrides())
	old, oldReps := r.hops, r.reps
	r.hops = hops
	r.reps = path.Representatives(ctx, r.m.prober, hops, r.dst)
	r.syncWatched()
	detail := "Änderung: " + diffPath(old, hops) + ". Neue Route: " + describePath(hops, r.names) + "."
	if repsText(oldReps) != repsText(r.reps) {
		detail += " Messpunkte: " + repsText(oldReps) + " → " + repsText(r.reps) + "."
	}
	r.logEvent(store.Event{Kind: KindPathChange, Severity: "warn", Title: "Routenwechsel erkannt", Detail: detail})
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
