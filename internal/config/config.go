// Package config holds the user-adjustable settings (thresholds, behaviour,
// appearance). They are stored as JSON in the database.
package config

import "time"

// Spike defines when a latency value counts as spike in one zone: it must
// exceed Factor × the usual value (median of the last minute) and be at
// least MinDeltaMs above it – or exceed AbsoluteMs (0 = off).
type Spike struct {
	Factor     float64 `json:"factor"`
	MinDeltaMs float64 `json:"minDeltaMs"`
	AbsoluteMs float64 `json:"absoluteMs"`
}

// Settings are all user settings.
type Settings struct {
	Spikes           map[string]Spike `json:"spikes"`           // per zone: LAN, ISP_EDGE, WAN
	OutageAfter      int              `json:"outageAfter"`      // consecutive lost seconds = outage
	ProbeTimeoutSec  float64          `json:"probeTimeoutSec"`  // no answer within → loss
	RediscoverMin    int              `json:"rediscoverMin"`    // route check interval
	IncidentPreSec   int              `json:"incidentPreSec"`   // context before an incident
	IncidentPostSec  int              `json:"incidentPostSec"`  // clean seconds ending an incident
	HighLatencyMs    float64          `json:"highLatencyMs"`    // report column "> X ms"
	PreventSleep     bool             `json:"preventSleep"`     // keep the PC awake while measuring
	AutoStart        bool             `json:"autoStart"`        // resume last target on app start
	LastTarget       string           `json:"lastTarget"`       // set automatically
	RetentionDays    int              `json:"retentionDays"`    // delete older sessions (0 = keep)
	Theme            string           `json:"theme"`            // auto | light | dark
	DefaultWindowMin int              `json:"defaultWindowMin"` // live chart window
}

// Defaults returns the factory settings.
func Defaults() Settings {
	return Settings{
		Spikes: map[string]Spike{
			"LAN":      {Factor: 3, MinDeltaMs: 15, AbsoluteMs: 100},
			"ISP_EDGE": {Factor: 2, MinDeltaMs: 20, AbsoluteMs: 150},
			"WAN":      {Factor: 2, MinDeltaMs: 20, AbsoluteMs: 250},
		},
		OutageAfter:      3,
		ProbeTimeoutSec:  3,
		RediscoverMin:    5,
		IncidentPreSec:   10,
		IncidentPostSec:  5,
		HighLatencyMs:    100,
		PreventSleep:     true,
		AutoStart:        false,
		RetentionDays:    0,
		Theme:            "auto",
		DefaultWindowMin: 30,
	}
}

// Normalize fills missing values with defaults and clamps ranges.
func (s Settings) Normalize() Settings {
	d := Defaults()
	if s.Spikes == nil {
		s.Spikes = map[string]Spike{}
	}
	for z, def := range d.Spikes {
		v, ok := s.Spikes[z]
		if !ok || v.Factor < 1 {
			v.Factor = def.Factor
		}
		if !ok {
			v = def
		}
		v.MinDeltaMs = clampF(v.MinDeltaMs, 0, 10000)
		v.AbsoluteMs = clampF(v.AbsoluteMs, 0, 100000)
		s.Spikes[z] = v
	}
	s.OutageAfter = clampI(s.OutageAfter, 1, 600, d.OutageAfter)
	s.ProbeTimeoutSec = clampF(s.ProbeTimeoutSec, 0.5, 10)
	if s.ProbeTimeoutSec == 0 {
		s.ProbeTimeoutSec = d.ProbeTimeoutSec
	}
	s.RediscoverMin = clampI(s.RediscoverMin, 1, 1440, d.RediscoverMin)
	s.IncidentPreSec = clampI(s.IncidentPreSec, 0, 120, d.IncidentPreSec)
	s.IncidentPostSec = clampI(s.IncidentPostSec, 1, 120, d.IncidentPostSec)
	if s.HighLatencyMs <= 0 {
		s.HighLatencyMs = d.HighLatencyMs
	}
	if s.RetentionDays < 0 {
		s.RetentionDays = 0
	}
	if s.Theme != "light" && s.Theme != "dark" {
		s.Theme = "auto"
	}
	if s.DefaultWindowMin < 0 {
		s.DefaultWindowMin = d.DefaultWindowMin
	}
	return s
}

// ProbeTimeout returns the probe timeout as duration.
func (s Settings) ProbeTimeout() time.Duration {
	return time.Duration(s.ProbeTimeoutSec * float64(time.Second))
}

// IsSpike reports whether rtt is a spike in zone given the usual value.
func (s Settings) IsSpike(zone string, rtt, usual time.Duration) bool {
	sp, ok := s.Spikes[zone]
	if !ok {
		sp = s.Spikes["WAN"]
	}
	ms := float64(rtt) / float64(time.Millisecond)
	if sp.AbsoluteMs > 0 && ms > sp.AbsoluteMs {
		return true
	}
	if usual <= 0 {
		return false
	}
	u := float64(usual) / float64(time.Millisecond)
	return ms > sp.Factor*u && ms-u > sp.MinDeltaMs
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampI(v, lo, hi, def int) int {
	if v == 0 {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
