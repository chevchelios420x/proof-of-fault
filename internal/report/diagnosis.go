package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chevchelios420x/proof-of-fault/internal/monitor"

	"github.com/chevchelios420x/proof-of-fault/internal/path"
	"github.com/chevchelios420x/proof-of-fault/internal/store"
)

// ZoneFindings sums up the relevant events attributed to one zone.
type ZoneFindings struct {
	Zone          path.Zone `json:"zone"`
	LossEvents    int       `json:"lossEvents"`
	LostSeconds   int       `json:"lostSeconds"`
	SpikeEvents   int       `json:"spikeEvents"`
	SpikeSeconds  int       `json:"spikeSeconds"`
	Outages       int       `json:"outages"`
	OutageSeconds int       `json:"outageSeconds"`
	Score         float64   `json:"score"`
}

// Diagnosis is the plain-language conclusion for non-experts.
type Diagnosis struct {
	Level        string         `json:"level"` // none | lan | isp | wan | unclear
	Headline     string         `json:"headline"`
	Confidence   string         `json:"confidence"` // hoch | mittel | gering
	Explanation  []string       `json:"explanation"`
	Advice       []string       `json:"advice"`
	Zones        []ZoneFindings `json:"zones"`
	Harmless     int            `json:"harmless"`
	Devices      map[string]int `json:"devices"` // seconds without answer per user-defined point
	RouteChanges int            `json:"routeChanges"`
	TopOrigin    string         `json:"topOrigin"`
	Incidents    int            `json:"incidents"`
	Classes      []ClassSummary `json:"classes"` // most frequent hop where problems start
}

