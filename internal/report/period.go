package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
	_ "time/tzdata" // Europe/Berlin also on Windows without zoneinfo

	"github.com/chevchelios420x/proof-of-fault/internal/config"
	"github.com/chevchelios420x/proof-of-fault/internal/docsis"
	"github.com/chevchelios420x/proof-of-fault/internal/netinfo"
	"github.com/chevchelios420x/proof-of-fault/internal/store"
)

// The period report summarises all measurements of the last 24 hours,
// 7, 30 and 90 days, e.g. as evidence for the provider or the
// Bundesnetzagentur. Disruptions count against the provider when the
// incident analysis attributed them to the provider access ("isp") or the
// provider network / internet behind it ("isp_core").

// PeriodDays are the evaluated periods.
var PeriodDays = []int{1, 7, 30, 90}

// Location is the time zone of the report (German local time).
var Location = func() *time.Location {
	if l, err := time.LoadLocation("Europe/Berlin"); err == nil {
		return l
	}
	return time.Local
}()

// Count is a number of disruptions and their total duration.
type Count struct {
	N      int     `json:"n"`
	Sec    float64 `json:"sec"`
	MaxSec float64 `json:"maxSec"`
}

func (c *Count) add(sec float64) {
	c.N++
	c.Sec += sec
	if sec > c.MaxSec {
		c.MaxSec = sec
	}
}

func (c *Count) merge(o Count) {
	c.N += o.N
	c.Sec += o.Sec
	if o.MaxSec > c.MaxSec {
		c.MaxSec = o.MaxSec
	}
}

// Loss is the number of probes and losses of a zone.
type Loss struct {
	Sent int `json:"sent"`
	Lost int `json:"lost"`
}

// Pct returns the loss in percent (0 without probes).
func (l Loss) Pct() float64 {
	if l.Sent == 0 {
		return 0
	}
	return 100 * float64(l.Lost) / float64(l.Sent)
}

// Bucket is one row of a period table (an hour or a day).
type Bucket struct {
	From        int64           `json:"from"` // unix ms
	To          int64           `json:"to"`
	Label       string          `json:"label"`
	MeasuredSec int             `json:"measuredSec"` // seconds with a probe of the main target
	ISP         Count           `json:"isp"`         // disruptions attributed to the provider
	ISPLong     int             `json:"ispLong"`     // provider disruptions of at least one minute
	Home        Count           `json:"home"`        // home network (control)
	TargetOnly  Count           `json:"targetOnly"`  // only main/backup target
	Loss        map[string]Loss `json:"loss"`        // LAN, ISP_EDGE, WAN
	Latency     Latency         `json:"latency"`     // main target (WAN)
	hist        *hist
}

// Latency summarises the round-trip times of the main target.
type Latency struct {
	N     int     `json:"n"`
	P50Ms float64 `json:"p50Ms"`
	P95Ms float64 `json:"p95Ms"`
	P99Ms float64 `json:"p99Ms"`
	MaxMs float64 `json:"maxMs"`
}

// Coverage is the share of the bucket that was measured (0…1).
func (b Bucket) Coverage() float64 {
	d := float64(b.To-b.From) / 1000
	if d <= 0 {
		return 0
	}
	return min(1, float64(b.MeasuredSec)/d)
}

// Availability is the share of the measured time without a provider
// disruption (0…1); 0 without measurements.
func (b Bucket) Availability() float64 {
	if b.MeasuredSec == 0 {
		return 0
	}
	return max(0, 1-b.ISP.Sec/float64(b.MeasuredSec))
}

// IncidentRow is one provider disruption of the list.
type IncidentRow struct {
	T       int64   `json:"t"`
	End     int64   `json:"end"`
	Sec     float64 `json:"sec"`
	Class   string  `json:"class"`
	Title   string  `json:"title"`
	Origin  string  `json:"origin"`
	Session int64   `json:"session"`
	Docsis  string  `json:"docsis"` // line status around the disruption ("" = no reading)
}

