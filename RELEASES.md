# Releases

Versionsschema: `vMAJOR.MINOR.PATCH` (Semantic Versioning). Solange die App in
Entwicklung ist, bleibt MAJOR bei `0`: neue Funktionen erhöhen MINOR, reine
Fehlerbehebungen PATCH. Die Version steht in `VERSION`; die Windows-EXE liegt als
`build/bin/proof-of-fault-vX.Y.Z.exe` im Repo und wird mit
`scripts/build-windows.sh` gebaut.

## v0.18.0 – 2026-09-28

- **Anschlussart** neben „Überwachung starten“: DSL · Kabel (DOCSIS) · Glasfaser.
  DSL und Glasfaser sind vorbereitet (noch ohne Zusatzprüfungen).
- **DOCSIS-Leitungswerte der Kabel-FRITZ!Box** (bei Anschlussart „Kabel“):
  Anmeldung und Abfrage nach dem Vorbild von DOCSight (MIT-Lizenz,
  github.com/itsDNNS/docsight), nativ in Go umgesetzt. Gelesen werden alle
  Downstream-/Upstream-Kanäle (DOCSIS 3.0/3.1) mit Pegel, SNR/MER, Modulation und
  den Zählern für korrigierbare/nicht korrigierbare Fehler; bewertet nach den
  Vodafone-Schwellwerten (DOCSight-Profil „VFKD“).
  - Abfrage beim Messbeginn, im einstellbaren Intervall (Standard 60 s,
    Minimum 15 s) sowie **bei Beginn und Ende jeder Störung**.
  - Ereignisprotokoll: Statuswechsel und neue nicht korrigierbare Fehler.
  - Live-Karte „DOCSIS-Leitungswerte“, Verlauf, HTML-Bericht (Tabelle aller
    Abfragen und die Werte rund um jede Störung) und eine Zeile in der Diagnose.
- ⚙ Einstellungen → „Anschluss & FRITZ!Box“: Adresse, Benutzer, Kennwort
  (unter Windows per DPAPI verschlüsselt gespeichert, wird nie angezeigt),
  Abfrageintervall und „Verbindung testen“.

## v0.17.0 – 2026-09-28

- **Messrechner & Netzwerk im Bericht**: Beim Start jeder Messung wird automatisch
  festgehalten: Rechnername, Betriebssystem, lokale IP (mit Präfix), verwendeter
  Netzwerkadapter, MAC/MTU, Standard-Gateway, DNS-Server sowie die komplette
  Routing-Tabelle, ARP-Tabelle und Adapter-Konfiguration (`route print`,
  `arp -a`, `ipconfig /all`; ohne Admin-Rechte, ohne aufblitzende Fenster).
  Steht ganz oben im HTML-Bericht (Tabellen aufklappbar) und im Verlauf als Karte
  „Messrechner & Netzwerk“ – so ist dokumentiert, über welchen Weg gemessen
  wurde, und mehrere Berichte lassen sich vergleichen.

## v0.16.1 – 2026-09-27

- Fehlerbehebung: Bei Beginn eines Ausfalls wird die Route neu geprüft. Während
  des Ausfalls endet der Traceroute aber früh (hinter der Störung antwortet
  nichts); diese abgeschnittene Route wurde übernommen und dabei der
  ISP_EDGE-Messpunkt entfernt – genau in der Störung fehlte er dann in der
  Auswertung. Unvollständige Routen ersetzen eine vollständige jetzt nicht mehr
  (Eintrag „Route unvollständig ermittelt – bisherige Route bleibt“); echte
  Routenwechsel werden weiterhin übernommen. Das reduziert auch falsche
  „Routenwechsel“-Meldungen.
- Wurde der Anbieter-Zugang in einer Störung nicht gemessen, sagt der Titel das
  jetzt („Anbieter-Zugang wurde nicht gemessen“) statt „antwortete noch“.

## v0.16.0 – 2026-09-27

- **Störungen werden geteilt, wenn sich ihre Art ändert**: Wechselt eine Störung
  z. B. von „nur Hauptziel“ zu „Anbieter-Zugang“ und bleibt das mindestens 3 s
  so, endet die bisherige Störung und eine neue beginnt (mit Vorlauf). Echte
  Ausfälle verschwinden so nicht mehr in einer langen Störung anderer Art.
