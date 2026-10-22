#!/bin/sh
# Gathers what the spike found on this VM into the shared folder's
# results/<system>-<desktop>-<session>/: versions, handlers, the app's
# remote, permissions and autostart entry, GNOME's background apps, the
# probe's log and secrets' digests, and the session journal's portal lines.
set -u
here=$(cd "$(dirname "$0")" && pwd)
desktop=$(printf '%s' "${XDG_CURRENT_DESKTOP:-unknown}" | tr ':A-Z' '-a-z')
# shellcheck source=/dev/null
name=$(. /etc/os-release && printf '%s-%s' "$ID" "${VERSION_ID:-rolling}")-$desktop-${XDG_SESSION_TYPE:-unknown}
out="$here/../results/$name"
mkdir -p "$out"
{
  echo "== system"
  grep PRETTY_NAME /etc/os-release
  uname -r
  echo "XDG_CURRENT_DESKTOP=${XDG_CURRENT_DESKTOP:-} XDG_SESSION_TYPE=${XDG_SESSION_TYPE:-}"
  echo "== versions"
  flatpak --version
  gnome-shell --version 2>/dev/null
  plasmashell --version 2>/dev/null
  gnome-software --version 2>/dev/null
  plasma-discover --version 2>/dev/null
  { rpm -qa 2>/dev/null || dpkg-query -W -f '${Package} ${Version}\n' 2>/dev/null; } |
    grep -iE '^(flatpak|xdg-desktop-portal|gnome-keyring|kf6-kwallet|kwallet|libkf6wallet|ksecretd|oo7|gnome-software|plasma-discover|ostree|libostree|gnupg|gpgme|libgpgme|kf6-kcoreaddons|libkf6coreaddons|xdg-utils)' | sort
  echo "== handlers"
  for t in x-scheme-handler/flatpak+https application/vnd.flatpak.ref application/vnd.flatpak.repo x-scheme-handler/appstream; do
    echo "$t: $(xdg-mime query default "$t" 2>&1)"
  done
  echo "== flatpak"
  flatpak remotes --show-details
  flatpak list --app --columns=application,version,branch,origin,installation
  flatpak info chat.dens.Spike 2>&1
  flatpak ps
  flatpak history 2>/dev/null | grep -i spike | tail -20
  echo "== the service's unit and session"
  flatpak ps --columns=instance,pid,child-pid,application,active,background 2>&1
  for pid in $(flatpak ps --columns=child-pid,application 2>/dev/null | awk '$2 == "chat.dens.Spike" { print $1 }'); do
    echo "child $pid: $(cat /proc/"$pid"/cgroup 2>&1)"
  done
  systemctl --user list-units --all --no-pager 'app-flatpak-chat.dens.Spike*' 'app-*chat.dens.Spike*' 2>&1
  loginctl show-user "$(id -u)" -p Linger -p State 2>&1
  busctl get-property org.freedesktop.login1 /org/freedesktop/login1 org.freedesktop.login1.Manager UserStopDelayUSec KillUserProcesses 2>&1
  echo "== permissions"
  flatpak permission-show chat.dens.Spike 2>&1
  echo "== autostart entry"
  cat ~/.config/autostart/chat.dens.Spike.desktop 2>&1
  echo "== background apps"
  busctl --user get-property org.freedesktop.background.Monitor /org/freedesktop/background/monitor \
    org.freedesktop.background.Monitor BackgroundApps 2>&1
  echo "== portal configuration"
  ls /usr/share/xdg-desktop-portal/portals/ 2>&1
  for f in /usr/share/xdg-desktop-portal/*portals.conf ~/.config/xdg-desktop-portal/*.conf; do
    [ -f "$f" ] && { echo "-- $f"; cat "$f"; }
  done
} >"$out/system.txt" 2>&1
for f in spike.log secrets.txt; do
  cp ~/.var/app/chat.dens.Spike/data/"$f" "$out/" 2>/dev/null
done
journalctl --user -b --no-pager -o short-precise 2>/dev/null |
  grep -iE 'portal|flatpak|chat\.dens\.Spike|kwallet|ksecretd|keyring|secret|oo7' | tail -500 >"$out/journal.txt"
echo "Collected in $out"
