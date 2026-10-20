# Arbeitsregeln für dieses Repo

- Direkt auf `main` committen und pushen (keine Feature-Branches).
- Jede Änderung, die an den Nutzer geht, bekommt eine neue Version:
  1. `VERSION` erhöhen (SemVer, `0.MINOR.PATCH`: Funktion → MINOR, Bugfix → PATCH).
  2. Eintrag oben in `RELEASES.md` ergänzen.
  3. `scripts/build-windows.sh` ausführen und nur die neuen
     `build/bin/proof-of-fault-vX.Y.Z.exe` und `build/bin/proof-of-fault-vX.Y.Z-linux-amd64`
     committen (alte Dateien per `git rm` entfernen).
  4. Links in `README.md` (Download-Tabelle und Linux-Beispiel) auf die neue Version aktualisieren.
- Vor dem Push: `go vet ./...` und `go test ./internal/...`.
- Keine Git-Tags anlegen.