// DocsisSummary counts the DOCSIS readings of a period.
type DocsisSummary struct {
	N        int      `json:"n"`
	Warning  int      `json:"warning"`
	Critical int      `json:"critical"`
	NonCorr  int64    `json:"nonCorr"`
	Issues   []string `json:"issues"` // most frequent findings
}

// Period is the evaluation of one period.
type Period struct {
	Days        int           `json:"days"`
	Title       string        `json:"title"`
	From        int64         `json:"from"`
	To          int64         `json:"to"`
	Total       Bucket        `json:"total"`
	Buckets     []Bucket      `json:"buckets"`
	DaysWithISP int           `json:"daysWithIsp"` // buckets with a provider disruption
	Durations   [4]int        `json:"durations"`   // provider disruptions <10 s, <1 min, <10 min, longer
	Docsis      DocsisSummary `json:"docsis"`
}

// PeriodReport holds all periods.
type PeriodReport struct {
	GeneratedAt int64               `json:"generatedAt"`
	Version     string              `json:"version"`
	TimeZone    string              `json:"timeZone"`
	Contract    config.Contract     `json:"contract"`
	Access      string              `json:"access"`
	Periods     []Period            `json:"periods"`
	Incidents   []IncidentRow       `json:"incidents"` // provider disruptions of the longest period
	Sessions    []store.SessionInfo `json:"sessions"`
	Targets     []string            `json:"targets"`
	Net         *netinfo.Snapshot   `json:"net"`
	DataSHA256  string              `json:"dataSha256"`
}

// isISP reports whether an incident class counts against the provider.
func isISP(c string) bool { return c == "isp" || c == "isp_core" }

