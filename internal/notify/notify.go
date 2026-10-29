// Package notify decides what notifies the member, and where it shows
// (M12.3). The service decides, since a page may be hidden, frozen or
// closed: a DM, or a message that mentions the member, in a den and
// channel they haven't muted, notifies them unless a page shows that
// channel on screen. Each channel's newest such message shows in one
// place: a page that may notify, the one on screen most lately, which
// shows it through the browser; with none, the desktop's helper; and on
// Android, the app's shell. Reading the channel takes it back wherever it
// shows.
package notify

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// TextLimit is how much of a message a notification shows, in characters.
const TextLimit = 120

// maxShown bounds the channels the hub remembers showing a notification
// for; past it, the oldest is forgotten, and stays where it shows.
const maxShown = 256

// pageBuffer bounds the events a page may fall behind by before its
// channel closes and it reloads.
const pageBuffer = 64

// Message is a message another member sent, as it arrives, with its den's
// and channel's names and its author's.
type Message struct {
	Den, DenName         string
	Channel, ChannelName string
	DM                   bool
	ID                   string
	Author               string
	// Mention is whether it mentions the member.
	Mention bool
	// Text is what it says, opened where it's a DM's, and "" where this
	// device couldn't open it; Files is how many files it carries.
	Text  string
	Files int
}

// Notification is what a place shows for a channel: its newest message
// that notifies, by its den, channel and ID, with the den's name, a title
// naming who sent it and where, and the start of what it says.
type Notification struct {
	// Key names the channel's notification, which a newer message's
	// replaces, and which reading the channel takes back.
	Key     string `json:"key"`
	Den     string `json:"den"`
	DenName string `json:"denName"`
	Channel string `json:"channel"`
	Message string `json:"message"`
	Title   string `json:"title"`
	Text    string `json:"text"`
	// Path is where the page shows the message, from its origin.
	Path string `json:"path"`
}

// Heading is the notification's title, with its den's name where it has
// one, for a place that shows one line above the text.
func (n Notification) Heading() string {
	if n.DenName == "" {
		return n.Title
	}
	return n.Title + " · " + n.DenName
}

// URL is the page's address at the message, from origin, the page's
// origin. A pairing token, where there's one, opens it in a browser that
// isn't paired, as dens open's link does.
func (n Notification) URL(origin, token string) string {
	if token == "" {
		return origin + n.Path
	}
	path, message, _ := strings.Cut(n.Path, "#")
	return origin + path + "#token=" + token + "&" + message
}

// Place shows notifications outside the pages: the desktop's helper, or
// the Android app's shell.
type Place interface {
	// Show shows n, replacing what it shows for n's channel, and reports
	// whether it could.
	Show(n Notification) bool
	// Withdraw takes back what it shows of keys.
	Withdraw(keys []string)
	// Settings hears the member's settings, as they are and as they
	// change.
	Settings(s types.Notifications)
}

// Event goes to a page: the status, at first and as it changes, or a
// notification to show or take back through the browser.
type Event struct {
	Status   *Status
	Show     *Notification
	Withdraw []string
}

// Status is what a page learns of notifications: the member's settings,
// and on Android, whether Android lets the app post notifications, as its
// shell last said, which the settings show.
type Status struct {
	types.Notifications
	Shell *ShellStatus `json:"shell,omitempty"`
}

// ShellStatus is the Android app's shell's say on notifications.
type ShellStatus struct {
	Allowed bool `json:"allowed"`
}

// Hub is the service's: the member's settings, the pages and what each
// shows, the places, and what shows where.
type Hub struct {
	log *xlog.Logger
	// helper is the desktop's helper, whose Show fails while none can
	// show a notification.
	helper Place
	// android is the Android form, whose page counts as on screen only
	// while its shell says the app is in front: the shell holds the page
	// as shown through a call, whatever the screen does (M12.2).
	android bool

	mu       sync.Mutex
	settings types.Notifications
	keep     func(types.Notifications) error
	pages    map[string]*page
	shell    Place
	front    bool
	// allowed is whether Android lets the app post notifications, as its
	// shell says.
	allowed bool
	// shown is each channel's notification, by key, oldest first in order.
	shown map[string]shown
	order []string
	// seen counts pages coming on screen, which orders them.
	seen uint64
}

