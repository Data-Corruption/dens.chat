#!/usr/bin/env bash
# Sets up the browser checks' Incus container, or brings an existing one up
# to date: Debian 13 with Chromium, Firefox ESR, Xvfb and PulseAudio, a user
# tester, and in tester's home this branch's development binary, the voice
# probe, the pinned Node.js, the media the checks send, and this directory.
# Then run a check with run.sh.
#
# The checks drive real browsers through every milestone's page features,
# from M2's calls on. CI doesn't run them: they're the check before each
# manual test, run by hand, and they take minutes each.
#
# Usage: scripts/test/browser/setup.sh [--no-build]
#   --no-build  push the binaries already built instead of building them
#
# Needs a local Incus daemon with access to the images: remote, as the
# lifecycle e2e does. CONTAINER names the container (dens-browsers).

set -euo pipefail
cd "$(dirname "$0")/../../.."

CONTAINER=${CONTAINER:-dens-browsers}
build=true
case ${1:-} in
    --no-build) build=false ;;
    "") ;;
    *) echo "usage: $0 [--no-build]" >&2; exit 2 ;;
esac

as_tester() {
    incus exec "$CONTAINER" --user 1000 --group 1000 --cwd /home/tester \
        --env HOME=/home/tester --env XDG_RUNTIME_DIR=/run/user/1000 -- "$@"
}

# push FILE DEST copies a file into tester's home. A binary a running
# instance holds can't be written over, so it goes in beside and moves.
push() {
    incus file push --quiet --uid 1000 --gid 1000 --mode "${3:-0644}" "$1" "$CONTAINER/home/tester/$2.new"
    incus exec "$CONTAINER" -- mv -f "/home/tester/$2.new" "/home/tester/$2"
}

if ! incus info "$CONTAINER" >/dev/null 2>&1; then
    image=images:debian/13
    incus image info dens-e2e/debian-13 >/dev/null 2>&1 && image=dens-e2e/debian-13
    echo "Creating $CONTAINER from $image ..."
    incus launch "$image" "$CONTAINER"
    for _ in $(seq 60); do
        incus exec "$CONTAINER" -- getent hosts deb.debian.org >/dev/null 2>&1 && break
        sleep 1
    done
fi
incus info "$CONTAINER" | grep -q '^Status: RUNNING' || incus start "$CONTAINER"

# What the container needs, which each run sees to.
packages=(chromium firefox-esr xvfb xauth pulseaudio pulseaudio-utils dbus-user-session python3 curl ca-certificates
    fonts-dejavu-core fonts-liberation espeak-ng procps iptables sudo)
if ! incus exec "$CONTAINER" -- dpkg-query -W "${packages[@]}" >/dev/null 2>&1; then
    incus exec "$CONTAINER" -- sh -c "apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends ${packages[*]} >/dev/null"
fi
incus exec "$CONTAINER" -- sh -c 'id tester >/dev/null 2>&1 || useradd -m -u 1000 -s /bin/bash tester'
# The calls check cuts the browsers' UDP to the den's media port, and this
# is all tester may run as root.
incus exec "$CONTAINER" -- sh -c 'echo "tester ALL=(root) NOPASSWD: /usr/sbin/iptables" >/etc/sudoers.d/tester-iptables && chmod 0440 /etc/sudoers.d/tester-iptables'
# The instances' control sockets, PulseAudio's and D-Bus's live in the
# runtime directory, which lingering keeps without a login.
incus exec "$CONTAINER" -- loginctl enable-linger tester
for _ in $(seq 30); do
    incus exec "$CONTAINER" -- test -d /run/user/1000 && break
    sleep 1
done

if $build; then
    ./scripts/build.sh
    mkdir -p out/browser
    go test -c -o out/browser/voice-probe ./internal/platform/http/client
fi
node=$(./scripts/vendor.sh node | sed -n 's/^node=//p')
media=$(./scripts/vendor.sh browser-media | sed -n 's/^browser-media=//p')

echo "Pushing to $CONTAINER ..."
push out/linux-amd64 dens-dev 0755
push out/browser/voice-probe voice-probe 0755
push "$node" node 0755
as_tester mkdir -p photos videos browser
for file in iphone-5.jpg pixel-2.jpg; do push "$media/$file" "photos/$file"; done
push internal/media/ffmpeg/testdata/rotated.heic photos/iphone-xr.heic
for file in iphone-x-h264.mov iphone-12-pro-hevc.mov; do push "$media/$file" "videos/$file"; done
push internal/media/ffmpeg/testdata/grain-60fps.mp4 videos/grain-60fps.mp4
push internal/media/ffmpeg/testdata/vp9.webm videos/vp9.webm
push internal/media/ffmpeg/testdata/late-sound.mp4 videos/late-sound.mp4
push internal/media/ffmpeg/testdata/with-gps.mp4 clip.mp4
for file in scripts/test/browser/*; do push "$file" "browser/$(basename "$file")" 0755; done

# Speech for the fake microphones, about a minute of it, and a photo that
# isn't from a phone.
as_tester sh -c '[ -s speech.wav ] || espeak-ng -s 150 -w speech.wav -f browser/speech.txt'
as_tester sh -c '[ -s photo.jpg ] || ./node browser/make-photo.mjs photo.jpg'
echo "Ready: run a check with scripts/test/browser/run.sh CHECK [BROWSER...]"
