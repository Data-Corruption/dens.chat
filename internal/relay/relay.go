// Package relay links this install with the member's other installs
// straight across their network (M20), and carries the voice keys of one
// to the call another holds. The dens only introduce two installs, in boxes
// sealed between the member's devices, which denclient seals and opens:
// who and here fill the settings' list of the member's devices, and hello
// and answer carry each side's half of a link. The link is Pion to Pion, on
// an ephemeral port, over host candidates on private IPv4 addresses alone,
// which stay in memory: never logged, written, or given to the page.
package relay

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"net"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/shortcuts"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// Dens is what Relay asks of the dens this install joined (denclient).
type Dens interface {
	// RelayDens lists the dens, connected now, that pass boxes between
	// the member's devices, where this device holds their seal.
	RelayDens() []string
	// MemberCalls lists the dens where the member is in a call.
	MemberCalls() []string
	// CallDen is the den of the call this install holds, "" for none.
	CallDen() string
	// SendRelay sends a note to another of the member's devices at a den,
	// by its key ID, or to every other where to is nil.
	SendRelay(den string, to denproto.Bytes, note []byte) error
}

// Hub is what Relay asks of the shortcut hub (internal/shortcuts).
type Hub interface {
	SetRelay(take func(shortcuts.Press))
	RelayHolds(device string, held []string)
	RelayToggle(device, action string)
}

// Config is what an install's Relay is.
type Config struct {
	// ID is the install's Relay ID, and Name the label its dens list it
	// under.
	ID   ID
	Name string
	// Keys is Keybind relay, as the settings keep it, and CanKeys whether
	// this install relays keys at all.
	Keys, CanKeys bool
	// Loopback has links made over loopback too, as development
	// instances and tests make them.
	Loopback bool
	Dens     Dens
	Hub      Hub
	Log      *xlog.Logger
	// Addrs lists this computer's addresses a link may take, and Now reads
	// the clock; tests set them.
	Addrs func() []netip.Addr
	Now   func() time.Time
}

const (
	// grace is how long a link nobody needs stays up, as across a call
	// moving from one device to another.
	grace = 2 * time.Second
	// askEvery bounds how often the member's devices are asked who's
	// there, and forget is how long one that said so is listed.
	askEvery = 10 * time.Second
	forget   = time.Minute
	// pingEvery is how often each side of a link says it's there.
	pingEvery = 250 * time.Millisecond
	// mostWait is the longest a hello that goes unanswered waits before
	// the next.
	mostWait = 2 * time.Minute
)

// Relay is the install's side of the links with the member's others.
type Relay struct {
	cfg   Config
	boxes chan inbox
	wake  chan struct{}
	asks  chan struct{}
	heres chan struct{}
	out   *keysOut

	mu      sync.Mutex
	ctx     context.Context
	keysOn  bool
	devices map[ID]*device
	peers   map[ID]*peer
	// asking is this install's hello, waiting for its answer; retry is
	// when it may ask again, after tries failures in a row; callDen is the
	// den of the call it last asked for.
	asking   *attempt
	retry    time.Time
	tries    int
	callDen  string
	asked    time.Time
	relaying bool
	// held is the den of the call this install held when last evaluated.
	held string
}

type inbox struct {
	den  string
	from denproto.Bytes
	note []byte
}

// device is another of the member's installs, as it last said. apart says
// the last link tried with it never came up: the two aren't on one
// network.
type device struct {
	name   string
	offers []string
	heard  time.Time
	apart  bool
}

// attempt is this install's hello, waiting for its answer.
type attempt struct {
	id    denproto.Bytes
	local *local
	den   string
	sent  time.Time
}

// state is what each side of a link says of itself: whether it holds a
// call, and whether its Keybind relay is on.
type state struct {
	Call bool `json:"call"`
	Keys bool `json:"keys"`
}

// needed says whether a link is needed by either side: keys go from one
// that holds no call to one that does, with Keybind relay on at both.
func needed(a, b state) bool {
	return a.Keys && b.Keys && a.Call != b.Call
}

// ctlMsg is what the ctl channel carries: each side's state.
type ctlMsg struct {
	T string `json:"t"`
	state
}

// peer is another of the member's installs this one links with, or tries
// to, through the den that introduced them.
type peer struct {
	id    ID
	den   string
	key   denproto.Bytes
	local *local
	link  *link
	in    *keysIn
	since time.Time
	// Guarded by Relay.mu: whether the link is up, and since when; what the
	// other side last said of itself, and whether it has; what this side
	// last said; and since when nobody has needed the link.
	up     bool
	upAt   time.Time
	theirs state
	heard  bool
	sent   *state
	idle   time.Time
}

