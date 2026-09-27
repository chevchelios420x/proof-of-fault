package config

import (
	"testing"
	"time"
)

func TestIsSpike(t *testing.T) {
	s := Defaults()
	ms := time.Millisecond
	if !s.IsSpike("WAN", 50*ms, 15*ms) {
		t.Error("50 ms over usual 15 ms should be a spike")
	}
	if s.IsSpike("WAN", 25*ms, 15*ms) {
		t.Error("25 ms over usual 15 ms is no spike")
	}
	if !s.IsSpike("LAN", 120*ms, 0) {
		t.Error("absolute threshold should apply without history")
	}
}

func TestNormalize(t *testing.T) {
	s := Settings{Spikes: map[string]Spike{"LAN": {Factor: 0.5}}}.Normalize()
	if s.OutageAfter != 3 || s.Spikes["WAN"].Factor != 2 || s.Spikes["LAN"].Factor != 3 || s.Theme != "auto" {
		t.Fatalf("unexpected %+v", s)
	}
}

func TestZones(t *testing.T) {
	s := Settings{Zones: []ZoneDef{{ID: "LAN", Name: "Mein Netz", Role: "WAN"}, {ID: "vpn", Name: "VPN", Role: "WAN"}, {ID: "bad", Name: "X", Role: "??"}}}.Normalize()
	if len(s.Zones) != 6 || s.Zones[0].Name != "Mein Netz" || s.Role("LAN") != "LAN" || s.Role("vpn") != "WAN" || s.Role("bad") != "none" {
		t.Fatalf("unexpected zones %+v", s.Zones)
	}
}