// page is a page's stream and what it says of itself: the channel it shows
// in each den, whether it's on screen, whether the browser lets it notify,
// and when it was last on screen.
type page struct {
	events  chan Event
	focus   map[string]string
	visible bool
	allowed bool
	seen    uint64
}

// shown is a notification out: its message, whether it's for a DM, and
// where it shows, a page's ID, or the helper or the shell, or "" for a page
// that's gone.
type shown struct {
	den, channel, message string
	dm                    bool
	at                    string
}

const (
	atHelper = "helper"
	atShell  = "shell"
)

// New returns a hub with the member's settings, which keep stores as they
// change, and the desktop's helper.
func New(log *xlog.Logger, settings types.Notifications, keep func(types.Notifications) error, helper Place, android bool) *Hub {
	return &Hub{log: log, settings: clone(settings), keep: keep, helper: helper, android: android,
		pages: map[string]*page{}, shown: map[string]shown{}}
}

func clone(s types.Notifications) types.Notifications {
	s.Muted = maps.Clone(s.Muted)
	for den, m := range s.Muted {
		m.Channels = slices.Clone(m.Channels)
		s.Muted[den] = m
	}
	return s
}

// Settings returns the member's settings.
func (h *Hub) Settings() types.Notifications {
	h.mu.Lock()
	defer h.mu.Unlock()
	return clone(h.settings)
}

// Status returns what the pages learn of notifications.
func (h *Hub) Status() Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.statusLocked()
}

func (h *Hub) statusLocked() Status {
	st := Status{Notifications: clone(h.settings)}
	if h.android {
		st.Shell = &ShellStatus{Allowed: h.allowed}
	}
	return st
}

// broadcastLocked tells every page the status.
func (h *Hub) broadcastLocked() {
	for _, p := range h.pages {
		h.sendLocked(p, Event{Status: ptr(h.statusLocked())})
	}
}

// Muted returns what the member muted in a den.
func (h *Hub) Muted(den string) types.Muted {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.settings.Muted[den]
	m.Channels = slices.Clone(m.Channels)
	return m
}

// SetKinds turns DMs' and mentions' notifications on or off, and shows
// or hides their messages' text, as the settings ask; nil leaves one as it
// is. What a kind turned off showed is taken back.
func (h *Hub) SetKinds(dms, mentions, hideText *bool) error {
	return h.change(func(s *types.Notifications) {
		if dms != nil {
			s.DMs = *dms
		}
		if mentions != nil {
			s.Mentions = *mentions
		}
		if hideText != nil {
			s.HideText = *hideText
		}
	})
}

// Mute mutes a den, or with channel one of its channels, or unmutes it,
// and takes back what it shows.
func (h *Hub) Mute(den, channel string, on bool) error {
	return h.change(func(s *types.Notifications) {
		m := s.Muted[den]
		switch {
		case channel == "":
			m.Den = on
		case on && !slices.Contains(m.Channels, channel):
			m.Channels = append(m.Channels, channel)
			slices.SortFunc(m.Channels, compareIDs)
		case !on:
			m.Channels = slices.DeleteFunc(m.Channels, func(c string) bool { return c == channel })
		}
		setMuted(s, den, m)
	})
}

func setMuted(s *types.Notifications, den string, m types.Muted) {
	if !m.Den && len(m.Channels) == 0 {
		delete(s.Muted, den)
		if len(s.Muted) == 0 {
			s.Muted = nil
		}
		return
	}
	if s.Muted == nil {
		s.Muted = map[string]types.Muted{}
	}
	s.Muted[den] = m
}

