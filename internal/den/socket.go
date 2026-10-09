package den

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/coder/websocket"
)

const (
	pingInterval  = 30 * time.Second
	pingTimeout   = 15 * time.Second
	writeTimeout  = 10 * time.Second
	eventsPerSend = 256
	replayChunk   = 500
	// frameBudget bounds a frame's events as JSON, below the 1 MiB a client
	// reads, with room for the array around them.
	frameBudget = denproto.MaxBody - 64<<10
)

// eventSize is about how much of a frame an event takes: its payload and
// type, and the object around them.
func eventSize(e denproto.Event) int { return len(e.D) + len(e.T) + 48 }

type closeRequest struct {
	code   websocket.StatusCode
	reason string
}

type socket struct {
	member    int64
	keyID     []byte
	tokenHash []byte
	closeReq  chan closeRequest
	// voiceOut holds the offers and endings of the call this socket joined,
	// which go to it alone.
	voiceOut chan denproto.Event
	// forGood marks a socket the den closed because its device or member is
	// done here, whose call ends rather than waits for the device (M3).
	forGood atomic.Bool
}

// voice queues an event of the socket's call. A full queue drops it: an
// offer that never arrives ends its call when its answer is due.
func (sock *socket) voice(t string, data any) {
	e, err := denproto.NewEvent(t, 0, data)
	if err != nil {
		return
	}
	select {
	case sock.voiceOut <- e:
	default:
	}
}

// requestClose asks the socket's writer to close it. The first request
// wins; later ones are dropped rather than blocking. A revoked device, a
// member who's gone, and a logout close it for good.
func (sock *socket) requestClose(code websocket.StatusCode, reason string) {
	if code == denproto.CloseRevoked || code == websocket.StatusNormalClosure {
		sock.forGood.Store(true)
	}
	select {
	case sock.closeReq <- closeRequest{code, reason}:
	default:
	}
}

// socketSet tracks open sockets, so the den can close them on shutdown, and
// a session's or member's sockets when access ends.
type socketSet struct {
	mu      sync.Mutex
	open    map[*socket]struct{}
	closing bool
	empty   *sync.Cond
}

func newSocketSet() *socketSet {
	s := &socketSet{open: map[*socket]struct{}{}}
	s.empty = sync.NewCond(&s.mu)
	return s
}

func (s *socketSet) add(sock *socket) (ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	n := 0
	for other := range s.open {
		if other.member == sock.member {
			n++
		}
	}
	if n >= maxSocketsPerMember {
		return false
	}
	s.open[sock] = struct{}{}
	return true
}

func (s *socketSet) remove(sock *socket) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.open, sock)
	if len(s.open) == 0 {
		s.empty.Broadcast()
	}
}

func (s *socketSet) closeWhere(match func(*socket) bool, code websocket.StatusCode, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sock := range s.open {
		if match(sock) {
			sock.requestClose(code, reason)
		}
	}
}

func (s *socketSet) closeSession(tokenHash []byte, code websocket.StatusCode, reason string) {
	s.closeWhere(func(sock *socket) bool { return bytes.Equal(sock.tokenHash, tokenHash) }, code, reason)
}

// closeKey closes the sockets of every session a device key started.
func (s *socketSet) closeKey(keyID []byte, code websocket.StatusCode, reason string) {
	s.closeWhere(func(sock *socket) bool { return bytes.Equal(sock.keyID, keyID) }, code, reason)
}

// CloseMemberSockets closes a member's sockets with a code: 4003 when
// their access ends, or 4008 to make them reconnect and resume.
func (d *Den) CloseMemberSockets(member int64, code websocket.StatusCode, reason string) {
	d.sockets.closeWhere(func(sock *socket) bool { return sock.member == member }, code, reason)
}

