package monitor

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chevchelios420x/proof-of-fault/internal/path"
	"github.com/chevchelios420x/proof-of-fault/internal/store"
)

// EvIncident is the UI event carrying a finished incident.
const EvIncident = "incident"

const (
	incMaxTicks = 900 // split very long disruptions
)

// Incident classes (see classifyTick).
const (
	ClassLAN     = "lan"      // home router unreachable
	ClassISP     = "isp"      // router fine, provider access and all targets gone
	ClassISPCore = "isp_core" // provider access answers, but all targets gone
	ClassTarget  = "target"   // only the main target gone, backup targets fine
	ClassAlt     = "alt"      // only backup target(s) gone
	ClassDevice  = "device"   // only user-defined LAN device(s) gone
)

var classPriority = []string{ClassLAN, ClassISP, ClassISPCore, ClassTarget, ClassAlt, ClassDevice}

// tickState is the judged state of one tick.
type tickState struct {
	t      *tick
	state  map[string]byte // '.' ok, 's' slow, 'x' no answer
	class  string          // "" = no disruption
	origin string
}

type incBuf struct {
	ticks       []*tickState
	pre         int
	lastProblem int
	class       string // class the incident is about
	// A different class that started at index pendingAt and has lasted
	// pendingN problem ticks; it splits the incident once it persists.
	pending   string
	pendingAt int
	pendingN  int
}

const (
	splitAfter      = 3   // seconds a new class must last to split an incident
	persistentAfter = 120 // seconds without answer → point counts as permanently down
)

// customStates judges the user-defined measuring points of a tick.
func (r *runner) customStates(t *tick, st map[string]byte) {
	for _, c := range t.custom {
		rtt, ok := t.result[c.key]
		if !ok || rtt < 0 {
			st[c.key] = 'x'
			continue
		}
		b := r.base[c.key]
		if b == nil {
			b = &baseline{}
			r.base[c.key] = b
		}
		st[c.key] = '.'
		if r.cfg.IsSpike(string(c.zone), rtt, b.median()) {
			st[c.key] = 's'
		}
		b.add(rtt)
	}
	// Ping and TCP check of the same host disagree: the host is reachable,
	// the failed protocol is probably filtered → "maybe false positive" ('p'),
	// which counts as reachable and neither starts nor extends an incident.
	for _, c := range t.custom {
		if st[c.key] != 'x' {
			continue
		}
		if other, ok := st[counterpart(c.key)]; ok && other != 'x' && other != 'p' {
			st[c.key] = 'p'
		}
	}
	r.markPersistent(t, st)
}

// markPersistent turns points that have not answered for persistentAfter
// seconds into 'd' (permanently down): they are logged once and no longer
// start or hold incidents, so real disruptions stay separate.
func (r *runner) markPersistent(t *tick, st map[string]byte) {
	for _, c := range t.custom {
		if st[c.key] != 'x' {
			if r.persist[c.key] >= persistentAfter {
				r.logEvent(store.Event{Kind: KindPersistentEnd, Severity: "ok", Zone: string(c.zone), Addr: c.addr,
					Title:  "Wieder erreichbar: " + c.label,
					Detail: fmt.Sprintf("Antwortet wieder nach %s ohne Antwort.", time.Duration(r.persist[c.key])*time.Second)})
			}
			r.persist[c.key] = 0
			continue
		}
		r.persist[c.key]++
		n := r.persist[c.key]
		if n == persistentAfter {
			r.logEvent(store.Event{Kind: KindPersistent, Severity: "warn", Zone: string(c.zone), Addr: c.addr,
				T:     t.at.Add(-time.Duration(n-1) * time.Second).UnixMilli(),
				Title: "Dauerhaft ohne Antwort: " + c.label,
				Detail: fmt.Sprintf("Seit %d s keine Antwort. Der Messpunkt wird bis zur nächsten Antwort als „dauerhaft gestört“ geführt und hält keine Störungen mehr offen "+
					"(z. B. Ping gefiltert oder Server aus). Tipp: TCP-Check aktivieren oder den Messpunkt prüfen.", n)})
		}
		if n >= persistentAfter {
			st[c.key] = 'd'
		}
	}
}

// counterpart returns the key of the other protocol of the same host
// ("dev:" ↔ "tcp:"), or "" for other keys.
func counterpart(key string) string {
	switch {
	case strings.HasPrefix(key, "dev:"):
		return "tcp:" + key[4:]
	case strings.HasPrefix(key, "tcp:"):
		return "dev:" + key[4:]
	}
	return ""
}

// otherProtocolAnswered reports whether the counterpart of key answered in t.
func otherProtocolAnswered(t *tick, key string) bool {
	rtt, ok := t.result[counterpart(key)]
	return ok && rtt >= 0
}

// ZoneNone marks user-defined measuring points that are shown and recorded
// but not used for the evaluation.
const ZoneNone = "none"

