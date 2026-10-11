// Package shortcuts carries the voice keys to other programs (M7). A page
// sees keys only while it has focus, and the service none at all, so a
// helper runs as the desktop user in their session, watches the keys they
// bound while a page is in a call, and sends each press over a stream on
// the control endpoint. The Hub is the service's side of that stream, and
// the Helper the desktop user's.
package shortcuts

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"sync"

	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// Command is the dens command that runs the helper, which the installer
// starts at the desktop user's login.
const Command = "shortcuts"

// Lines on the stream, besides pings. The service sends hello first, then
// state whenever the keys or its call change, and replaced when a newer
// helper takes over. The helper sends source, what it can watch, after
// each state, and press for each press and release of a bound key.
const (
	msgHello    = "hello"
	msgState    = "state"
	msgReplaced = "replaced"
	msgSource   = "source"
	msgPress    = "press"
)

type hello struct {
	Version string `json:"version"`
}

// state is what the helper watches: the keys, by action, while the
// instance holds a call.
type state struct {
	Keys map[string]types.VoiceKey `json:"keys"`
	Call bool                      `json:"call"`
}

// Source is what the helper's keys can do, which pages learn: the actions
// it watches and, where the desktop confirms the keys and names them, its
// names (M7.2). Note says what the system keeps from it, for the settings
// to show, and Problem why it can't watch any.
type Source struct {
	Covers   []string          `json:"covers"`
	Confirms bool              `json:"confirms,omitempty"`
	Names    map[string]string `json:"names,omitempty"`
	Note     string            `json:"note,omitempty"`
	Problem  string            `json:"problem,omitempty"`
}

// Press is an action's key going down or up.
type Press struct {
	Action string `json:"action"`
	Down   bool   `json:"down"`
}

// Status is what a page learns of the keys and the helper.
type Status struct {
	Keys   map[string]types.VoiceKey `json:"keys"`
	Helper bool                      `json:"helper"`
	Source
}

// Event goes to pages: the status as it changes, or a press during a call.
type Event struct {
	Status *Status
	Press  *Press
}

// pageBuffer bounds the events a page may fall behind by before its
// channel closes and it reloads.
const pageBuffer = 64

// Hub is the service's side: the member's keys, whether the install holds a
// call, the one helper connected, and the pages listening.
type Hub struct {
	version string
	log     *xlog.Logger

	mu     sync.Mutex
	keys   map[string]types.VoiceKey
	call   bool
	helper *link
	source Source
	pages  map[chan Event]struct{}
}

// link is a connected helper; wake tells its writer the state changed.
type link struct {
	stream *control.Stream
	wake   chan struct{}
}

// NewHub returns a hub for a service at version, with the member's keys.
func NewHub(version string, keys map[string]types.VoiceKey, log *xlog.Logger) *Hub {
	return &Hub{version: version, log: log, keys: maps.Clone(keys), pages: make(map[chan Event]struct{})}
}

