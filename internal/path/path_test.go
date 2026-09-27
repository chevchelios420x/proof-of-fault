package path

import "testing"

func zonesOf(addrs ...string) []Zone {
	hops := make([]Hop, len(addrs))
	for i, a := range addrs {
		hops[i] = Hop{TTL: i + 1, Addr: a, Responsive: a != ""}
	}
	Classify(hops, nil)
	out := make([]Zone, len(hops))
	for i, h := range hops {
		out[i] = h.Zone
	}
	return out
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name  string
		addrs []string
		want  []Zone
	}{
		{"plain", []string{"192.168.178.1", "62.155.1.1", "80.1.1.1", "1.1.1.1"},
			[]Zone{LAN, ISPEdge, WAN, WAN}},
		{"cgnat", []string{"192.168.1.1", "100.64.0.1", "84.1.1.1", "1.1.1.1"},
			[]Zone{LAN, ISPEdge, ISPEdge, WAN}},
		{"cascaded router + silent hop", []string{"192.168.0.1", "192.168.100.1", "", "84.1.1.1", "8.8.8.8"},
			[]Zone{LAN, LAN, LAN, ISPEdge, WAN}},
	}
	for _, c := range cases {
		got := zonesOf(c.addrs...)
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: hop %d = %s, want %s (all: %v)", c.name, i+1, got[i], c.want[i], got)
				break
			}
		}
	}
}

func TestClassifyOverride(t *testing.T) {
	hops := []Hop{{TTL: 1, Addr: "192.168.1.1"}, {TTL: 2, Addr: "10.0.0.1"}, {TTL: 3, Addr: "84.1.1.1"}}
	Classify(hops, map[string]Zone{"10.0.0.1": ISPEdge})
	if hops[1].Zone != ISPEdge || !hops[1].Manual || hops[0].Manual {
		t.Fatalf("override not applied: %+v", hops)
	}
}
