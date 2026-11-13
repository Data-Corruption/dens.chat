package relay

import (
	"encoding/binary"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Sound (M20.2): one computer's sound, read by its helper, crosses a link
// as RTP to the device that listens, whose service hands each piece to the
// page that asked. A page listens to one device at a time, and a device's
// sound goes to one listener: a new one takes over, and the one before is
// told. An install plays or sends, never both, since two devices playing
// each other would loop: listening pauses its own sharing.

// Why a listening ended, as the page that listened is told.
const (
	// WhyPage is another page of this install's listening in its place.
	WhyPage = "page"
	// WhyTaken is another of the member's devices listening in its place.
	WhyTaken = "taken"
	// WhyStopped is the member stopping it on the device that shared.
	WhyStopped = "stopped"
	// WhyUnshared is the device no longer sharing its sound, as its switch
	// went off or it began listening itself.
	WhyUnshared = "unshared"
	// WhyNoAnswer is no answer from the device, and WhyLost none after its
	// link was lost.
	WhyNoAnswer = "noanswer"
	WhyLost     = "lost"
	// WhyApart is a link that never came up: the two aren't on one
	// network.
	WhyApart = "apart"
	// WhyLocal is the member refusing this install its network, where the
	// system asks them, as Android does.
	WhyLocal = "local"
	// WhyClosing is the service stopping.
	WhyClosing = "closing"
)

// ErrNotSharing is a listening asked of a device that doesn't share its
// sound.
var ErrNotSharing = errors.New("that device isn't sharing its sound")

// listenQueue bounds the packets waiting for the page: 256 are 1.28 s.
const listenQueue = 256

// Listening is a page's listening to another of the member's devices'
// sound: its packets as they come, and its end, with why.
type Listening struct {
	r      *Relay
	device ID
	// Name is the device's, as it last said.
	Name    string
	packets chan []byte
	done    chan struct{}
	once    sync.Once
	why     string
	// Guarded by Relay.mu: whether a link with the device came up during
	// it; how many hellos have gone unanswered or found no link since;
	// and when it may ask again.
	linked bool
	tries  int
	retry  time.Time
	// The packets that came, and those lost on the way, by the gaps in
	// their places; next is the place the next is due at, which a link
	// replacing another may reach for at once with it.
	got, lost atomic.Int64
	nextMu    sync.Mutex
	next      uint32
}

// Packets are the device's sound for the page: each its place in the
// stream, in frames, 4 bytes little-endian, then the piece as the helper
// cut it, its rate and its frames. One that finds the page behind goes.
func (l *Listening) Packets() <-chan []byte { return l.packets }

// Done is closed as the listening ends.
func (l *Listening) Done() <-chan struct{} { return l.done }

// Why says why the listening ended, once Done is closed: "" for the page
// stopping it.
func (l *Listening) Why() string {
	<-l.done
	return l.why
}

// Stop ends the listening, as the page stops or goes.
func (l *Listening) Stop() { l.r.endListening(l, "") }

func (l *Listening) finish(why string) {
	l.once.Do(func() {
		l.why = why
		close(l.done)
	})
}

func (l *Listening) push(at uint32, piece []byte) {
	frames := uint32(len(piece)-4) / 4
	l.nextMu.Lock()
	if gap := int32(at - l.next); l.got.Load() > 0 && gap > 0 && frames > 0 {
		l.lost.Add(int64(uint32(gap) / frames))
	}
	l.next = at + frames
	l.got.Add(1)
	l.nextMu.Unlock()
	b := make([]byte, 4+len(piece))
	binary.LittleEndian.PutUint32(b, at)
	copy(b[4:], piece)
	select {
	case l.packets <- b:
	default:
	}
}

// Listen starts this install's listening to another of the member's
// devices, by its Relay ID, which must share its sound. A listening
// already under way here ends, its page told another took over.
func (r *Relay) Listen(device string) (*Listening, error) {
	id, err := ParseID(device)
	if err != nil {
		return nil, err
	}
	now := r.cfg.Now()
	r.mu.Lock()
	d := r.devices[id]
	if d == nil || id == r.cfg.ID || !slices.Contains(d.offers, offerSound) || now.Sub(d.heard) >= forget {
		r.mu.Unlock()
		return nil, ErrNotSharing
	}
	old := r.listening
	l := &Listening{r: r, device: id, Name: d.name, packets: make(chan []byte, listenQueue), done: make(chan struct{})}
	r.listening = l
	var dropped *attempt
	if a := r.listenAsk; a != nil && a.to != id {
		dropped, r.listenAsk = a, nil
	}
	paused := old == nil && r.soundOn && r.cfg.CanSound
	r.mu.Unlock()
	if dropped != nil {
		dropped.local.close()
	}
	if old != nil {
		old.finish(WhyPage)
		r.cfg.Log.Debugf("Relay with %s: stopped listening to its sound, %d packets heard and %d lost: %s", old.device.short(), old.got.Load(),
			old.lost.Load(), WhyPage)
	}
	r.cfg.Log.Debugf("Relay with %s: listening to its sound", id.short())
	// Listening pauses this install's own sharing, which its other
	// devices hear of.
	if paused {
		poke(r.heres)
	}
	poke(r.wake)
	return l, nil
}

// endListening ends a listening, if it's still this install's.
func (r *Relay) endListening(l *Listening, why string) {
	r.mu.Lock()
	if r.listening != l {
		r.mu.Unlock()
		l.finish(why)
		return
	}
	r.listening = nil
	a := r.listenAsk
	r.listenAsk = nil
	resumed := r.soundOn && r.cfg.CanSound
	r.mu.Unlock()
	if a != nil {
		a.local.close()
	}
	l.finish(why)
	if why == "" {
		why = "the page stopped"
	}
	r.cfg.Log.Debugf("Relay with %s: stopped listening to its sound, %d packets heard and %d lost: %s", l.device.short(), l.got.Load(),
		l.lost.Load(), why)
	if resumed {
		poke(r.heres)
	}
	poke(r.wake)
}

// endListeningTo ends this install's listening to a device, as the device
// said why, on the link it's listening on.
func (r *Relay) endListeningTo(p *peer, why string) {
	r.mu.Lock()
	l := r.listening
	ok := l != nil && l.device == p.id && r.peers[p.id] == p
	r.mu.Unlock()
	if ok {
		r.endListening(l, why)
	}
}

// listenHello asks the device this install listens to for a link, for its
// sound, through the den and to the device key it last said it through.
func (r *Relay) listenHello(d *device, now time.Time) {
	r.mu.Lock()
	l := r.listening
	if l != nil {
		l.tries++
	}
	r.mu.Unlock()
	if l == nil {
		return
	}
	loc, err := newLocal(r.cfg.Addrs())
	if err != nil {
		r.cfg.Log.Debugf("Relay: couldn't ask for a link: %v", err)
		r.endListening(l, WhyApart)
		return
	}
	n := r.note(kindHello)
	n.Attempt, n.Wants, n.Half = denproto.Random(attemptSize), []string{offerSound}, loc.half()
	if err := r.send(d.den, d.key, n); err != nil {
		loc.close()
		r.cfg.Log.Debugf("Relay: couldn't send a hello")
		r.endListening(l, WhyNoAnswer)
		return
	}
	r.mu.Lock()
	if r.listening != l {
		r.mu.Unlock()
		loc.close()
		return
	}
	r.listenAsk = &attempt{id: n.Attempt, local: loc, den: d.den, to: l.device, sent: now}
	r.mu.Unlock()
	r.cfg.Log.Debugf("Relay with %s: asked for a link, for its sound", l.device.short())
}

// onSound takes a piece of a device's sound, for the page that listens to
// it. One too short to hold its rate and a frame goes.
func (r *Relay) onSound(p *peer, at uint32, piece []byte) {
	if len(piece) < 8 {
		return
	}
	r.mu.Lock()
	l := r.listening
	ok := l != nil && l.device == p.id && r.peers[p.id] == p
	r.mu.Unlock()
	if ok {
		l.push(at, piece)
	}
}

// takeSound sends a piece of this computer's sound, as the helper read it,
// to the device that listens.
func (r *Relay) takeSound(piece []byte) {
	r.mu.Lock()
	p := r.listener
	r.mu.Unlock()
	if p != nil {
		p.link.sendSound(piece)
	}
}

// captureProblem hears why the helper reads no sound, "" once it does.
func (r *Relay) captureProblem(why string) {
	r.mu.Lock()
	r.captureWhy = why
	r.mu.Unlock()
	r.publish()
}

// StopSharing stops this computer's sound going to the device that
// listens, which is told, as the member stops it here. Its switch stays
// on: listening again starts again.
func (r *Relay) StopSharing() {
	r.mu.Lock()
	p := r.listener
	r.listener = nil
	r.mu.Unlock()
	if p != nil {
		r.cfg.Log.Debugf("Relay with %s: stopped sharing this computer's sound with it", p.id.short())
		_ = p.link.ctl.Send([]byte(`{"t":"` + ctlStopped + `"}`))
	}
	poke(r.wake)
}

// Status is what the pages learn of Relay: the device this computer's
// sound goes to, while one listens, and why none comes; and the device
// holding the member's call, while this install's keys go there, for the
// call's button.
type Status struct {
	Sharing *Sharing `json:"sharing"`
	Problem string   `json:"problem,omitempty"`
	Call    *Holding `json:"call,omitempty"`
}

// Sharing names the device this computer's sound goes to.
type Sharing struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Holding names the device that holds the member's call, and says whether
// the member is muted there.
type Holding struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Muted bool   `json:"muted,omitempty"`
}

