#!/usr/bin/env bash
# Runs run.sh in a fresh unprivileged Incus container per distro and collects
# the results under out/spikes/linux/<run>/. Images are the local aliases
# dens-spike/<distro>; copy them first, for example:
#   incus image copy images:fedora/44 local: --alias dens-spike/fedora-44
# PRIVILEGED=true runs privileged containers. systemd before 256 can't create
# its credential host secret inside an unprivileged container.
set -euo pipefail
# incus init and exec read stdin when it isn't a terminal.
exec </dev/null

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
distros=${DISTROS:-"fedora-43 fedora-44 debian-12 debian-13 ubuntu-noble ubuntu-resolute archlinux-current"}
privileged=${PRIVILEGED:-false}
run_dir="$repo/out/spikes/linux/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$run_dir"

(cd "$here/.." && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$run_dir/dens-spike" ./linuxsvc)

for distro in $distros; do
	name="dens-spike-$distro"
	log="$run_dir/$distro.log"
	echo "== $distro"
	incus delete -f "$name" >/dev/null 2>&1 || true
	incus init "dens-spike/$distro" "$name" -c security.privileged="$privileged" -c security.nesting=true >/dev/null
	incus start "$name"
	timeout 180 incus exec "$name" -- systemctl is-system-running --wait >/dev/null 2>&1 || true
	incus file push --quiet "$run_dir/dens-spike" "$name/root/dens-spike" --mode 0755
	incus file push --quiet "$here/run.sh" "$name/root/run.sh" --mode 0755
	if timeout 600 incus exec "$name" -- /root/run.sh >"$log" 2>&1; then
		echo "   run.sh finished"
	else
		echo "   run.sh exited $?"
	fi
	mkdir -p "$run_dir/$distro"
	incus file pull --quiet -r "$name/root/results/" "$run_dir/$distro/" >/dev/null 2>&1 || echo "   no results"
	incus delete -f "$name" >/dev/null
done
echo "results: $run_dir"
