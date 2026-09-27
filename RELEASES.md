# Releases

Versionsschema: `vMAJOR.MINOR.PATCH` (Semantic Versioning). Solange die App in
Entwicklung ist, bleibt MAJOR bei `0`: neue Funktionen erhöhen MINOR, reine
Fehlerbehebungen PATCH. Die Version steht in `VERSION`; die Windows-EXE liegt als
`build/bin/proof-of-fault-vX.Y.Z.exe` im Repo und wird mit
`scripts/build-windows.sh` gebaut.

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