// change applies f to the settings, keeps them, tells the pages and the
// places, and takes back what they no longer notify of.
func (h *Hub) change(f func(*types.Notifications)) error {
	h.mu.Lock()
	next := clone(h.settings)
	f(&next)
	if err := h.keep(next); err != nil {
		h.mu.Unlock()
		return err
	}
	h.settings = next
	var gone []shown
	for _, key := range slices.Clone(h.order) {
		if s := h.shown[key]; !h.notifiesLocked(s.den, s.channel, s.dm, !s.dm) {
			gone = append(gone, h.forgetLocked(key))
		}
	}
	h.broadcastLocked()
	places := h.placesLocked()
	h.mu.Unlock()
	for _, p := range places {
		p.Settings(clone(next))
	}
	h.withdraw(gone)
	return nil
}

func ptr[T any](v T) *T { return &v }

// notifiesLocked is whether a message in a channel notifies, as a DM or as
// a mention, by the settings and mutes.
func (h *Hub) notifiesLocked(den, channel string, dm, mention bool) bool {
	s := h.settings
	if !(dm && s.DMs) && !(mention && !dm && s.Mentions) {
		return false
	}
	m := s.Muted[den]
	return !m.Den && !slices.Contains(m.Channels, channel)
}

func (h *Hub) placesLocked() []Place {
	var out []Place
	for _, p := range []Place{h.helper, h.shell} {
		if p != nil {
			out = append(out, p)
		}
	}
	return out
}

// Watch returns a page's events, starting with the settings, and stops
// them. A page that falls behind has its channel closed, and reloads.
func (h *Hub) Watch(id string) (<-chan Event, func()) {
	ch := make(chan Event, pageBuffer)
	h.mu.Lock()
	p := &page{events: ch, focus: map[string]string{}}
	h.pages[id] = p
	ch <- Event{Status: ptr(h.statusLocked())}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.pages[id] == p {
			h.dropPageLocked(id)
		}
	}
}

// dropPageLocked forgets a page. What it showed stays in the browser as
// long as the browser keeps it, where nothing takes it back but the page,
// as it shows the channel; the hub keeps that it notified, so the message
// doesn't notify again.
func (h *Hub) dropPageLocked(id string) {
	p := h.pages[id]
	delete(h.pages, id)
	close(p.events)
	for key, s := range h.shown {
		if s.at == id {
			s.at = ""
			h.shown[key] = s
		}
	}
}

// Focus records the channel a page shows in a den, "" for none.
func (h *Hub) Focus(id, den, channel string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.pages[id]
	if p == nil {
		return
	}
	if channel == "" {
		delete(p.focus, den)
	} else {
		p.focus[den] = channel
	}
}

// SetPage records what a page says of itself: whether it's on screen, and
// whether the browser lets it notify.
func (h *Hub) SetPage(id string, visible, allowed bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.pages[id]
	if p == nil {
		return
	}
	if visible && !p.visible {
		h.seen++
		p.seen = h.seen
	}
	p.visible, p.allowed = visible, allowed
}

// SetShell is the Android app's shell as its stream opens, which hears the
// settings at once.
func (h *Hub) SetShell(p Place) {
	h.mu.Lock()
	h.shell = p
	settings := clone(h.settings)
	h.mu.Unlock()
	p.Settings(settings)
}

// DropShell forgets the shell p once its stream closes, unless a newer
// one took its place. What it posted stays posted, and the next takes it
// back: the shell, not its stream, keeps it.
func (h *Hub) DropShell(p Place) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.shell == p {
		h.shell, h.front = nil, false
	}
}

// SetFront records whether the Android app is in front, as its shell says.
func (h *Hub) SetFront(on bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.front = on
}

// SetAllowed records whether Android lets the app post notifications, as
// its shell says, and tells the pages.
func (h *Hub) SetAllowed(on bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.allowed != on {
		h.allowed = on
		h.broadcastLocked()
	}
}

