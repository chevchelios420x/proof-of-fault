package monitor

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/chevchelios420x/proof-of-fault/internal/path"
	"github.com/chevchelios420x/proof-of-fault/internal/store"
)

func newTestRunner(t *testing.T) *runner {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m := New(nil, st, func(string, any) {})
	reps := []path.Representative{{Zone: path.LAN}, {Zone: path.ISPEdge}, {Zone: path.WAN}}
	return newRunner(m, 1, reps[2].Addr, nil, reps)
}

// feed sends one second of results: ok[i] tells whether zone i answered.
func feed(r *runner, at time.Time, ok ...bool) {
	for i, z := range path.Zones {
		rtt := 10 * time.Millisecond
		if !ok[i] {
			rtt = -1
		}
		r.handle(result{zone: string(z), at: at, rtt: rtt})
	}
}

func TestOutageAttribution(t *testing.T) {
	cases := []struct {
		name string
		ok   []bool
		want path.Zone
	}{
		{"isp down", []bool{true, false, false}, path.ISPEdge},
		{"lan down", []bool{false, false, false}, path.LAN},
		{"only target down", []bool{true, true, false}, path.WAN},
		{"edge rate limiting is no outage", []bool{true, false, true}, ""},
	}
	for _, c := range cases {
		r := newTestRunner(t)
		t0 := time.Now()
		for i := 0; i < 5; i++ {
			feed(r, t0.Add(time.Duration(i)*time.Second), c.ok...)
		}
		if r.fault != c.want {
			t.Errorf("%s: fault = %q, want %q", c.name, r.fault, c.want)
		}
		feed(r, t0.Add(6*time.Second), true, true, true)
		if r.fault != "" {
			t.Errorf("%s: fault not cleared after recovery", c.name)
		}
	}
}

func TestTickEvents(t *testing.T) {
	r := newTestRunner(t)
	order := []series{
		{key: "LAN", ttl: 1, zone: path.LAN, label: "Hop 1"},
		{key: "hop:88.1.1.1", ttl: 2, zone: path.ISPEdge, label: "Hop 2"},
		{key: "ISP_EDGE", ttl: 3, zone: path.ISPEdge, label: "Hop 3"},
		{key: "WAN", ttl: targetTTL, zone: path.WAN, label: "Ziel"},
	}
	ms := time.Millisecond
	seq := 0
	run := func(lan, hop, isp, wan time.Duration) {
		seq++
		r.seq = seq
		r.finishTick(&tick{seq: seq, at: time.Now(), order: order, result: map[string]time.Duration{
			"LAN": lan, "hop:88.1.1.1": hop, "ISP_EDGE": isp, "WAN": wan}})
	}
	for i := 0; i < 20; i++ { // baseline
		run(2*ms, 8*ms, 10*ms, 20*ms)
	}
	run(2*ms, -1, 10*ms, 20*ms)    // harmless hop loss
	run(2*ms, 8*ms, -1, -1)        // real loss from hop 3
	run(2*ms, 8*ms, 80*ms, 120*ms) // spike from hop 3
	for i := 0; i < 5; i++ {
		run(2*ms, 8*ms, 10*ms, 20*ms)
	}
	evs, err := r.m.store.Events(r.sid)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range evs {
		got[e.Kind] = e.Zone + "@" + e.Addr + "/" + e.Title
	}
	for kind, wantZone := range map[string]string{KindLossHop: "ISP_EDGE", KindLoss: "ISP_EDGE", KindSpike: "ISP_EDGE"} {
		if g, ok := got[kind]; !ok || g[:len(wantZone)] != wantZone {
			t.Errorf("%s: got %q, want zone %s (all: %v)", kind, g, wantZone, got)
		}
	}
}

