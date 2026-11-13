#!/usr/bin/env python3
"""Sums up a probe's log: where it ran, the link, the clocks, keys, sound and CPU.

Usage: spikes/relay/run/summary.py LOG...
"""
import re
import sys


def pct(v, p):
    return v[min(len(v) - 1, int(len(v) * p))]


def num(pattern, line, kind=float):
    return kind(re.search(pattern, line).group(1))


def found(pattern, lines, kind=float):
    return [kind(m.group(1)) for l in lines for m in [re.search(pattern, l)] if m]


for path in sys.argv[1:]:
    lines = open(path, errors='replace').read().splitlines()
    name = path.rsplit('/', 1)[-1]
    where = next((re.search(r'where=(.*)', l).group(1).strip() for l in lines if ' where=' in l), '?')
    print('== %s: %s' % (name, where[:110]))
    ups = ['%sms on attempt %s' % re.search(r'up after=([\d.]+)ms attempt=(\d+)', l).groups() for l in lines if ' link  up after' in l]
    fails = sum(1 for l in lines if re.search(r' link  attempt=\d+ (failed|gave up)', l))
    quiet = sum(1 for l in lines if ' link  quiet' in l)
    downs = sum(1 for l in lines if ' link  down after' in l)
    pair = next((re.search(r'pair (.*)', l).group(1) for l in lines if ' link  pair ' in l), '')
    print('   link: up %s, failed attempts %d, quiet %d, down %d; %s' % (ups, fails, quiet, downs, pair))
    rtt = [l for l in lines if ' clock rtt ' in l]
    if rtt:
        meds = sorted(num(r'med=([\d.]+)ms', l) for l in rtt)
        drift = found(r'drift=(-?[\d.]+)ppm', lines)
        print('   round trip: least %s ms, median %s, worst %s; pings unanswered %d; clocks drift %s ppm' % (
            min(num(r'min=([\d.]+)ms', l) for l in rtt), pct(meds, .5), max(num(r'max=([\d.]+)ms', l) for l in rtt),
            num(r'lost=(\d+)', rtt[-1], int), drift[-1] if drift else '?'))
    keys = sorted(k for k in found(r'keys  got \S+ \S+ delay=(-?[\d.]+)ms', lines) if k >= 0)
    letgo = sum(1 for l in lines if 'keys  let go' in l and 'went quiet' in l)
    read = sum(1 for l in lines if ' keys  read ' in l)
    if keys or read:
        k = 'none'
        if keys:
            k = 'median %s ms, 95%% %s, 99%% %s, worst %s' % (pct(keys, .5), pct(keys, .95), pct(keys, .99), keys[-1])
        lost = found(r'keys  got .* lost=(\d+)', lines, int)
        print('   keys: read %d changes here; got %d from the peer: %s; states lost %s; let go on quiet %d' % (
            read, len(keys), k, lost[-1] if lost else 0, letgo))
    rx = [l for l in lines if ' sound rx ' in l]
    if rx:
        def tot(key):
            return sum(num(key + r'=(-?\d+)', l, int) for l in rx)
        print('   sound in: %d packets, lost %d, late over 5 ms %d, 10 ms %d, 20 ms %d, 50 ms %d; latest %s ms, longest wait %s ms; ten-second windows with a loss %d of %d' % (
            num(r'total=(\d+)', rx[-1], int), num(r'total-lost=(-?\d+)', rx[-1], int), tot('over5'), tot('over10'), tot('over20'), tot('over50'),
            max(num(r'late-max=([\d.]+)ms', l) for l in rx), max(num(r'gap-max=([\d.]+)ms', l) for l in rx),
            sum(1 for l in rx if num(r' lost=(-?\d+)', l, int) > 0), len(rx)))
    tx = [l for l in lines if ' sound tx ' in l]
    if tx:
        print('   sound out: %d packets, dropped %d, failed %d, most waiting %d B' % (
            sum(num(r'packets=(\d+)', l, int) for l in tx), sum(num(r'dropped=(\d+)', l, int) for l in tx),
            sum(num(r'failed=(\d+)', l, int) for l in tx), max(num(r'queued-max=(\d+)B', l, int) for l in tx)))
    cap = [l for l in lines if ' cap   rate=' in l]
    if cap:
        gaps = sorted(num(r'gap-max=([\d.]+)ms', l) for l in cap)
        print("   capture: %d s, seconds with silence of the probe's own %d, longest wait between pieces: median %s ms, worst %s; events %d" % (
            len(cap), sum(1 for l in cap if num(r'filled=(\d+)', l, int) > 0), pct(gaps, .5), gaps[-1],
            sum(1 for l in lines if ' cap   event ' in l)))
    cpu = sorted(found(r' cpu   share=([\d.]+)%', lines))
    if cpu:
        print('   processor: median %s%% of a core, most %s%%' % (pct(cpu, .5), cpu[-1]))
    clicks = sorted(found(r'click played .* delay=([\d.]+)ms', lines))
    if clicks:
        print('   clicks: %d timed, from capture to this output: least %s ms, median %s, most %s' % (len(clicks), clicks[0], pct(clicks, .5), clicks[-1]))