// close closes the link and lets go of its port, and of what the other
// side held. A link that ended on its own has closed already.
func (p *peer) close() {
	p.link.close()
	p.local.close()
	p.in.letGo()
}

// Device is another of the member's installs, as the settings list it: by
// its name, with what it offers, whether it's linked with this one, and
// whether the last link tried with it never came up (Apart).
type Device struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Offers []string `json:"offers"`
	Linked bool     `json:"linked,omitempty"`
	Apart  bool     `json:"apart,omitempty"`
}

// New returns an install's Relay, which Run starts.
func New(cfg Config) *Relay {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Addrs == nil {
		loopback := cfg.Loopback
		cfg.Addrs = func() []netip.Addr { return localAddrs(loopback) }
	}
	return &Relay{cfg: cfg, boxes: make(chan inbox, 64), wake: make(chan struct{}, 1), asks: make(chan struct{}, 1),
		heres: make(chan struct{}, 1), out: newKeysOut(cfg.Now), keysOn: cfg.Keys, devices: map[ID]*device{}, peers: map[ID]*peer{}}
}

// Can says what this install relays: keys, or nothing.
func (r *Relay) Can() []string {
	if r.cfg.CanKeys {
		return []string{offerKeys}
	}
	return []string{}
}

// Box takes a note from another of the member's devices, as denclient
// opened it. It never blocks: a note past what's waiting goes.
func (r *Relay) Box(den string, from denproto.Bytes, note []byte) {
	select {
	case r.boxes <- inbox{den: den, from: from, note: note}:
	default:
	}
}

// Changed says the dens, the member's calls or this install's changed.
func (r *Relay) Changed() { poke(r.wake) }

func poke(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// SetKeys turns Keybind relay on or off, as the settings do, and tells the
// member's other devices.
func (r *Relay) SetKeys(on bool) {
	r.mu.Lock()
	r.keysOn = on
	var peers []*peer
	if !on {
		for _, p := range r.peers {
			peers = append(peers, p)
		}
	}
	r.mu.Unlock()
	for _, p := range peers {
		p.in.letGo()
	}
	poke(r.heres)
	poke(r.wake)
}

// Devices lists the member's other installs that said they're here, and
// asks them again, at most every ten seconds.
func (r *Relay) Devices() []Device {
	now := r.cfg.Now()
	r.mu.Lock()
	if now.Sub(r.asked) >= askEvery {
		r.asked = now
		poke(r.asks)
	}
	list := []Device{}
	for id, d := range r.devices {
		p := r.peers[id]
		linked := p != nil && p.up
		if !linked && now.Sub(d.heard) >= forget {
			continue
		}
		list = append(list, Device{ID: id.String(), Name: d.name, Offers: append([]string{}, d.offers...), Linked: linked, Apart: d.apart && !linked})
	}
	r.mu.Unlock()
	slices.SortFunc(list, func(a, b Device) int { return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID)) })
	return list
}

// Run introduces and links until ctx ends, then closes every link.
func (r *Relay) Run(ctx context.Context) error {
	r.mu.Lock()
	r.ctx = ctx
	r.mu.Unlock()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	r.evaluate()
	for {
		select {
		case <-ctx.Done():
			r.stop()
			return nil
		case in := <-r.boxes:
			r.handle(in)
			r.evaluate()
		case <-r.asks:
			r.broadcast(kindWho)
		case <-r.heres:
			r.broadcast(kindHere)
		case <-r.wake:
			r.evaluate()
		case <-tick.C:
			r.evaluate()
		}
	}
}

func (r *Relay) stop() {
	r.mu.Lock()
	peers := slices.Collect(maps.Values(r.peers))
	clear(r.peers)
	a := r.asking
	r.asking = nil
	relaying := r.relaying
	r.relaying = false
	r.mu.Unlock()
	for _, p := range peers {
		p.close()
	}
	if a != nil {
		a.local.close()
	}
	if relaying {
		r.cfg.Hub.SetRelay(nil)
	}
}

// note is a note of a kind from this install: what it is and offers.
func (r *Relay) note(kind string) note {
	r.mu.Lock()
	keys := r.keysOn && r.cfg.CanKeys
	r.mu.Unlock()
	n := note{Kind: kind, ID: append(denproto.Bytes{}, r.cfg.ID[:]...), Name: r.cfg.Name, At: r.cfg.Now().UnixMilli()}
	if keys {
		n.Offers = []string{offerKeys}
	}
	return n
}

