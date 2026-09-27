package report

import (
	"bufio"
	"fmt"
	"html/template"
	"io"
	"math"
	"strings"
	"time"

	"github.com/chevchelios420x/proof-of-fault/internal/config"
	"github.com/chevchelios420x/proof-of-fault/internal/monitor"
	"github.com/chevchelios420x/proof-of-fault/internal/path"
	"github.com/chevchelios420x/proof-of-fault/internal/store"
)

func csvLine(s store.Sample) string {
	rtt := ""
	if s.RTT >= 0 {
		rtt = fmt.Sprintf("%.3f", float64(s.RTT)/float64(time.Millisecond))
	}
	lost := 0
	if s.RTT < 0 {
		lost = 1
	}
	return fmt.Sprintf("%s;%d;%s;%s;%d\n", s.At.Format(time.RFC3339Nano), s.At.UnixMilli(), s.Zone, rtt, lost)
}

// WriteCSV writes the raw samples (semicolon separated for German Excel).
func WriteCSV(w io.Writer, samples []store.Sample) error {
	bw := bufio.NewWriter(w)
	bw.WriteString("zeit;unix_ms;zone;rtt_ms;verlust\n")
	for _, s := range samples {
		bw.WriteString(csvLine(s))
	}
	return bw.Flush()
}

var zoneColor = map[path.Zone]string{path.LAN: "#2a9d8f", path.ISPEdge: "#e76f51", path.WAN: "#264653"}

// chartSVG renders a latency chart per zone; losses are red ticks.
func chartSVG(series []Series) template.HTML {
	const w, h, pad = 1000.0, 220.0, 40.0
	var b strings.Builder
	for _, s := range series {
		if len(s.T) == 0 || strings.HasPrefix(string(s.Zone), "hop:") || strings.HasPrefix(string(s.Zone), "dev:") {
			continue
		}
		t0, t1 := float64(s.T[0]), float64(s.T[len(s.T)-1])
		if t1 == t0 {
			t1 = t0 + 1
		}
		maxRTT := 1.0
		for _, v := range s.RTTMs {
			maxRTT = math.Max(maxRTT, v)
		}
		x := func(t int64) float64 { return pad + (float64(t)-t0)/(t1-t0)*(w-2*pad) }
		y := func(v float64) float64 { return h - pad - v/maxRTT*(h-2*pad) }

		fmt.Fprintf(&b, `<h3>%s</h3><svg viewBox="0 0 %.0f %.0f" class="chart">`, s.Zone, w, h)
		fmt.Fprintf(&b, `<line x1="%.0f" y1="%.0f" x2="%.0f" y2="%.0f" stroke="#999"/>`, pad, h-pad, w-pad, h-pad)
		fmt.Fprintf(&b, `<text x="2" y="%.0f" font-size="11">%.0f ms</text><text x="2" y="%.0f" font-size="11">0</text>`, pad, maxRTT, h-pad)
		fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" font-size="11">%s</text>`, pad, h-8, time.UnixMilli(s.T[0]).Format("02.01. 15:04:05"))
		fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" font-size="11" text-anchor="end">%s</text>`, w-pad, h-8, time.UnixMilli(s.T[len(s.T)-1]).Format("02.01. 15:04:05"))

		// Downsample to at most ~2000 points: keep max per bucket so spikes survive.
		step := len(s.T)/2000 + 1
		var pts []string
		for i := 0; i < len(s.T); i += step {
			end := min(i+step, len(s.T))
			best, lost := -1.0, false
			for j := i; j < end; j++ {
				if s.RTTMs[j] < 0 {
					lost = true
				} else if s.RTTMs[j] > best {
					best = s.RTTMs[j]
				}
			}
			if lost {
				fmt.Fprintf(&b, `<line x1="%.1f" y1="%.0f" x2="%.1f" y2="%.0f" stroke="#d62828" stroke-width="1.5"/>`, x(s.T[i]), pad, x(s.T[i]), h-pad)
			}
			if best >= 0 {
				pts = append(pts, fmt.Sprintf("%.1f,%.1f", x(s.T[i]), y(best)))
			}
		}
		fmt.Fprintf(&b, `<polyline fill="none" stroke="%s" stroke-width="1" points="%s"/></svg>`, zoneColor[s.Zone], strings.Join(pts, " "))
	}
	return template.HTML(b.String())
}

var funcs = template.FuncMap{
	"ts": func(ms int64) string {
		if ms == 0 {
			return "läuft"
		}
		return time.UnixMilli(ms).Format("02.01.2006 15:04:05")
	},
	"dur":    func(sec float64) string { return (time.Duration(sec) * time.Second).Round(time.Second).String() },
	"f1":     func(v float64) string { return fmt.Sprintf("%.1f", v) },
	"kind":   kindText,
	"cls":    monitor.ClassName,
	"inc":    func(i int) int { return i + 1 },
	"matrix": matrixHTML,
	"f2":     func(v float64) string { return fmt.Sprintf("%.2f", v) },
}