- **Dauerhaft gestörte Messpunkte**: Antwortet ein manueller Messpunkt länger als
  2 Minuten gar nicht (z. B. Ping gefiltert, Server aus), wird er als „dauerhaft
  gestört“ geführt (Matrix: dunkelrot), einmal im Ereignisprotokoll vermerkt und
  hält keine Störungen mehr offen. Antwortet er wieder, gibt es den Eintrag
  „wieder erreichbar“.
- **Kommentar pro Messung**: frei editierbares Textfeld im Verlauf (und für die
  laufende Messung oben neben Start/Stop), z. B. „per LAN-Kabel“ oder „nach
  Router-Tausch“. Erscheint in der Sitzungsliste und im HTML-Bericht – so lassen
  sich mehrere Auswertungen vergleichen.
- Fehlerbehebung: „Läuft seit …“ zeigt die richtige Dauer, wenn die App bei
  bereits laufender Messung geöffnet wird (z. B. automatisches Fortsetzen).

## v0.15.0 – 2026-09-27

- **„Möglicher Fehlalarm“**: Widersprechen sich Ping und TCP-Check desselben
  Hosts (z. B. Ping weg, TCP:443 ok), gilt der Host als erreichbar. Das
  ausgefallene Protokoll wird in dieser Sekunde als möglicher Fehlalarm markiert
  (lila gestreift in der Störungs-Matrix, „X s Fehlalarm?“) statt als Verlust.
  Es startet keine Störung, hält keine offen, erzeugt keinen Ereignis-Eintrag
  und keinen Signalton. Damit bleiben dauerhaft gefilterte Pings (z. B. 1.1.1.1)
  ohne Einfluss auf die Auswertung, und echte Ausfälle werden wieder als eigene
  Störungen erkannt.

## v0.14.2 – 2026-09-27

- Lange Störungen sprengen das Layout nicht mehr: Die Seite scrollt nie
  horizontal, gescrollt wird nur innerhalb der Störungs-Matrix bzw. des
  Ereignisprotokolls. Die Zeilenbeschriftungen und Zonen-Überschriften der
  Matrix bleiben beim horizontalen Scrollen links stehen; lange Texte brechen um.
- Störungen: Buttons „alle aufklappen“ / „alle einklappen“.

## v0.14.1 – 2026-09-27

- Fehlerbehebung TCP-Check unter Windows: Eine abgelehnte Verbindung (Port
  geschlossen) wurde fälschlich als Verlust gezählt, weil Windows dafür einen
  eigenen Fehlercode (WSAECONNREFUSED) liefert. Sie zählt jetzt wie vorgesehen
  als „erreichbar“. (Der automatische Windows-Build auf GitHub ist dadurch wieder
  grün.)

## v0.14.0 – 2026-09-27

- Live-Diagramm, Legende nach Zonen:
  - **Klick auf einen Gruppennamen** blendet die ganze Gruppe ein/aus
    (durchgestrichen = alle aus).
  - **◉ je Gruppe** hebt die Gruppe hervor: ihre Linien bleiben kräftig, alle
    anderen werden stark abgeblendet (nochmal klicken zum Aufheben).
    Überfahren des Gruppennamens zeigt das als Vorschau.
  - **Überfahren eines Legenden-Eintrags** hebt genau diese Linie hervor.
- **Überfahren einer Zeile in der Route-Tabelle oder bei den manuellen
  Messpunkten** hebt die zugehörige Linie im Live-Diagramm hervor (in der
  TCP-Spalte die TCP-Linie).

## v0.13.0 – 2026-09-27

- **Vorgeschlagene Ausweichziele**: 1.1.1.1 (Cloudflare), 9.9.9.9 (Quad9) und
  8.8.8.8 (Google) stehen einmalig als manuelle Messpunkte in Zone WAN in der
  Liste – ausgeschaltet, bis man sie aktiviert. Gelöschte Vorschläge lassen sich
  per Button wieder hinzufügen.
- **Ping und TCP-Check je Host getrennt**: Schalter „an/aus“ (misst überhaupt
  bzw. gar nicht, auch nicht gewertet) und daneben ein Häkchen für die Linie im
  Diagramm (nur ein-/ausblenden, die Messung läuft weiter). Ein Host kann auch
  nur per TCP überwacht werden.

## v0.12.0 – 2026-09-27