// statusBuffer bounds the statuses a page may fall behind by before its
// channel closes and it reloads.
const statusBuffer = 8

// Watch returns the statuses as they change, starting with the one now,
// and stops them. A watcher that falls behind has its channel closed.
func (r *Relay) Watch() (<-chan Status, func()) {
	ch := make(chan Status, statusBuffer)
	r.mu.Lock()
	r.watchers[ch] = struct{}{}
	ch <- r.status
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		if _, ok := r.watchers[ch]; ok {
			delete(r.watchers, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
}

// publish tells the watchers the status, where it changed.
func (r *Relay) publish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	var st Status
	if p := r.listener; p != nil {
		st.Sharing = &Sharing{ID: p.id.String(), Name: r.nameLocked(p.id)}
		st.Problem = r.captureWhy
	}
	if p := r.keysTo; p != nil {
		st.Call = &Holding{ID: p.id.String(), Name: r.nameLocked(p.id), Muted: r.keysMuted}
	}
	if same(st, r.status) {
		return
	}
	r.status = st
	for ch := range r.watchers {
		select {
		case ch <- st:
		default:
			delete(r.watchers, ch)
			close(ch)
		}
	}
}

// nameLocked is a device's name, as it last said.
func (r *Relay) nameLocked(id ID) string {
	if d := r.devices[id]; d != nil {
		return d.name
	}
	return ""
}

func same(a, b Status) bool {
	if (a.Sharing == nil) != (b.Sharing == nil) || a.Problem != b.Problem || (a.Call == nil) != (b.Call == nil) {
		return false
	}
	return (a.Sharing == nil || *a.Sharing == *b.Sharing) && (a.Call == nil || *a.Call == *b.Call)
}
