// Package notify decides what notifies the member, and where it shows. The
// service decides, since a page may be hidden, frozen or closed: a DM, a
// message that mentions the member, or any message in a channel they set
// to All messages, in a den and channel they haven't muted, notifies them
// unless a page shows that channel on screen. Staff's @everyone notifies
// as a mention does, unless the member turns it off. Do not disturb holds
// all of it, sound and all, until its end passes by the wall clock.
// Each channel's newest such message shows in one place: a page that may
// notify, the one on screen most lately, which shows it through the
// browser; with none, the desktop's helper; and on Android, the app's
// shell. Reading the channel takes it back wherever it shows.
//
// It places the message's sound too, so a message makes one sound: with
// the page's notification, which the page plays, the browser's own made
// silent; with the helper's or the shell's, the desktop's or Android's
// own; and where none shows, as with the kind turned off, alone, to the
// page on screen most lately. It keeps the member's choices for the page's
// sounds beside their notifications', and knows which sounds they replaced
// with files of their own, which the pages hear with the status.
//
// An event the member is interested in notifies them as it starts, where
// a message would show, unless they turned events off or muted its den,
// and goes as the event ends. Events notify by default: each is one the
// member marked.
package notify

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

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
	// Mention is whether it mentions the member by name, and Everyone
	// whether it's staff's @everyone, which notifies as a mention
	// unless the member silenced it.
	Mention  bool
	Everyone bool
	// Text is what it says, opened where it's a DM's, and "" where this
	// device couldn't open it; Files is how many files it carries.
	Text  string
	Files int
}

// Notification is what a place shows for a channel: its newest message
// that notifies, by its den, channel and ID, with the den's name, a title
// naming who sent it and where, and the start of what it says. An event's
// start names the event in place of a channel and message.
type Notification struct {
	// Key names the channel's notification, which a newer message's
	// replaces, and which reading the channel takes back; or the event's.
	Key     string `json:"key"`
	Den     string `json:"den"`
	DenName string `json:"denName"`
	Channel string `json:"channel"`
	Message string `json:"message"`
	Event   string `json:"event,omitempty"`
	Title   string `json:"title"`
	Text    string `json:"text"`
	// Path is where the page shows the message, or the event, from its
	// origin.
	Path string `json:"path"`
}

