// Package relay links this install with the member's other installs
// straight across their network (M20), and carries the voice keys of one
// to the call another holds, and from M20.2 one computer's sound to play
// on another. The dens only introduce two installs, in boxes sealed
// between the member's devices, which denclient seals and opens: who and
// here fill the settings' list of the member's devices, and hello and
// answer carry each side's half of a link. The link is Pion to Pion, on an
// ephemeral port, over host candidates on private IPv4 addresses alone,
// which stay in memory: never logged, written, or given to the page. The
// Android app relays its call's button's presses, and listens, once
// Android lets it reach its network.
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

	"github.com/wlynxg/anet"
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
	// MemberMuted says whether the member is muted, by themselves or by
	// staff, in their call at a den, whichever device holds it.
	MemberMuted(den string) bool
	// SendRelay sends a note to another of the member's devices at a den,
	// by its key ID, or to every other where to is nil.
	SendRelay(den string, to denproto.Bytes, note []byte) error
}

// Hub is what Relay asks of the shortcut hub (internal/shortcuts): the
// keys, and from M20.2 the computer's sound, which the helper reads.
type Hub interface {
	SetRelay(take func(shortcuts.Press))
	RelayHolds(device string, held []string)
	RelayToggle(device, action string)
	SetSound(take func(piece []byte), problem func(string))
}

// Config is what an install's Relay is.
type Config struct {
	// ID is the install's Relay ID, and Name the label its dens list it
	// under.
	ID   ID
	Name string
	// Keys is Keybind relay, as the settings keep it: a computer's helper
	// reads the keys, and a phone's page sends its call's button's presses.
	Keys bool
	// Sound is Share this computer's sound, as the settings keep it, and
	// CanSound whether this install shares its sound at all, as a
	// computer's helper reads it (M20.2); every install listens.
	Sound, CanSound bool
	// AsksLocal has the install link with nothing until SetLocal says the
	// system lets it reach its network, which Android 17 asks the member.
	AsksLocal bool
	// Loopback has links made over loopback too, as development
	// instances and tests make them.
	Loopback bool
	Dens     Dens
	Hub      Hub
	Log      *xlog.Logger
	// Wants hears what Relay asks of the system, as it changes.
	Wants func(Wants)
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
	// listenWait is how long a hello for a device's sound waits for its
	// answer, which comes through the den at once from a device that
	// shares it.
	listenWait = 10 * time.Second
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
	soundOn bool
	devices map[ID]*device
	peers   map[ID]*peer
	// asking is this install's hello for keys, waiting for its answer;
	// retry is when it may ask again, after tries failures in a row;
	// callDen is the den of the call it last asked for.
	asking   *attempt
	retry    time.Time
	tries    int
	callDen  string
	asked    time.Time
	relaying bool
	// held is the den of the call this install held when last evaluated.
	held string
	// Sound (M20.2): listening is this install's listening to another
	// device's sound, and listenAsk its hello waiting for an answer;
	// listener is the device this install's sound goes to, capturing
	// whether the helper reads it, and captureWhy why none comes.
	listening  *Listening
	listenAsk  *attempt
	listener   *peer
	capturing  bool
	captureWhy string
	status     Status
	watchers   map[chan Status]struct{}
	// Whether the system lets this install reach its network, and whether
	// the member refused it when asked; the device holding the member's
	// call, which this install's keys go to, and whether the member is
	// muted there; and what Relay last asked of the system.
	local, refused bool
	keysTo         *peer
	keysMuted      bool
	wants          Wants
}

// Wants is what Relay asks of the system the install runs on: On while a
// switch is on or the install listens, which is when Android asks the
// member for the network, and Listening while it plays another's sound,
// which the Android app plays as media.
type Wants struct {
	On        bool `json:"on"`
	Listening bool `json:"listening"`
}

type inbox struct {
	den  string
	from denproto.Bytes
	note []byte
}

// device is another of the member's installs, as it last said, at the den
// and with the device key it last said it through. apart says the last
// link tried with it never came up: the two aren't on one network.
type device struct {
	name   string
	offers []string
	heard  time.Time
	apart  bool
	den    string
	key    denproto.Bytes
}

// attempt is a hello of this install's, waiting for its answer: one for
// keys goes to every other device, and one for sound to the device whose
// sound it asks for.
type attempt struct {
	id    denproto.Bytes
	local *local
	den   string
	to    ID
	sent  time.Time
}

