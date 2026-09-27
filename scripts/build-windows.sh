#!/usr/bin/env bash
# Builds build/bin/proof-of-fault-vX.Y.Z.exe from the version in VERSION and
# keeps wails.json (Windows file version) in sync.
set -euo pipefail
cd "$(dirname "$0")/.."
v=$(tr -d '[:space:]' < VERSION)
sed -i -E "s/(\"productVersion\": \")[^\"]*/\1$v/" wails.json
wails build -platform windows/amd64 -clean -o "proof-of-fault-v$v.exe"
echo "build/bin/proof-of-fault-v$v.exe"