// BuildPeriods evaluates the given periods ending at now.
func BuildPeriods(st *store.Store, now time.Time, days []int, version string) (PeriodReport, error) {
	now = now.In(Location)
	cfg := st.Settings()
	rep := PeriodReport{GeneratedAt: now.UnixMilli(), Version: version, TimeZone: Location.String(),
		Contract: cfg.Contract, Access: cfg.Access}
	longest := 0
	for _, d := range days {
		longest = max(longest, d)
	}
	start := now.AddDate(0, 0, -longest)
	sessions, err := st.SessionsBetween(start.UnixMilli(), now.UnixMilli())
	if err != nil {
		return rep, err
	}
	rep.Sessions = sessions
	seen := map[string]bool{}
	for _, s := range sessions {
		if !seen[s.Target] {
			seen[s.Target] = true
			rep.Targets = append(rep.Targets, s.Target)
		}
	}
	for i := len(sessions) - 1; i >= 0 && rep.Net == nil; i-- {
		if len(sessions[i].NetInfo) > 0 {
			var n netinfo.Snapshot
			if json.Unmarshal(sessions[i].NetInfo, &n) == nil {
				rep.Net = &n
			}
		}
	}

	// Incidents and DOCSIS readings of all sessions (loaded once).
	type inc struct {
		store.Incident
		sid int64
	}
	var incidents []inc
	var readings []docsis.Snapshot
	for _, s := range sessions {
		ins, err := st.Incidents(s.ID)
		if err != nil {
			return rep, err
		}
		for _, in := range ins {
			incidents = append(incidents, inc{in, s.ID})
		}
		raws, _ := st.DocsisSnapshots(s.ID)
		for _, r := range raws {
			var d docsis.Snapshot
			if json.Unmarshal(r, &d) == nil {
				readings = append(readings, d)
			}
		}
	}
	sort.Slice(incidents, func(i, j int) bool { return incidents[i].T < incidents[j].T })

	fill := func(b *Bucket) error {
		b.Loss = map[string]Loss{}
		b.hist = newHist()
		for _, s := range sessions {
			for _, z := range []string{"LAN", "ISP_EDGE", "WAN"} {
				n, lost, err := st.ZoneCounts(s.ID, z, b.From, b.To)
				if err != nil {
					return err
				}
				l := b.Loss[z]
				l.Sent += n
				l.Lost += lost
				b.Loss[z] = l
				if z == "WAN" {
					b.MeasuredSec += n
				}
			}
			if err := st.EachRTT(s.ID, "WAN", b.From, b.To, b.hist.add); err != nil {
				return err
			}
		}
		for _, in := range incidents {
			if in.T < b.From || in.T >= b.To {
				continue
			}
			ispSec := float64(in.Classes["isp"] + in.Classes["isp_core"])
			switch {
			case isISP(in.Class):
				if ispSec == 0 {
					ispSec = float64(in.Seconds)
				}
				b.ISP.add(ispSec)
				if ispSec >= 60 {
					b.ISPLong++
				}
			case in.Class == "lan" || in.Class == "device":
				b.Home.add(float64(in.Seconds))
			default:
				b.TargetOnly.add(float64(in.Seconds))
			}
		}
		b.Latency = b.hist.latency()
		return nil
	}

	for _, d := range days {
		p := Period{Days: d, From: now.AddDate(0, 0, -d).UnixMilli(), To: now.UnixMilli()}
		p.Title = fmt.Sprintf("%d Tage", d)
		if d == 1 {
			p.Title = "24 Stunden"
		}
		// Rows: hours for one day, calendar days otherwise (first and last
		// row may be partial).
		from := time.UnixMilli(p.From).In(Location)
		var cur time.Time
		if d == 1 {
			cur = from.Truncate(time.Hour)
		} else {
			cur = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, Location)
		}
		p.Total = Bucket{From: p.From, To: p.To, Loss: map[string]Loss{}, hist: newHist()}
		for cur.Before(now) {
			next := cur.AddDate(0, 0, 1)
			label := cur.Format("Mo 02.01.2006")
			if d == 1 {
				next = cur.Add(time.Hour)
				label = cur.Format("02.01. 15:04") + "–" + next.Format("15:04")
			}
			b := Bucket{From: max(cur.UnixMilli(), p.From), To: min(next.UnixMilli(), p.To), Label: germanWeekday(label, cur)}
			if err := fill(&b); err != nil {
				return rep, err
			}
			p.Total.MeasuredSec += b.MeasuredSec
			p.Total.ISP.merge(b.ISP)
			p.Total.ISPLong += b.ISPLong
			p.Total.Home.merge(b.Home)
			p.Total.TargetOnly.merge(b.TargetOnly)
			for z, l := range b.Loss {
				t := p.Total.Loss[z]
				t.Sent += l.Sent
				t.Lost += l.Lost
				p.Total.Loss[z] = t
			}
			p.Total.hist.merge(b.hist)
			if b.ISP.N > 0 {
				p.DaysWithISP++
			}
			b.hist = nil
			p.Buckets = append(p.Buckets, b)
			cur = next
		}
		p.Total.Latency = p.Total.hist.latency()
		p.Total.hist = nil
		for _, in := range incidents {
			if in.T < p.From || in.T >= p.To || !isISP(in.Class) {
				continue
			}
			sec := float64(in.Classes["isp"] + in.Classes["isp_core"])
			if sec == 0 {
				sec = float64(in.Seconds)
			}
			switch {
			case sec < 10:
				p.Durations[0]++
			case sec < 60:
				p.Durations[1]++
			case sec < 600:
				p.Durations[2]++
			default:
				p.Durations[3]++
			}
		}
		issues := map[string]int{}
		for _, r := range readings {
			if r.T < p.From || r.T >= p.To {
				continue
			}
			p.Docsis.N++
			switch r.Status {
			case "warning":
				p.Docsis.Warning++
			case "critical":
				p.Docsis.Critical++
			}
			p.Docsis.NonCorr += r.NonCorrDelta
			for _, is := range r.Issues {
				issues[is]++
			}
		}
		p.Docsis.Issues = topKeys(issues, 5)
		rep.Periods = append(rep.Periods, p)
	}

	// Provider disruptions of the longest period, with the line status.
	for _, in := range incidents {
		if in.T < start.UnixMilli() || !isISP(in.Class) {
			continue
		}
		sec := float64(in.Classes["isp"] + in.Classes["isp_core"])
		if sec == 0 {
			sec = float64(in.Seconds)
		}
		row := IncidentRow{T: in.T, End: in.End, Sec: sec, Class: in.Class, Title: in.Title, Origin: in.Origin, Session: in.sid}
		worst := ""
		for _, r := range DocsisAround(readings, in.T, in.End) {
			if rank(r.Status) > rank(worst) {
				worst = r.Status
			}
		}
		row.Docsis = worst
		rep.Incidents = append(rep.Incidents, row)
	}

	b, _ := json.Marshal(struct {
		P []Period
		I []IncidentRow
	}{rep.Periods, rep.Incidents})
	sum := sha256.Sum256(b)
	rep.DataSHA256 = hex.EncodeToString(sum[:])
	return rep, nil
}