// Diagnose derives the diagnosis from the event log.
func Diagnose(events []store.Event) Diagnosis {
	byZone := map[path.Zone]*ZoneFindings{}
	for _, z := range path.Zones {
		byZone[z] = &ZoneFindings{Zone: z}
	}
	d := Diagnosis{}
	origins := map[string]int{}
	originLabel := map[string]string{}
	for _, e := range events {
		f := byZone[path.Zone(e.Zone)]
		switch e.Kind {
		case "loss_hop":
			d.Harmless++
			continue
		case "path_change":
			d.RouteChanges++
			continue
		case "loss_device":
			if d.Devices == nil {
				d.Devices = map[string]int{}
			}
			d.Devices[strings.TrimPrefix(e.Title, "Keine Antwort von ")] += e.Count
			continue
		}
		if f == nil {
			continue
		}
		switch e.Kind {
		case "loss":
			f.LossEvents++
			f.LostSeconds += e.Count
		case "spike":
			f.SpikeEvents++
			f.SpikeSeconds += e.Count
		case "outage_start":
			f.Outages++
		case "outage_end":
			f.OutageSeconds += e.Count
		default:
			continue
		}
		if e.Addr != "" && (e.Kind == "loss" || e.Kind == "spike" || e.Kind == "outage_start") {
			origins[e.Addr]++
			if e.TTL > 0 {
				originLabel[e.Addr] = fmt.Sprintf("Hop %d (%s, Zone %s)", e.TTL, e.Addr, e.Zone)
			} else {
				originLabel[e.Addr] = fmt.Sprintf("dem Ziel %s", e.Addr)
			}
		}
	}

	var total float64
	for _, z := range path.Zones {
		f := byZone[z]
		// Outages weigh fully, lost seconds fully, spikes less (they hurt,
		// but are no interruption).
		f.Score = float64(f.OutageSeconds) + float64(f.LostSeconds) + 0.3*float64(f.SpikeSeconds)
		total += f.Score
		d.Zones = append(d.Zones, *f)
	}
	best := 0
	for i, f := range d.Zones {
		if f.Score > d.Zones[best].Score {
			best = i
		}
	}
	if len(origins) > 0 {
		keys := make([]string, 0, len(origins))
		for k := range origins {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return origins[keys[i]] > origins[keys[j]] })
		d.TopOrigin = originLabel[keys[0]]
	}

	events_ := 0
	for _, f := range d.Zones {
		events_ += f.LossEvents + f.SpikeEvents + f.Outages
	}
	if total == 0 {
		d.Level, d.Confidence = "none", "hoch"
		d.Headline = "Keine Störungen festgestellt."
		d.Explanation = []string{"Während der Messung gab es weder Paketverluste noch auffällige Latenzspitzen auf dem Weg zum Ziel."}
		if d.Harmless > 0 {
			d.Explanation = append(d.Explanation, fmt.Sprintf("%d harmlose Einzelverluste an Zwischen-Hops (ICMP-Drosselung) wurden ignoriert – sie beeinträchtigen die Verbindung nicht.", d.Harmless))
		}
		d.Explanation = append(d.Explanation, deviceLines(d.Devices)...)
		d.Advice = []string{"Messung länger laufen lassen, bis die Störung wieder auftritt – das Protokoll hält dann genau fest, wo sie beginnt."}
		return d
	}

	top := d.Zones[best]
	share := top.Score / total
	switch {
	case share >= 0.75 && events_ >= 3:
		d.Confidence = "hoch"
	case share >= 0.5:
		d.Confidence = "mittel"
	default:
		d.Confidence = "gering"
	}
	if share < 0.5 {
		d.Level = "unclear"
		d.Headline = "Störungen in mehreren Bereichen – keine eindeutige Ursache."
	} else {
		switch top.Zone {
		case path.LAN:
			d.Level = "lan"
			d.Headline = "Das Problem liegt sehr wahrscheinlich in Ihrem eigenen Heimnetz (Router, WLAN oder Kabel)."
		case path.ISPEdge:
			d.Level = "isp"
			d.Headline = "Das Problem liegt sehr wahrscheinlich beim Internetanbieter (Ihr Anschluss bzw. das Netz des Anbieters)."
		case path.WAN:
			d.Level = "wan"
			d.Headline = "Ihr Anschluss arbeitet sauber – die Störungen entstehen weiter hinten im Internet (Zielserver oder Übergänge zu anderen Netzen)."
		}
	}

	for _, f := range d.Zones {
		if f.Score == 0 {
			d.Explanation = append(d.Explanation, fmt.Sprintf("%s: keine Auffälligkeiten.", zoneName(f.Zone)))
			continue
		}
		d.Explanation = append(d.Explanation, fmt.Sprintf("%s: %d Verlust-Ereignis(se) mit %d s Verlust, %d Latenzspitze(n) mit %d s, %d Ausfall/Ausfälle mit %d s Dauer.",
			zoneName(f.Zone), f.LossEvents, f.LostSeconds, f.SpikeEvents, f.SpikeSeconds, f.Outages, f.OutageSeconds))
	}
	if d.TopOrigin != "" {
		d.Explanation = append(d.Explanation, "Die Störungen beginnen am häufigsten ab "+d.TopOrigin+": Davor war die Verbindung jeweils in Ordnung, ab dort bis zum Ziel nicht.")
	}
	if d.Harmless > 0 {
		d.Explanation = append(d.Explanation, fmt.Sprintf("%d Einzelverluste an Zwischen-Hops wurden als harmlos erkannt (ICMP-Drosselung, das Ziel antwortete) und nicht gewertet.", d.Harmless))
	}
	d.Explanation = append(d.Explanation, deviceLines(d.Devices)...)
	if d.RouteChanges > 0 {
		d.Explanation = append(d.Explanation, fmt.Sprintf("Die Route hat sich %d-mal geändert (Details im Ereignisprotokoll). Häufige Routenwechsel können auf Instabilität im Anbieter-Netz hindeuten.", d.RouteChanges))
	}

	switch d.Level {
	case "lan":
		d.Advice = []string{
			"Den PC per LAN-Kabel direkt am Router anschließen und erneut messen (WLAN ist die häufigste Ursache).",
			"Router neu starten, Kabel und Netzteil prüfen; bei Verstärkern/Mesh: direkt am Hauptrouter messen.",
			"Der Internetanbieter ist für diesen Bereich nicht verantwortlich.",
		}
	case "isp":
		d.Advice = []string{
			"Störung beim Internetanbieter melden und den HTML-Bericht (oder als PDF gedruckt) mitschicken.",
			"Konkrete Zeitpunkte aus dem Ereignisprotokoll nennen und darauf hinweisen, dass der eigene Router zu diesen Zeiten erreichbar war.",
			"Für einen belastbaren Nachweis per LAN-Kabel messen und die Messung über mehrere Tage laufen lassen.",
		}
	case "wan":
		d.Advice = []string{
			"Zum Vergleich ein anderes Ziel messen (z. B. 1.1.1.1 und 8.8.8.8). Ist nur ein Ziel betroffen, liegt es am Zielbetreiber.",
			"Sind alle Ziele betroffen, kann es an den Übergängen des Anbieters ins Internet liegen (Peering) – dann ebenfalls den Anbieter informieren.",
		}
	default:
		d.Advice = []string{
			"Messung länger laufen lassen, damit sich ein Muster ergibt.",
			"Per LAN-Kabel messen, um das WLAN als Ursache auszuschließen.",
		}
	}
	return d
}

func zoneName(z path.Zone) string {
	switch z {
	case path.LAN:
		return "Heimnetz (LAN)"
	case path.ISPEdge:
		return "Anschluss/Anbieter (ISP_EDGE)"
	case path.WAN:
		return "Internet/Ziel (WAN)"
	}
	return string(z)
}

func deviceLines(dev map[string]int) []string {
	keys := make([]string, 0, len(dev))
	for k := range dev {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		out = append(out, fmt.Sprintf("Manueller Messpunkt %s: insgesamt %d s ohne Antwort (Details im Ereignisprotokoll; fließt nicht in die Bewertung des Internetwegs ein).", k, dev[k]))
	}
	return out
}

