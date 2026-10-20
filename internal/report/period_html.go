package report

import (
	"fmt"
	"html/template"
	"io"
	"math"
	"strings"
	"time"
)

// WritePeriodHTML writes the period report (24 h, 7, 30, 90 days) as a
// self-contained, printable HTML document.
func WritePeriodHTML(w io.Writer, r PeriodReport) error {
	return periodTmpl.Execute(w, r)
}

func fmtDur(sec float64) string {
	s := int(math.Round(sec))
	switch {
	case s < 60:
		return fmt.Sprintf("%d s", s)
	case s < 3600:
		return fmt.Sprintf("%d min %02d s", s/60, s%60)
	default:
		return fmt.Sprintf("%d h %02d min", s/3600, s%3600/60)
	}
}

func pct(v float64, digits int) string {
	return strings.Replace(fmt.Sprintf("%.*f %%", digits, 100*v), ".", ",", 1)
}

func num1(v float64) string { return strings.Replace(fmt.Sprintf("%.1f", v), ".", ",", 1) }

// periodChart draws provider disruption time (red bars) and measurement
// coverage (grey bars below) per row of a period.
func periodChart(p Period) template.HTML {
	n := len(p.Buckets)
	if n == 0 {
		return ""
	}
	const W, H, cov = 1000.0, 150.0, 26.0
	maxSec := 0.0
	for _, b := range p.Buckets {
		maxSec = math.Max(maxSec, b.ISP.Sec)
	}
	top := niceCeil(maxSec)
	bw := W / float64(n)
	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg class="chart" viewBox="0 0 %.0f %.0f" xmlns="http://www.w3.org/2000/svg" font-family="Segoe UI,Arial" font-size="11">`, W+60, H+cov+40)
	fmt.Fprintf(&sb, `<text x="0" y="10" fill="#666">Störungsdauer Anbieter (max. %s)</text>`, fmtDur(top))
	for i, b := range p.Buckets {
		x := 50 + float64(i)*bw
		if b.ISP.Sec > 0 {
			h := math.Max(1.5, (H-20)*b.ISP.Sec/top)
			fmt.Fprintf(&sb, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="#d62828"><title>%s: %d Störungen, %s</title></rect>`,
				x+bw*0.1, H-h, bw*0.8, h, template.HTMLEscapeString(b.Label), b.ISP.N, fmtDur(b.ISP.Sec))
		}
		c := b.Coverage()
		fmt.Fprintf(&sb, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"><title>%s: %s gemessen</title></rect>`,
			x+bw*0.1, H+8+cov*(1-c), bw*0.8, math.Max(0.5, cov*c), covColor(c), template.HTMLEscapeString(b.Label), pct(c, 0))
		step := max(1, n/12)
		if i%step == 0 {
			lbl := b.Label
			if len(lbl) > 9 {
				lbl = lbl[:9]
			}
			fmt.Fprintf(&sb, `<text x="%.1f" y="%.1f" fill="#666">%s</text>`, x, H+cov+24, template.HTMLEscapeString(lbl))
		}
	}
	fmt.Fprintf(&sb, `<line x1="50" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#999"/>`, H, W+50, H)
	fmt.Fprintf(&sb, `<text x="0" y="%.1f" fill="#666">Messung</text>`, H+8+cov/2+4)
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}

func covColor(c float64) string {
	switch {
	case c >= 0.95:
		return "#52b788"
	case c >= 0.5:
		return "#f4a261"
	}
	return "#bbb"
}

func niceCeil(v float64) float64 {
	if v <= 0 {
		return 60
	}
	for _, s := range []float64{10, 30, 60, 120, 300, 600, 1800, 3600, 7200, 14400, 43200, 86400} {
		if v <= s {
			return s
		}
	}
	return math.Ceil(v/86400) * 86400
}