// CloseSockets tells every client the den is restarting (close 1012) and
// waits up to timeout for the sockets to close. The caller must have closed
// the den listener first, so no client reconnects to this process.
func (d *Den) CloseSockets(timeout time.Duration) {
	s := d.sockets
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	s.closeWhere(func(*socket) bool { return true }, websocket.StatusServiceRestart, "den restarting")
	done := make(chan struct{})
	go func() {
		s.mu.Lock()
		for len(s.open) > 0 {
			s.empty.Wait()
		}
		s.mu.Unlock()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// parseResume reads "<epoch>.<seq>"; anything else means no resume point.
func parseResume(v string) (string, uint64) {
	epoch, seqText, ok := strings.Cut(v, ".")
	if !ok {
		return "", 0
	}
	seq, err := strconv.ParseUint(seqText, 10, 64)
	if err != nil {
		return "", 0
	}
	return epoch, seq
}

// ServeSocket upgrades an authenticated request to the event stream.
func (d *Den) ServeSocket(w http.ResponseWriter, r *http.Request, s *Session) {
	sock := &socket{member: s.MemberID, keyID: s.KeyID, tokenHash: s.TokenHash, closeReq: make(chan closeRequest, 1),
		voiceOut: make(chan denproto.Event, 16)}
	if !d.sockets.add(sock) {
		denproto.WriteError(w, denproto.Errorf(http.StatusTooManyRequests, denproto.CodeRateLimited,
			"too many connections for this member, or the den is stopping"))
		return
	}
	defer d.sockets.remove(sock)
	// A call waits for its device when its socket closes, or ends with it.
	defer d.socketClosed(sock)
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(denproto.MaxClientFrame)
	d.socketsChanged(s.MemberID, 1)
	defer d.socketsChanged(s.MemberID, -1)

	// The request context ends when the service stops. The socket outlives
	// it until the den closes it with 1012, after the listener has closed.
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()

	epoch, after := parseResume(r.URL.Query().Get("resume"))
	sub, missed, resumed, seq := d.Hub.Subscribe(s.MemberID, IsStaff(s.Role), epoch, after)
	defer d.Hub.Unsubscribe(sub)

	write := func(events ...denproto.Event) bool {
		frame, err := denproto.EncodeFrame(events...)
		if err != nil {
			return false
		}
		wctx, wcancel := context.WithTimeout(ctx, writeTimeout)
		defer wcancel()
		return c.Write(wctx, websocket.MessageText, frame) == nil
	}
	// sendReady sends a snapshot starting at seq. Subscribing first means
	// nothing published after the snapshot is lost; an event racing it may
	// repeat state the snapshot already has, and applying one twice is
	// harmless.
	sendReady := func(seq uint64) bool {
		// The role can change while the socket is open; the hub knows the
		// current one.
		snap, err := d.snapshot(ctx, s.MemberID, d.Hub.Staff(sub), seq)
		if err != nil {
			d.log.Errorf("den snapshot: %v", err)
			c.Close(websocket.StatusInternalError, "")
			return false
		}
		ready, err := denproto.NewEvent(denproto.EventReady, 0, snap)
		return err == nil && write(ready)
	}

	if resumed {
		// Presence and calls aren't replayed, so a resumed client gets who's
		// online and in calls now.
		first, _ := denproto.NewEvent(denproto.EventResumed, 0, nil)
		online, _ := denproto.NewEvent(denproto.EventPresence, 0, denproto.Presence{Online: d.online(), Full: true})
		calls, _ := denproto.NewEvent(denproto.EventVoiceState, 0, denproto.VoiceState{Calls: d.visibleCalls(d.Hub.Staff(sub)), Full: true})
		batch := []denproto.Event{first, online, calls}
		size := eventSize(first) + eventSize(online) + eventSize(calls)
		for _, e := range missed {
			if len(batch) >= replayChunk || size+eventSize(e) > frameBudget {
				if !write(batch...) {
					return
				}
				batch, size = batch[:0], 0
			}
			batch = append(batch, e)
			size += eventSize(e)
		}
		if len(batch) > 0 && !write(batch...) {
			return
		}
	} else if !sendReady(seq) {
		return
	}

	var expires atomic.Int64
	expires.Store(s.ExpiresAt.UnixMilli())
	ephemeral := make(chan denproto.Event, 8)
	go d.readClient(ctx, cancel, c, s, sock, sub, &expires, ephemeral)
	go func() {
		t := time.NewTicker(pingInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pctx, pcancel := context.WithTimeout(ctx, pingTimeout)
				err := c.Ping(pctx)
				pcancel()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()

	expiry := time.NewTimer(time.Until(time.UnixMilli(expires.Load())))
	defer expiry.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-sock.closeReq:
			c.Close(req.code, req.reason)
			return
		case e, ok := <-sub.Events:
			if !ok {
				if sub.Slow() {
					c.Close(denproto.CloseTooSlow, "too slow")
				}
				return
			}
			// Under load, send everything already queued as one frame. A
			// refresh ends the batch: it replaces the client's state, so
			// events before it go first and the snapshot follows.
			var batch []denproto.Event
			size := 0
			for {
				if e.T == eventRefresh {
					if len(batch) > 0 && !write(batch...) {
						return
					}
					batch, size = nil, 0
					if !sendReady(e.Seq) {
						return
					}
				} else {
					if len(batch) > 0 && size+eventSize(e) > frameBudget {
						if !write(batch...) {
							return
						}
						batch, size = nil, 0
					}
					batch = append(batch, e)
					size += eventSize(e)
				}
				if len(batch) >= eventsPerSend {
					break
				}
				var more bool
				select {
				case e, more = <-sub.Events:
				default:
				}
				if !more {
					break
				}
			}
			if len(batch) > 0 && !write(batch...) {
				return
			}
		case e := <-ephemeral:
			if !write(e) {
				return
			}
		case e := <-sock.voiceOut:
			if !write(e) {
				return
			}
		case <-expiry.C:
			left := time.Until(time.UnixMilli(expires.Load()))
			if left <= 0 {
				c.Close(denproto.CloseSessionExpired, "session expired")
				return
			}
			expiry.Reset(left)
		}
	}
}

// readClient handles frames from the client until the socket closes.
func (d *Den) readClient(ctx context.Context, cancel context.CancelFunc, c *websocket.Conn, s *Session,
	sock *socket, sub *Sub, expires *atomic.Int64, ephemeral chan<- denproto.Event) {
	defer cancel()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		events, err := denproto.DecodeFrame(data)
		if err != nil {
			sock.requestClose(websocket.StatusInvalidFramePayloadData, "malformed frame")
			return
		}
		for _, e := range events {
			switch e.T {
			case denproto.EventAuthRenew:
				var req denproto.Renew
				if json.Unmarshal(e.D, &req) != nil {
					continue
				}
				until, err := d.Renew(ctx, s, req.Nonce, req.Proof)
				if denproto.IsCode(err, denproto.CodeKeyRevoked) {
					sock.requestClose(denproto.CloseRevoked, denproto.CloseReasonKeyRevoked)
					return
				}
				if err != nil {
					// The client retries; if the token expires first, the
					// socket closes with 4001 and the client logs in again.
					var perr *denproto.Error
					if !errors.As(err, &perr) {
						d.log.Warnf("renew session: %v", err)
					}
					continue
				}
				expires.Store(until.UnixMilli())
				renewed, _ := denproto.NewEvent(denproto.EventAuthRenewed, 0, denproto.Renewed{ExpiresAt: until.UnixMilli()})
				select {
				case ephemeral <- renewed:
				default:
				}
			case denproto.EventFocus:
				// Each focus looks its channels up, so it shares the member's
				// budget for writes; one past it is dropped, and the next
				// change or connection sends focus again.
				var f denproto.Focus
				if json.Unmarshal(e.D, &f) == nil && len(f.Channels) <= denproto.MaxFocus &&
					d.Allow(LimitWrite, strconv.FormatInt(s.MemberID, 10)) == nil {
					d.focus(ctx, sub, f.Channels)
				}
			case denproto.EventTyping:
				var t denproto.Typing
				if json.Unmarshal(e.D, &t) == nil {
					d.typing(sub, t.ChannelID)
				}
			case denproto.EventVoiceJoin:
				var req denproto.VoiceJoin
				if json.Unmarshal(e.D, &req) == nil {
					d.joinCall(ctx, s, sock, sub, req)
				}
			case denproto.EventVoiceAnswer:
				var req denproto.VoiceAnswer
				if json.Unmarshal(e.D, &req) == nil {
					d.answerCall(sock, req)
				}
			case denproto.EventVoiceMute:
				var req denproto.VoiceMute
				if json.Unmarshal(e.D, &req) == nil && d.Allow(LimitWrite, strconv.FormatInt(s.MemberID, 10)) == nil {
					d.muteCall(sock, req)
				}
			case denproto.EventVoiceLeave:
				d.leaveCall(sock)
			case denproto.EventVoiceRestart:
				if d.Allow(LimitWrite, strconv.FormatInt(s.MemberID, 10)) == nil {
					d.restartCall(sock)
				}
			case denproto.EventVoiceShare:
				var req denproto.VoiceShare
				if json.Unmarshal(e.D, &req) == nil {
					d.shareCall(s, sock, req)
				}
			case denproto.EventVoiceWatch:
				var req denproto.VoiceWatch
				if json.Unmarshal(e.D, &req) == nil {
					d.watchCall(s, sock, req)
				}
			}
			// Unknown event types are ignored, as the protocol requires.
		}
	}
}
