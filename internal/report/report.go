// Package report builds evaluations of a stored session and exports them as
// CSV or a self-contained HTML document (printable to PDF).
package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/chevchelios420x/proof-of-fault/internal/metrics"
	"github.com/chevchelios420x/proof-of-fault/internal/path"
	"github.com/chevchelios420x/proof-of-fault/internal/store"
)

// Peak is one of the highest latencies of a zone.
type Peak struct {
	T     int64   `json:"t"` // unix ms
	RTTMs float64 `json:"rttMs"`
}

// ZoneReport holds the evaluation of one zone.
type ZoneReport struct {
	Zone    path.Zone       `json:"zone"`
	Summary metrics.Summary `json:"summary"`
	Peaks   []Peak          `json:"peaks"`
	// SpikesOver100 counts samples above 100 ms.
	SpikesOver100 int `json:"spikesOver100"`
}

// Report is the complete evaluation of a session.
type Report struct {
	Session       store.SessionInfo  `json:"session"`
	GeneratedAt   int64              `json:"generatedAt"`
	DurationSec   float64            `json:"durationSec"`
	Hops          []path.Hop         `json:"hops"`
	PathChanges   int                `json:"pathChanges"`
	Zones         []ZoneReport       `json:"zones"`
	Outages       []store.Outage     `json:"outages"`
	OutageSeconds map[string]float64 `json:"outageSeconds"`
	Verdict       string             `json:"verdict"`
	DataSHA256    string             `json:"dataSha256"`
}

// Series is the chart data of a session (aligned per zone).
type Series struct {
	Zone  path.Zone `json:"zone"`
	T     []int64   `json:"t"`   // unix ms
	RTTMs []float64 `json:"rtt"` // -1 = loss
}

// Build evaluates a session.
func Build(st *store.Store, id int64) (Report, []Series, []store.Sample, error) {
	var rep Report
	info, err := st.Session(id)
	if err != nil {
		return rep, nil, nil, err
	}
	samples, err := st.Samples(id)
	if err != nil {
		return rep, nil, nil, err
	}
	outages, err := st.Outages(id)
	if err != nil {
		return rep, nil, nil, err
	}
	hopsJSON, _ := st.LatestPath(id)
	json.Unmarshal([]byte(hopsJSON), &rep.Hops)
	names := st.HopNames()
	for i := range rep.Hops {
		rep.Hops[i].Name = names[rep.Hops[i].Addr]
	}
	rep.PathChanges, _ = st.PathChanges(id)

	rep.Session = info
	rep.GeneratedAt = time.Now().UnixMilli()
	end := info.EndedAt
	if end == 0 {
		end = time.Now().UnixMilli()
	}
	rep.DurationSec = float64(end-info.StartedAt) / 1000
	rep.Outages = outages
	rep.OutageSeconds = map[string]float64{}
	for _, o := range outages {
		rep.OutageSeconds[o.Zone] += o.Seconds
	}

	byZone := map[path.Zone][]store.Sample{}
	h := sha256.New()
	for _, s := range samples {
		byZone[path.Zone(s.Zone)] = append(byZone[path.Zone(s.Zone)], s)
		h.Write([]byte(csvLine(s)))
	}
	rep.DataSHA256 = hex.EncodeToString(h.Sum(nil))

	var series []Series
	for _, z := range path.Zones {
		ss := byZone[z]
		if len(ss) == 0 {
			continue
		}
		rtts := make([]time.Duration, len(ss))
		ser := Series{Zone: z, T: make([]int64, len(ss)), RTTMs: make([]float64, len(ss))}
		var peaks []Peak
		zr := ZoneReport{Zone: z}
		for i, s := range ss {
			rtts[i] = s.RTT
			ser.T[i] = s.At.UnixMilli()
			ser.RTTMs[i] = -1
			if s.RTT >= 0 {
				ms := float64(s.RTT) / float64(time.Millisecond)
				ser.RTTMs[i] = ms
				peaks = append(peaks, Peak{T: ser.T[i], RTTMs: ms})
				if ms > 100 {
					zr.SpikesOver100++
				}
			}
		}
		sort.Slice(peaks, func(i, j int) bool { return peaks[i].RTTMs > peaks[j].RTTMs })
		if len(peaks) > 10 {
			peaks = peaks[:10]
		}
		zr.Summary = metrics.Summarize(rtts)
		zr.Peaks = peaks
		rep.Zones = append(rep.Zones, zr)
		series = append(series, ser)
	}
	// Individually watched hops: chart lines only, not part of the zone stats.
	var hopKeys []string
	for k := range byZone {
		if strings.HasPrefix(string(k), "hop:") {
			hopKeys = append(hopKeys, string(k))
		}
	}
	sort.Strings(hopKeys)
	for _, k := range hopKeys {
		ss := byZone[path.Zone(k)]
		ser := Series{Zone: path.Zone(k), T: make([]int64, len(ss)), RTTMs: make([]float64, len(ss))}
		for i, s := range ss {
			ser.T[i] = s.At.UnixMilli()
			ser.RTTMs[i] = -1
			if s.RTT >= 0 {
				ser.RTTMs[i] = float64(s.RTT) / float64(time.Millisecond)
			}
		}
		series = append(series, ser)
	}
	rep.Verdict = verdict(rep)
	return rep, series, samples, nil
}

func verdict(r Report) string {
	isp := r.OutageSeconds[string(path.ISPEdge)]
	lan := r.OutageSeconds[string(path.LAN)]
	wan := r.OutageSeconds[string(path.WAN)]
	switch {
	case isp == 0 && lan == 0 && wan == 0:
		return "Keine Ausfälle erkannt. Latenz/Jitter bitte anhand der Tabellen bewerten."
	case isp >= lan && isp >= wan:
		return "Ausfälle beginnen im Provider-Netz (ISP_EDGE): Das Heimnetz (LAN) war während der Ausfälle erreichbar, der erste Provider-Hop und das Ziel nicht. Die Störung liegt beim Anschluss/Provider."
	case lan >= wan:
		return "Ausfälle beginnen im lokalen Netz (LAN): Der eigene Router war nicht erreichbar. Bitte WLAN/Kabel/Router prüfen, bevor der Provider kontaktiert wird."
	default:
		return "Ausfälle nur am Ziel/Internet (WAN): Heimnetz und Provider-Zugang waren erreichbar. Die Störung liegt hinter dem Anschluss (Peering/Transit/Zielserver)."
	}
}