func TestCustomPointEvent(t *testing.T) {
	r := newTestRunner(t)
	order := []series{{key: "LAN", ttl: 1, zone: path.LAN}, {key: "WAN", ttl: targetTTL, zone: path.WAN}}
	dev := series{key: DevKey("192.168.0.1"), zone: path.LAN, addr: "192.168.0.1", label: "Gerät Modem (192.168.0.1)"}
	for seq := 1; seq <= 8; seq++ {
		rtt := time.Duration(-1)
		if seq > 3 {
			rtt = time.Millisecond
		}
		r.seq = seq
		r.finishTick(&tick{seq: seq, at: time.Now(), order: order, custom: []series{dev},
			result: map[string]time.Duration{"LAN": time.Millisecond, "WAN": 5 * time.Millisecond, dev.key: rtt}})
	}
	evs, _ := r.m.store.Events(r.sid)
	if len(evs) != 1 || evs[0].Kind != KindLossDevice || evs[0].Count != 3 {
		t.Fatalf("unexpected events %+v", evs)
	}
}

func TestIncidents(t *testing.T) {
	order := []series{
		{key: "LAN", ttl: 1, zone: path.LAN, label: "Hop 1"},
		{key: "ISP_EDGE", ttl: 2, zone: path.ISPEdge, label: "Hop 2"},
		{key: "WAN", ttl: targetTTL, zone: path.WAN, label: "Ziel"},
	}
	alt := series{key: DevKey("1.1.1.1"), zone: path.WAN, label: "Gerät Cloudflare"}
	ms := time.Millisecond
	cases := []struct {
		name          string
		isp, wan, alt time.Duration
		want          string
	}{
		{"provider access", -1, -1, -1, ClassISP},
		{"only main target", 10 * ms, -1, 10 * ms, ClassTarget},
		{"all targets, edge ok", 10 * ms, -1, -1, ClassISPCore},
		{"only backup", 10 * ms, 20 * ms, -1, ClassAlt},
	}
	for _, c := range cases {
		r := newTestRunner(t)
		seq := 0
		run := func(isp, wan, a time.Duration) {
			seq++
			r.seq = seq
			r.finishTick(&tick{seq: seq, at: time.Now(), order: order, custom: []series{alt},
				result: map[string]time.Duration{"LAN": ms, "ISP_EDGE": isp, "WAN": wan, alt.key: a}})
		}
		for i := 0; i < 15; i++ {
			run(10*ms, 20*ms, 15*ms)
		}
		for i := 0; i < 4; i++ {
			run(c.isp, c.wan, c.alt)
		}
		for i := 0; i < 6; i++ {
			run(10*ms, 20*ms, 15*ms)
		}
		ins, _ := r.m.store.Incidents(r.sid)
		if len(ins) != 1 || ins[0].Class != c.want || ins[0].Seconds != 4 || ins[0].PreRoll != r.cfg.IncidentPreSec {
			t.Errorf("%s: got %+v", c.name, ins)
			continue
		}
		if s := ins[0].Series[len(ins[0].Series)-1]; !s.Custom || len(s.States) != len(ins[0].Columns) {
			t.Errorf("%s: bad matrix row %+v", c.name, s)
		}
	}
}

