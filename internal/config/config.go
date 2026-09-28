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

// ZoneSounds selects which events of a zone play a sound.
type ZoneSounds struct {
	Spike  bool `json:"spike"`
	Loss   bool `json:"loss"`
	Outage bool `json:"outage"`
}

// Sounds configures acoustic signals.
type Sounds struct {
	Enabled     bool                  `json:"enabled"`
	Volume      int                   `json:"volume"`      // 0..100
	CooldownSec int                   `json:"cooldownSec"` // min. pause between equal signals
	RouteChange bool                  `json:"routeChange"`
	Zones       map[string]ZoneSounds `json:"zones"` // LAN, ISP_EDGE, WAN
}

// ZoneDef describes a zone. The built-in zones LAN, ISP_EDGE, WAN and "none"
// can be renamed and recolored; user zones additionally name the built-in
// Role whose evaluation rules they follow.
type ZoneDef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
	Role        string `json:"role"` // LAN | ISP_EDGE | WAN | none
	Builtin     bool   `json:"builtin"`
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
	Sounds           Sounds           `json:"sounds"`
	Zones            []ZoneDef        `json:"zones"`

	// Access technology of the internet connection: "dsl", "docsis" or
	// "fibre". Only DOCSIS adds checks so far (FRITZ!Box cable data).
	Access string `json:"access"`
	Fritz  Fritz  `json:"fritz"`
}

// Fritz holds the FRITZ!Box access for line diagnostics.
type Fritz struct {
	URL         string `json:"url"`                // e.g. http://192.168.178.1
	User        string `json:"user"`               // empty = last used user of the box
	Password    string `json:"password,omitempty"` // stored protected; never sent to the UI
	HasPassword bool   `json:"hasPassword"`        // UI: a password is stored
	IntervalSec int    `json:"intervalSec"`        // regular DOCSIS reading
}

func builtinZones() []ZoneDef {
	return []ZoneDef{
		{ID: "LAN", Name: "LAN – Heimnetz", Color: "#2a9d8f", Role: "LAN", Builtin: true, Description: "Eigenes Netz bis zum Router, der zum Anbieter führt."},
		{ID: "ISP_EDGE", Name: "ISP_EDGE – Anbieter", Color: "#e76f51", Role: "ISP_EDGE", Builtin: true, Description: "Anschluss und erste Knoten des Internetanbieters."},
		{ID: "WAN", Name: "WAN – Internet/Ziele", Color: "#3a5a8c", Role: "WAN", Builtin: true, Description: "Internet hinter dem Anbieter, Ziel und Ausweichziele."},
		{ID: "none", Name: "Nicht gewertet", Color: "#888888", Role: "none", Builtin: true, Description: "Wird gemessen und angezeigt, aber nicht ausgewertet."},
	}
}

// Role returns the evaluation role of a zone ID ("" for unknown IDs).
func (s Settings) Role(zone string) string {
	for _, z := range s.Zones {
		if z.ID == zone {
			return z.Role
		}
	}
	return ""
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
		Sounds: Sounds{
			Enabled: false, Volume: 60, CooldownSec: 10, RouteChange: true,
			Zones: map[string]ZoneSounds{
				"LAN":      {Outage: true},
				"ISP_EDGE": {Loss: true, Outage: true},
				"WAN":      {Loss: true, Outage: true},
			},
		},
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
	s.Zones = normalizeZones(s.Zones)
	if s.Sounds.Zones == nil {
		s.Sounds.Zones = d.Sounds.Zones
	}
	for z, v := range d.Sounds.Zones {
		if _, ok := s.Sounds.Zones[z]; !ok {
			s.Sounds.Zones[z] = v
		}
	}
	if s.Sounds.Volume < 0 || s.Sounds.Volume > 100 {
		s.Sounds.Volume = d.Sounds.Volume
	}
	if s.Sounds.CooldownSec < 0 {
		s.Sounds.CooldownSec = 0
	}
	if s.Access != "dsl" && s.Access != "docsis" && s.Access != "fibre" {
		s.Access = "dsl"
	}
	if s.Fritz.URL == "" {
		s.Fritz.URL = "http://192.168.178.1"
	}
	if s.Fritz.IntervalSec == 0 {
		s.Fritz.IntervalSec = 60
	}
	if s.Fritz.IntervalSec < 15 {
		s.Fritz.IntervalSec = 15
	}
	if s.Fritz.IntervalSec > 3600 {
		s.Fritz.IntervalSec = 3600
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

var validRole = map[string]bool{"LAN": true, "ISP_EDGE": true, "WAN": true, "none": true}

// normalizeZones keeps the built-in zones (first, in fixed order, with fixed
// role) and valid user zones with unique IDs.
func normalizeZones(in []ZoneDef) []ZoneDef {
	out := builtinZones()
	byID := map[string]ZoneDef{}
	for _, z := range in {
		byID[z.ID] = z
	}
	for i, b := range out {
		if z, ok := byID[b.ID]; ok {
			if z.Name != "" {
				out[i].Name = z.Name
			}
			if z.Color != "" {
				out[i].Color = z.Color
			}
			out[i].Description = z.Description
		}
	}
	seen := map[string]bool{"LAN": true, "ISP_EDGE": true, "WAN": true, "none": true}
	for _, z := range in {
		if z.ID == "" || seen[z.ID] || z.Name == "" {
			continue
		}
		if !validRole[z.Role] {
			z.Role = "none"
		}
		if z.Color == "" {
			z.Color = "#888888"
		}
		z.Builtin = false
		seen[z.ID] = true
		out = append(out, z)
	}
	return out
}
