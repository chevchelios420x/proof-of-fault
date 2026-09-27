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
	incPreRoll  = 10  // seconds shown before a disruption
	incPostRoll = 5   // clean seconds that end a disruption (and are shown)
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
}

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
		if m := b.median(); m > 0 && float64(rtt) > spikeFactor*float64(m) && rtt-m > spikeMinDelta {
			st[c.key] = 's'
		}
		b.add(rtt)
	}
}

// classifyTick decides whether a tick shows a disruption and where it is.
// Hop-only losses (the path behind answers) are ICMP rate limiting and no
// disruption.
func classifyTick(t *tick, st map[string]byte, lostFrom int) (class, origin string) {
	n := len(t.order)
	if n == 0 {
		return "", ""
	}
	lost := func(key string) bool { return st[key] == 'x' }
	mainLost := lost(t.order[n-1].key)
	var altLost, altOK, devLost []string
	for _, c := range t.custom {
		switch {
		case path.Zone(c.zone) == path.WAN && lost(c.key):
			altLost = append(altLost, c.label)
		case path.Zone(c.zone) == path.WAN:
			altOK = append(altOK, c.label)
		case lost(c.key):
			devLost = append(devLost, c.label)
		}
	}
	allTargetsLost := mainLost && len(altOK) == 0
	if lostFrom < n {
		origin = t.order[lostFrom].label
	}
	switch {
	case allTargetsLost && st[string(path.LAN)] == 'x':
		return ClassLAN, origin
	case allTargetsLost && st[string(path.ISPEdge)] == 'x':
		return ClassISP, origin
	case allTargetsLost:
		return ClassISPCore, origin
	case mainLost:
		return ClassTarget, t.order[n-1].label
	case len(altLost) > 0:
		return ClassAlt, strings.Join(altLost, ", ")
	case len(devLost) > 0:
		return ClassDevice, strings.Join(devLost, ", ")
	}
	return "", ""
}

// trackIncident collects disrupted ticks (plus context) into incidents.
func (r *runner) trackIncident(ts *tickState) {
	if ts.class != "" {
		if r.inc == nil {
			r.inc = &incBuf{ticks: append([]*tickState(nil), r.recent...)}
			r.inc.pre = len(r.inc.ticks)
		}
		r.inc.ticks = append(r.inc.ticks, ts)
		r.inc.lastProblem = len(r.inc.ticks) - 1
	} else if r.inc != nil {
		r.inc.ticks = append(r.inc.ticks, ts)
		if len(r.inc.ticks)-1-r.inc.lastProblem >= incPostRoll {
			r.closeIncident()
		}
	}
	if r.inc != nil && len(r.inc.ticks) >= incMaxTicks {
		r.closeIncident()
	}
	r.recent = append(r.recent, ts)
	if len(r.recent) > incPreRoll {
		r.recent = r.recent[len(r.recent)-incPreRoll:]
	}
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
	isCustom := func(s series) bool { return strings.HasPrefix(s.key, "dev:") }
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
		row := store.IncidentSeries{Key: s.key, Label: s.label, Zone: string(s.zone), Custom: isCustom(s), Target: s.ttl >= targetTTL}
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
			if st == 'x' && i >= b.pre && i <= b.lastProblem {
				row.Lost++
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
		title = "Heimnetz gestört: der eigene Router antwortete nicht"
	case ClassISP:
		title = "Anbieter-Zugang gestört: Router OK, ab " + in.Origin + " keine Antwort"
	case ClassISPCore:
		title = "Kein Internet: alle Ziele weg, Anbieter-Zugang antwortete noch"
	case ClassTarget:
		title = "Nur das Hauptziel war weg – Ausweichziele erreichbar"
	case ClassAlt:
		title = "Nur Ausweichziel(e) weg: " + in.Origin
	case ClassDevice:
		title = "Nur Gerät(e) im Heimnetz weg: " + in.Origin
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
		return "nur LAN-Gerät"
	}
	return c
}

func orNone(xs []string) string {
	if len(xs) == 0 {
		return "keine"
	}
	return strings.Join(xs, ", ")
}
