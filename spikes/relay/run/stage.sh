#!/usr/bin/env bash
# Stages the Relay spike for the machines that aren't here: the probe for
# Windows and Linux with their scripts in VMware's shared folder, where
# the Windows machine and its guests both find them and leave their logs,
# and beside them the package for the member with the KVM.
#
# Usage: spikes/relay/run/stage.sh
#
# STAGE is the shared folder's directory for it.

set -euo pipefail
cd "$(dirname "$0")/../.."
STAGE=${STAGE:-/mnt/c/Users/datac/dens-vm/relay}
mkdir -p relay/bin "$STAGE/logs" "$STAGE/kvm/relay-kvm"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o relay/bin/relayprobe-linux-amd64 ./relay
GOOS=windows GOARCH=amd64 go build -trimpath -o relay/bin/relayprobe-windows-amd64.exe ./relay
cp relay/bin/relayprobe-linux-amd64 "$STAGE/relayprobe-linux-amd64"
cp relay/bin/relayprobe-windows-amd64.exe "$STAGE/relayprobe.exe"
cp relay/stage/guest.sh relay/stage/link.ps1 relay/stage/service.ps1 relay/stage/capture.ps1 "$STAGE/"
sed 's/$/\r/' relay/stage/STEPS.txt >"$STAGE/STEPS.txt"
cp relay/bin/relayprobe-linux-amd64 relay/stage/kvm/README.txt relay/stage/kvm/nixos.sh "$STAGE/kvm/relay-kvm/"
cp relay/bin/relayprobe-windows-amd64.exe "$STAGE/kvm/relay-kvm/relayprobe.exe"
# Windows' own line endings for what cmd.exe and Notepad read.
sed 's/$/\r/' relay/stage/kvm/windows.cmd >"$STAGE/kvm/relay-kvm/windows.cmd"
sed 's/$/\r/' relay/stage/kvm/README.txt >"$STAGE/kvm/relay-kvm/README.txt"
(cd "$STAGE/kvm" && rm -f relay-kvm.zip && python3 -I -c '
import os, zipfile
with zipfile.ZipFile("relay-kvm.zip", "w", zipfile.ZIP_DEFLATED) as z:
    for name in sorted(os.listdir("relay-kvm")):
        info = zipfile.ZipInfo.from_file(os.path.join("relay-kvm", name), "relay-kvm/" + name)
        info.external_attr = (0o755 if name.endswith((".sh", "amd64")) else 0o644) << 16
        with open(os.path.join("relay-kvm", name), "rb") as f:
            z.writestr(info, f.read(), zipfile.ZIP_DEFLATED)
')
echo "Staged in $STAGE:"
ls -la "$STAGE" "$STAGE/kvm"