var htmlTpl = template.Must(template.New("r").Funcs(funcs).Parse(`<!doctype html>
<html lang="de"><head><meta charset="utf-8"><title>proof-of-fault Messprotokoll #{{.R.Session.ID}}</title>
<style>
body{font-family:Segoe UI,Arial,sans-serif;margin:24px;color:#222;max-width:1100px}
table{border-collapse:collapse;margin:8px 0 20px;width:100%}
th,td{border:1px solid #ccc;padding:4px 8px;text-align:right;font-size:13px}
th:first-child,td:first-child{text-align:left}
th{background:#f1f1f1}
.verdict{background:#fff4e5;border-left:6px solid #e76f51;padding:10px 16px}
.lvl-none{background:#eef8f1;border-color:#2a9d8f}.lvl-lan{border-color:#2a9d8f}.lvl-wan{border-color:#3a5a8c}
.log td{text-align:left;vertical-align:top}.log td:nth-child(1),.log td:nth-child(2){white-space:nowrap}
.c{display:inline-block;width:10px;height:10px;vertical-align:middle;border-radius:2px}
.c-ok{background:#52b788}.c-s{background:#f4a261}.c-x{background:#d62828}.c-n{background:#ddd}
table.mx{width:auto;border-collapse:separate;border-spacing:1px}table.mx td{padding:0;border:none;width:9px;height:14px}
table.mx td.lbl{padding:0 8px 0 0;width:auto;white-space:nowrap;text-align:left;font-size:12px}
table.mx td.pre{opacity:.45}table.mx td.grp{font-size:11px;font-weight:700;text-transform:uppercase;color:#666;padding:6px 0 2px 6px;text-align:left;width:auto}.inc{page-break-inside:avoid;margin-bottom:18px}.mxwrap{overflow-x:auto}
.inclist td{text-align:left}
.sev-crit{background:#fde2e2}.sev-warn{background:#fff6e0}.sev-info td{color:#666}.sev-ok{background:#e9f7ef}
.chart{width:100%;height:auto;border:1px solid #eee}
small{color:#666}
@media print{body{margin:0}.chart{page-break-inside:avoid}}
</style></head><body>
<h1>Messprotokoll Internetverbindung</h1>
<p><b>Ziel:</b> {{.R.Session.Target}} ({{.R.Session.TargetIP}}) &nbsp; <b>Messung:</b> {{ts .R.Session.StartedAt}} – {{ts .R.Session.EndedAt}} ({{dur .R.DurationSec}})<br>
<b>Messrechner:</b> <small>{{.R.Session.HostInfo}}</small> &nbsp; <b>Erstellt:</b> {{ts .R.GeneratedAt}}</p>
<div class="verdict lvl-{{.R.Diagnosis.Level}}">
<h2 style="margin-top:0">Diagnose: {{.R.Diagnosis.Headline}}</h2>
<p><b>Sicherheit der Einschätzung:</b> {{.R.Diagnosis.Confidence}}</p>
<ul>{{range .R.Diagnosis.Explanation}}<li>{{.}}</li>{{end}}</ul>
<p><b>Empfehlung:</b></p>
<ul>{{range .R.Diagnosis.Advice}}<li>{{.}}</li>{{end}}</ul>
</div>

<h2>Kennzahlen je Zone</h2>
<table><tr><th>Zone</th><th>Probes</th><th>Verlust %</th><th>Min</th><th>Ø</th><th>P50</th><th>P95</th><th>P99</th><th>Max</th><th>Jitter (RFC 3550)</th><th>&gt;{{printf "%.0f" .R.HighLatencyMs}} ms</th><th>Ausfallzeit</th></tr>
{{range .R.Zones}}<tr><td>{{.Zone}}</td><td>{{.Summary.Sent}}</td><td>{{f2 .Summary.LossPct}}</td><td>{{f1 .Summary.MinMs}}</td><td>{{f1 .Summary.AvgMs}}</td><td>{{f1 .Summary.P50Ms}}</td><td>{{f1 .Summary.P95Ms}}</td><td>{{f1 .Summary.P99Ms}}</td><td>{{f1 .Summary.MaxMs}}</td><td>{{f2 .Summary.JitterMs}}</td><td>{{.SpikesOver100}}</td><td>{{dur (index $.R.OutageSeconds (printf "%s" .Zone))}}</td></tr>
{{end}}</table>
<small>Alle Latenzen in ms (Round-Trip). Ausfall = mindestens 3 aufeinanderfolgende Verluste; zugeordnet wird die erste Zone, ab der alle weiteren Zonen bis zum Ziel nicht antworten. Verluste nur an einem Zwischen-Hop bei erreichbarem Ziel (ICMP-Ratenbegrenzung) werden nicht als Ausfall gewertet.</small>

<h2>Ausfälle</h2>
{{if .R.Outages}}<table><tr><th>Zone (Ursache)</th><th>Beginn</th><th>Ende</th><th>Dauer (s)</th></tr>
{{range .R.Outages}}<tr><td>{{.Zone}}</td><td>{{ts .Start}}</td><td>{{ts .End}}</td><td>{{f1 .Seconds}}</td></tr>{{end}}</table>
{{else}}<p>Keine.</p>{{end}}

<h2>Störungen ({{len .R.Incidents}})</h2>
{{if .R.Incidents}}
<small>Eine Störung beginnt, sobald das Ziel, ein Ausweichziel oder ein manuell eingetragenes Gerät nicht antwortet (Verluste nur an einem Zwischen-Hop zählen nicht). Die Matrix zeigt jede Sekunde für jeden Messpunkt: <span class="c c-ok"></span> antwortet, <span class="c c-s"></span> deutlich langsamer als normal, <span class="c c-x"></span> keine Antwort, <span class="c c-n"></span> nicht gemessen. Blasse Spalten: 10 s davor bzw. 5 s danach (zum Vergleich).</small>
<table class="inclist"><tr><th>#</th><th>Beginn</th><th>Dauer</th><th>Art</th><th>Beschreibung</th></tr>
{{range $i, $in := .R.Incidents}}<tr><td><a href="#inc{{$in.ID}}">{{inc $i}}</a></td><td>{{ts $in.T}}</td><td>{{$in.Seconds}} s</td><td>{{cls $in.Class}}</td><td>{{$in.Title}}</td></tr>
{{end}}</table>
{{range $i, $in := .R.Incidents}}{{if lt $i 200}}
<div class="inc" id="inc{{$in.ID}}"><h3>Störung {{inc $i}}: {{$in.Title}}</h3>
<p><b>{{ts $in.T}} – {{ts $in.End}}</b> · {{$in.Detail}}</p>
{{matrix $in $.R.ZoneDefs}}</div>
{{end}}{{end}}
{{if gt (len .R.Incidents) 200}}<p><small>Matrix nur für die ersten 200 Störungen; alle sind in der Liste oben und im Ereignis-Export enthalten.</small></p>{{end}}
{{else}}<p>Keine.</p>{{end}}

<h2>Ereignisprotokoll</h2>
<small>Jede Messsekunde wird über alle Messpunkte gemeinsam ausgewertet: Ein Verlust oder eine Latenzspitze wird dem ersten Hop zugeordnet, ab dem alle weiteren Messpunkte bis zum Ziel betroffen waren. Verluste nur an einem Zwischen-Hop (das Ziel antwortete) sind als harmlos markiert.</small>
<table class="log"><tr><th>Beginn</th><th>Ende</th><th>Art</th><th>Zone</th><th>Ereignis</th><th>Details</th></tr>
{{range .R.Events}}<tr class="sev-{{.Severity}}"><td>{{ts .T}}</td><td>{{if .End}}{{ts .End}}{{end}}</td><td>{{kind .Kind}}</td><td>{{.Zone}}</td><td>{{.Title}}</td><td>{{.Detail}}</td></tr>
{{end}}</table>

<h2>Latenzverlauf</h2>
<small>Linie = Latenz (Maximum je Zeitabschnitt), rote Striche = Paketverlust.</small>
{{.Chart}}

<h2>Spitzenwerte (Top 10 je Zone)</h2>
<table><tr><th>Zone</th><th>Zeitpunkt</th><th>RTT (ms)</th></tr>
{{range .R.Zones}}{{$z := .Zone}}{{range .Peaks}}<tr><td>{{$z}}</td><td>{{ts .T}}</td><td>{{f1 .RTTMs}}</td></tr>{{end}}{{end}}</table>

<h2>Route (Stand zuletzt, {{.R.PathChanges}} Routenwechsel)</h2>
<table><tr><th>TTL</th><th>Adresse</th><th>Name</th><th>Zone</th><th>RTT (ms)</th></tr>
{{range .R.Hops}}<tr><td>{{.TTL}}</td><td>{{if .Responsive}}{{.Addr}}{{else}}* (antwortet nicht auf ICMP, kein Fehler){{end}}</td><td>{{.Name}}</td><td>{{.Zone}}</td><td>{{f1 .RTTMs}}</td></tr>{{end}}</table>

<p><small>SHA-256 der Rohdaten (CSV-Zeilen): {{.R.DataSHA256}}<br>Erstellt mit proof-of-fault.</small></p>
</body></html>`))