- **TCP-Check für manuelle Messpunkte** (Spalte „TCP-Check“, Port wählbar,
  Standard 443): zusätzlich zum Ping wird jede Sekunde ein TCP-Verbindungsaufbau
  gemessen (Dauer ≈ eine Round-Trip-Zeit, Verbindung wird sofort geschlossen;
  ohne Admin-Rechte). Abgelehnte Verbindung (Port geschlossen) zählt als
  erreichbar. Eigene gepunktete Linie im Live-Diagramm und im Verlauf, eigene
  Zeile in der Störungs-Matrix, zählt in der Zone des Messpunkts (z. B. als
  Ausweichziel in WAN). Sinnvoll, weil viele Anbieter/Server Pings drosseln
  oder nachrangig behandeln – TCP entspricht echtem Web-Traffic.

## v0.11.0 – 2026-09-27

- **Störungs-Matrix nach Zonen gruppiert** (App und HTML-Bericht): Hops und
  manuelle Messpunkte stehen unter der Überschrift ihrer Zone, in der Reihenfolge
  der Zonenliste.
- **Zonenliste in den Einstellungen (⚙ → Zonen)**: Die Standardzonen (LAN,
  ISP_EDGE, WAN, Nicht gewertet) lassen sich umbenennen, umfärben und
  beschreiben. **Eigene Zonen** (z. B. „VPN-Server“) können hinzugefügt werden;
  „Auswertung“ legt fest, ob sie wie LAN, ISP_EDGE, WAN (Ausweichziel) oder gar
  nicht gewertet werden. Manuelle Messpunkte können jeder Zone zugeordnet werden.
- Diagramm-Legende, Zonen-Karten und Auswahlfelder verwenden die Namen und Farben
  aus der Zonenliste.

## v0.10.1 – 2026-09-27

- Das Zielfeld startet immer mit dem zuletzt verwendeten Ziel (aus der Datenbank,
  bleibt also auch nach Updates erhalten) statt mit 1.1.1.1. Läuft bereits eine
  Messung, wird deren Ziel angezeigt.

## v0.10.0 – 2026-09-27

- **Signaltöne** (⚙ Einstellungen → „Signaltöne“): Ton bei Routenwechsel und pro
  Zone (LAN / ISP_EDGE / WAN) einzeln wählbar bei Latenzspitze, Paketverlust und
  Ausfall. Jede Art hat einen eigenen Klang (▶ zum Anhören), Lautstärke und
  Mindestpause zwischen gleichen Tönen einstellbar. Standardmäßig aus.

## v0.9.0 – 2026-09-27

- **Einstellungen (⚙ oben rechts)**, dauerhaft gespeichert und sofort wirksam:
  - Latenzspitzen **je Zone**: Faktor × üblicher Wert, Mindestabstand in ms und
    optional eine feste Grenze in ms (Standard: LAN 3× / +15 ms / 100 ms,
    ISP_EDGE 2× / +20 ms / 150 ms, WAN 2× / +20 ms / 250 ms).
  - Ausfall ab N Sekunden, Timeout, Vor-/Nachlauf von Störungen,
    Routenprüf-Intervall, Grenze „hohe Latenz“ im Bericht.
  - Farbschema, Standard-Zeitraum des Live-Diagramms.
- **Dark Mode**: automatisch nach Windows-Einstellung oder fest hell/dunkel.
- **Vereinfachungen**:
  - Standby wird während einer Messung verhindert (abschaltbar), damit keine
    Lücken im Nachweis entstehen.
  - Optional: beim Start der App die letzte Messung automatisch fortsetzen.
  - Optional: alte Messungen nach N Tagen automatisch löschen.
  - Einzelne Messungen im Verlauf löschen (🗑).
- Farbpalette für dunklen Hintergrund angepasst (kein Schwarz/Dunkelblau mehr).

## v0.8.0 – 2026-09-27

- Live-Latenzverlauf mit wählbarem Zeitraum: 5, 30, 60 oder 90 Minuten oder die
  gesamte Messung (Standard 30 min, wird gemerkt). Das Diagramm läuft mit den
  neuesten Werten mit; ein Zoom per Ziehen hält die Ansicht fest, Doppelklick
  kehrt zum gewählten Zeitraum zurück.

## v0.7.0 – 2026-09-27

- **Manuelle Messpunkte fließen in die Auswertung ihrer Zone ein**: LAN/ISP_EDGE-
  Punkte gehören zu diesem Bereich (fällt z. B. die Fritz!Box zusammen mit allen
  Zielen aus, wird die Störung dem Heimnetz zugeordnet), WAN-Punkte sind
  Ausweichziele. Neue Zone **„keine (nicht werten)“**: wird gemessen und
  angezeigt, aber nicht ausgewertet.