// verdict is the plain-language assessment of the longest period.
func verdict(r PeriodReport) template.HTML {
	if len(r.Periods) == 0 {
		return ""
	}
	p := r.Periods[len(r.Periods)-1]
	if p.Total.MeasuredSec == 0 {
		return "Im Auswertungszeitraum liegen keine Messungen vor."
	}
	var parts []string
	cov := p.Total.Coverage()
	parts = append(parts, fmt.Sprintf("In den letzten %s wurde der Anschluss %s der Zeit gemessen (%s).",
		p.Title, pct(cov, 1), fmtDur(float64(p.Total.MeasuredSec))))
	if p.Total.ISP.N == 0 {
		parts = append(parts, "Es wurden <b>keine Störungen</b> festgestellt, die dem Anbieter zuzuordnen sind.")
	} else {
		parts = append(parts, fmt.Sprintf("Dabei traten <b>%d Störungen</b> auf, die dem Anbieter zuzuordnen sind (Heimnetz und Router funktionierten, die Verbindung brach erst beim bzw. hinter dem Anbieter ab), zusammen <b>%s</b>, die längste %s. Betroffen waren <b>%d von %d Tagen</b>; %d Störungen dauerten eine Minute oder länger.",
			p.Total.ISP.N, fmtDur(p.Total.ISP.Sec), fmtDur(p.Total.ISP.MaxSec), p.DaysWithISP, len(p.Buckets), p.Total.ISPLong))
		parts = append(parts, fmt.Sprintf("Die Verfügbarkeit der Anbieterstrecke lag damit bei <b>%s</b> der gemessenen Zeit.", pct(p.Total.Availability(), 3)))
	}
	if p.Total.Home.N == 0 {
		parts = append(parts, "Im eigenen Heimnetz traten im selben Zeitraum keine Störungen auf.")
	} else {
		parts = append(parts, fmt.Sprintf("Im eigenen Heimnetz traten %d Störungen auf (zusammen %s); sie sind getrennt ausgewiesen und nicht dem Anbieter angerechnet.", p.Total.Home.N, fmtDur(p.Total.Home.Sec)))
	}
	if p.Docsis.N > 0 && p.Docsis.Warning+p.Docsis.Critical > 0 {
		parts = append(parts, fmt.Sprintf("Die Leitungswerte des Kabelmodems waren bei %d von %d Abfragen auffällig (%d kritisch).", p.Docsis.Warning+p.Docsis.Critical, p.Docsis.N, p.Docsis.Critical))
	}
	if cov < 0.9 {
		parts = append(parts, "<i>Hinweis: Die Messabdeckung liegt unter 90 %; für lückenlose Belege die Messung dauerhaft laufen lassen.</i>")
	}
	return template.HTML(strings.Join(parts, " "))
}

var periodFuncs = template.FuncMap{
	"dur":     fmtDur,
	"pct":     pct,
	"num":     num1,
	"chart":   periodChart,
	"verdict": verdict,
	"ts": func(ms int64) string {
		if ms == 0 {
			return "–"
		}
		return time.UnixMilli(ms).In(Location).Format("02.01.2006 15:04:05")
	},
	"day": func(ms int64) string { return time.UnixMilli(ms).In(Location).Format("02.01.2006 15:04") },
	"loss": func(b Bucket, z string) string {
		l := b.Loss[z]
		if l.Sent == 0 {
			return "–"
		}
		return strings.Replace(fmt.Sprintf("%.2f %%", l.Pct()), ".", ",", 1)
	},
	"ms": func(v float64) string {
		if v == 0 {
			return "–"
		}
		return num1(v) + " ms"
	},
	"className": func(c string) string {
		if c == "isp" {
			return "Anbieter-Zugang"
		}
		return "Anbieter-Netz/Internet"
	},
	"docsisText": func(s string) string {
		switch s {
		case "good":
			return "unauffällig"
		case "warning":
			return "auffällig"
		case "critical":
			return "kritisch"
		}
		return "–"
	},
	"access": func(a string) string {
		switch a {
		case "docsis":
			return "Kabel (DOCSIS)"
		case "dsl":
			return "DSL"
		case "fibre":
			return "Glasfaser"
		case "mobile":
			return "LTE/5G"
		}
		return a
	},
	"last":  func(ps []Period) Period { return ps[len(ps)-1] },
	"isec":  func(s int) string { return fmtDur(float64(s)) },
	"add":   func(a, b int) int { return a + b },
	"lines": func(s string) []string { return strings.Split(strings.TrimSpace(s), "\n") },
	"join":  func(xs []string) string { return strings.Join(xs, ", ") },
}