// WriteHTML writes a self-contained HTML report.
func WriteHTML(w io.Writer, r Report, series []Series) error {
	return htmlTpl.Execute(w, struct {
		R     Report
		Chart template.HTML
	}{r, chartSVG(series)})
}

func kindText(k string) string {
	switch k {
	case "session":
		return "Messung"
	case "path", "path_change":
		return "Route"
	case "zones":
		return "Zonen"
	case "loss":
		return "Verlust"
	case "loss_hop":
		return "Verlust (harmlos)"
	case "loss_device":
		return "Gerät"
	case "spike":
		return "Latenzspitze"
	case "outage_start":
		return "Ausfall"
	case "outage_end":
		return "Ausfall Ende"
	}
	return k
}

// WriteEventsCSV writes the event log (semicolon separated).
func WriteEventsCSV(w io.Writer, events []store.Event) error {
	bw := bufio.NewWriter(w)
	bw.WriteString("beginn;ende;art;schwere;zone;ttl;adresse;ereignis;details;sekunden;wert_ms\n")
	clean := func(s string) string { return strings.NewReplacer(";", ",", "\n", " ").Replace(s) }
	for _, e := range events {
		end := ""
		if e.End > 0 {
			end = time.UnixMilli(e.End).Format(time.RFC3339)
		}
		fmt.Fprintf(bw, "%s;%s;%s;%s;%s;%d;%s;%s;%s;%d;%.1f\n", time.UnixMilli(e.T).Format(time.RFC3339), end,
			kindText(e.Kind), e.Severity, e.Zone, e.TTL, e.Addr, clean(e.Title), clean(e.Detail), e.Count, e.ValueMs)
	}
	return bw.Flush()
}