func (r *Relay) send(den string, to denproto.Bytes, n note) error {
	b, err := n.encode()
	if err != nil {
		return err
	}
	return r.cfg.Dens.SendRelay(den, to, b)
}

// broadcast sends a note of a kind to the member's other devices at every
// den that passes them: who, to ask who's there, or here, as Keybind relay
// changes.
func (r *Relay) broadcast(kind string) {
	if !r.cfg.CanKeys {
		return
	}
	n := r.note(kind)
	for _, den := range r.cfg.Dens.RelayDens() {
		if r.send(den, nil, n) != nil {
			r.cfg.Log.Debugf("Relay: couldn't send %s through a den", kind)
		}
	}
}

// handle takes a note from another of the member's devices.
func (r *Relay) handle(in inbox) {
	if !r.cfg.CanKeys {
		return
	}
	now := r.cfg.Now()
	n, err := decodeNote(in.note, now, privateAddr(r.cfg.Loopback))
	if err != nil {
		r.cfg.Log.Debugf("Relay: a note from another of the member's devices was refused: %v", err)
		return
	}
	var id ID
	copy(id[:], n.ID)
	if id == r.cfg.ID {
		return
	}
	r.mu.Lock()
	d := r.devices[id]
	if d == nil {
		d = &device{}
		r.devices[id] = d
	}
	d.name, d.offers, d.heard = n.Name, n.Offers, now
	// A device that turned Keybind relay on, or took a call, is worth
	// asking again at once: a hello waiting may have reached it before.
	var dropped *attempt
	if n.Kind == kindHere && slices.Contains(n.Offers, offerKeys) {
		r.tries, r.retry = 0, time.Time{}
		dropped, r.asking = r.asking, nil
	}
	r.mu.Unlock()
	if dropped != nil {
		dropped.local.close()
	}
	switch n.Kind {
	case kindWho:
		if r.send(in.den, in.from, r.note(kindHere)) != nil {
			r.cfg.Log.Debugf("Relay: couldn't say this install is here")
		}
	case kindHello:
		r.answer(in, id, n)
	case kindAnswer:
		r.answered(in, id, n)
	}
}

// answer answers a hello for keys from another of the member's devices,
// where this install holds the call in the den it came through and has
// Keybind relay on. A device asks only while it has no link to the call,
// so one this side still holds is stale, as when that device started
// again before the link here failed, and the new one replaces it.
func (r *Relay) answer(in inbox, id ID, n note) {
	if !slices.Contains(n.Wants, offerKeys) || r.cfg.Dens.CallDen() != in.den {
		return
	}
	r.mu.Lock()
	on := r.keysOn
	r.mu.Unlock()
	if !on {
		return
	}
	loc, err := newLocal(r.cfg.Addrs())
	if err != nil {
		r.cfg.Log.Debugf("Relay with %s: couldn't answer: %v", id.short(), err)
		return
	}
	a := r.note(kindAnswer)
	a.Attempt, a.Half = n.Attempt, loc.half()
	if err := r.send(in.den, in.from, a); err != nil {
		loc.close()
		r.cfg.Log.Debugf("Relay with %s: couldn't send its answer", id.short())
		return
	}
	r.cfg.Log.Debugf("Relay with %s: answered its hello", id.short())
	r.link(id, in.den, in.from, loc, n.Half)
}

// answered takes the answer to this install's hello.
func (r *Relay) answered(in inbox, id ID, n note) {
	r.mu.Lock()
	a := r.asking
	if a == nil || a.den != in.den || !bytes.Equal(a.id, n.Attempt) {
		r.mu.Unlock()
		return
	}
	r.asking = nil
	p := r.peers[id]
	linked := p != nil && p.up
	r.mu.Unlock()
	if linked {
		a.local.close()
		return
	}
	r.cfg.Log.Debugf("Relay with %s: its answer came", id.short())
	r.link(id, in.den, in.from, a.local, n.Half)
}