// state is what each side of a link says of itself: whether it holds a
// call, and whether its Keybind relay is on; and from M20.2 whether it
// shares its sound now, and whether it listens to the other side's.
type state struct {
	Call   bool `json:"call"`
	Keys   bool `json:"keys"`
	Share  bool `json:"share,omitempty"`
	Listen bool `json:"listen,omitempty"`
}

// needed says whether a link is needed by either side: keys go from one
// that holds no call to one that does, with Keybind relay on at both, and
// sound from one that shares it to one that listens.
func needed(a, b state) bool {
	return a.Keys && b.Keys && a.Call != b.Call || a.Listen && b.Share || b.Listen && a.Share
}

// What the ctl channel carries: each side's state, and from M20.2 a
// listener's end, told by the side whose sound it was: another device took
// over, or the member stopped sharing there.
const (
	ctlState   = "state"
	ctlTaken   = "taken"
	ctlStopped = "stopped"
)

type ctlMsg struct {
	T string `json:"t"`
	state
}

// peer is another of the member's installs this one links with, or tries
// to, through the den that introduced them, from a hello's attempt, this
// side's own or the other's.
type peer struct {
	id      ID
	den     string
	key     denproto.Bytes
	attempt denproto.Bytes
	mine    bool
	local   *local
	link    *link
	in      *keysIn
	since   time.Time
	// Guarded by Relay.mu: whether the link is up, and since when; what the
	// other side last said of itself, and whether it has; what this side
	// last said; since when nobody has needed the link; and whether the
	// other side listened to this one's sound as last seen, so a listener
	// is taken as it starts.
	up       bool
	upAt     time.Time
	theirs   state
	heard    bool
	sent     *state
	idle     time.Time
	listened bool
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
		heres: make(chan struct{}, 1), out: newKeysOut(cfg.Now), keysOn: cfg.Keys, soundOn: cfg.Sound, devices: map[ID]*device{},
		peers: map[ID]*peer{}, watchers: map[chan Status]struct{}{}, local: !cfg.AsksLocal}
}

// Can says what this install relays: keys, and its sound where it shares
// it.
func (r *Relay) Can() []string {
	can := []string{offerKeys}
	if r.cfg.CanSound {
		can = append(can, offerSound)
	}
	return can
}

// SetLocal says whether the system lets this install reach its network,
// and whether the member refused it when asked. Refused, a listening
// ends, saying so.
func (r *Relay) SetLocal(on, refused bool) {
	refused = refused && !on
	r.mu.Lock()
	changed := r.local != on || r.refused != refused
	r.local, r.refused = on, refused
	r.mu.Unlock()
	if changed {
		r.cfg.Log.Infof("Relay: the system lets this install reach its network: %t", on)
		poke(r.wake)
	}
}

// Refused says whether the member refused this install its network when
// the system asked, as the settings say.
func (r *Relay) Refused() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.refused
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

// SetSound turns Share this computer's sound on or off, as the settings
// do, and tells the member's other devices (M20.2). Off, whoever listens
// hears that it stopped.
func (r *Relay) SetSound(on bool) {
	r.mu.Lock()
	r.soundOn = on
	r.mu.Unlock()
	poke(r.heres)
	poke(r.wake)
}

// sharesLocked says whether this install shares its sound now: its switch
// is on, and it isn't listening to another's, since two devices playing
// each other would loop.
func (r *Relay) sharesLocked() bool {
	return r.soundOn && r.cfg.CanSound && r.listening == nil
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
	asks := []*attempt{r.asking, r.listenAsk}
	r.asking, r.listenAsk = nil, nil
	relaying, capturing := r.relaying, r.capturing
	r.relaying, r.capturing, r.listener, r.keysTo = false, false, nil, nil
	l := r.listening
	r.listening = nil
	r.mu.Unlock()
	for _, p := range peers {
		p.close()
	}
	for _, a := range asks {
		if a != nil {
			a.local.close()
		}
	}
	if relaying {
		r.cfg.Hub.SetRelay(nil)
	}
	if capturing {
		r.cfg.Hub.SetSound(nil, nil)
	}
	if l != nil {
		l.finish(WhyClosing)
	}
	r.publish()
}