// classifyTick decides whether a tick shows a disruption and where it is.
// User-defined points count for the zone chosen for them: WAN points are
// backup targets, LAN/ISP_EDGE points belong to that part of the path.
// Hop-only losses (the path behind answers) are ICMP rate limiting and no
// disruption; points with zone "none" are ignored.
func classifyTick(t *tick, st map[string]byte, lostFrom int) (class, origin string) {
	n := len(t.order)
	if n == 0 {
		return "", ""
	}
	lost := func(key string) bool { return st[key] == 'x' }
	mainLost := lost(t.order[n-1].key)
	var altLost, altOK []string
	zoneLost := map[path.Zone][]string{} // lost points per zone (path + custom)
	for _, z := range []path.Zone{path.LAN, path.ISPEdge} {
		if lost(string(z)) {
			for _, s := range t.order {
				if s.key == string(z) {
					zoneLost[z] = append(zoneLost[z], s.label)
				}
			}
		}
	}
	for _, c := range t.custom {
		switch {
		case st[c.key] == 'd': // permanently down: ignored for the evaluation
		case c.zone == ZoneNone || c.zone == "":
		case c.zone == path.WAN && lost(c.key):
			altLost = append(altLost, c.label)
		case c.zone == path.WAN:
			altOK = append(altOK, c.label)
		case lost(c.key):
			zoneLost[c.zone] = append(zoneLost[c.zone], c.label)
		}
	}
	allTargetsLost := mainLost && len(altOK) == 0
	if lostFrom < n {
		origin = t.order[lostFrom].label
	}
	orig := func(z path.Zone) string {
		if len(zoneLost[z]) > 0 && (origin == "" || path.Zone(z) != path.WAN) {
			return strings.Join(zoneLost[z], ", ")
		}
		return origin
	}
	switch {
	case allTargetsLost && len(zoneLost[path.LAN]) > 0:
		return ClassLAN, orig(path.LAN)
	case allTargetsLost && len(zoneLost[path.ISPEdge]) > 0:
		return ClassISP, orig(path.ISPEdge)
	case allTargetsLost:
		return ClassISPCore, origin
	case mainLost:
		return ClassTarget, t.order[n-1].label
	case len(altLost) > 0:
		return ClassAlt, strings.Join(altLost, ", ")
	}
	// Targets fine: only custom LAN/ISP_EDGE points count here; a silent
	// router hop alone is ICMP rate limiting.
	var dev []string
	for _, c := range t.custom {
		if (c.zone == path.LAN || c.zone == path.ISPEdge) && lost(c.key) {
			dev = append(dev, c.label)
		}
	}
	if len(dev) > 0 {
		return ClassDevice, strings.Join(dev, ", ")
	}
	return "", ""
}

// trackIncident collects disrupted ticks (plus context) into incidents.
func (r *runner) trackIncident(ts *tickState) {
	if ts.class != "" {
		if r.inc == nil {
			r.inc = &incBuf{ticks: append([]*tickState(nil), r.recent...), class: ts.class}
			r.inc.pre = len(r.inc.ticks)
		}
		b := r.inc
		b.ticks = append(b.ticks, ts)
		idx := len(b.ticks) - 1
		switch {
		case ts.class == b.class:
			b.pending, b.pendingN = "", 0
			b.lastProblem = idx
		case ts.class == b.pending:
			b.pendingN++
		default:
			b.pending, b.pendingAt, b.pendingN = ts.class, idx, 1
		}
		if b.pending != "" && b.pendingN >= splitAfter {
			r.splitIncident()
		} else if b.pending != "" {
			b.lastProblem = idx // until the change is confirmed it belongs here
		}
	} else if r.inc != nil {
		r.inc.ticks = append(r.inc.ticks, ts)
		if len(r.inc.ticks)-1-r.inc.lastProblem >= r.cfg.IncidentPostSec {
			r.closeIncident()
		}
	}
	if r.inc != nil && len(r.inc.ticks) >= incMaxTicks {
		r.closeIncident()
	}
	r.recent = append(r.recent, ts)
	if len(r.recent) > r.cfg.IncidentPreSec {
		r.recent = r.recent[len(r.recent)-r.cfg.IncidentPreSec:]
	}
}

// splitIncident ends the current incident where the new class began and
// continues with a new incident (with the usual pre-roll) for that class.
func (r *runner) splitIncident() {
	b := r.inc
	at := b.pendingAt
	// Old part: up to the last tick before the new class started.
	old := &incBuf{ticks: b.ticks[:at], pre: b.pre, lastProblem: at - 1, class: b.class}
	for old.lastProblem >= old.pre && old.ticks[old.lastProblem].class != b.class {
		old.lastProblem--
	}
	if old.lastProblem >= old.pre {
		in := buildIncident(old)
		in, _ = r.m.store.AddIncident(r.sid, in)
		r.m.emit(EvIncident, in)
	}
	start := max(0, at-r.cfg.IncidentPreSec)
	nb := &incBuf{ticks: append([]*tickState(nil), b.ticks[start:]...), class: b.pending}
	nb.pre = at - start
	nb.lastProblem = len(nb.ticks) - 1
	r.inc = nb
}

func (r *runner) closeIncident() {
	b := r.inc
	r.inc = nil
	if b == nil || b.lastProblem < b.pre {
		return
	}
	in := buildIncident(b)
	in, _ = r.m.store.AddIncident(r.sid, in)
	r.m.emit(EvIncident, in)
}

