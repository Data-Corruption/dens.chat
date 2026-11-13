package main

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
)

// A session is what rides one link while it's up: the two probes' clocks
// compared, each side's keys, and each side's sound.

// message is what the ctl and now channels carry, as JSON.
type message struct {
	T    string   `json:"t"`
	N    uint64   `json:"n,omitempty"`
	A    int64    `json:"a,omitempty"`    // the sender's clock, in nanoseconds
	B    int64    `json:"b,omitempty"`    // the answerer's clock
	Held []string `json:"held,omitempty"` // keys: every key held now
	At   int64    `json:"at,omitempty"`   // keys: when the last change was read; click: when it was captured
	Text string   `json:"text,omitempty"`
}

const (
	pingEvery = 250 * time.Millisecond
	// A side that hears nothing for this long takes the link as quiet, and
	// lets go of the keys the other side held.
	quietAfter = 300 * time.Millisecond
	keysIdle   = 100 * time.Millisecond
	keysBusy   = 20 * time.Millisecond
)

// clock compares the two probes' clocks from pings, as NTP does: the
// answer with the shortest round trip gives the offset.
type clock struct {
	mu      sync.Mutex
	samples []clockSample
	offset  atomic.Int64 // the other side's clock minus this one's
	rtt     atomic.Int64
	sent    map[uint64]time.Duration
	lost    int
	first   *clockSample
}

type clockSample struct {
	at     time.Duration
	rtt    time.Duration
	offset time.Duration
}

func (c *clock) pong(m message) {
	t3 := now()
	t0, t1 := time.Duration(m.A), time.Duration(m.B)
	s := clockSample{at: t3, rtt: t3 - t0, offset: t1 - (t0+t3)/2}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sent, m.N)
	c.samples = append(c.samples, s)
	if len(c.samples) > 40 {
		c.samples = c.samples[1:]
	}
	best := c.samples[0]
	for _, x := range c.samples {
		if x.rtt < best.rtt {
			best = x
		}
	}
	c.offset.Store(int64(best.offset))
	c.rtt.Store(int64(best.rtt))
	if c.first == nil && len(c.samples) == 40 {
		c.first = &best
	}
}

// report logs the round trips of the last ten seconds, the pings that got
// no answer, and how far the two clocks have moved apart since the first
// good comparison.
func (c *clock) report() {
	c.mu.Lock()
	defer c.mu.Unlock()
	cutoff := now() - 10*time.Second
	var rtts []time.Duration
	for _, s := range c.samples {
		if s.at >= cutoff {
			rtts = append(rtts, s.rtt)
		}
	}
	for n, at := range c.sent {
		if at < now()-2*time.Second {
			c.lost++
			delete(c.sent, n)
		}
	}
	if len(rtts) == 0 {
		logf("clock", "no answers lost=%d", c.lost)
		return
	}
	sort.Slice(rtts, func(i, j int) bool { return rtts[i] < rtts[j] })
	drift := ""
	if c.first != nil {
		best := c.samples[0]
		for _, x := range c.samples {
			if x.rtt < best.rtt {
				best = x
			}
		}
		if span := best.at - c.first.at; span > 30*time.Second {
			drift = " drift=" + ppm(float64(best.offset-c.first.offset)/float64(span))
		}
	}
	logf("clock", "rtt min=%s med=%s max=%s lost=%d offset=%s%s", ms(rtts[0]), ms(rtts[len(rtts)/2]), ms(rtts[len(rtts)-1]), c.lost,
		ms(time.Duration(c.offset.Load())), drift)
}

func session(ctx context.Context, l *Link, o options, sound *Sound, keys *Keys) {
	c := &clock{sent: map[uint64]time.Duration{}}
	var heard atomic.Int64
	heard.Store(int64(now()))
	hear := func() { heard.Store(int64(now())) }

	sendNow := func(m message) {
		b, _ := json.Marshal(m)
		_ = l.now.Send(b)
	}
	sendCtl := func(m message) {
		b, _ := json.Marshal(m)
		_ = l.ctl.Send(b)
	}
	l.now.OnMessage(func(msg webrtc.DataChannelMessage) {
		hear()
		var m message
		if json.Unmarshal(msg.Data, &m) != nil {
			return
		}
		switch m.T {
		case "ping":
			sendNow(message{T: "pong", N: m.N, A: m.A, B: int64(now())})
		case "pong":
			c.pong(m)
		case "keys":
			keys.apply(m, time.Duration(c.offset.Load()), time.Duration(c.rtt.Load()))
		}
	})
	l.ctl.OnMessage(func(msg webrtc.DataChannelMessage) {
		hear()
		var m message
		if json.Unmarshal(msg.Data, &m) != nil {
			return
		}
		switch m.T {
		case "hello":
			logf("peer", "%s", m.Text)
		case "click":
			sound.peerClick(time.Duration(m.At) - time.Duration(c.offset.Load()))
		}
	})
	if l.pcm != nil {
		l.pcm.OnMessage(func(msg webrtc.DataChannelMessage) {
			hear()
			sound.receive(msg.Data)
		})
	} else {
		take := func(pkt []byte) {
			hear()
			sound.receive(pkt)
		}
		l.onSound.Store(&take)
		defer l.onSound.Store(nil)
	}

	host, _ := os.Hostname()
	sendCtl(message{T: "hello", Text: "host=" + host + " os=" + runtime.GOOS + " where=" + where() + " send=" + o.send + " keys=" + o.keys})
	sound.attach(l, func(at time.Duration) { sendCtl(message{T: "click", At: int64(at)}) })
	defer sound.detach()

	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	report := time.NewTicker(10 * time.Second)
	defer report.Stop()
	watch := time.NewTicker(25 * time.Millisecond)
	defer watch.Stop()
	tick := time.NewTicker(keysBusy)
	defer tick.Stop()

	var pings, keySeq uint64
	var lastKeys, lastChange time.Duration
	var lastVersion uint64
	quiet := false
	sendKeys := func() {
		held, at, version := keys.snapshot()
		if version != lastVersion {
			lastVersion, lastChange = version, now()
		}
		keySeq++
		sendNow(message{T: "keys", N: keySeq, Held: held, At: int64(at)})
		lastKeys = now()
	}
	for {
		select {
		case <-ctx.Done():
			keys.letGo("the link closed", 0)
			return
		case <-ping.C:
			pings++
			c.mu.Lock()
			c.sent[pings] = now()
			c.mu.Unlock()
			sendNow(message{T: "ping", N: pings, A: int64(now())})
		case <-report.C:
			c.report()
			sound.report()
		case <-keys.changed:
			sendKeys()
		case <-tick.C:
			// A state, not an event: every message says every key held,
			// so a lost one costs the wait for the next, not a key stuck.
			held, _, version := keys.snapshot()
			busy := len(held) > 0 || version != lastVersion || now()-lastChange < 200*time.Millisecond
			if busy || now()-lastKeys >= keysIdle {
				sendKeys()
			}
		case <-watch.C:
			silent := now() - time.Duration(heard.Load())
			if !quiet && silent > quietAfter {
				quiet = true
				logf("link", "quiet: nothing heard for %s", ms(silent))
				keys.letGo("the link went quiet", silent)
			} else if quiet && silent < quietAfter {
				quiet = false
				logf("link", "heard again")
			}
		}
	}
}

func ppm(ratio float64) string {
	return formatFloat(ratio*1e6, 1) + "ppm"
}
