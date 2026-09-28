package docsis

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Channel is one DOCSIS channel.
type Channel struct {
	ID         int     `json:"id"`
	Version    string  `json:"version"` // "3.0" or "3.1"
	Frequency  string  `json:"frequency"`
	Power      float64 `json:"power"` // dBmV
	SNR        float64 `json:"snr"`   // dB (MSE/MER), 0 = unknown (upstream)
	Modulation string  `json:"modulation"`
	Corr       int64   `json:"corr"`    // corrected codewords (downstream)
	NonCorr    int64   `json:"nonCorr"` // uncorrectable codewords (downstream)
	Status     string  `json:"status"`  // good | warning | critical
	Issue      string  `json:"issue,omitempty"`
}

// Snapshot is one reading of all channels, rated.
type Snapshot struct {
	T      int64     `json:"t"` // unix ms (set by the caller)
	Reason string    `json:"reason,omitempty"`
	DS     []Channel `json:"ds"`
	US     []Channel `json:"us"`

	Status     string   `json:"status"` // worst channel status (or error rating)
	Issues     []string `json:"issues"`
	DSPowerMin float64  `json:"dsPowerMin"`
	DSPowerMax float64  `json:"dsPowerMax"`
	SNRMin     float64  `json:"snrMin"`
	USPowerMin float64  `json:"usPowerMin"`
	USPowerMax float64  `json:"usPowerMax"`
	Corr       int64    `json:"corr"`    // totals over all downstream channels
	NonCorr    int64    `json:"nonCorr"` // (counters since the box's last restart)

	// Differences to the previous snapshot (filled by the caller).
	CorrDelta    int64 `json:"corrDelta"`
	NonCorrDelta int64 `json:"nonCorrDelta"`
}

// num reads a JSON number or numeric string ("4.2", "-37.5 dB").
func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		f := strings.Fields(strings.ReplaceAll(x, ",", "."))
		if len(f) > 0 {
			if n, err := strconv.ParseFloat(f[0], 64); err == nil {
				return n
			}
		}
	}
	return 0
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

// upstream DOCSIS 3.1 power is reported 6 dB too low by the box (DOCSight).
const us31PowerOffset = 6.0

