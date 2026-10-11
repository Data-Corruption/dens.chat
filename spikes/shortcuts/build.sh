#!/bin/sh
# Builds the shortcuts spike for Windows, with the manifest, into
# out/spikes/shortcuts, and prints the manifest the .exe carries.
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
out="$root/out/spikes/shortcuts"
mkdir -p "$out"
cd "$root/spikes"
go run ./shortcuts/winres -arch amd64 -out shortcuts/rsrc_windows_amd64.syso
GOOS=windows GOARCH=amd64 go build -o "$out/dens-shortcuts-spike.exe" ./shortcuts
go run ./shortcuts/winres -check "$out/dens-shortcuts-spike.exe"