// Watch returns a page's events, starting with the status, and stops them.
// A page that falls behind has its channel closed, and reloads.
func (h *Hub) Watch() (<-chan Event, func()) {
	ch := make(chan Event, pageBuffer)
	h.mu.Lock()
	h.pages[ch] = struct{}{}
	ch <- Event{Status: h.statusLocked()}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.pages[ch]; ok {
			delete(h.pages, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// Keys returns the member's keys.
func (h *Hub) Keys() map[string]types.VoiceKey {
	h.mu.Lock()
	defer h.mu.Unlock()
	return maps.Clone(h.keys)
}

// SetKeys takes the member's keys as the settings change them.
func (h *Hub) SetKeys(keys map[string]types.VoiceKey) {
	h.mu.Lock()
	h.keys = maps.Clone(keys)
	h.wakeLocked()
	h.broadcastLocked(Event{Status: h.statusLocked()})
	h.mu.Unlock()
}

// SetCall says whether the install holds a call: the helper watches keys
// only while it does.
func (h *Hub) SetCall(active bool) {
	h.mu.Lock()
	if h.call != active {
		h.call = active
		h.wakeLocked()
	}
	h.mu.Unlock()
}

// Serve is the stream of a helper's shortcuts request. One helper is
// connected at a time: the newest replaces the last, which then exits.
func (h *Hub) Serve(ctx context.Context, s *control.Stream) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Closing the stream as Serve returns, or as the service stops, ends
	// its Recv, its pings and its writer.
	context.AfterFunc(ctx, func() { _ = s.Close() })
	l := &link{stream: s, wake: make(chan struct{}, 1)}
	h.mu.Lock()
	old := h.helper
	h.helper, h.source = l, Source{}
	h.broadcastLocked(Event{Status: h.statusLocked()})
	h.mu.Unlock()
	if old != nil {
		// The old helper closes its end as it leaves. Closing it here
		// could reset the stream before the line is read, on a socket
		// with the helper's own lines still unread, and the old helper
		// would come back and take over again. Until it goes, the hub
		// ignores it.
		_ = old.stream.Send(msgReplaced, nil)
	}
	h.log.Info("The shortcut helper connected")
	defer h.drop(l)

	if err := s.Send(msgHello, hello{Version: h.version}); err != nil {
		return err
	}
	go s.KeepAlive(ctx)
	go h.sendState(ctx, l)
	for {
		msg, err := s.Recv()
		if err != nil {
			return err
		}
		switch msg.T {
		case msgSource:
			var src Source
			if json.Unmarshal(msg.D, &src) != nil {
				continue
			}
			h.setSource(l, src)
		case msgPress:
			var p Press
			if json.Unmarshal(msg.D, &p) != nil || !slices.Contains(types.VoiceActions, p.Action) {
				continue
			}
			h.press(l, p)
		}
	}
}

// sendState sends the helper the state now and whenever it changes.
func (h *Hub) sendState(ctx context.Context, l *link) {
	for {
		h.mu.Lock()
		st := state{Keys: maps.Clone(h.keys), Call: h.call}
		h.mu.Unlock()
		if st.Keys == nil {
			st.Keys = map[string]types.VoiceKey{}
		}
		if l.stream.Send(msgState, st) != nil {
			_ = l.stream.Close()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-l.wake:
		}
	}
}

func (h *Hub) setSource(l *link, src Source) {
	covers := make([]string, 0, len(src.Covers))
	for _, a := range src.Covers {
		if slices.Contains(types.VoiceActions, a) && !slices.Contains(covers, a) {
			covers = append(covers, a)
		}
	}
	src.Covers = covers
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.helper != l {
		return
	}
	if src.Problem != "" && src.Problem != h.source.Problem {
		h.log.Warnf("The shortcut helper can't watch keys: %s", src.Problem)
	}
	h.source = src
	h.broadcastLocked(Event{Status: h.statusLocked()})
}

// press passes a press to the pages while the install holds a call; the
// helper watches only then, so a press outside one is late, and dropped.
func (h *Hub) press(l *link, p Press) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.helper != l || !h.call {
		return
	}
	h.broadcastLocked(Event{Press: &p})
}

func (h *Hub) drop(l *link) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.helper != l {
		return
	}
	h.helper, h.source = nil, Source{}
	h.broadcastLocked(Event{Status: h.statusLocked()})
	h.log.Info("The shortcut helper disconnected")
}

func (h *Hub) wakeLocked() {
	if h.helper == nil {
		return
	}
	select {
	case h.helper.wake <- struct{}{}:
	default:
	}
}

func (h *Hub) statusLocked() *Status {
	st := &Status{Keys: maps.Clone(h.keys), Helper: h.helper != nil, Source: h.source}
	if st.Keys == nil {
		st.Keys = map[string]types.VoiceKey{}
	}
	if st.Covers == nil {
		st.Covers = []string{}
	}
	return st
}

func (h *Hub) broadcastLocked(e Event) {
	for ch := range h.pages {
		select {
		case ch <- e:
		default:
			delete(h.pages, ch)
			close(ch)
		}
	}
}