func TestCustomZones(t *testing.T) {
	order := []series{
		{key: "LAN", ttl: 1, zone: path.LAN, label: "Hop 1"},
		{key: "ISP_EDGE", ttl: 2, zone: path.ISPEdge, label: "Hop 2"},
		{key: "WAN", ttl: targetTTL, zone: path.WAN, label: "Ziel"},
	}
	fritz := series{key: DevKey("192.168.0.1"), zone: path.LAN, label: "Fritz"}
	ignored := series{key: DevKey("192.168.144.9"), zone: ZoneNone, label: "PVE"}
	tk := &tick{order: order, custom: []series{fritz, ignored}}
	cases := []struct {
		name string
		st   map[string]byte
		want string
	}{
		{"lan custom down with all targets", map[string]byte{"LAN": '.', "ISP_EDGE": 'x', "WAN": 'x', fritz.key: 'x', ignored.key: '.'}, ClassLAN},
		{"lan custom down, internet ok", map[string]byte{"LAN": '.', "ISP_EDGE": '.', "WAN": '.', fritz.key: 'x', ignored.key: '.'}, ClassDevice},
		{"zone none ignored", map[string]byte{"LAN": '.', "ISP_EDGE": '.', "WAN": '.', fritz.key: '.', ignored.key: 'x'}, ""},
	}
	for _, c := range cases {
		if got, _ := classifyTick(tk, c.st, len(order)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPingTCPDisagreement(t *testing.T) {
	order := []series{
		{key: "LAN", ttl: 1, zone: path.LAN},
		{key: "WAN", ttl: targetTTL, zone: path.WAN},
	}
	ping := series{key: DevKey("1.1.1.1"), zone: path.WAN, label: "CF"}
	tcp := series{key: TCPKey("1.1.1.1"), zone: path.WAN, label: "CF TCP"}
	r := newTestRunner(t)
	ms := time.Millisecond
	for seq := 1; seq <= 30; seq++ {
		wan := 20 * ms
		if seq >= 15 && seq < 19 {
			wan = -1 // real target outage in between
		}
		r.seq = seq
		r.finishTick(&tick{seq: seq, at: time.Now(), order: order, custom: []series{ping, tcp},
			result: map[string]time.Duration{"LAN": ms, "WAN": wan, ping.key: -1, tcp.key: 15 * ms}})
	}
	r.closeIncident()
	ins, _ := r.m.store.Incidents(r.sid)
	if len(ins) != 1 || ins[0].Class != ClassTarget || ins[0].Seconds != 4 {
		t.Fatalf("ping-only loss must not hold incidents open: %+v", ins)
	}
	for _, s := range ins[0].Series {
		if s.Key == ping.key && (s.Lost != 0 || s.Maybe != 4) {
			t.Fatalf("ping row should be 'maybe false positive': %+v", s)
		}
	}
	evs, _ := r.m.store.Events(r.sid)
	for _, e := range evs {
		if e.Kind == KindLossDevice {
			t.Fatalf("no device loss event expected: %+v", e)
		}
	}
}

func TestIncidentSplitAndPersistent(t *testing.T) {
	order := []series{
		{key: "LAN", ttl: 1, zone: path.LAN},
		{key: "ISP_EDGE", ttl: 2, zone: path.ISPEdge},
		{key: "WAN", ttl: targetTTL, zone: path.WAN},
	}
	alt := series{key: DevKey("9.9.9.9"), zone: path.WAN, label: "Quad9"}
	dead := series{key: DevKey("1.1.1.1"), zone: path.WAN, label: "CF (ping gefiltert)"}
	r := newTestRunner(t)
	ms := time.Millisecond
	seq := 0
	run := func(n int, isp, wan, a time.Duration) {
		for i := 0; i < n; i++ {
			seq++
			r.seq = seq
			r.finishTick(&tick{seq: seq, at: time.Now(), order: order, custom: []series{alt, dead},
				result: map[string]time.Duration{"LAN": ms, "ISP_EDGE": isp, "WAN": wan, alt.key: a, dead.key: -1}})
		}
	}
	run(persistentAfter+10, 10*ms, 20*ms, 15*ms) // dead point becomes permanent
	run(5, 10*ms, -1, 15*ms)                     // only main target → "target"
	run(5, -1, -1, -1)                           // provider access down → split
	run(10, 10*ms, 20*ms, 15*ms)
	r.closeIncident()
	ins, _ := r.m.store.Incidents(r.sid)
	var classes []string
	for _, in := range ins {
		classes = append(classes, in.Class)
	}
	// First the dead point is an ordinary "backup target down" incident
	// until it counts as permanent (then that incident ends); afterwards the
	// real disruptions are separate incidents of their own class.
	if len(ins) != 3 || ins[0].Class != ClassAlt || ins[0].Seconds > persistentAfter ||
		ins[1].Class != ClassTarget || ins[1].Seconds != 5 || ins[2].Class != ClassISP || ins[2].Seconds != 5 {
		t.Fatalf("want [alt target isp] with 5 s each for the last two, got %v %+v", classes, ins)
	}
	evs, _ := r.m.store.Events(r.sid)
	found := false
	for _, e := range evs {
		found = found || e.Kind == KindPersistent
	}
	if !found {
		t.Fatal("missing persistent event")
	}
}
