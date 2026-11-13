#!/usr/bin/env bash
# Sums up what each page's worklet counted in a probe's log: for each page
# by name, how it was shown, the buffer it kept, and its worst moments.
#
# Usage: spikes/relay/run/players-summary.sh LOG

set -euo pipefail
python3 -I - "$1" <<'PY'
import json, re, sys
pages = {}
for line in open(sys.argv[1], errors='replace'):
    m = re.search(r' page  name=(\S+) (\{.*\})\s*$', line)
    if not m:
        continue
    try:
        d = json.loads(m.group(2))
    except ValueError:
        continue
    p = pages.setdefault(m.group(1), {'play': [], 'ctx': [], 'timer': []})
    if d.get('kind') in p:
        p[d['kind']].append(d)
for name in sorted(pages):
    p = pages[name]
    play = p['play']
    if not play:
        print(f'{name}: never played')
        continue
    last = play[-1]
    # The first minute holds the browsers' own starting; what counts is after.
    first = play[min(29, len(play) - 1)]
    steady = play[30:] or play
    seen = sorted({f"{c['visible']}{'' if c['focus'] else ',unfocused'}" for c in p['ctx']})
    states = sorted({c['state'] for c in p['ctx']})
    ctx = p['ctx'][-1] if p['ctx'] else {}
    steer = sorted(s['steer'] for s in steady)
    print(f"{name}: {len(play) * 2} s reported, shown as {'/'.join(seen)}, context {'/'.join(states)}, "
          f"base {ctx.get('base')} ms, output {ctx.get('output')} ms, {ctx.get('rate')} Hz")
    print(f"    after the first minute: underruns {last['underruns'] - first['underruns']}, skips {last['skips'] - first['skips']}, "
          f"lost {last['lost'] - first['lost']}, stale {last['stale'] - first['stale']}, packets {last['packets'] - first['packets']} "
          f"(in it: {first['underruns']} underruns, {first['skips']} skips); not playing in {sum(1 for s in steady if not s['playing'])} reports")
    print(f"    buffer ms: lowest {min(s['min'] for s in steady)}, average {sum(s['avg'] for s in steady) / len(steady):.1f}, highest {max(s['max'] for s in steady)}; "
          f"steer ppm: median {steer[len(steer) // 2]}, from {steer[0]} to {steer[-1]}")
    print(f"    worst gap between the worklet's calls {max(s['callGap'] for s in steady)} ms, between packets reaching it {max(s['arriveGap'] for s in steady)} ms; "
          f"the page's timer late by up to {max((t['lateMax'] for t in p['timer']), default=0)} ms")
PY
