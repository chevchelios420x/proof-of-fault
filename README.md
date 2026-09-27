# proof-of-fault

Desktop-App zur forensischen Beweissicherung bei Internetstörungen (Latenzspitzen,
Jitter, Micro-Drops, Paketverlust). Die App misst dauerhaft drei Zonen und zeigt,
**wo** eine Störung beginnt:

| Zone | Gemessen wird | Bedeutung |
|---|---|---|
| `LAN` | letzter Router im Heimnetz | WLAN, Kabel, eigener Router |
| `ISP_EDGE` | erster Hop des Providers (auch CGNAT `100.64/10`) | Anschluss / Last Mile |
| `WAN` | das eingegebene Ziel | Peering, Transit, Zielserver |

Ein Ausfall (≥ 3 Verluste in Folge) wird der **ersten Zone zugeordnet, ab der alle
weiteren Zonen bis zum Ziel nicht antworten**. Verluste nur an einem Zwischen-Hop bei
erreichbarem Ziel (ICMP-Ratenbegrenzung) zählen nicht als Ausfall.

Status: **Windows** (IPv4). Linux/macOS folgen (siehe `internal/probe`).

## Benutzung (Windows)

1. `proof-of-fault.exe` starten. Es sind keine Admin-Rechte und keine Installation
   nötig; die WebView2-Runtime ist bei Windows 10/11 vorhanden.
2. Ziel eingeben (z. B. `1.1.1.1`) → **Überwachung starten**.
3. Laufen lassen, bis die Störung auftritt. Messdaten landen in
   `%AppData%\proof-of-fault\data.db`.
4. **Verlauf & Berichte** → Sitzung wählen → **Bericht (HTML/PDF)** oder
   **Rohdaten (CSV)**. Den HTML-Bericht im Browser öffnen und mit „Drucken → Als PDF
   speichern“ in eine PDF umwandeln.

Tipps für belastbare Nachweise: Den PC **per LAN-Kabel** anschließen (sonst sieht der
Provider WLAN als Ursache). Mehrere Stunden bis Tage messen und den Energiesparmodus
bzw. Standby deaktivieren.

## Bauen

Voraussetzungen: Go (Version aus `go.mod`), Node.js 20+,
`go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0`

```
wails build -platform windows/amd64     # -> build/bin/proof-of-fault.exe
wails dev                               # Entwicklung mit Hot-Reload
go test ./internal/...
```

Jeder Push auf `main` baut die EXE auch per GitHub Actions (Artefakt
`proof-of-fault-windows-amd64`).

## Aufbau

```
app.go / main.go     Wails-Bindings (dünne API-Schicht für die GUI)
internal/probe       unprivilegierte Probes, je OS eine Datei
                     (Windows: IcmpSendEcho2 aus iphlpapi.dll, wie tracert.exe)
internal/path        Traceroute, Zonen-Einteilung, Messpunkt je Zone
internal/monitor     1 Probe/s je Zone, Ausfall-Zuordnung, Routenwechsel (alle 5 min)
internal/metrics     RFC-3550-Jitter, P50/P95/P99
internal/store       SQLite (WAL, reines Go ohne CGO)
internal/report      Auswertung, CSV- und HTML-Export mit SHA-256 der Rohdaten
frontend/            Svelte + uPlot
```

Eine neue Plattform braucht nur eine Implementierung von `probe.Prober`
(`icmp_linux.go`, `icmp_darwin.go`) mit passendem Build-Tag.
