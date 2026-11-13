package main

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

// Keys holds the keys this side reads, to send, and the keys the other
// side holds, as it last said.
type Keys struct {
	mu      sync.Mutex
	held    map[string]bool
	at      time.Duration
	version uint64
	changed chan struct{}

	remote    map[string]time.Duration // each key the other side holds, and since when here
	remoteSeq uint64
	lost      uint64
}

func newKeys() *Keys {
	return &Keys{held: map[string]bool{}, remote: map[string]time.Duration{}, changed: make(chan struct{}, 1)}
}

func (k *Keys) set(name string, down bool, at time.Duration) {
	k.mu.Lock()
	if k.held[name] == down {
		k.mu.Unlock()
		return
	}
	if down {
		k.held[name] = true
	} else {
		delete(k.held, name)
	}
	k.at = at
	k.version++
	k.mu.Unlock()
	logf("keys", "read %s %s", name, upDown(down))
	select {
	case k.changed <- struct{}{}:
	default:
	}
}

func (k *Keys) snapshot() ([]string, time.Duration, uint64) {
	k.mu.Lock()
	defer k.mu.Unlock()
	held := make([]string, 0, len(k.held))
	for name := range k.held {
		held = append(held, name)
	}
	sort.Strings(held)
	return held, k.at, k.version
}

// apply takes the other side's keys. The delay is from its reading the
// change to this side having it, by the two clocks compared.
func (k *Keys) apply(m message, offset, rtt time.Duration) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if m.N <= k.remoteSeq && k.remoteSeq-m.N < 1<<20 {
		return
	}
	if k.remoteSeq != 0 && m.N > k.remoteSeq+1 {
		k.lost += m.N - k.remoteSeq - 1
	}
	k.remoteSeq = m.N
	// Before the clocks are compared there's no delay to give.
	delay := time.Duration(-1)
	if rtt > 0 {
		delay = now() - (time.Duration(m.At) - offset)
	}
	want := map[string]bool{}
	for _, name := range m.Held {
		want[name] = true
		if _, ok := k.remote[name]; !ok {
			k.remote[name] = now()
			logf("keys", "got %s down delay=%s rtt=%s lost=%d", name, ms(delay), ms(rtt), k.lost)
		}
	}
	for name, since := range k.remote {
		if !want[name] {
			delete(k.remote, name)
			logf("keys", "got %s up delay=%s rtt=%s held=%s lost=%d", name, ms(delay), ms(rtt), ms(now()-since), k.lost)
		}
	}
}

// letGo releases every key the other side held, as the page would once the
// link can't say they're still held.
func (k *Keys) letGo(why string, silent time.Duration) {
	k.mu.Lock()
	defer k.mu.Unlock()
	for name, since := range k.remote {
		delete(k.remote, name)
		logf("keys", "let go of %s: %s, %s after the last word, held=%s", name, why, ms(silent), ms(now()-since))
	}
	k.remoteSeq = 0
}

// read reads this side's keys until the context ends: made-up presses, or
// the named keys through the helper's own reader.
func (k *Keys) read(ctx context.Context, spec string) {
	if spec == "synth" || strings.HasPrefix(spec, "synth:") {
		// Held 400 ms of every second, or as long as given: a long hold
		// is what a dropped link must let go of.
		hold := 400 * time.Millisecond
		if _, v, ok := strings.Cut(spec, ":"); ok {
			if d, err := time.ParseDuration(v); err == nil {
				hold = d
			}
		}
		for n := 0; ctx.Err() == nil; n++ {
			k.set("Synth", true, now())
			sleep(ctx, hold)
			k.set("Synth", false, now())
			sleep(ctx, time.Second-hold%time.Second)
		}
		return
	}
	state, err := host.OpenKeyState()
	if err != nil {
		logf("keys", "no reader: %v", err)
		return
	}
	defer state.Close()
	type bound struct {
		name string
		key  host.Key
	}
	var keys []bound
	for _, code := range strings.Split(spec, ",") {
		key, ok := state.Key(code)
		if !ok {
			logf("keys", "no key %s", strconv.Quote(code))
			continue
		}
		keys = append(keys, bound{code, key})
	}
	logf("keys", "reading %d keys every 10ms", len(keys))
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, b := range keys {
				k.set(b.name, state.Down(b.key), now())
			}
		}
	}
}

func upDown(down bool) string {
	if down {
		return "down"
	}
	return "up"
}

func formatFloat(v float64, digits int) string { return strconv.FormatFloat(v, 'f', digits, 64) }