// note is a note of a kind from this install: what it is and offers.
func (r *Relay) note(kind string) note {
	r.mu.Lock()
	keys := r.keysOn
	sound := r.sharesLocked()
	r.mu.Unlock()
	n := note{Kind: kind, ID: append(denproto.Bytes{}, r.cfg.ID[:]...), Name: r.cfg.Name, At: r.cfg.Now().UnixMilli()}
	if keys {
		n.Offers = append(n.Offers, offerKeys)
	}
	if sound {
		n.Offers = append(n.Offers, offerSound)
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
// den that passes them: who, to ask who's there, or here, as what this
// install offers changes.
func (r *Relay) broadcast(kind string) {
	n := r.note(kind)
	for _, den := range r.cfg.Dens.RelayDens() {
		if r.send(den, nil, n) != nil {
			r.cfg.Log.Debugf("Relay: couldn't send %s through a den", kind)
		}
	}
}

// handle takes a note from another of the member's devices.
func (r *Relay) handle(in inbox) {
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
	d.name, d.offers, d.heard, d.den, d.key = n.Name, n.Offers, now, in.den, slices.Clone(in.from)
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

// answer answers a hello from another of the member's devices that this
// install can serve: for keys, where it holds the call in the den the
// hello came through and has Keybind relay on; for sound, where it shares
// its sound (M20.2). A device asks only while it has no link here, so a
// link this side holds from that device's earlier hello went stale, as
// when it started again before the link here failed, and the new one
// replaces it. A hello that crossed this side's own, which the other
// device answered, goes unanswered where this side's attempt wins: see
// keepsLocked.
func (r *Relay) answer(in inbox, id ID, n note) {
	keys := slices.Contains(n.Wants, offerKeys) && r.cfg.Dens.CallDen() == in.den
	sound := slices.Contains(n.Wants, offerSound)
	r.mu.Lock()
	keys = keys && r.keysOn
	sound = sound && r.sharesLocked()
	keep := r.keepsLocked(id, n.Attempt, false)
	local := r.local
	r.mu.Unlock()
	if !keys && !sound || keep || !local {
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
	r.link(id, in.den, in.from, n.Attempt, loc, n.Half, true)
}

// answered takes the answer to one of this install's hellos.
func (r *Relay) answered(in inbox, id ID, n note) {
	r.mu.Lock()
	var a *attempt
	for _, at := range []**attempt{&r.asking, &r.listenAsk} {
		if x := *at; x != nil && x.den == in.den && bytes.Equal(x.id, n.Attempt) && (x.to == ID{} || x.to == id) {
			a, *at = x, nil
		}
	}
	r.mu.Unlock()
	if a == nil {
		return
	}
	r.cfg.Log.Debugf("Relay with %s: its answer came", id.short())
	r.link(id, in.den, in.from, a.id, a.local, n.Half, false)
}

// keepsLocked says whether the link this install has with a device stays
// in place of one from a new attempt, mine where it's this side's own
// hello's: where the two hellos crossed, each side asking the other, the
// link of the greater attempt is kept, on both sides alike. A side that
// asks again gave up its earlier attempt, whose link the new one replaces.
func (r *Relay) keepsLocked(id ID, attempt denproto.Bytes, mine bool) bool {
	old := r.peers[id]
	return old != nil && old.mine != mine && bytes.Compare(old.attempt, attempt) > 0
}

// link starts a link with another of the member's installs from the two
// halves, of the hello's attempt, and watches it, in place of any other
// link with it but one keepsLocked keeps.
func (r *Relay) link(id ID, den string, key, attempt denproto.Bytes, loc *local, theirs *half, theyAsked bool) {
	r.mu.Lock()
	keep := r.keepsLocked(id, attempt, !theyAsked)
	r.mu.Unlock()
	if keep {
		loc.close()
		return
	}
	offer := bytes.Compare(r.cfg.ID[:], id[:]) < 0
	p := &peer{id: id, den: den, key: key, attempt: attempt, mine: !theyAsked, local: loc, in: newKeysIn(r.cfg.Hub, id.String(), r.cfg.Now),
		since: r.cfg.Now()}
	k, err := loc.dial(theirs, offer, r.cfg.Loopback, func(b []byte) { r.onCtl(p, b) }, func(b []byte) { r.onNow(p, b) },
		func(at uint32, piece []byte) { r.onSound(p, at, piece) })
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
// and closes it. A link closed as another replaced it, or as nobody
// needed it, says nothing of the network.
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
	if r.peers[p.id] != p {
		r.mu.Unlock()
		return
	}
	if d := r.devices[p.id]; d != nil {
		d.apart = apart
	}
	l := r.listening
	if l != nil && l.device != p.id {
		l = nil
	}
	if apart {
		r.failedLocked(now)
	} else {
		p.up, p.upAt = true, now
		r.tries = 0
		if l != nil {
			l.linked, l.tries = true, 0
		}
	}
	r.mu.Unlock()
	if apart {
		r.cfg.Log.Infof("Relay with %s: the link never came up, so the two aren't on one network", p.id.short())
		if l != nil {
			r.endListening(l, WhyApart)
		}
		return
	}
	r.cfg.Log.Infof("Relay with %s: linked in %s", p.id.short(), now.Sub(p.since).Round(time.Millisecond))
	poke(r.wake)
	r.session(ctx, p)
	r.cfg.Log.Infof("Relay with %s: the link closed after %s", p.id.short(), r.cfg.Now().Sub(now).Round(time.Second))
	r.mu.Lock()
	// A link lost can come back at once, through a new introduction.
	r.retry = r.cfg.Now().Add(time.Second)
	if l := r.listening; l != nil && l.device == p.id {
		l.retry = r.retry
	}
	r.mu.Unlock()
}

// onCtl takes what a link's ctl channel carries: the other side's state,
// and the end of this install's listening to it, as it says why.
func (r *Relay) onCtl(p *peer, b []byte) {
	p.in.hear()
	var c ctlMsg
	if json.Unmarshal(b, &c) != nil {
		return
	}
	switch c.T {
	case ctlState:
		r.mu.Lock()
		p.theirs, p.heard = c.state, true
		r.mu.Unlock()
		poke(r.wake)
	case ctlTaken:
		r.endListeningTo(p, WhyTaken)
	case ctlStopped:
		r.endListeningTo(p, WhyStopped)
	}
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

// ending is a listening to end, and why, once Relay's lock is let go.
type ending struct {
	l   *Listening
	why string
}

// evaluate goes over the links: each told this side's state as it
// changes, those nobody needs closed, the hub's keys handed over while one
// goes to a call, and a hello sent where the member's call is held by a
// device this one has no link to; and from M20.2 the device this
// install's sound goes to, the newest that listens taking over, the
// helper reading the sound while one does, and a hello sent for the sound
// this install listens to where it has no link to its device. It sends
// no hello until the system lets the install reach its network, and tells
// the system what Relay wants of it.
func (r *Relay) evaluate() {
	dens := r.cfg.Dens.RelayDens()
	calls := r.cfg.Dens.MemberCalls()
	held := r.cfg.Dens.CallDen()
	// The den of the member's call, and whether they're muted there, for
	// the page's button.
	memberDen := ""
	for _, d := range calls {
		if slices.Contains(dens, d) {
			memberDen = d
			break
		}
	}
	muted := memberDen != "" && r.cfg.Dens.MemberMuted(memberDen)
	now := r.cfg.Now()
	r.mu.Lock()
	shares := r.sharesLocked()
	base := state{Call: held != "", Keys: r.keysOn, Share: shares}
	var expired []*attempt
	if a := r.asking; a != nil && now.Sub(a.sent) >= introduction {
		expired, r.asking = append(expired, a), nil
		r.failedLocked(now)
	}
	callDen := ""
	if base.Keys && !base.Call {
		callDen = memberDen
	}
	if callDen != r.callDen {
		r.callDen, r.tries, r.retry = callDen, 0, time.Time{}
	}
	covered := r.asking != nil
	relaying := false
	var keysTo *peer
	var tell, closing, taken []*peer
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
			if base.Keys && !base.Call {
				relaying, keysTo = true, p
			}
		}
		mine := base
		mine.Listen = r.listening != nil && r.listening.device == id
		if p.sent == nil || *p.sent != mine {
			s := mine
			p.sent = &s
			tell = append(tell, p)
		}
		// A device that starts listening to this one's sound takes it
		// from whichever listened before, which is told.
		if p.heard {
			if p.theirs.Listen && !p.listened && shares {
				if r.listener != nil && r.listener != p {
					taken = append(taken, r.listener)
				}
				r.listener = p
			}
			p.listened = p.theirs.Listen
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
	if l := r.listener; l != nil && (!shares || r.peers[l.id] != l || !l.theirs.Listen) {
		r.listener = nil
	}
	capture := r.listener != nil
	captureChanged := capture != r.capturing
	r.capturing = capture
	hello := callDen != "" && !covered && !now.Before(r.retry) && r.local
	handOver := relaying != r.relaying
	r.relaying = relaying
	r.keysTo, r.keysMuted = keysTo, muted && keysTo != nil
	// Taking a call, this install says it's here, for a device whose hello
	// went unanswered while the den held the call for this one.
	tookCall := held != "" && held != r.held && base.Keys
	r.held = held

	// The sound this install listens to: its device told this side
	// listens, or asked for a link where there's none.
	var ends []ending
	var listenTo *device
	if l := r.listening; l != nil {
		p := r.peers[l.device]
		// A hello waits for its answer even where a link came from the
		// device's own hello, which crossed it: the device may keep this
		// one's. Unanswered, it ends the listening where there's no link.
		unanswered := r.listenAsk != nil && now.Sub(r.listenAsk.sent) >= listenWait
		if unanswered {
			expired = append(expired, r.listenAsk)
			r.listenAsk = nil
		}
		why := WhyNoAnswer
		if l.linked {
			why = WhyLost
		}
		switch {
		case r.refused && p == nil:
			ends = append(ends, ending{l, WhyLocal})
		case !r.local && p == nil:
			// The system asks the member first, as Android does.
		case p != nil:
			if p.up && p.heard && !p.theirs.Share {
				ends = append(ends, ending{l, WhyUnshared})
			}
		case unanswered:
			ends = append(ends, ending{l, why})
		case r.listenAsk != nil:
		case !now.Before(l.retry):
			if d := r.devices[l.device]; d != nil && l.tries < 2 {
				dd := *d
				listenTo = &dd
			} else {
				ends = append(ends, ending{l, why})
			}
		}
	}
	wants := Wants{On: r.keysOn || r.soundOn && r.cfg.CanSound || r.listening != nil, Listening: r.listening != nil}
	wanted := wants != r.wants
	r.wants = wants
	r.mu.Unlock()
	if tookCall {
		poke(r.heres)
	}
	if wanted && r.cfg.Wants != nil {
		r.cfg.Wants(wants)
	}

	for _, a := range expired {
		a.local.close()
		r.cfg.Log.Debugf("Relay: a hello went unanswered")
	}
	for _, p := range tell {
		if b, err := json.Marshal(ctlMsg{T: ctlState, state: *p.sent}); err == nil {
			_ = p.link.ctl.Send(b)
		}
	}
	for _, p := range taken {
		r.cfg.Log.Debugf("Relay with %s: another device took this computer's sound", p.id.short())
		_ = p.link.ctl.Send([]byte(`{"t":"` + ctlTaken + `"}`))
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
	if captureChanged {
		if capture {
			r.cfg.Hub.SetSound(r.takeSound, r.captureProblem)
		} else {
			r.cfg.Hub.SetSound(nil, nil)
		}
	}
	for _, e := range ends {
		r.endListening(e.l, e.why)
	}
	if hello {
		r.hello(callDen, now)
	}
	if listenTo != nil {
		r.listenHello(listenTo, now)
	}
	r.publish()
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
// interfaces that are up, and loopback's only where it has none and
// loopback is allowed: any install but a development instance refuses a
// half that names it. anet lists them where net can't: Android refuses
// an app net's netlink requests.
func localAddrs(loopback bool) []netip.Addr {
	ifaces, err := anet.Interfaces()
	if err != nil {
		return nil
	}
	private := privateAddr(false)
	var out, lo []netip.Addr
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := anet.InterfaceAddrsByInterface(&iface)
		if err != nil {
			continue
		}
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(n.IP)
			if ip = ip.Unmap(); !ok {
				continue
			}
			switch {
			case private(ip) && !slices.Contains(out, ip) && len(out) < maxAddrs:
				out = append(out, ip)
			case loopback && ip.Is4() && ip.IsLoopback() && len(lo) == 0:
				lo = append(lo, ip)
			}
		}
	}
	if len(out) == 0 {
		return lo
	}
	return out
}
