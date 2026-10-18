#!/usr/bin/env bash
# The M9.1 check's instance: alice's, made with no den port, so it hosts
# nothing until she turns hosting on, paired and with its local password.
# Its den listens on the default port, 8485, once it does.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
for name in m2a m2b m2c; do stop "$name"; done
rm -rf "$HOME/.local/share/dens-dev"/m2? "$HOME"/jar-m2?
start m2a --port 18484 --media-udp-port 17881 --media-tcp-port 17882
pair alice