// onScreenLocked is whether a page shows a den's channel on screen.
func (h *Hub) onScreenLocked(den, channel string) bool {
	if h.android && !h.front {
		return false
	}
	for _, p := range h.pages {
		if p.visible && p.focus[den] == channel {
			return true
		}
	}
	return false
}

// pageLocked is the page a notification shows in: of those the browser
// lets notify, the one on screen now or most lately.
func (h *Hub) pageLocked() (string, *page) {
	var id string
	var best *page
	for pid, p := range h.pages {
		if !p.allowed {
			continue
		}
		if best == nil || (p.visible && !best.visible) || (p.visible == best.visible && p.seen > best.seen) {
			id, best = pid, p
		}
	}
	return id, best
}

// Message notifies the member of a message, where it should.
func (h *Hub) Message(m Message) {
	key := m.Den + "/" + m.Channel
	h.mu.Lock()
	if !h.notifiesLocked(m.Den, m.Channel, m.DM, m.Mention) || h.onScreenLocked(m.Den, m.Channel) {
		h.mu.Unlock()
		return
	}
	// A message notifies once, though the den may send it again after the
	// connection drops.
	last, had := h.shown[key]
	if had && compareIDs(m.ID, last.message) <= 0 {
		h.mu.Unlock()
		return
	}
	n := compose(m, h.settings.HideText)
	rec := shown{den: m.Den, channel: m.Channel, message: m.ID, dm: m.DM}
	if id, p := h.pageLocked(); p != nil {
		rec.at = id
		h.rememberLocked(key, rec)
		h.sendLocked(p, Event{Show: &n})
		h.mu.Unlock()
		if had && last.at != id {
			h.withdraw([]shown{last})
		}
		h.log.Debugf("Notified the member of message %s in den %s's channel %s, through a page", m.ID, m.Den, m.Channel)
		return
	}
	helper, shell := h.helper, h.shell
	h.mu.Unlock()
	// The channel's notification moves only where it must: a newer one
	// replaces it where it shows.
	switch {
	case helper != nil && helper.Show(n):
		rec.at = atHelper
	case shell != nil && shell.Show(n):
		rec.at = atShell
	}
	if had && last.at != rec.at {
		h.withdraw([]shown{last})
	}
	h.mu.Lock()
	if rec.at == "" {
		h.forgetLocked(key)
	} else {
		h.rememberLocked(key, rec)
	}
	h.mu.Unlock()
	if rec.at == "" {
		h.log.Debugf("Nothing here shows a notification for message %s in den %s's channel %s", m.ID, m.Den, m.Channel)
		return
	}
	h.log.Debugf("Notified the member of message %s in den %s's channel %s, through the %s", m.ID, m.Den, m.Channel, rec.at)
}

// placeOf is the place named at, or one that does nothing.
func (h *Hub) placeOf(at string) Place {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case at == atHelper && h.helper != nil:
		return h.helper
	case at == atShell && h.shell != nil:
		return h.shell
	}
	return nowhere{}
}

type nowhere struct{}

func (nowhere) Show(Notification) bool       { return false }
func (nowhere) Withdraw([]string)            {}
func (nowhere) Settings(types.Notifications) {}

// rememberLocked keeps what shows for a channel, forgetting the oldest past
// maxShown.
func (h *Hub) rememberLocked(key string, s shown) {
	if _, ok := h.shown[key]; ok {
		h.order = slices.DeleteFunc(h.order, func(k string) bool { return k == key })
	}
	h.shown[key] = s
	h.order = append(h.order, key)
	if len(h.order) > maxShown {
		delete(h.shown, h.order[0])
		h.order = h.order[1:]
	}
}

// forgetLocked forgets a channel's notification, and returns it.
func (h *Hub) forgetLocked(key string) shown {
	s := h.shown[key]
	delete(h.shown, key)
	h.order = slices.DeleteFunc(h.order, func(k string) bool { return k == key })
	return s
}

