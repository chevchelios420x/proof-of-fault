// Package metrics implements latency statistics: RFC 3550 interarrival
// jitter and exact percentiles.
package metrics

import (
	"math"
	"sort"
	"time"
)

// Jitter is the RFC 3550 (section 6.4.1) interarrival jitter estimator,
// applied to round-trip times: D = (R_i - R_{i-1}) - (S_i - S_{i-1}) reduces
// to RTT_i - RTT_{i-1}; J += (|D| - J) / 16.
type Jitter struct {
	prev  time.Duration
	have  bool
	value float64 // nanoseconds
}

// Add feeds a successful RTT sample. Losses must not be fed; call Reset if a
// gap should break the difference chain.
func (j *Jitter) Add(rtt time.Duration) {
	if j.have {
		d := math.Abs(float64(rtt - j.prev))
		j.value += (d - j.value) / 16
	}
	j.prev, j.have = rtt, true
}

// Value returns the current jitter estimate.
func (j *Jitter) Value() time.Duration { return time.Duration(j.value) }

// Summary describes a set of RTT samples.
type Summary struct {
	Sent     int     `json:"sent"`
	Received int     `json:"received"`
	LossPct  float64 `json:"lossPct"`
	MinMs    float64 `json:"minMs"`
	AvgMs    float64 `json:"avgMs"`
	P50Ms    float64 `json:"p50Ms"`
	P95Ms    float64 `json:"p95Ms"`
	P99Ms    float64 `json:"p99Ms"`
	MaxMs    float64 `json:"maxMs"`
	JitterMs float64 `json:"jitterMs"` // RFC 3550
}

// Summarize computes a Summary. rtts holds one entry per probe in send
// order; a negative value marks a loss.
func Summarize(rtts []time.Duration) Summary {
	s := Summary{Sent: len(rtts)}
	ok := make([]float64, 0, len(rtts))
	var j Jitter
	for _, r := range rtts {
		if r < 0 {
			continue
		}
		j.Add(r)
		ok = append(ok, ms(r))
	}
	s.Received = len(ok)
	if s.Sent > 0 {
		s.LossPct = 100 * float64(s.Sent-s.Received) / float64(s.Sent)
	}
	if len(ok) == 0 {
		return s
	}
	s.JitterMs = ms(j.Value())
	var sum float64
	for _, v := range ok {
		sum += v
	}
	s.AvgMs = sum / float64(len(ok))
	sort.Float64s(ok)
	s.MinMs, s.MaxMs = ok[0], ok[len(ok)-1]
	s.P50Ms = Percentile(ok, 50)
	s.P95Ms = Percentile(ok, 95)
	s.P99Ms = Percentile(ok, 99)
	return s
}

// Percentile returns the p-th percentile (0..100) of sorted values using
// linear interpolation between closest ranks.
func Percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := p / 100 * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	frac := rank - float64(lo)
	return sorted[lo] + (sorted[hi]-sorted[lo])*frac
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