func buildIncident(b *incBuf) store.Incident {
	// Rows: path series in TTL order, then user-defined points.
	var rows []series
	seen := map[string]bool{}
	for _, ts := range b.ticks {
		for _, s := range append(append([]series(nil), ts.t.order...), ts.t.custom...) {
			if !seen[s.key] {
				seen[s.key] = true
				rows = append(rows, s)
			}
		}
	}
	isCustom := func(s series) bool { return strings.HasPrefix(s.key, "dev:") || strings.HasPrefix(s.key, "tcp:") }
	sort.SliceStable(rows, func(i, j int) bool {
		ci, cj := isCustom(rows[i]), isCustom(rows[j])
		if ci != cj {
			return !ci
		}
		return !ci && rows[i].ttl < rows[j].ttl
	})

	in := store.Incident{
		T:        b.ticks[b.pre].t.at.UnixMilli(),
		End:      b.ticks[b.lastProblem].t.at.Add(interval).UnixMilli(),
		Seconds:  b.lastProblem - b.pre + 1,
		PreRoll:  b.pre,
		PostRoll: len(b.ticks) - 1 - b.lastProblem,
		Classes:  map[string]int{},
	}
	origins := map[string]int{}
	for i, ts := range b.ticks {
		in.Columns = append(in.Columns, ts.t.at.UnixMilli())
		if i >= b.pre && i <= b.lastProblem && ts.class != "" {
			in.Classes[ts.class]++
			if ts.origin != "" {
				origins[ts.origin]++
			}
		}
	}
	for _, s := range rows {
		zone := string(s.zone)
		if s.group != "" {
			zone = s.group
		}
		row := store.IncidentSeries{Key: s.key, Label: s.label, Zone: zone, Custom: isCustom(s), Target: s.ttl >= targetTTL}
		var states []byte
		for i, ts := range b.ticks {
			st, ok := ts.state[s.key]
			if !ok {
				st = '-'
			}
			states = append(states, st)
			rtt := -1.0
			if d, ok := ts.t.result[s.key]; ok && d >= 0 {
				rtt = float64(d.Microseconds()) / 1000
			}
			row.RTT = append(row.RTT, rtt)
			if i >= b.pre && i <= b.lastProblem {
				switch st {
				case 'x':
					row.Lost++
				case 'p':
					row.Maybe++
				}
			}
		}
		row.States = string(states)
		in.Series = append(in.Series, row)
	}

	best := ""
	for _, c := range classPriority {
		if in.Classes[c] > in.Classes[best] || best == "" && in.Classes[c] > 0 {
			best = c
		}
	}
	in.Class = best
	for o, n := range origins {
		if n > origins[in.Origin] {
			in.Origin = o
		}
	}
	in.Title, in.Detail = describeIncident(in)
	return in
}

func describeIncident(in store.Incident) (title, detail string) {
	var down, up []string
	for _, s := range in.Series {
		switch {
		case s.Lost == 0:
			up = append(up, s.Label)
		case s.Lost >= in.Seconds:
			down = append(down, s.Label)
		default:
			down = append(down, fmt.Sprintf("%s (%d von %d s)", s.Label, s.Lost, in.Seconds))
		}
	}
	dur := (time.Duration(in.Seconds) * time.Second).String()
	switch in.Class {
	case ClassLAN:
		title = "Heimnetz gestört: keine Antwort von " + in.Origin
	case ClassISP:
		title = "Anbieter-Zugang gestört: Heimnetz OK, keine Antwort von " + in.Origin
	case ClassISPCore:
		title = "Kein Internet: alle Ziele weg, Anbieter-Zugang antwortete noch"
	case ClassTarget:
		title = "Nur das Hauptziel war weg – Ausweichziele erreichbar"
	case ClassAlt:
		title = "Nur Ausweichziel(e) weg: " + in.Origin
	case ClassDevice:
		title = "Nur einzelne Messpunkte weg (Internet OK): " + in.Origin
	}
	title += " (" + dur + ")"
	detail = "Ohne Antwort: " + orNone(down) + ". Antworteten normal: " + orNone(up) + "."
	if len(in.Classes) > 1 {
		var parts []string
		for _, c := range classPriority {
			if in.Classes[c] > 0 {
				parts = append(parts, fmt.Sprintf("%s %d s", ClassName(c), in.Classes[c]))
			}
		}
		detail += " Verlauf: " + strings.Join(parts, ", ") + "."
	}
	return title, detail
}

// ClassName is a short German name of an incident class.
func ClassName(c string) string {
	switch c {
	case ClassLAN:
		return "Heimnetz"
	case ClassISP:
		return "Anbieter-Zugang"
	case ClassISPCore:
		return "Anbieter-Netz/Internet"
	case ClassTarget:
		return "nur Hauptziel"
	case ClassAlt:
		return "nur Ausweichziel"
	case ClassDevice:
		return "nur Einzel-Messpunkt"
	}
	return c
}

func orNone(xs []string) string {
	if len(xs) == 0 {
		return "keine"
	}
	return strings.Join(xs, ", ")
}
