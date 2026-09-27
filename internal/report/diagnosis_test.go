package report

import (
	"strings"
	"testing"

	"github.com/chevchelios420x/proof-of-fault/internal/netinfo"
	"github.com/chevchelios420x/proof-of-fault/internal/store"
)

func TestDiagnose(t *testing.T) {
	if d := Diagnose(nil); d.Level != "none" {
		t.Fatalf("empty log: level %s", d.Level)
	}
	evs := []store.Event{
		{Kind: "loss", Zone: "ISP_EDGE", Addr: "188.1.1.1", TTL: 2, Count: 3},
		{Kind: "loss", Zone: "ISP_EDGE", Addr: "188.1.1.1", TTL: 2, Count: 2},
		{Kind: "outage_start", Zone: "ISP_EDGE", Addr: ""},
		{Kind: "outage_end", Zone: "ISP_EDGE", Count: 20},
		{Kind: "spike", Zone: "WAN", Count: 1},
		{Kind: "loss_hop", Zone: "WAN"},
		{Kind: "path_change"},
	}
	d := Diagnose(evs)
	if d.Level != "isp" || d.Confidence != "hoch" || d.Harmless != 1 || d.RouteChanges != 1 {
		t.Fatalf("unexpected diagnosis %+v", d)
	}
	if d.TopOrigin == "" {
		t.Fatal("missing origin")
	}
}

func TestHTMLWithIncident(t *testing.T) {
	in := store.Incident{ID: 1, T: 1000, End: 5000, Seconds: 4, Class: "isp", Title: "x", PreRoll: 1, PostRoll: 1,
		Columns: []int64{0, 1000, 2000, 3000, 4000, 5000},
		Series:  []store.IncidentSeries{{Label: "Ziel", States: ".xxxx.", RTT: []float64{10, -1, -1, -1, -1, 11}}}}
	r := Report{Incidents: []store.Incident{in}, Diagnosis: DiagnoseAll(nil, []store.Incident{in})}
	var b strings.Builder
	if err := WriteHTML(&b, r, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `class="c-x"`) || r.Diagnosis.Level != "isp" {
		t.Fatalf("matrix or diagnosis missing: level=%s", r.Diagnosis.Level)
	}
}

func TestHTMLNetInfo(t *testing.T) {
	r := Report{Net: &netinfo.Snapshot{Hostname: "pc", LocalIP: "192.168.0.6", Gateways: []string{"192.168.0.1"}, Routes: "0.0.0.0 0.0.0.0 192.168.0.1", ARP: "192.168.0.1 bc-24-11"}}
	var b strings.Builder
	if err := WriteHTML(&b, r, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Messrechner", "192.168.0.6", "Standard-Gateway", "ARP-Tabelle"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
}
