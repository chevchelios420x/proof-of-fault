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

Die aktuelle EXE liegt im Repo unter [`build/bin/proof-of-fault-v0.15.0.exe`](build/bin/proof-of-fault-v0.15.0.exe). Änderungen je Version: [RELEASES.md](RELEASES.md).

1. `proof-of-fault.exe` starten. Es sind keine Admin-Rechte und keine Installation
   nötig; die WebView2-Runtime ist bei Windows 10/11 vorhanden.
2. Ziel eingeben (z. B. `1.1.1.1`) → **Überwachung starten**.
3. In der Route-Tabelle kann jeder Hop per Häkchen als eigene Linie in den
   Latenzverlauf aufgenommen werden. Die Zone jedes Hops lässt sich per Auswahlfeld
   korrigieren (z. B. ein privates `10.x`-Transfernetz des Providers → `ISP_EDGE`).
   Zusätzlich kann jeder Hop einen eigenen Namen bekommen (erscheint in Legende und
   Bericht). Zone, Name und Häkchen werden pro Hop-Adresse dauerhaft gespeichert.
   Unter „Weitere Messpunkte“ lassen sich zusätzliche Geräte/Adressen (z. B. ein
   zweiter Router, Repeater, NAS) eintragen, die jede Sekunde mitgemessen werden.
4. Laufen lassen, bis die Störung auftritt. Messdaten landen in
   `%AppData%\proof-of-fault\data.db` (siehe [Wo liegen die Daten?](#wo-liegen-die-daten)).
5. **Verlauf & Berichte** → Sitzung wählen → **Bericht (HTML/PDF)** oder
   **Rohdaten (CSV)**. Den HTML-Bericht im Browser öffnen und mit „Drucken → Als PDF
   speichern“ in eine PDF umwandeln.

**Ereignisprotokoll und Diagnose:** Jede Messsekunde wird über alle Messpunkte
gemeinsam ausgewertet. Paketverluste und Latenzspitzen werden mit Zeit, Dauer und
dem Hop protokolliert, ab dem sie bis zum Ziel auftreten; Verluste nur an einem
Zwischen-Hop gelten als harmlos. Routenwechsel werden mit der genauen Änderung
geloggt. Daraus erstellt die App eine verständliche Diagnose („Das Problem liegt
sehr wahrscheinlich beim Internetanbieter …“) mit Empfehlungen – live, im Verlauf
und im HTML-Bericht.

**Störungen:** Jede Störung wird mit einer Sekunden-Matrix aller Hops und manuellen
Hosts gespeichert (wer hat wann nicht geantwortet). Manuelle Hosts in der Zone WAN
dienen als Ausweichziele – so unterscheidet die App „nur das Ziel war weg“ von
„das Internet war weg“.

**Einstellungen (⚙):** Schwellwerte für Latenzspitzen je Zone, Ausfall-Grenze,
Timeout, Signaltöne je Zone und bei Routenwechsel, Farbschema (hell/dunkel/automatisch), Standby-Sperre während der Messung,
automatisches Fortsetzen beim Start und automatisches Löschen alter Messungen.

Tipps für belastbare Nachweise: Den PC **per LAN-Kabel** anschließen (sonst sieht der
Provider WLAN als Ursache). Mehrere Stunden bis Tage messen und den Energiesparmodus
bzw. Standby deaktivieren.

## Wo liegen die Daten?

Alle Messdaten speichert die App in einer SQLite-Datenbank im Benutzerprofil:

```
%AppData%\proof-of-fault\data.db
```

(meist `C:\Users\<Name>\AppData\Roaming\proof-of-fault\data.db`; im Explorer
einfach `%AppData%\proof-of-fault` in die Adresszeile eingeben).

**Inhalt:** alle Messsitzungen (Ziel, Start/Ende), jede einzelne Messung
(Zeitpunkt, Zone bzw. Hop, Latenz oder Verlust), erkannte Ausfälle, die ermittelte
Route samt Routenwechseln sowie pro Hop die gewählte Zone, der Name und ob er im
Diagramm angezeigt wird.

**Dateien:** Während die App läuft, liegen daneben `data.db-wal` und `data.db-shm`
(SQLite-WAL-Modus: neue Messwerte stehen zuerst in der `-wal`-Datei). Zum Sichern
oder Kopieren die App vorher beenden – oder immer alle drei Dateien zusammen kopieren.

**Größe:** grob 10–15 MB pro Tag bei 3 Messpunkten; jeder zusätzlich angehakte Hop
etwa ein Drittel mehr (Schätzung).

**Nicht in der Datenbank:**

- Exporte (HTML-Bericht, CSV) landen dort, wo du sie im Speichern-Dialog ablegst.
- Oberflächen-Einstellungen (zuletzt eingegebenes Ziel, ausgeblendete Linien) speichert
  die WebView2-Komponente in einem eigenen Profilordner, vermutlich
  `%AppData%\proof-of-fault-vX.Y.Z.exe\EBWebView`. Er hängt am EXE-Namen, deshalb
  gehen diese Einstellungen bei einer neuen Version verloren. Messdaten und
  Zonenwahl sind davon nicht betroffen.

**Alles löschen:** App beenden und den Ordner `%AppData%\proof-of-fault` entfernen
(optional auch die `EBWebView`-Ordner der alten EXE-Versionen).

## Bauen

Voraussetzungen: Go (Version aus `go.mod`), Node.js 20+,
`go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0`

```
scripts/build-windows.sh                # -> build/bin/proof-of-fault-vX.Y.Z.exe (Version aus VERSION)
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
