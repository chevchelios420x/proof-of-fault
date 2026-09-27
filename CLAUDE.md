# Arbeitsregeln für dieses Repo

- Direkt auf `main` committen und pushen (keine Feature-Branches).
- Jede Änderung, die an den Nutzer geht, bekommt eine neue Version:
  1. `VERSION` erhöhen (SemVer, `0.MINOR.PATCH`: Funktion → MINOR, Bugfix → PATCH).
  2. Eintrag oben in `RELEASES.md` ergänzen.
  3. `scripts/build-windows.sh` ausführen und nur die neue
     `build/bin/proof-of-fault-vX.Y.Z.exe` committen (alte EXE per `git rm` entfernen).
  4. Link in `README.md` auf die neue EXE aktualisieren.
- Vor dem Push: `go vet ./...` und `go test ./internal/...`.
