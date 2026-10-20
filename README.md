# proof-of-fault

Programm zur forensischen Beweissicherung bei Internetstörungen (Latenzspitzen,
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

Läuft als **Hintergrunddienst mit Weboberfläche** unter **Windows** und **Linux
(Ubuntu)** – ohne Administratorrechte, IPv4. Bedienung im Browser, auch von einem
anderen Rechner oder dem Handy aus.

## Download

| System | Datei |
|---|---|
| Windows 10/11 | [`build/bin/proof-of-fault-v0.21.0.exe`](build/bin/proof-of-fault-v0.21.0.exe) |
| Linux (Ubuntu, x86-64) | [`build/bin/proof-of-fault-v0.21.0-linux-amd64`](build/bin/proof-of-fault-v0.21.0-linux-amd64) |

Änderungen je Version: [RELEASES.md](RELEASES.md).

## Benutzung

### Windows

1. EXE doppelklicken. Beim ersten Start richtet sie sich ohne Admin-Rechte ein:
   Kopie nach `%LOCALAPPDATA%\Programs\proof-of-fault\`, Autostart bei der
   Anmeldung (Registry `HKCU\…\Run`), Dienst starten, Browser öffnet sich
   angemeldet unter `http://127.0.0.1:8443/`.
2. Erneuter Doppelklick (oder eine neuere EXE) öffnet wieder die Oberfläche bzw.
   aktualisiert den Dienst. Die heruntergeladene Datei kann danach gelöscht werden.
3. Der Dienst misst weiter, auch wenn der Browser geschlossen ist – aber nur,
   solange du angemeldet bist (ein echter Windows-Dienst bräuchte Admin-Rechte).
   Für 24/7-Messungen ist eine kleine Linux-VM besser.

### Linux (Ubuntu-VM, 24/7)

```
chmod +x proof-of-fault-v0.21.0-linux-amd64
./proof-of-fault-v0.21.0-linux-amd64          # als normaler Benutzer, nicht mit sudo
```

Der erste Start kopiert das Programm nach `~/.local/bin/proof-of-fault`, legt einen
**systemd-Benutzerdienst** an (`systemctl --user status proof-of-fault`), aktiviert
„Linger“ (Dienst läuft ohne Anmeldung und nach Neustart) und zeigt Adresse und
**Zugangsschlüssel** (UUID) an:

```
Weboberfläche:      https://192.168.1.50:8443/
Zugangsschlüssel:   1b4e28ba-2fa1-41d2-883f-0016d3cca427
```

Im Browser die Adresse öffnen, die Zertifikatswarnung einmal bestätigen
(selbst signiert, Verbindung trotzdem verschlüsselt) und den Schlüssel eingeben.
Weitere Befehle: `proof-of-fault status`, `new-key`, `uninstall`,
`install -listen 0.0.0.0:8443 -tls=true`.

Ping ohne root nutzt die „Ping-Sockets“ von Linux (`net.ipv4.ping_group_range`, bei
Ubuntu Standard). Fehlt die Freigabe, meldet das Programm den einmaligen
`sysctl`-Befehl. Wenn `loginctl enable-linger` nicht erlaubt ist, einmalig
`sudo loginctl enable-linger <benutzer>` ausführen.

### Messen

1. Ziel eingeben (z. B. `1.1.1.1`) → **Überwachung starten**. Unter ⚙ → Komfort
   „Nach einem Neustart … fortsetzen“ einschalten, dann misst der Dienst lückenlos.
2. In der Route-Tabelle kann jeder Hop per Häkchen als eigene Linie in den
   Latenzverlauf aufgenommen werden; Zone und Name sind je Hop einstellbar.
   Unter „Weitere Messpunkte“ lassen sich zusätzliche Geräte/Adressen eintragen.
3. **Verlauf & Berichte** → Sitzung wählen → **Bericht (HTML/PDF)**, **Rohdaten
   (CSV)**, **Ereignisse (CSV)** oder **Export (.pof)**.

### Nachweis für Anbieter / Bundesnetzagentur

**Verlauf & Berichte → „Nachweis für Anbieter / Bundesnetzagentur“** fasst alle
Messungen der letzten **24 Stunden, 7, 30 und 90 Tage** zusammen: Messabdeckung,
Störungen, die dem Anbieter zuzuordnen sind (Anzahl, Dauer, längste, betroffene Tage),
Verfügbarkeit, Paketverlust je Abschnitt, Latenz (Median/P95/P99), Stunden- bzw.
Tagestabellen mit Diagramm, vollständige Störungsliste mit Leitungswerten,
Messverfahren, rechtliche Hinweise (§ 57/§ 58 TKG) und Prüfsumme. Vertrags- und
Ticketdaten unter ⚙ → „Vertrag & Nachweis“ eintragen. Im Browser „Drucken → Als PDF
speichern“.

Für eine Minderung wegen zu geringer **Geschwindigkeit** zählt nur das Messprotokoll
der [Breitbandmessung](https://breitbandmessung.de); dieser Bericht belegt
**Störungen/Unterbrechungen**.

### Export & Import

**Export (.pof)** enthält eine komplette Messung (alle Rohdaten, Ereignisse,
Störungen, Routen, Leitungswerte) als ZIP mit SHA-256-Prüfsummen. **Messung
importieren** liest sie auf jedem anderen proof-of-fault wieder ein – Windows ↔
Linux – und wertet sie dort genauso aus (auch im Nachweis-Bericht). Veränderte
Dateien werden beim Import abgelehnt.

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

**Kabelanschluss (DOCSIS):** Bei Anschlussart „Kabel (DOCSIS)“ liest die App die
Leitungswerte der FRITZ!Box (Pegel, SNR/MER, Fehlerzähler) beim Start, im Intervall
und bei jeder Störung mit. Login und Auswertung basieren auf
[DOCSight](https://github.com/itsDNNS/docsight) von Dennis Braun (MIT-Lizenz).

Tipps für belastbare Nachweise: Den PC **per LAN-Kabel** anschließen (sonst sieht der
Provider WLAN als Ursache). Mehrere Stunden bis Tage messen und den Energiesparmodus
bzw. Standby deaktivieren.

## Wo liegen die Daten?

| | Windows | Linux |
|---|---|---|
| Datenbank, Einstellungen, Zugangsschlüssel, Zertifikat, Protokoll | `%AppData%\proof-of-fault\` | `~/.config/proof-of-fault/` |
| Programm | `%LOCALAPPDATA%\Programs\proof-of-fault\` | `~/.local/bin/proof-of-fault` |
| Autostart | Registry `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` | `~/.config/systemd/user/proof-of-fault.service` |

- `data.db` (+ `-wal`/`-shm`): SQLite mit allen Messungen. Zum Kopieren den Dienst
  stoppen oder alle drei Dateien zusammen kopieren – besser: **Export (.pof)**.
- `agent.json`: Adresse (`listen`), HTTPS (`tls`) und Zugangsschlüssel (`token`).
- `cert.pem`/`key.pem`: selbst signiertes Zertifikat (nur bei HTTPS).
- `secret.key` (Linux): Schlüssel für das gespeicherte FRITZ!Box-Kennwort; unter
  Windows schützt DPAPI das Kennwort.
- `proof-of-fault.log`: Protokoll des Dienstes.

Größe grob 10–15 MB pro Tag bei 3 Messpunkten; jeder zusätzliche Messpunkt etwa ein
Drittel mehr (Schätzung).

**Alles entfernen:** `proof-of-fault uninstall`, danach die Ordner oben löschen.

## Bauen

Voraussetzungen: Go (Version aus `go.mod`), Node.js 20+. Reines Go ohne CGO – beide
Programme lassen sich von jedem System aus bauen.

```
scripts/build-windows.sh    # -> build/bin/proof-of-fault-vX.Y.Z.exe und …-linux-amd64
go run . run                # Dienst im Vordergrund (Entwicklung)
cd frontend && npm run dev  # Oberfläche mit Hot-Reload, API über Proxy zum Dienst
go test ./internal/...
```

Jeder Push auf `main` baut beide Programme auch per GitHub Actions.

## Aufbau

```
main.go              Befehle (install/run/status …), Start als Dienst
service_*.go         Autostart je OS (Windows: HKCU-Run, Linux: systemd --user)
server.go / agent.go Webserver, Anmeldung per UUID, Live-Daten per SSE, HTTPS
app.go               API-Schicht (jede Methode = /api/call/<Name>)
internal/probe       unprivilegierte Probes, je OS eine Datei
                     (Windows: IcmpSendEcho2 aus iphlpapi.dll, Linux: Ping-Sockets)
internal/path        Traceroute, Zonen-Einteilung, Messpunkt je Zone
internal/monitor     1 Probe/s je Zone, Ausfall-Zuordnung, Routenwechsel (alle 5 min)
internal/metrics     RFC-3550-Jitter, P50/P95/P99
internal/store       SQLite (WAL, reines Go ohne CGO)
internal/store       … und Export/Import (.pof) einer Messung
internal/report      Auswertung, CSV- und HTML-Export, Nachweis-Bericht 1/7/30/90 Tage
frontend/            Svelte + uPlot
```

Eine neue Plattform braucht eine Implementierung von `probe.Prober` und
`service_<os>.go`.
