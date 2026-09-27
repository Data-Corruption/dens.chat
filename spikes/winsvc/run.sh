#!/usr/bin/env bash
# Runs the Windows spike from WSL. Builds the binary into a work directory in
# the Windows user's temp folder, starts the elevated orchestrator (one UAC
# prompt), runs the desktop-user client while the service is up, then lets the
# orchestrator test recovery and stop, and remove everything. Results are
# copied to out/spikes/windows/<run>/.
set -euo pipefail
exec </dev/null

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
ps() { powershell.exe -NoProfile -NonInteractive -Command "$1" | tr -d '\r'; }

run=$(date +%Y%m%d-%H%M%S)
win_temp=$(ps '[IO.Path]::GetTempPath()')
work_win="${win_temp}dens-spike-$run"
work_wsl=$(wslpath -u "$work_win")
mkdir -p "$work_wsl"
(cd "$here/.." && GOOS=windows GOARCH=amd64 go build -trimpath -o "$work_wsl/dens-spike.exe" ./winsvc)
user_sid=$(ps '[Security.Principal.WindowsIdentity]::GetCurrent().User.Value')
echo "work directory: $work_win"
echo "desktop user:   $user_sid"

echo "starting the elevated orchestrator; approve the UAC prompt for dens-spike.exe"
ps "Start-Process -FilePath '$work_win\\dens-spike.exe' -ArgumentList 'orchestrate','-user','$user_sid','-work','$work_win' -Verb RunAs -WindowStyle Hidden"

wait_for() {
	local deadline=$((SECONDS + $2))
	until [ -e "$work_wsl/$1" ] || [ -e "$work_wsl/failed" ] || [ -e "$work_wsl/done" ]; do
		if ((SECONDS >= deadline)); then
			echo "timed out waiting for $1"
			return 1
		fi
		sleep 1
	done
}

if wait_for installed 900 && [ -e "$work_wsl/installed" ]; then
	echo "service installed; running the client as the desktop user"
	"$work_wsl/dens-spike.exe" client -work "$work_win" >"$work_wsl/client.json" || echo "client exited $?"
else
	echo "install did not complete; see orchestrate.log"
fi
touch "$work_wsl/uninstall.now"
wait_for "done" 300 || true

dest="$repo/out/spikes/windows/$run"
mkdir -p "$dest"
cp "$work_wsl"/*.json "$work_wsl"/*.log "$dest"/ 2>/dev/null || true
echo "results: $dest"