// Parse reads the data.lua "docInfo" payload.
func Parse(raw json.RawMessage) (Snapshot, error) {
	var d struct {
		DS map[string][]map[string]any `json:"channelDs"`
		US map[string][]map[string]any `json:"channelUs"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return Snapshot{}, fmt.Errorf("DOCSIS-Daten unlesbar: %w", err)
	}
	var s Snapshot
	conv := func(list []map[string]any, version string, up bool) []Channel {
		var out []Channel
		for _, m := range list {
			c := Channel{
				ID: int(num(m["channelID"])), Version: version, Frequency: str(m["frequency"]),
				Power: num(m["powerLevel"]), Modulation: str(m["modulation"]),
				Corr: int64(num(m["corrErrors"])), NonCorr: int64(num(m["nonCorrErrors"])),
			}
			if c.Modulation == "" {
				c.Modulation = str(m["type"])
			}
			if up && version == "3.1" {
				c.Power += us31PowerOffset
			}
			if !up {
				if v, ok := m["mer"]; ok {
					c.SNR = math.Abs(num(v))
				} else if v, ok := m["mse"]; ok {
					c.SNR = math.Abs(num(v))
				}
			}
			out = append(out, c)
		}
		return out
	}
	s.DS = append(conv(d.DS["docsis30"], "3.0", false), conv(d.DS["docsis31"], "3.1", false)...)
	s.US = append(conv(d.US["docsis30"], "3.0", true), conv(d.US["docsis31"], "3.1", true)...)
	if len(s.DS) == 0 && len(s.US) == 0 {
		return s, fmt.Errorf("keine DOCSIS-Kanäle gefunden – ist das eine Kabel-FRITZ!Box?")
	}
	sort.Slice(s.DS, func(i, j int) bool { return s.DS[i].ID < s.DS[j].ID })
	sort.Slice(s.US, func(i, j int) bool { return s.US[i].ID < s.US[j].ID })
	s.rate()
	return s, nil
}

// Thresholds follow DOCSight's "VFKD" profile (Vodafone pNTP interface
// specification v1.06, CableLabs DOCSIS 3.1 PHY); dBmV / dB.
type band struct{ good, warn [2]float64 }

var dsPower = map[string]band{
	"64qam":   {[2]float64{-10, 7}, [2]float64{-12, 12}},
	"256qam":  {[2]float64{-4, 13}, [2]float64{-6, 15}},
	"1024qam": {[2]float64{-2, 15}, [2]float64{-4, 16}},
	"4096qam": {[2]float64{-2, 15}, [2]float64{-4, 16}},
	"ofdm":    {[2]float64{-12, 12}, [2]float64{-15, 15}},
}

var usPower = map[string]band{
	"sc_qam": {[2]float64{41.1, 47}, [2]float64{37.1, 51}},
	"ofdma":  {[2]float64{44.1, 47}, [2]float64{40.1, 48}},
}

type snrMin struct{ good, warn float64 }

var snr = map[string]snrMin{
	"64qam":   {27, 25},
	"256qam":  {33, 31},
	"1024qam": {39, 37},
	"4096qam": {40, 38},
	"ofdm":    {27, 25.5},
}

func modKey(c Channel, up bool) string {
	m := strings.ToLower(strings.ReplaceAll(c.Modulation, "-", ""))
	if up {
		if c.Version == "3.1" || strings.Contains(m, "ofdma") {
			return "ofdma"
		}
		return "sc_qam"
	}
	if c.Version == "3.1" {
		return "ofdm"
	}
	for _, k := range []string{"4096qam", "1024qam", "256qam", "64qam"} {
		if strings.Contains(m, k) {
			return k
		}
	}
	return "256qam"
}

var rank = map[string]int{"good": 0, "warning": 1, "critical": 2}

func worse(a, b string) string {
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func rateBand(v float64, b band) string {
	switch {
	case v >= b.good[0] && v <= b.good[1]:
		return "good"
	case v >= b.warn[0] && v <= b.warn[1]:
		return "warning"
	}
	return "critical"
}

func (s *Snapshot) rate() {
	s.Status = "good"
	first := true
	for i := range s.DS {
		c := &s.DS[i]
		k := modKey(*c, false)
		c.Status = rateBand(c.Power, dsPower[k])
		if c.Status != "good" {
			c.Issue = fmt.Sprintf("Pegel %.1f dBmV außerhalb %v", c.Power, dsPower[k].good)
		}
		if c.SNR > 0 {
			t := snr[k]
			st := "good"
			switch {
			case c.SNR < t.warn:
				st = "critical"
			case c.SNR < t.good:
				st = "warning"
			}
			if rank[st] > rank["good"] {
				c.Issue = strings.TrimPrefix(c.Issue+"; ", "; ") + fmt.Sprintf("SNR/MER %.1f dB unter %.1f dB", c.SNR, t.good)
			}
			c.Status = worse(c.Status, st)
		}
		if c.Status != "good" {
			s.Issues = append(s.Issues, fmt.Sprintf("Downstream-Kanal %d (%s, %s): %s", c.ID, c.Version, c.Modulation, c.Issue))
		}
		s.Status = worse(s.Status, c.Status)
		s.Corr += c.Corr
		s.NonCorr += c.NonCorr
		if first || c.Power < s.DSPowerMin {
			s.DSPowerMin = c.Power
		}
		if first || c.Power > s.DSPowerMax {
			s.DSPowerMax = c.Power
		}
		if c.SNR > 0 && (s.SNRMin == 0 || c.SNR < s.SNRMin) {
			s.SNRMin = c.SNR
		}
		first = false
	}
	first = true
	for i := range s.US {
		c := &s.US[i]
		k := modKey(*c, true)
		c.Status = rateBand(c.Power, usPower[k])
		if c.Status != "good" {
			c.Issue = fmt.Sprintf("Sendepegel %.1f dBmV außerhalb %v", c.Power, usPower[k].good)
			s.Issues = append(s.Issues, fmt.Sprintf("Upstream-Kanal %d (%s): %s", c.ID, c.Version, c.Issue))
		}
		s.Status = worse(s.Status, c.Status)
		if first || c.Power < s.USPowerMin {
			s.USPowerMin = c.Power
		}
		if first || c.Power > s.USPowerMax {
			s.USPowerMax = c.Power
		}
		first = false
	}
}

// ApplyDelta compares with the previous snapshot of the same box. New
// uncorrectable codewords are rated like DOCSight (share of all newly
// counted codewords with errors): ≥ 1 % warning, ≥ 3 % critical. A counter
// reset (box restart) yields no delta.
func (s *Snapshot) ApplyDelta(prev *Snapshot) {
	if prev == nil || s.Corr < prev.Corr || s.NonCorr < prev.NonCorr {
		return
	}
	s.CorrDelta = s.Corr - prev.Corr
	s.NonCorrDelta = s.NonCorr - prev.NonCorr
	if s.NonCorrDelta == 0 {
		return
	}
	pct := 100 * float64(s.NonCorrDelta) / float64(s.NonCorrDelta+s.CorrDelta)
	st := "warning"
	if pct >= 3 {
		st = "critical"
	}
	s.Status = worse(s.Status, st)
	s.Issues = append(s.Issues, fmt.Sprintf("%d neue nicht korrigierbare Fehler seit der letzten Abfrage (%.1f %% der fehlerhaften Codewörter)", s.NonCorrDelta, pct))
}
