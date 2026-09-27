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
		r.handle(result{zone: z, at: at, rtt: rtt})
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