// link starts a link with another of the member's installs from the two
// halves, and watches it.
func (r *Relay) link(id ID, den string, key denproto.Bytes, loc *local, theirs *half) {
	offer := bytes.Compare(r.cfg.ID[:], id[:]) < 0
	p := &peer{id: id, den: den, key: key, local: loc, in: newKeysIn(r.cfg.Hub, id.String(), r.cfg.Now), since: r.cfg.Now()}
	k, err := loc.dial(theirs, offer, r.cfg.Loopback, func(b []byte) { r.onCtl(p, b) }, func(b []byte) { r.onNow(p, b) })
	if err != nil {
		loc.close()
		r.cfg.Log.Debugf("Relay with %s: %v", id.short(), err)
		return
	}
	p.link = k
	r.mu.Lock()
	old := r.peers[id]
	r.peers[id] = p
	ctx := r.ctx
	r.mu.Unlock()
	if old != nil {
		old.close()
	}
	go r.watch(ctx, p)
}

// watch waits for a link to come up, holds its session while it lasts,
// and closes it.
func (r *Relay) watch(ctx context.Context, p *peer) {
	deadline := time.NewTimer(introduction)
	defer deadline.Stop()
	defer func() {
		p.close()
		r.mu.Lock()
		if r.peers[p.id] == p {
			delete(r.peers, p.id)
		}
		r.mu.Unlock()
		poke(r.wake)
	}()
	apart := false
	select {
	case <-p.link.up:
	case <-p.link.done:
		apart = true
	case <-deadline.C:
		apart = true
	case <-ctx.Done():
		return
	}
	now := r.cfg.Now()
	r.mu.Lock()
	if d := r.devices[p.id]; d != nil {
		d.apart = apart
	}
	if apart {
		r.failedLocked(now)
	} else {
		p.up, p.upAt = true, now
		r.tries = 0
	}
	r.mu.Unlock()
	if apart {
		r.cfg.Log.Infof("Relay with %s: the link never came up, so the two aren't on one network", p.id.short())
		return
	}
	r.cfg.Log.Infof("Relay with %s: linked in %s", p.id.short(), now.Sub(p.since).Round(time.Millisecond))
	poke(r.wake)
	r.session(ctx, p)
	r.cfg.Log.Infof("Relay with %s: the link closed after %s", p.id.short(), r.cfg.Now().Sub(now).Round(time.Second))
	r.mu.Lock()
	// A link lost can come back at once, through a new introduction.
	r.retry = r.cfg.Now().Add(time.Second)
	r.mu.Unlock()
}

// onCtl takes what a link's ctl channel carries: the other side's state.
func (r *Relay) onCtl(p *peer, b []byte) {
	p.in.hear()
	var c ctlMsg
	if json.Unmarshal(b, &c) != nil || c.T != "state" {
		return
	}
	r.mu.Lock()
	p.theirs, p.heard = c.state, true
	r.mu.Unlock()
	poke(r.wake)
}

// onNow takes what a link's now channel carries: pings, and the other
// side's keys, while Keybind relay is on here.
func (r *Relay) onNow(p *peer, b []byte) {
	p.in.hear()
	var km keysMsg
	if json.Unmarshal(b, &km) != nil || km.T != "keys" {
		return
	}
	r.mu.Lock()
	take := r.keysOn
	r.mu.Unlock()
	if take {
		p.in.apply(km)
	}
}

// session carries what this side sends on a link while it's up: pings,
// and keys while the other side holds the call.
func (r *Relay) session(ctx context.Context, p *peer) {
	k := p.link
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	keys := time.NewTicker(keysBusy)
	defer keys.Stop()
	quiet := time.NewTicker(quietAfter / 12)
	defer quiet.Stop()
	var lastKeys time.Time
	pingMsg := []byte(`{"t":"ping"}`)
	for {
		select {
		case <-ctx.Done():
			return
		case <-k.done:
			return
		case <-ping.C:
			_ = k.now.Send(pingMsg)
		case <-quiet.C:
			p.in.check()
		case <-keys.C:
			r.mu.Lock()
			sends := r.relaying && p.theirs.Call && p.theirs.Keys
			r.mu.Unlock()
			if !sends {
				continue
			}
			now := r.cfg.Now()
			if r.out.busy() || now.Sub(lastKeys) >= keysIdle {
				if b, err := json.Marshal(r.out.state()); err == nil {
					_ = k.now.Send(b)
				}
				lastKeys = now
			}
		}
	}
}

// failedLocked backs off after a hello went unanswered, or a link never
// came up: 2 seconds, then twice as long each time, up to two minutes.
func (r *Relay) failedLocked(now time.Time) {
	r.tries++
	wait := min(time.Second<<min(r.tries, 7), mostWait)
	r.retry = now.Add(wait)
}

