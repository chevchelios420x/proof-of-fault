#!/usr/bin/env bash
# Builds the Windows EXE and the Linux binary from the version in VERSION:
#   build/bin/proof-of-fault-vX.Y.Z.exe
#   build/bin/proof-of-fault-vX.Y.Z-linux-amd64
# Pure Go (no CGO), so both are cross-compiled from any OS.
set -euo pipefail
cd "$(dirname "$0")/.."
v=$(tr -d '[:space:]' < VERSION)

(cd frontend && npm ci --no-audit --no-fund >/dev/null && npx vite build >/dev/null)

# Windows resources: icon, file version, manifest (no admin rights).
rm -f rsrc_windows_*.syso
go run github.com/tc-hib/go-winres@v0.3.3 simply \
  --icon build/appicon.png --manifest gui --admin=false \
  --product-name proof-of-fault --file-description "proof-of-fault – Nachweis von Internet-Störungen" \
  --product-version "$v.0" --file-version "$v.0" --arch amd64

mkdir -p build/bin
rm -f build/bin/proof-of-fault-v*.exe build/bin/proof-of-fault-v*-linux-amd64
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui" -o "build/bin/proof-of-fault-v$v.exe" .
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "build/bin/proof-of-fault-v$v-linux-amd64" .
rm -f rsrc_windows_*.syso
echo "build/bin/proof-of-fault-v$v.exe"
echo "build/bin/proof-of-fault-v$v-linux-amd64"