func rank(s string) int {
	switch s {
	case "good":
		return 1
	case "warning":
		return 2
	case "critical":
		return 3
	}
	return 0
}

func topKeys(m map[string]int, n int) []string {
	type kv struct {
		k string
		v int
	}
	var all []kv
	for k, v := range m {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v || all[i].v == all[j].v && all[i].k < all[j].k })
	var out []string
	for i := 0; i < len(all) && i < n; i++ {
		out = append(out, fmt.Sprintf("%s (%d×)", all[i].k, all[i].v))
	}
	return out
}

var weekdays = [...]string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"}

func germanWeekday(label string, t time.Time) string {
	if len(label) > 2 && label[:2] == "Mo" {
		return weekdays[t.Weekday()] + label[2:]
	}
	return label
}

// hist is a latency histogram: 0.1 ms steps up to 100 ms, 1 ms steps up to
// 5 s, then one overflow bucket. Enough for percentiles over 90 days without
// keeping millions of values.
type hist struct {
	c     []uint32
	n     int
	maxUs int64
}

const (
	histFine   = 1000 // 0.1 ms buckets below 100 ms
	histCoarse = 4900 // 1 ms buckets from 100 ms to 5 s
)

func newHist() *hist { return &hist{c: make([]uint32, histFine+histCoarse+1)} }

func (h *hist) add(us int64) {
	var i int
	switch {
	case us < 100_000:
		i = int(us / 100)
	case us < 5_000_000:
		i = histFine + int((us-100_000)/1000)
	default:
		i = histFine + histCoarse
	}
	h.c[max(0, i)]++
	h.n++
	h.maxUs = max(h.maxUs, us)
}

func (h *hist) merge(o *hist) {
	for i, v := range o.c {
		h.c[i] += v
	}
	h.n += o.n
	h.maxUs = max(h.maxUs, o.maxUs)
}

// quantile returns the upper edge of the bucket holding quantile q (ms).
func (h *hist) quantile(q float64) float64 {
	if h.n == 0 {
		return 0
	}
	want := int(q*float64(h.n-1)) + 1
	sum := 0
	for i, v := range h.c {
		sum += int(v)
		if sum >= want {
			switch {
			case i < histFine:
				return float64(i+1) / 10
			case i < histFine+histCoarse:
				return float64(100 + i - histFine + 1)
			default:
				return float64(h.maxUs) / 1000
			}
		}
	}
	return float64(h.maxUs) / 1000
}

func (h *hist) latency() Latency {
	return Latency{N: h.n, P50Ms: h.quantile(0.5), P95Ms: h.quantile(0.95), P99Ms: h.quantile(0.99), MaxMs: float64(h.maxUs) / 1000}
}