// sendLocked sends a page an event, or closes its stream when it's fallen
// behind.
func (h *Hub) sendLocked(p *page, e Event) {
	select {
	case p.events <- e:
	default:
		for id, q := range h.pages {
			if q == p {
				h.dropPageLocked(id)
			}
		}
	}
}

// withdraw takes notifications back where they show.
func (h *Hub) withdraw(gone []shown) {
	if len(gone) == 0 {
		return
	}
	byPlace := map[string][]string{}
	for _, s := range gone {
		if s.at != "" {
			byPlace[s.at] = append(byPlace[s.at], s.den+"/"+s.channel)
		}
	}
	for at, keys := range byPlace {
		if at == atHelper || at == atShell {
			h.placeOf(at).Withdraw(keys)
			continue
		}
		h.mu.Lock()
		if p := h.pages[at]; p != nil {
			h.sendLocked(p, Event{Withdraw: keys})
		}
		h.mu.Unlock()
	}
}

// Cleared takes back the notifications of a den's channel, or with channel
// "" all of the den's, for messages up to through: the member read them, or
// the den no longer keeps them.
func (h *Hub) Cleared(den, channel, through string) {
	h.clear(func(s shown) bool {
		return s.den == den && (channel == "" || s.channel == channel) && compareIDs(s.message, through) <= 0
	})
}

// Deleted takes back a deleted message's notification.
func (h *Hub) Deleted(den, channel, message string) {
	h.clear(func(s shown) bool { return s.den == den && s.channel == channel && s.message == message })
}

// clear takes back the notifications gone says to.
func (h *Hub) clear(gone func(shown) bool) {
	h.mu.Lock()
	var out []shown
	for _, key := range slices.Clone(h.order) {
		if gone(h.shown[key]) {
			out = append(out, h.forgetLocked(key))
		}
	}
	h.mu.Unlock()
	h.withdraw(out)
}

// ChannelGone takes back a deleted channel's notification, and forgets its
// mute.
func (h *Hub) ChannelGone(den, channel string) {
	h.clear(func(s shown) bool { return s.den == den && s.channel == channel })
	h.mu.Lock()
	muted := slices.Contains(h.settings.Muted[den].Channels, channel)
	h.mu.Unlock()
	if muted {
		if err := h.Mute(den, channel, false); err != nil {
			h.log.Warnf("Couldn't forget a deleted channel's mute: %v", err)
		}
	}
}

// Forgot takes back what a den this install no longer joins shows, and
// forgets its mutes.
func (h *Hub) Forgot(den string) {
	h.clear(func(s shown) bool { return s.den == den })
	h.mu.Lock()
	_, muted := h.settings.Muted[den]
	h.mu.Unlock()
	if !muted {
		return
	}
	if err := h.change(func(s *types.Notifications) { setMuted(s, den, types.Muted{}) }); err != nil {
		h.log.Warnf("Couldn't forget a den's mutes: %v", err)
	}
}

// compose is a message's notification: who sent it and, for a mention,
// where, and the start of what it says, unless the member hides it.
func compose(m Message, hideText bool) Notification {
	n := Notification{Key: m.Den + "/" + m.Channel, Den: m.Den, DenName: m.DenName, Channel: m.Channel, Message: m.ID,
		Title: m.Author, Path: "/den/" + m.Den + "/" + m.Channel + "#message=" + m.ID}
	if !m.DM {
		n.Title = m.Author + " in #" + m.ChannelName
	}
	if !hideText {
		n.Text = denproto.Plain(m.Text, TextLimit)
	}
	if n.Text != "" {
		return n
	}
	switch {
	case hideText && m.DM:
		n.Text = "Sent you a message"
	case hideText:
		n.Text = "Mentioned you"
	case m.Files == 1:
		n.Text = "Sent a file"
	case m.Files > 1:
		n.Text = "Sent " + strconv.Itoa(m.Files) + " files"
	default:
		n.Text = "Sent you a message"
	}
	return n
}

// compareIDs orders the den's IDs, which are decimal and grow.
func compareIDs(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
