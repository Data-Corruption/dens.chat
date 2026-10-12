#!/usr/bin/env bash
# The M5.1 check's den: two channels whose messages are three days old, as
# far as the den knows, with retention still off.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
random=$(channel random)
send bob "$general" "old one, from bob" >/dev/null
send alice "$general" "old two, from alice" >/dev/null
send alice "$random" "old in random, hey @bob" >/dev/null
# Backdated with the den stopped, which reads them back as it starts.
stop m2a
python3 - "$HOME/.local/share/dens-dev/m2a/data/db/app.db" <<'PY'
import sqlite3, sys
db = sqlite3.connect(sys.argv[1])
db.execute("UPDATE den_messages SET created_at = created_at - 3*86400000")
db.commit()
PY
start_den
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "RANDOM=$random"