- **Tooltips (?)** an allen Kennzahlen, Spalten, Karten und Buttons mit
  Erklärungen für Laien.
- **Diagramm-Legende nach Zonen gruppiert** (LAN / ISP_EDGE / WAN / nicht gewertet)
  mit Messwerten unter dem Mauszeiger; Klick blendet Linien ein/aus.
- **Deutlich unterscheidbare Farben**: jede Linie hat eine eigene Farbe (kein Rot,
  das bleibt für Verluste), identisch mit dem Punkt in der Tabelle; manuelle
  Messpunkte gestrichelt.
- **Logarithmische Skala** automatisch, sobald Werte von wenigen ms bis mehrere
  100 ms vorkommen (umschaltbar: automatisch / linear / logarithmisch).

## v0.6.0 – 2026-09-27

- **Störungen mit Sekunden-Matrix** (live, im Verlauf und im HTML-Bericht): Sobald
  das Ziel, ein Ausweichziel oder ein manuelles Gerät nicht antwortet, wird eine
  Störung aufgezeichnet – für **jeden Hop und jeden manuellen Host** Sekunde für
  Sekunde (antwortet / langsam / keine Antwort, mit RTT als Tooltip), inklusive
  10 s davor und 5 s danach. So sieht man auf einen Blick, wer während des
  Ausfalls nicht mehr geantwortet hat.
- **Manuelle Hosts in Zone WAN gelten als Ausweichziele** (z. B. 1.1.1.1, eigener
  VPS). Jede Störung wird eingeordnet: Heimnetz · Anbieter-Zugang · Anbieter-Netz
  (alle Ziele weg, Zugang antwortet) · nur Hauptziel · nur Ausweichziel · nur
  LAN-Gerät.
- **Diagnose basiert jetzt auf diesen Störungen**: Anzahl und Dauer je Art, jeweils
  längste Störung mit Zeitpunkt, klare Aussage und Empfehlung.
- Störungen werden im Latenzdiagramm als farbige Bereiche hinterlegt; „im Diagramm
  zeigen“ zoomt direkt auf eine Störung.
- Messpunkt-Karten zeigen zusätzlich „davon echt (bis Ziel)“: nur Verluste, bei
  denen auch alle folgenden Messpunkte ausfielen – der Rest ist ICMP-Drosselung.

## v0.5.0 – 2026-09-27

- **Manuelle Messpunkte** (Route-Karte → „Weitere Messpunkte“): beliebige IPs oder
  Hostnamen zusätzlich jede Sekunde anpingen, z. B. ein zweiter Router/Modem
  (wie 192.168.0.1, falls er aus der Route verschwindet), Repeater, NAS oder
  andere Server. Mit Name und Zone (Auto: private Adresse → LAN, sonst WAN), an-
  und abschaltbar, dauerhaft gespeichert; eigene Linie im Diagramm und im Verlauf.
- Ereignisprotokoll: „Keine Antwort von Gerät …“ mit Dauer und dem Zustand des
  Internetwegs zur selben Zeit (Heimrouter / Ziel erreichbar oder nicht). Die
  Diagnose listet diese Geräte separat auf, ohne sie in die Bewertung des
  Internetwegs einzurechnen.

## v0.4.0 – 2026-09-27

- **Ereignisprotokoll** (live, im Verlauf, im HTML-Bericht und als CSV-Export):
  jede Messsekunde wird über alle Messpunkte gemeinsam ausgewertet.
  - Paketverlust wird dem ersten Hop zugeordnet, ab dem alle weiteren Messpunkte
    bis zum Ziel keine Antwort bekamen (mit Hop, Adresse, Name, Zone, Beginn, Ende,
    Dauer und dem letzten noch funktionierenden Hop davor).
  - Verluste nur an einem Zwischen-Hop bei erreichbarem Ziel werden als „harmlos“
    (ICMP-Drosselung) protokolliert und nicht gewertet.
  - Latenzspitzen (mehr als doppelt so langsam wie normal und mindestens 20 ms
    darüber) werden ebenso dem Hop zugeordnet, ab dem sie bis zum Ziel auftreten.
  - Ausfälle mit Beginn und Ende/Dauer.
  - Routenwechsel mit genauer Änderung pro Hop (alt → neu), neuer Route und
    geänderten Messpunkten; bei Beginn und Ende eines Ausfalls wird die Route
    sofort neu geprüft.
  - Start/Ende der Messung und Zonen-Änderungen durch den Benutzer.