// Start is an event that started, which the member is interested in,
// with its den's name, and its place: a voice channel's name, or
// the line of text its maker wrote.
type Start struct {
	Den, DenName string
	Event        string
	Name         string
	Channel      string
	Place        string
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

// Event goes to a page: the status, at first and as it changes; a
// notification to show through the browser, whose sound the page plays
// with it, or to take back; or a message's sound alone.
type Event struct {
	Status   *Status
	Show     *Notification
	Withdraw []string
	Sound    *Sounded
}

// Sounded is a message whose sound a page plays alone, by its den,
// channel and ID.
type Sounded struct {
	Den     string `json:"den"`
	Channel string `json:"channel"`
	Message string `json:"message"`
}

// Status is what a page learns of notifications: the member's settings,
// the page's sounds among them; the sounds they replaced with files of
// their own; and on Android, whether Android lets the app post
// notifications, as its shell last said, which the settings show.
type Status struct {
	types.Notifications
	// Own is each sound the member replaced, by name, with its file's
	// version, which the page asks for the file by.
	Own   map[string]int64 `json:"own,omitempty"`
	Shell *ShellStatus     `json:"shell,omitempty"`
}

// ShellStatus is the Android app's shell's say on notifications.
type ShellStatus struct {
	Allowed bool `json:"allowed"`
}

// Hub is the service's: the member's settings, the pages and what each
// shows, the places, and what shows where.
type Hub struct {
	// Now is the wall clock, which a test may replace before the hub is
	// used.
	Now func() time.Time

	log *xlog.Logger
	// helper is the desktop's helper, whose Show fails while none can
	// show a notification.
	helper Place
	// android is the Android form, whose page counts as on screen only
	// while its shell says the app is in front: the shell holds the page
	// as shown through a call, whatever the screen does.
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
	// own is each sound the member replaced with a file of their own, by
	// name, with its file's version.
	own map[string]int64
	// shown is each channel's notification, by key, oldest first in order,
	// or its message whose sound played alone.
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
// that's gone, or a message whose sound played alone, which shows nowhere.
// An event's start names the event alone.
type shown struct {
	den, channel, message string
	event                 string
	dm                    bool
	// mention and everyone are what the message was, which a change to
	// the settings weighs again.
	mention, everyone bool
	at                string
	// held is the channel's newest message Do not disturb held, which
	// mustn't notify once it ends, though the den sends it again.
	held string
}

const (
	atHelper = "helper"
	atShell  = "shell"
)

// newest is the newest message the hub weighed for the channel.
func (s shown) newest() string {
	if compareIDs(s.held, s.message) > 0 {
		return s.held
	}
	return s.message
}

// key is the key a notification shows under: its channel's, or its
// event's.
func (s shown) key() string {
	if s.event != "" {
		return eventKey(s.den, s.event)
	}
	return s.den + "/" + s.channel
}

// eventKey is the key an event's start shows under.
func eventKey(den, event string) string { return den + "/event-" + event }

// New returns a hub with the member's settings, which keep stores as they
// change, and the desktop's helper.
func New(log *xlog.Logger, settings types.Notifications, keep func(types.Notifications) error, helper Place, android bool) *Hub {
	return &Hub{Now: time.Now, log: log, settings: clone(settings), keep: keep, helper: helper, android: android,
		pages: map[string]*page{}, own: map[string]int64{}, shown: map[string]shown{}}
}

func clone(s types.Notifications) types.Notifications {
	s.Muted = maps.Clone(s.Muted)
	for den, m := range s.Muted {
		m.Channels, m.All = slices.Clone(m.Channels), slices.Clone(m.All)
		s.Muted[den] = m
	}
	s.Sounds = maps.Clone(s.Sounds)
	for name, snd := range s.Sounds {
		if snd.Volume != nil {
			snd.Volume = ptr(*snd.Volume)
			s.Sounds[name] = snd
		}
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
	if len(h.own) > 0 {
		st.Own = maps.Clone(h.own)
	}
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

// Muted returns what the member muted in a den, and its channels at All
// messages.
func (h *Hub) Muted(den string) types.Muted {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.settings.Muted[den]
	m.Channels, m.All = slices.Clone(m.Channels), slices.Clone(m.All)
	return m
}

// Kinds are the kinds of notification the settings turn on or off, and
// whether they show their messages' text; nil leaves one as it is.
type Kinds struct {
	DMs, Mentions, HideText *bool
	// Everyone is staff's @everyone, on unless the member turns it
	// off.
	Everyone *bool
	// Events are the events the member is interested in, as they start,
	// on unless the member turns them off.
	Events *bool
}

// SetKinds turns DMs' and mentions' notifications on or off, staff's
// @everyone, and events, and shows or hides their messages' text, as the
// settings ask. What a kind turned off showed is taken back.
func (h *Hub) SetKinds(k Kinds) error {
	return h.change(func(s *types.Notifications) {
		if k.DMs != nil {
			s.DMs = *k.DMs
		}
		if k.Mentions != nil {
			s.Mentions = *k.Mentions
		}
		if k.HideText != nil {
			s.HideText = *k.HideText
		}
		if k.Everyone != nil {
			s.EveryoneOff = !*k.Everyone
		}
		if k.Events != nil {
			s.EventsOff = !*k.Events
		}
	})
}

// SoundChange changes one of the page's sounds: off or on, and its
// volume in percent; nil leaves either as it is.
type SoundChange struct {
	Off    *bool
	Volume *int
}

// SetSounds changes the page's sounds, by name, as changes say. A
// sound back on at its default volume leaves the settings, as it was before
// the member touched it.
func (h *Hub) SetSounds(changes map[string]SoundChange) error {
	return h.change(func(s *types.Notifications) {
		for name, c := range changes {
			snd := s.Sounds[name]
			if c.Off != nil {
				snd.Off = *c.Off
			}
			if c.Volume != nil {
				snd.Volume = ptr(*c.Volume)
			}
			if !snd.Off && snd.Volume == nil {
				delete(s.Sounds, name)
				continue
			}
			if s.Sounds == nil {
				s.Sounds = map[string]types.Sound{}
			}
			s.Sounds[name] = snd
		}
		if len(s.Sounds) == 0 {
			s.Sounds = nil
		}
	})
}

// SetOwn records that the member replaced a sound with a file of their own,
// by its version, or with version 0 that they put its default back, and
// tells the pages. The database keeps the files; the hub, which
// ones there are.
func (h *Hub) SetOwn(name string, version int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if version == 0 {
		delete(h.own, name)
	} else {
		h.own[name] = version
	}
	h.broadcastLocked()
}

// Level is how much of a channel notifies: every message, those that
// mention the member, as until they choose, or none.
type Level string

const (
	LevelAll      Level = "all"
	LevelMentions Level = "mentions"
	LevelNothing  Level = "nothing"
)

// SetLevel sets a channel's level, and takes back what it no longer
// notifies of.
func (h *Hub) SetLevel(den, channel string, level Level) error {
	if level != LevelAll && level != LevelMentions && level != LevelNothing {
		return fmt.Errorf("no level %q", level)
	}
	return h.change(func(s *types.Notifications) { setLevel(s, den, channel, level) })
}

// setLevel puts a channel in the list of its level, out of the other's.
func setLevel(s *types.Notifications, den, channel string, level Level) {
	m := s.Muted[den]
	m.Channels, m.All = without(m.Channels, channel), without(m.All, channel)
	switch level {
	case LevelNothing:
		m.Channels = with(m.Channels, channel)
	case LevelAll:
		m.All = with(m.All, channel)
	}
	setMuted(s, den, m)
}

func with(ids []string, id string) []string {
	ids = append(ids, id)
	slices.SortFunc(ids, compareIDs)
	return ids
}

func without(ids []string, id string) []string {
	ids = slices.DeleteFunc(ids, func(c string) bool { return c == id })
	if len(ids) == 0 {
		return nil
	}
	return ids
}

// SetDoNotDisturb turns Do not disturb on until the wall clock reaches
// until, or with until zero until it's turned off, or turns it off. What
// already shows stays.
func (h *Hub) SetDoNotDisturb(on bool, until time.Time) error {
	return h.change(func(s *types.Notifications) {
		s.DoNotDisturb, s.DoNotDisturbUntil = on, time.Time{}
		if on {
			// Round drops the monotonic reading, which stops while a
			// computer sleeps.
			s.DoNotDisturbUntil = until.Round(0)
		}
	})
}

// Mute mutes a den, or with channel one of its channels, or unmutes it,
// and takes back what it shows. A channel unmuted is at Mentions.
func (h *Hub) Mute(den, channel string, on bool) error {
	return h.change(func(s *types.Notifications) {
		m := s.Muted[den]
		switch {
		case channel == "":
			m.Den = on
			setMuted(s, den, m)
		case on:
			setLevel(s, den, channel, LevelNothing)
		case slices.Contains(m.Channels, channel):
			setLevel(s, den, channel, LevelMentions)
		}
	})
}

func setMuted(s *types.Notifications, den string, m types.Muted) {
	if !m.Den && len(m.Channels) == 0 && len(m.All) == 0 {
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
		s := h.shown[key]
		still := h.notifiesLocked(Message{Den: s.den, Channel: s.channel, DM: s.dm, Mention: s.mention, Everyone: s.everyone})
		if s.event != "" {
			still = h.eventNotifiesLocked(s.den)
		}
		if !still {
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

// notifiesLocked is whether a message in a channel notifies, as a DM, as
// a mention or in a channel at All messages, by the settings and mutes.
// Staff's @everyone is a mention the member may silence apart. All messages
// stands without Mentions, since choosing it for a channel asks for it. Do
// not disturb isn't weighed here, since what it holds stays where it shows.
func (h *Hub) notifiesLocked(m Message) bool {
	s := h.settings
	mention := m.Mention || m.Everyone && !s.EveryoneOff
	if !(m.DM && s.DMs) && !(mention && !m.DM && s.Mentions) && !slices.Contains(s.Muted[m.Den].All, m.Channel) {
		return false
	}
	return !h.mutedLocked(m.Den, m.Channel)
}

// quietLocked is whether Do not disturb holds what would notify now. The
// wall clock is read as each message comes, since no timer runs while a
// phone sleeps.
func (h *Hub) quietLocked() bool {
	s := h.settings
	return s.DoNotDisturb && (s.DoNotDisturbUntil.IsZero() || h.Now().Round(0).Before(s.DoNotDisturbUntil))
}

// eventNotifiesLocked is whether an event's start in a den notifies: unless
// the member turned events off, or muted the den.
func (h *Hub) eventNotifiesLocked(den string) bool {
	return !h.settings.EventsOff && !h.settings.Muted[den].Den
}

// mutedLocked is whether the member muted a den, or its channel.
func (h *Hub) mutedLocked(den, channel string) bool {
	m := h.settings.Muted[den]
	return m.Den || slices.Contains(m.Channels, channel)
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
func (h *Hub) pageLocked() (string, *page) { return h.latestLocked(true) }

// latestLocked is the page on screen now or most lately, of those the
// browser lets notify where allowed says so.
func (h *Hub) latestLocked(allowed bool) (string, *page) {
	var id string
	var best *page
	for pid, p := range h.pages {
		if allowed && !p.allowed {
			continue
		}
		if best == nil || (p.visible && !best.visible) || (p.visible == best.visible && p.seen > best.seen) {
			id, best = pid, p
		}
	}
	return id, best
}

// soundLocked sends a message's sound alone to the page on screen most
// lately, and reports whether there was one to play it. On Android
// the page plays it only while the app is in front: behind, the shell may
// hold it as shown through a call, or it may be frozen, to play what it
// missed as it thaws.
func (h *Hub) soundLocked(rec shown) bool {
	_, p := h.latestLocked(false)
	if p == nil || (h.android && !h.front) {
		return false
	}
	h.sendLocked(p, Event{Sound: &Sounded{Den: rec.den, Channel: rec.channel, Message: rec.message}})
	return true
}

// Message notifies the member of a message where it should, and places its
// sound: a DM, or a message that mentions them, of a kind they
// turned on, or any message in a channel at All messages, in a den and
// channel they haven't muted and no page shows on screen, makes one sound,
// unless Do not disturb holds it. The page showing its notification plays it;
// the helper's or the shell's notification has the desktop's or Android's
// own; and where nothing can show one, as when the browser won't let a
// page notify and no helper runs, the page on screen most lately plays it
// alone, while a page is open. A kind turned off makes no sound either.
func (h *Hub) Message(m Message) {
	key := m.Den + "/" + m.Channel
	h.mu.Lock()
	if !h.notifiesLocked(m) || h.onScreenLocked(m.Den, m.Channel) {
		h.mu.Unlock()
		return
	}
	// A message notifies, or sounds, once, though the den may send it again
	// after the connection drops.
	last, had := h.shown[key]
	if had && compareIDs(m.ID, last.newest()) <= 0 {
		h.mu.Unlock()
		return
	}
	if h.quietLocked() {
		// What showed for the channel before stays, as does where.
		if had {
			last.held = m.ID
			h.shown[key] = last
		} else {
			h.rememberLocked(key, shown{den: m.Den, channel: m.Channel, dm: m.DM, mention: m.Mention, everyone: m.Everyone, held: m.ID})
		}
		h.mu.Unlock()
		h.log.Debugf("Do not disturb held message %s in den %s's channel %s", m.ID, m.Den, m.Channel)
		return
	}
	rec := shown{den: m.Den, channel: m.Channel, message: m.ID, dm: m.DM, mention: m.Mention, everyone: m.Everyone}
	n := compose(m, h.settings.HideText)
	switch at := h.deliverLocked(key, rec, n, last, had); at {
	case atSound:
		h.log.Debugf("Nothing here shows a notification for message %s in den %s's channel %s; a page sounded it", m.ID, m.Den, m.Channel)
	case "":
		h.log.Debugf("Nothing here shows a notification for message %s in den %s's channel %s", m.ID, m.Den, m.Channel)
	default:
		h.log.Debugf("Notified the member of message %s in den %s's channel %s, through %s", m.ID, m.Den, m.Channel, at)
	}
}

// atSound says deliverLocked played a notification's sound alone.
const atSound = "its sound alone"

// deliverLocked shows a notification under its key where it should: in the
// page the browser lets notify that was on screen now or most lately, else
// through the helper or the shell, and where none can, its sound alone in
// the page on screen most lately. last is what showed under the key
// before, if had, which a newer one replaces where it shows, and takes
// back where it moved from. It returns where it showed, "a page", "the
// helper" or "the shell", atSound, or "" for nowhere. The caller holds
// h.mu, which it unlocks.
func (h *Hub) deliverLocked(key string, rec shown, n Notification, last shown, had bool) string {
	if id, p := h.pageLocked(); p != nil {
		rec.at = id
		h.rememberLocked(key, rec)
		h.sendLocked(p, Event{Show: &n})
		h.mu.Unlock()
		if had && last.at != id {
			h.withdraw([]shown{last})
		}
		return "a page"
	}
	helper, shell := h.helper, h.shell
	h.mu.Unlock()
	// A key's notification moves only where it must: a newer one replaces
	// it where it shows.
	switch {
	case helper != nil && helper.Show(n):
		rec.at = atHelper
	case shell != nil && shell.Show(n):
		rec.at = atShell
	}
	if had && last.at != rec.at {
		h.withdraw([]shown{last})
	}
	// With nothing to show it, its sound goes alone to the page on screen
	// most lately.
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case rec.at != "":
		h.rememberLocked(key, rec)
		return "the " + rec.at
	case h.soundLocked(rec):
		h.rememberLocked(key, rec)
		return atSound
	}
	h.forgetLocked(key)
	return ""
}

// Started notifies the member of an event that started, which they're
// interested in, where a message would show, unless they turned
// events off or muted its den, or Do not disturb holds it. It shows once,
// though a socket that resumes may hear of it again; a page showing the
// den doesn't keep it from showing, since the event may be off its screen.
func (h *Hub) Started(st Start) {
	key := eventKey(st.Den, st.Event)
	h.mu.Lock()
	if _, had := h.shown[key]; had || !h.eventNotifiesLocked(st.Den) {
		h.mu.Unlock()
		return
	}
	rec := shown{den: st.Den, event: st.Event}
	if h.quietLocked() {
		h.rememberLocked(key, rec)
		h.mu.Unlock()
		h.log.Debugf("Do not disturb held event %s in den %s starting", st.Event, st.Den)
		return
	}
	n := composeStart(st, h.settings.HideText)
	switch at := h.deliverLocked(key, rec, n, shown{}, false); at {
	case atSound:
		h.log.Debugf("Nothing here shows a notification for event %s in den %s; a page sounded it", st.Event, st.Den)
	case "":
		h.log.Debugf("Nothing here shows a notification for event %s in den %s", st.Event, st.Den)
	default:
		h.log.Infof("Notified the member of event %s in den %s starting, through %s", st.Event, st.Den, at)
	}
}

// EventOver takes back an event's notification, as it ends or goes.
func (h *Hub) EventOver(den, event string) {
	h.clear(func(s shown) bool { return s.den == den && s.event == event })
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
			byPlace[s.at] = append(byPlace[s.at], s.key())
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
// the den no longer keeps them. An event's stays.
func (h *Hub) Cleared(den, channel, through string) {
	h.clear(func(s shown) bool {
		return s.den == den && s.event == "" && (channel == "" || s.channel == channel) && compareIDs(s.message, through) <= 0
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
// level.
func (h *Hub) ChannelGone(den, channel string) {
	h.clear(func(s shown) bool { return s.den == den && s.channel == channel })
	h.mu.Lock()
	m := h.settings.Muted[den]
	set := slices.Contains(m.Channels, channel) || slices.Contains(m.All, channel)
	h.mu.Unlock()
	if set {
		if err := h.SetLevel(den, channel, LevelMentions); err != nil {
			h.log.Warnf("Couldn't forget a deleted channel's level: %v", err)
		}
	}
}

// Forgot takes back what a den this install no longer joins shows, and
// forgets its mutes and levels.
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
	case hideText && m.Everyone && !m.Mention:
		n.Text = "Mentioned everyone"
	case hideText && !m.Mention:
		n.Text = "Sent a message"
	case hideText:
		n.Text = "Mentioned you"
	case m.Files == 1:
		n.Text = "Sent a file"
	case m.Files > 1:
		n.Text = "Sent " + strconv.Itoa(m.Files) + " files"
	case !m.DM:
		n.Text = "Sent a message"
	default:
		n.Text = "Sent you a message"
	}
	return n
}

// composeStart is an event's notification as it starts: its name, and
// that it's live, and where, unless the member hides the text and its
// maker wrote the place, which a voice channel's name isn't.
func composeStart(st Start, hideText bool) Notification {
	n := Notification{Key: eventKey(st.Den, st.Event), Den: st.Den, DenName: st.DenName, Event: st.Event,
		Title: st.Name, Text: "Live now", Path: "/den/" + st.Den + "/events#event=" + st.Event}
	switch {
	case st.Channel != "":
		n.Text = "Live now in " + st.Channel
	case st.Place != "" && !hideText:
		n.Text = "Live now: " + denproto.Plain(st.Place, TextLimit)
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
