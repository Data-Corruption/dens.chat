# The Go these scripts run: go.mod's, whatever Go is on PATH, since CI and
# releases build with go.mod's and the generators' checks compare what it
# formats byte for byte. GOTOOLCHAIN makes the go on PATH run that version,
# fetched into the module cache the first time. Sourced by every script
# that runs go; where no go.mod is beside the scripts, as in the release
# host's container that publishes the Flatpak, it changes nothing.

go_toolchain_init() {
  local mod version
  mod="$(dirname "${BASH_SOURCE[0]}")/../../go.mod"
  [[ -f "$mod" ]] || return 0
  # tr: a Windows checkout may give go.mod CRLF.
  version=$(tr -d '\r' <"$mod" | sed -n 's/^go \([0-9]*\.[0-9]*\.[0-9]*\)$/\1/p')
  if [[ -z "$version" ]]; then
    printf "error: go.mod's go line isn't a full version such as 1.26.7, which GOTOOLCHAIN needs\n" >&2
    return 1
  fi
  export GOTOOLCHAIN="go$version"
}
go_toolchain_init

# go_fmt runs gofmt from go.mod's Go: the gofmt on PATH stays PATH's Go's,
# whose formatting can differ.
go_fmt() {
  "$(go env GOROOT)/bin/gofmt" "$@"
}