- **Diagnose für Laien** (live als Zwischenstand, im Verlauf und oben im
  HTML-Bericht): klare Aussage, wo das Problem liegt (Heimnetz, Anbieter oder
  Internet/Ziel), mit Sicherheit der Einschätzung, Begründung in Zahlen, dem Hop,
  ab dem die Störungen meist beginnen, und konkreten Handlungsempfehlungen.
- Hinweis: Sitzungen aus Versionen vor v0.4.0 haben kein Ereignisprotokoll.

## v0.3.0 – 2026-09-27

- Live-Diagramm und Route-Tabelle passen zusammen: eine Linie pro Tabellenzeile,
  in Routen-Reihenfolge (TTL), mit derselben Farbe wie der Punkt in der Tabelle.
  Zonen-Messpunkte behalten ihre Zonenfarbe, alle anderen Hops bekommen eine
  eindeutige Farbe (keine doppelten Grautöne mehr).
- Checkbox „Diagramm“ = Linie sichtbar. Sie funktioniert jetzt auch für
  Zonen-Messpunkte (vorher gesperrt) und bleibt mit der Legende synchron: Ein- und
  Ausblenden über die Legende ändert die Checkbox und umgekehrt.
- Hops lassen sich optional benennen (Spalte „Name“). Der Name wird pro Adresse
  gespeichert und erscheint in der Diagramm-Legende („Hop 2 · Heimrouter (LAN)“),
  im Verlauf und im HTML-Bericht.
- Die Auswahl der angehakten Hops wird jetzt in der Datenbank gespeichert und
  übersteht damit auch Versionswechsel.

## v0.2.1 – 2026-09-27

- Live-Latenzverlauf bleibt stehen, solange die Maus über dem Diagramm ist
  (Hinweis „Angehalten“); ein per Ziehen gesetzter Zoom bleibt bei neuen Daten
  erhalten (Doppelklick setzt zurück).
- Über die Legende ausgeblendete Linien bleiben ausgeblendet, wenn weitere Hops
  per Checkbox hinzukommen; ihre Verlust-Spur wird ebenfalls ausgeblendet.
- Hops, die bereits als Zonen-Messpunkt dienen (z. B. Heimrouter als LAN), zeigen
  in der Route-Tabelle eine aktive, gesperrte Checkbox in Zonenfarbe und werden
  nicht doppelt gemessen.
- Legende zeigt „–“ statt „Verlust“, wenn die Maus nicht über dem Diagramm ist.

## v0.2.0 – 2026-09-27

- Jeder antwortende Hop aus dem Traceroute kann per Häkchen als eigene Linie in
  den Live-Latenzverlauf aufgenommen werden (1 Probe/s; direkter Ping, sonst
  TTL-begrenzt). Die Auswahl bleibt über Neustarts erhalten.
- Zone pro Hop in der Route-Tabelle änderbar (Auto / LAN / ISP_EDGE / WAN). Die
  Auswahl wird pro Hop-Adresse dauerhaft gespeichert und sofort angewendet.
- Hop-Messreihen werden gespeichert und im Verlauf-Diagramm sowie in der CSV
  angezeigt.
- Versionsanzeige in der GUI (Kopfzeile und Fenstertitel), EXE mit
  Versions-Suffix.

## v0.1.0 – 2026-09-27

Erste Windows-Version.

- Unprivilegierte ICMP-Messung über `IcmpSendEcho2` (keine Admin-Rechte).
- Traceroute mit automatischer Einteilung in LAN / ISP_EDGE (inkl. CGNAT) / WAN.
- 1 Probe/s je Zone; Ausfall (≥ 3 Verluste in Folge) wird der ersten Zone
  zugeordnet, ab der alle weiteren Zonen nicht antworten; ICMP-gedrosselte
  Zwischen-Hops zählen nicht als Ausfall; Routenprüfung alle 5 Minuten.
- RFC-3550-Jitter, P50/P95/P99, Spitzenwerte, Ausfallzeiten.
- SQLite-Speicherung (WAL), Svelte-GUI mit uPlot-Live-Diagramm.
- Export als HTML-Bericht (druckbar als PDF) und CSV mit SHA-256 der Rohdaten.
