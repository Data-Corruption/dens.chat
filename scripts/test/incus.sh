# Incus helpers shared by the e2e harnesses. Source it from the repository
# root; it defines functions and the INCUS command, and runs nothing else.

# distro_spec NAME prints "canonical-name image privileged". systemd before
# 256 can't create its credential host secret in an unprivileged container.
distro_spec() {
  case "$1" in
    debian-12) echo "debian-12 debian/bookworm true" ;;
    debian|debian-13) echo "debian-13 debian/trixie false" ;;
    ubuntu-24.04) echo "ubuntu-24.04 ubuntu/noble true" ;;
    ubuntu|ubuntu-26.04) echo "ubuntu-26.04 ubuntu/resolute false" ;;
    fedora-43) echo "fedora-43 fedora/43 false" ;;
    fedora|fedora-44) echo "fedora-44 fedora/44 false" ;;
    arch) echo "arch archlinux/current false" ;;
    *) return 1 ;;
  esac
}

# incus_setup sets INCUS to a command with admin access to the local
# daemon: directly through incus-admin, or through passwordless sudo.
incus_setup() {
  command -v incus >/dev/null 2>&1 || { echo "error: incus is required" >&2; exit 1; }
  INCUS=(incus)
  if ! incus info >/dev/null 2>&1 || { [[ "$(id -u)" != 0 ]] && [[ " $(id -nG) " != *" incus-admin "* ]]; }; then
    if command -v sudo >/dev/null 2>&1 && sudo -n incus info >/dev/null 2>&1; then
      INCUS=(sudo -n incus)
    else
      echo "error: the harness needs admin access to a local Incus daemon" >&2
      echo "initialize it with 'sudo incus admin init --minimal' and join incus-admin" >&2
      exit 1
    fi
  fi
}

wait_for_boot() {
  local name=$1 tries=0
  while (( tries < 480 )); do
    # shellcheck disable=SC2016 # evaluated in the guest
    if "${INCUS[@]}" exec "$name" -- sh -c '
      state=$(systemctl is-system-running 2>/dev/null || :)
      [ "$state" = running ] || [ "$state" = degraded ] || exit 1
      ! command -v ip >/dev/null 2>&1 || ip route show default 2>/dev/null | grep -q .
    ' >/dev/null 2>&1; then
      return 0
    fi
    tries=$((tries + 1))
    sleep 0.25
  done
  echo "error: timed out waiting for $name to boot" >&2
  return 1
}

# launch_container DISTRO NAME starts a fresh container for a distro and
# waits for it to boot. A local image alias dens-e2e/<distro> takes
# precedence over the images: remote.
launch_container() {
  local requested=$1 name=$2 distro image privileged
  read -r distro image privileged < <(distro_spec "$requested") || return
  if "${INCUS[@]}" image info "dens-e2e/$distro" >/dev/null 2>&1; then
    image="dens-e2e/$distro"
  else
    image="images:$image"
  fi
  echo ">> Launching $name from $image (privileged=$privileged)"
  local config=(-c security.privileged="$privileged" -c security.nesting=true)
  if [[ "$privileged" == true ]]; then
    # A privileged container gets the host kernel's binfmt_misc table, which
    # systemd empties at shutdown; on WSL that stops Windows programs from
    # running until WSL restarts. Mount it read-only in the container.
    config+=(-c "raw.lxc=lxc.mount.entry = /proc/sys/fs/binfmt_misc proc/sys/fs/binfmt_misc none rbind,ro,create=dir,optional 0 0")
  fi
  "${INCUS[@]}" init "$image" "$name" "${config[@]}" >/dev/null || return
  "${INCUS[@]}" start "$name" || return
  wait_for_boot "$name"
}

# push_release NAME DIR copies a fixture release to /release in a container.
push_release() {
  local name=$1 dir=$2
  "${INCUS[@]}" file push -q -r "$dir" "$name/" || return
  if [[ "$(basename "$dir")" != release ]]; then
    "${INCUS[@]}" exec "$name" -- mv "/$(basename "$dir")" /release || return
  fi
  "${INCUS[@]}" exec "$name" -- chown -R 0:0 /release
}
