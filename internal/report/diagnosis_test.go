package report

import (
	"testing"

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