// evaluate goes over the links: each told this side's state as it
// changes, those nobody needs closed, the hub's keys handed over while one
// goes to a call, and a hello sent where the member's call is held by a
// device this one has no link to.
func (r *Relay) evaluate() {
	dens := r.cfg.Dens.RelayDens()
	calls := r.cfg.Dens.MemberCalls()
	held := r.cfg.Dens.CallDen()
	now := r.cfg.Now()
	r.mu.Lock()
	mine := state{Call: held != "", Keys: r.keysOn && r.cfg.CanKeys}
	var expired *attempt
	if a := r.asking; a != nil && now.Sub(a.sent) >= introduction {
		expired, r.asking = a, nil
		r.failedLocked(now)
	}
	callDen := ""
	if mine.Keys && !mine.Call {
		for _, d := range calls {
			if slices.Contains(dens, d) {
				callDen = d
				break
			}
		}
	}
	if callDen != r.callDen {
		r.callDen, r.tries, r.retry = callDen, 0, time.Time{}
	}
	covered := r.asking != nil
	relaying := false
	var tell, closing []*peer
	for id, p := range r.peers {
		// A link coming up, or up but not yet heard from, may be the one
		// to the call.
		if !p.up || !p.heard {
			covered = true
		}
		if !p.up {
			continue
		}
		if p.theirs.Call && p.theirs.Keys {
			covered = true
			relaying = relaying || (mine.Keys && !mine.Call)
		}
		if p.sent == nil || *p.sent != mine {
			s := mine
			p.sent = &s
			tell = append(tell, p)
		}
		switch {
		case needed(mine, p.theirs) || (!p.heard && now.Sub(p.upAt) < grace):
			p.idle = time.Time{}
		case p.idle.IsZero():
			p.idle = now
		case now.Sub(p.idle) >= grace:
			closing = append(closing, p)
			delete(r.peers, id)
		}
	}
	hello := callDen != "" && !covered && !now.Before(r.retry)
	handOver := relaying != r.relaying
	r.relaying = relaying
	// Taking a call, this install says it's here, for a device whose hello
	// went unanswered while the den held the call for this one.
	tookCall := held != "" && held != r.held && mine.Keys
	r.held = held
	r.mu.Unlock()
	if tookCall {
		poke(r.heres)
	}

	if expired != nil {
		expired.local.close()
		r.cfg.Log.Debugf("Relay: no device holding the call answered")
	}
	for _, p := range tell {
		if b, err := json.Marshal(ctlMsg{T: "state", state: mine}); err == nil {
			_ = p.link.ctl.Send(b)
		}
	}
	for _, p := range closing {
		r.cfg.Log.Debugf("Relay with %s: nothing needs the link", p.id.short())
		p.close()
	}
	if handOver {
		if relaying {
			r.cfg.Hub.SetRelay(r.out.take)
		} else {
			r.cfg.Hub.SetRelay(nil)
		}
	}
	if hello {
		r.hello(callDen, now)
	}
}

// hello asks the member's other devices at a den for a link, for keys:
// the one holding the member's call answers.
func (r *Relay) hello(den string, now time.Time) {
	loc, err := newLocal(r.cfg.Addrs())
	if err != nil {
		r.mu.Lock()
		r.failedLocked(now)
		r.mu.Unlock()
		r.cfg.Log.Debugf("Relay: couldn't ask for a link: %v", err)
		return
	}
	n := r.note(kindHello)
	n.Attempt, n.Wants, n.Half = denproto.Random(attemptSize), []string{offerKeys}, loc.half()
	if err := r.send(den, nil, n); err != nil {
		loc.close()
		r.mu.Lock()
		r.failedLocked(now)
		r.mu.Unlock()
		r.cfg.Log.Debugf("Relay: couldn't send a hello")
		return
	}
	r.mu.Lock()
	r.asking = &attempt{id: n.Attempt, local: loc, den: den, sent: now}
	r.mu.Unlock()
	r.cfg.Log.Debugf("Relay: asked for a link to the device holding the call")
}

// localAddrs lists this computer's IPv4 addresses on private networks, on
// interfaces that are up, and loopback's where it's allowed.
func localAddrs(loopback bool) []netip.Addr {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	allowed := privateAddr(loopback)
	var out []netip.Addr
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(n.IP)
			if ip = ip.Unmap(); ok && allowed(ip) && !slices.Contains(out, ip) && len(out) < maxAddrs {
				out = append(out, ip)
			}
		}
	}
	return out
}