// ClassSummary sums up the incidents of one class.
type ClassSummary struct {
	Class     string `json:"class"`
	Name      string `json:"name"`
	Count     int    `json:"count"`
	Seconds   int    `json:"seconds"`
	Longest   int    `json:"longest"`
	LongestAt int64  `json:"longestAt"`
}

// DiagnoseAll combines event log and incidents. When incidents exist they
// decide the verdict: they compare every measuring point second by second
// (including backup targets) and are the most reliable basis.
func DiagnoseAll(events []store.Event, incidents []store.Incident) Diagnosis {
	d := Diagnose(events)
	d.Incidents = len(incidents)
	if len(incidents) == 0 {
		return d
	}
	by := map[string]*ClassSummary{}
	total := 0
	for _, in := range incidents {
		c := by[in.Class]
		if c == nil {
			c = &ClassSummary{Class: in.Class, Name: monitor.ClassName(in.Class)}
			by[in.Class] = c
		}
		c.Count++
		c.Seconds += in.Seconds
		if in.Seconds > c.Longest {
			c.Longest, c.LongestAt = in.Seconds, in.T
		}
		total += in.Seconds
	}
	for _, cl := range []string{monitor.ClassLAN, monitor.ClassISP, monitor.ClassISPCore, monitor.ClassTarget, monitor.ClassAlt, monitor.ClassDevice} {
		if c := by[cl]; c != nil {
			d.Classes = append(d.Classes, *c)
		}
	}
	// Provider access and provider core both point to the provider.
	group := map[string]int{}
	for _, c := range d.Classes {
		switch c.Class {
		case monitor.ClassISP, monitor.ClassISPCore:
			group["isp"] += c.Seconds
		default:
			group[c.Class] += c.Seconds
		}
	}
	best := ""
	for g, s := range group {
		if best == "" || s > group[best] {
			best = g
		}
	}
	share := float64(group[best]) / float64(total)
	switch {
	case share >= 0.75 && len(incidents) >= 3:
		d.Confidence = "hoch"
	case share >= 0.5:
		d.Confidence = "mittel"
	default:
		d.Confidence = "gering"
	}
	lines := []string{fmt.Sprintf("%d Störung(en) mit zusammen %s. Jede Störung wurde Sekunde für Sekunde über alle Messpunkte (Route und manuelle Hosts) ausgewertet:", len(incidents), fmtSec(total))}
	for _, c := range d.Classes {
		lines = append(lines, fmt.Sprintf("%d× %s, zusammen %s (längste: %s am %s).", c.Count, c.Name, fmtSec(c.Seconds), fmtSec(c.Longest),
			time.UnixMilli(c.LongestAt).Format("02.01. 15:04:05")))
	}
	d.Explanation = append(lines, d.Explanation...)

	if share < 0.5 {
		d.Level = "unclear"
		d.Headline = "Störungen unterschiedlicher Art – keine eindeutige Ursache. Details in der Störungsliste."
		return d
	}
	ispCore := by[monitor.ClassISPCore] != nil && by[monitor.ClassISP] == nil
	switch best {
	case monitor.ClassLAN:
		d.Level = "lan"
		d.Headline = "Das Problem liegt in Ihrem eigenen Heimnetz: Während der Störungen war schon Ihr Router nicht erreichbar."
	case "isp":
		d.Level = "isp"
		d.Headline = "Das Problem liegt beim Internetanbieter: Ihr Router war erreichbar, aber ab dem Anbieter-Zugang waren alle Ziele weg."
		if ispCore {
			d.Headline = "Das Problem liegt im Netz des Internetanbieters: Router und erster Anbieter-Knoten antworteten, trotzdem waren alle Ziele im Internet gleichzeitig weg."
		}
	case monitor.ClassTarget:
		d.Level = "wan"
		d.Headline = "Ihr Anschluss funktioniert: Nur das Hauptziel war zeitweise weg, die Ausweichziele waren gleichzeitig erreichbar. Das Problem liegt beim Zielserver bzw. auf dem Weg dorthin."
		d.Advice = []string{"Den Betreiber des Hauptziels informieren; der Internetanbieter ist hier sehr wahrscheinlich nicht verantwortlich."}
	case monitor.ClassAlt:
		d.Level = "wan"
		d.Headline = "Ihr Anschluss funktioniert: Nur einzelne Ausweichziele waren zeitweise weg. Das betrifft diese Server, nicht Ihre Verbindung."
		d.Advice = []string{"Die betroffenen Server bzw. deren Betreiber prüfen (z. B. Firewall, Überlastung, VPN-Tunnel)."}
	case monitor.ClassDevice:
		d.Level = "lan"
		d.Headline = "Nur einzelne Geräte im Heimnetz waren zeitweise weg – der Internetweg war nicht betroffen."
		d.Advice = []string{"Die betroffenen Geräte prüfen (WLAN-Empfang, Energiesparmodus, Stromversorgung)."}
	}
	return d
}

func fmtSec(s int) string {
	return (time.Duration(s) * time.Second).String()
}