// matrixHTML renders the per-second state matrix of an incident.
func matrixHTML(in store.Incident, zones []config.ZoneDef) template.HTML {
	var b strings.Builder
	b.WriteString(`<div class="mxwrap"><table class="mx">`)
	cols := len(in.Columns) + 1
	for _, g := range groupSeries(in.Series, zones) {
		fmt.Fprintf(&b, `<tr><td class="grp" colspan="%d" style="border-left:4px solid %s">%s</td></tr>`, cols,
			template.HTMLEscapeString(g.color), template.HTMLEscapeString(g.name))
		for _, s := range g.series {
			writeMatrixRow(&b, in, s)
		}
	}
	b.WriteString("</table></div>")
	return template.HTML(b.String())
}

type seriesGroup struct {
	name, color string
	series      []store.IncidentSeries
}

// groupSeries orders incident rows by zone (in the configured zone order;
// unknown zones last), keeping the path order within a zone.
func groupSeries(series []store.IncidentSeries, zones []config.ZoneDef) []seriesGroup {
	var out []seriesGroup
	used := map[int]bool{}
	for _, z := range zones {
		g := seriesGroup{name: z.Name, color: z.Color}
		for i, s := range series {
			if s.Zone == z.ID {
				g.series = append(g.series, s)
				used[i] = true
			}
		}
		if len(g.series) > 0 {
			out = append(out, g)
		}
	}
	rest := seriesGroup{name: "Sonstige", color: "#888888"}
	for i, s := range series {
		if !used[i] {
			rest.series = append(rest.series, s)
		}
	}
	if len(rest.series) > 0 {
		out = append(out, rest)
	}
	return out
}

func writeMatrixRow(b *strings.Builder, in store.Incident, s store.IncidentSeries) {
	state := map[byte]string{'.': "c-ok", 's': "c-s", 'x': "c-x", '-': "c-n"}
	text := map[byte]string{'.': "antwortet", 's': "langsam", 'x': "keine Antwort", '-': "nicht gemessen"}
	fmt.Fprintf(b, `<tr><td class="lbl">%s</td>`, template.HTMLEscapeString(s.Label))
	for i := 0; i < len(s.States) && i < len(in.Columns); i++ {
		c := s.States[i]
		pre := ""
		if i < in.PreRoll || i >= len(in.Columns)-in.PostRoll {
			pre = " pre"
		}
		rtt := ""
		if i < len(s.RTT) && s.RTT[i] >= 0 {
			rtt = fmt.Sprintf(" · %.1f ms", s.RTT[i])
		}
		fmt.Fprintf(b, `<td class="%s%s" title="%s · %s%s"></td>`, state[c], pre,
			time.UnixMilli(in.Columns[i]).Format("15:04:05"), text[c], rtt)
	}
	b.WriteString("</tr>")
}
