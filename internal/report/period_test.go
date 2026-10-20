package report

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chevchelios420x/proof-of-fault/internal/store"
)

func TestBuildPeriods(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now()
	id, _ := st.CreateSession("1.1.1.1", "1.1.1.1", nil)
	// Two hours of samples 3 days ago, one second lost.
	base := now.Add(-72 * time.Hour)
	for i := 0; i < 7200; i++ {
		at := base.Add(time.Duration(i) * time.Second)
		rtt := time.Duration(10+i%5) * time.Millisecond
		if i == 100 {
			rtt = -1
		}
		st.AddSample(store.Sample{SessionID: id, Zone: "WAN", At: at, RTT: rtt})
		st.AddSample(store.Sample{SessionID: id, Zone: "LAN", At: at, RTT: time.Millisecond})
	}
	st.AddIncident(id, store.Incident{T: base.Add(100 * time.Second).UnixMilli(), End: base.Add(190 * time.Second).UnixMilli(),
		Seconds: 90, Class: "isp", Title: "Anbieter weg", Classes: map[string]int{"isp": 90}})
	st.AddIncident(id, store.Incident{T: base.Add(500 * time.Second).UnixMilli(), Seconds: 4, Class: "lan"})
	st.EndSession(id)

	r, err := BuildPeriods(st, time.Now(), PeriodDays, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Periods) != 4 {
		t.Fatalf("periods: %d", len(r.Periods))
	}
	p1, p7 := r.Periods[0], r.Periods[1]
	if p1.Total.ISP.N != 0 || p7.Total.ISP.N != 1 || p7.Total.ISP.Sec != 90 || p7.Total.ISPLong != 1 || p7.Total.Home.N != 1 {
		t.Fatalf("p1 %+v p7 %+v", p1.Total.ISP, p7.Total)
	}
	if p7.Total.MeasuredSec != 7200 || p7.Total.Loss["WAN"].Lost != 1 || len(p7.Buckets) < 7 {
		t.Fatalf("p7 measured %d loss %+v buckets %d", p7.Total.MeasuredSec, p7.Total.Loss, len(p7.Buckets))
	}
	if l := p7.Total.Latency; l.P50Ms < 11.9 || l.P50Ms > 12.2 || l.MaxMs != 14 {
		t.Fatalf("latency %+v", l)
	}
	if len(p1.Buckets) < 24 || len(p1.Buckets) > 25 {
		t.Fatalf("hourly buckets %d", len(p1.Buckets))
	}
	if len(r.Incidents) != 1 {
		t.Fatalf("incidents %d", len(r.Incidents))
	}
	var buf bytes.Buffer
	if err := WritePeriodHTML(&buf, r); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{"24 Stunden", "90 Tage", "Anbieter weg", "1 min 30 s", "§ 58 TKG"} {
		if !strings.Contains(html, want) {
			t.Errorf("report misses %q", want)
		}
	}
}
