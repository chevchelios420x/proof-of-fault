package metrics

import (
	"math"
	"testing"
	"time"
)

func TestJitterConstantRTTIsZero(t *testing.T) {
	var j Jitter
	for i := 0; i < 100; i++ {
		j.Add(20 * time.Millisecond)
	}
	if j.Value() != 0 {
		t.Fatalf("jitter = %v, want 0", j.Value())
	}
}

func TestJitterConvergesToAlternatingDelta(t *testing.T) {
	var j Jitter
	for i := 0; i < 1000; i++ {
		if i%2 == 0 {
			j.Add(10 * time.Millisecond)
		} else {
			j.Add(20 * time.Millisecond)
		}
	}
	if got := j.Value(); math.Abs(float64(got-10*time.Millisecond)) > float64(time.Microsecond) {
		t.Fatalf("jitter = %v, want ~10ms", got)
	}
}

func TestPercentile(t *testing.T) {
	v := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	cases := map[float64]float64{0: 1, 50: 5.5, 100: 10, 90: 9.1}
	for p, want := range cases {
		if got := Percentile(v, p); math.Abs(got-want) > 1e-9 {
			t.Errorf("P%v = %v, want %v", p, got, want)
		}
	}
}

func TestSummarizeLoss(t *testing.T) {
	s := Summarize([]time.Duration{10 * time.Millisecond, -1, 30 * time.Millisecond, -1})
	if s.Sent != 4 || s.Received != 2 || s.LossPct != 50 {
		t.Fatalf("unexpected summary %+v", s)
	}
	if s.MinMs != 10 || s.MaxMs != 30 || s.AvgMs != 20 {
		t.Fatalf("unexpected latency stats %+v", s)
	}
}