var periodTmpl = template.Must(template.New("period").Funcs(periodFuncs).Parse(`<!DOCTYPE html>
<html lang="de"><head><meta charset="utf-8">
<title>Nachweis Verbindungsstörungen – {{ts .GeneratedAt}}</title>
<style>
body{font-family:Segoe UI,Arial,sans-serif;margin:24px auto;color:#222;max-width:1100px;padding:0 16px}
h1{font-size:22px;margin-bottom:2px}h2{font-size:18px;margin-top:28px;border-bottom:2px solid #3a5a8c;padding-bottom:3px}h3{font-size:15px;margin:18px 0 6px}
table{border-collapse:collapse;margin:8px 0 18px;width:100%}
th,td{border:1px solid #ccc;padding:3px 7px;text-align:right;font-size:12.5px}
th:first-child,td:first-child{text-align:left}th{background:#f1f1f1}
table.kv th{text-align:left;width:26%;background:#f6f6f6}table.kv td{text-align:left}
.verdict{background:#fff4e5;border-left:6px solid #e76f51;padding:10px 16px;line-height:1.5}
.ok{background:#eef8f1;border-color:#2a9d8f}
.bad{background:#fde2e2}.warn{background:#fff6e0}.dim td{color:#999}
.chart{width:100%;height:auto;border:1px solid #eee;margin:4px 0 8px}
small,.muted{color:#666}.mono{font-family:Consolas,monospace;font-size:11px;word-break:break-all}
.note{font-size:12.5px;line-height:1.5;background:#f6f6f6;padding:8px 14px;border-left:4px solid #999}
.toc a{margin-right:14px}
@media print{body{margin:0;max-width:none}h2{page-break-before:always}h2.first{page-break-before:avoid}table,svg{page-break-inside:avoid}.toc{display:none}}
</style></head><body>
<h1>Nachweis von Verbindungsstörungen des Internetanschlusses</h1>
<div class="muted">Erstellt am {{ts .GeneratedAt}} ({{.TimeZone}}) mit proof-of-fault {{.Version}} · Auswertungszeiträume: 24 Stunden, 7, 30 und 90 Tage (jeweils bis zum Erstellungszeitpunkt)</div>
<p class="toc">{{range .Periods}}<a href="#p{{.Days}}">{{.Title}}</a>{{end}}<a href="#list">Störungsliste</a><a href="#method">Messverfahren</a><a href="#legal">Hinweise</a></p>

<h2 class="first">1. Angaben zum Anschluss</h2>
<table class="kv">
<tr><th>Anschlussinhaber</th><td>{{or .Contract.Name "–"}}</td></tr>
<tr><th>Anschlussadresse</th><td>{{or .Contract.Address "–"}}</td></tr>
<tr><th>Anbieter</th><td>{{or .Contract.Provider "–"}}</td></tr>
<tr><th>Kunden-/Vertragsnummer</th><td>{{or .Contract.CustomerNo "–"}}</td></tr>
<tr><th>Tarif / vereinbarte Leistung</th><td>{{or .Contract.Tariff "–"}}</td></tr>
<tr><th>Anschlussart</th><td>{{access .Access}}</td></tr>
<tr><th>Störungsmeldungen an den Anbieter</th><td>{{if .Contract.Tickets}}{{range lines .Contract.Tickets}}{{.}}<br>{{end}}{{else}}–{{end}}</td></tr>
{{with .Net}}<tr><th>Messrechner</th><td>{{.Hostname}} ({{.OS}}), IP {{.LocalIP}}{{if .Prefix}} ({{.Prefix}}){{end}}, Gateway {{join .Gateways}}</td></tr>{{end}}
<tr><th>Messziel(e)</th><td>{{join .Targets}}</td></tr>
</table>

<h2>2. Zusammenfassung</h2>
<div class="verdict {{if eq (last .Periods).Total.ISP.N 0}}ok{{end}}">{{verdict .}}</div>
<table>
<tr><th>Kennzahl</th>{{range .Periods}}<th>{{.Title}}</th>{{end}}</tr>
<tr><td>Zeitraum</td>{{range .Periods}}<td><small>{{day .From}} –<br>{{day .To}}</small></td>{{end}}</tr>
<tr><td>Messabdeckung (gemessene Zeit)</td>{{range .Periods}}<td>{{pct .Total.Coverage 1}}<br><small>{{isec .Total.MeasuredSec}}</small></td>{{end}}</tr>
<tr><td><b>Störungen durch den Anbieter</b></td>{{range .Periods}}<td class="{{if .Total.ISP.N}}bad{{end}}"><b>{{.Total.ISP.N}}</b></td>{{end}}</tr>
<tr><td>davon 1 Minute oder länger</td>{{range .Periods}}<td>{{.Total.ISPLong}}</td>{{end}}</tr>
<tr><td>Gesamtdauer der Störungen</td>{{range .Periods}}<td>{{dur .Total.ISP.Sec}}</td>{{end}}</tr>
<tr><td>längste Störung</td>{{range .Periods}}<td>{{dur .Total.ISP.MaxSec}}</td>{{end}}</tr>
<tr><td>{{if eq (index .Periods 0).Days 1}}Stunden/Tage{{else}}Tage{{end}} mit Störung</td>{{range .Periods}}<td>{{.DaysWithISP}} von {{len .Buckets}}</td>{{end}}</tr>
<tr><td><b>Verfügbarkeit Anbieterstrecke</b> (gemessene Zeit ohne Anbieter-Störung)</td>{{range .Periods}}<td><b>{{if .Total.MeasuredSec}}{{pct .Total.Availability 3}}{{else}}–{{end}}</b></td>{{end}}</tr>
<tr><td>Dauerverteilung: &lt;10 s / &lt;1 min / &lt;10 min / länger</td>{{range .Periods}}<td>{{index .Durations 0}} / {{index .Durations 1}} / {{index .Durations 2}} / {{index .Durations 3}}</td>{{end}}</tr>
<tr><td>Paketverlust Heimnetz (Router)</td>{{range .Periods}}<td>{{loss .Total "LAN"}}</td>{{end}}</tr>
<tr><td>Paketverlust erster Anbieter-Knoten</td>{{range .Periods}}<td>{{loss .Total "ISP_EDGE"}}</td>{{end}}</tr>
<tr><td>Paketverlust Messziel (Internet)</td>{{range .Periods}}<td>{{loss .Total "WAN"}}</td>{{end}}</tr>
<tr><td>Latenz Messziel Median / P95 / P99</td>{{range .Periods}}<td>{{ms .Total.Latency.P50Ms}}<br>{{ms .Total.Latency.P95Ms}}<br>{{ms .Total.Latency.P99Ms}}</td>{{end}}</tr>
<tr><td>Kontrolle: Störungen im Heimnetz (nicht angerechnet)</td>{{range .Periods}}<td>{{.Total.Home.N}} ({{dur .Total.Home.Sec}})</td>{{end}}</tr>
<tr><td>Kontrolle: nur Ziel betroffen (nicht angerechnet)</td>{{range .Periods}}<td>{{.Total.TargetOnly.N}}</td>{{end}}</tr>
<tr><td>Leitungswerte Kabelmodem: auffällig / Abfragen</td>{{range .Periods}}<td>{{if .Docsis.N}}{{add .Docsis.Warning .Docsis.Critical}} / {{.Docsis.N}}{{else}}–{{end}}</td>{{end}}</tr>
</table>

{{range .Periods}}
<h2 id="p{{.Days}}">{{if eq .Days 1}}3{{else if eq .Days 7}}4{{else if eq .Days 30}}5{{else}}6{{end}}. Auswertung {{.Title}}</h2>
<p class="muted">{{day .From}} bis {{day .To}} · {{if eq .Days 1}}stündlich{{else}}täglich{{end}} · rote Balken: Störungsdauer durch den Anbieter, untere Balken: Messabdeckung (grün ≥ 95 %)</p>
{{chart .}}
<table>
<tr><th>{{if eq .Days 1}}Stunde{{else}}Tag{{end}}</th><th>gemessen</th><th>Störungen Anbieter</th><th>davon ≥ 1 min</th><th>Dauer</th><th>längste</th><th>Verfügbarkeit</th><th>Verlust Anbieter-Knoten</th><th>Verlust Ziel</th><th>Latenz P95</th><th>Heimnetz</th></tr>
{{range .Buckets}}<tr class="{{if .ISP.N}}{{if .ISPLong}}bad{{else}}warn{{end}}{{else if not .MeasuredSec}}dim{{end}}">
<td>{{.Label}}</td><td>{{pct .Coverage 0}}</td><td>{{.ISP.N}}</td><td>{{.ISPLong}}</td><td>{{if .ISP.N}}{{dur .ISP.Sec}}{{else}}–{{end}}</td><td>{{if .ISP.N}}{{dur .ISP.MaxSec}}{{else}}–{{end}}</td>
<td>{{if .MeasuredSec}}{{pct .Availability 2}}{{else}}–{{end}}</td><td>{{loss . "ISP_EDGE"}}</td><td>{{loss . "WAN"}}</td><td>{{ms .Latency.P95Ms}}</td><td>{{.Home.N}}</td></tr>
{{end}}
<tr><th>Summe</th><th>{{pct .Total.Coverage 1}}</th><th>{{.Total.ISP.N}}</th><th>{{.Total.ISPLong}}</th><th>{{dur .Total.ISP.Sec}}</th><th>{{dur .Total.ISP.MaxSec}}</th><th>{{if .Total.MeasuredSec}}{{pct .Total.Availability 3}}{{else}}–{{end}}</th><th>{{loss .Total "ISP_EDGE"}}</th><th>{{loss .Total "WAN"}}</th><th>{{ms .Total.Latency.P95Ms}}</th><th>{{.Total.Home.N}}</th></tr>
</table>
{{if .Docsis.Issues}}<p><small>Häufigste Auffälligkeiten der Leitungswerte: {{join .Docsis.Issues}}</small></p>{{end}}
{{end}}

<h2 id="list">7. Liste der Störungen durch den Anbieter ({{(last .Periods).Title}})</h2>
{{if .Incidents}}
<table>
<tr><th>#</th><th>Beginn</th><th>Ende</th><th>Dauer</th><th>Bereich</th><th>Beschreibung</th><th>erster ausgefallener Messpunkt</th><th>Leitungswerte</th><th>Messung</th></tr>
{{range $i, $x := .Incidents}}<tr class="{{if ge $x.Sec 60.0}}bad{{end}}"><td>{{add $i 1}}</td><td>{{ts $x.T}}</td><td>{{ts $x.End}}</td><td>{{dur $x.Sec}}</td><td style="text-align:left">{{className $x.Class}}</td><td style="text-align:left">{{$x.Title}}</td><td style="text-align:left">{{or $x.Origin "–"}}</td><td>{{docsisText $x.Docsis}}</td><td>#{{$x.Session}}</td></tr>
{{end}}</table>
{{else}}<p>Keine.</p>{{end}}

<h2 id="method">8. Messverfahren</h2>
<div class="note">
<p>Ein Messprogramm (proof-of-fault) prüft <b>jede Sekunde</b> drei Abschnitte der Verbindung getrennt: das <b>Heimnetz</b> bis zum eigenen Router, den <b>ersten Knoten des Anbieters</b> und ein <b>Ziel im Internet</b>. Dazu werden ICMP-Echo-Anfragen (Ping) gesendet; optional ergänzen TCP-Verbindungstests und weitere Ziele (z. B. 1.1.1.1, 9.9.9.9, 8.8.8.8) die Messung. Eine Anfrage ohne Antwort innerhalb der Wartezeit gilt als Verlust.</p>
<p><b>Zuordnung:</b> Eine Störung wird dem Anbieter nur angerechnet, wenn in derselben Sekunde das Heimnetz einwandfrei antwortete, aber der Anbieter-Knoten und/oder alle Internet-Ziele ausfielen („Anbieter-Zugang“ bzw. „Anbieter-Netz/Internet“). Störungen im Heimnetz sowie Ausfälle einzelner Ziele bei erreichbaren Ausweichzielen werden getrennt ausgewiesen und <b>nicht</b> angerechnet. Einzelne Verluste an Zwischenknoten, die Ping-Anfragen nur gedrosselt beantworten, werden nicht als Störung gewertet.</p>
<p><b>Messabdeckung</b> ist der Anteil der Zeit, in der gemessen wurde. Die <b>Verfügbarkeit</b> bezieht sich nur auf die gemessene Zeit. Latenz-Perzentile (P95/P99) sind auf 0,1 ms (unter 100 ms) bzw. 1 ms genau.</p>
<p>Alle Rohdaten (jede einzelne Messung mit Zeitstempel) sind in der Datenbank des Messprogramms gespeichert und können je Messung als CSV oder als prüfsummengesicherte Export-Datei (.pof) übergeben werden.</p>
</div>

<h2 id="legal">9. Hinweise</h2>
<div class="note">
<p>Dieser Bericht dokumentiert Unterbrechungen der Verbindung. Er ersetzt nicht das Messprotokoll der Breitbandmessung der Bundesnetzagentur (breitbandmessung.de), das für eine <b>Minderung wegen zu geringer Geschwindigkeit</b> (§ 57 Abs. 4 TKG) maßgeblich ist.</p>
<p>Bei <b>Störungen</b> ist der Anbieter nach § 58 TKG zur unverzüglichen und unentgeltlichen Beseitigung verpflichtet; wird eine gemeldete Störung nicht fristgerecht behoben, kann ein Anspruch auf Entschädigung bestehen. Voraussetzung ist eine <b>Störungsmeldung beim Anbieter</b> (Datum, Ticketnummer – siehe Abschnitt 1). Bei Streitigkeiten kann die Schlichtungsstelle Telekommunikation der Bundesnetzagentur angerufen werden.</p>
<p><small>Keine Rechtsberatung. Angaben ohne Gewähr; maßgeblich sind der Vertrag und die geltende Fassung des TKG.</small></p>
</div>

<h2>10. Datengrundlage</h2>
<table>
<tr><th>Messung</th><th>Ziel</th><th>Beginn</th><th>Ende</th><th style="text-align:left">Kommentar</th></tr>
{{range .Sessions}}<tr><td>#{{.ID}}</td><td>{{.Target}}</td><td>{{ts .StartedAt}}</td><td>{{if .EndedAt}}{{ts .EndedAt}}{{else}}läuft{{end}}</td><td style="text-align:left">{{.Note}}</td></tr>{{end}}
</table>
<p class="muted">Prüfsumme (SHA-256) der ausgewerteten Kennzahlen und der Störungsliste:<br><span class="mono">{{.DataSHA256}}</span></p>
</body></html>`))
