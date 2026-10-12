package denclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/cache"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
)

// The client's cache (M6): another den's files come through it, kept as
// they're read, and from M6.2 its messages, kept as pages and events bring
// them, with the den's state. What went leaves it as exactly that: a
// deleted message and its files, a file taken off one, a deleted channel,
// what retention reached and a replaced picture. After a ready, the den's
// changes since the client last synced say what went while it was away.

// syncEvery is how often a connected client asks a den for its changes,
// which keeps its version within the den's floor however long it stays.
const syncEvery = 24 * time.Hour

// syncRetry is how long a client first waits to ask again after a
// failure, doubling up to syncRetryMax while it keeps failing.
const (
	syncRetry    = time.Minute
	syncRetryMax = 30 * time.Minute
)

// saveEvery is how often the cache keeps a den's state while it changes,
// for the page to show while the den is away (M6.2).
const saveEvery = 30 * time.Second

// catchUpPages is how many pages after the newest message a channel's
// cache holds the client asks for, when its run no longer reaches the
// newest, before it takes the newest page and leaves a gap.
const catchUpPages = 2

// maxOwners bounds the files a den connection remembers what they're on;
// past it they're forgotten, and the page's next load of their messages
// brings them back.
const maxOwners = 50_000

// ErrOffline answers a page of history the cache doesn't hold, while the
// den can't be reached (M6.2).
var ErrOffline = errors.New("This den is offline, and this computer hasn't kept these messages.")

// fileOwner is the message a channel's file is on, as the page got it.
type fileOwner struct{ message, channel int64 }

// noteLocked records what the files of a channel's message the page gets
// are on, so a file the cache keeps can leave with them. The caller holds
// c.mu.
func (c *conn) noteLocked(m denproto.Message) {
	if len(m.Attachments) == 0 {
		return
	}
	message, err1 := denproto.ParseID(m.ID)
	channel, err2 := denproto.ParseID(m.ChannelID)
	if err1 != nil || err2 != nil {
		return
	}
	if len(c.owners) >= maxOwners {
		clear(c.owners)
	}
	for _, f := range m.Attachments {
		c.owners[f.ID] = fileOwner{message: message, channel: channel}
	}
}

// noteOwnFiles records what the member's own channel files are on, as the
// Files section lists them (M5).
func (c *conn) noteOwnFiles(files []denproto.OwnFile) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, f := range files {
		if f.Sealed || f.MessageID == "" {
			continue
		}
		c.noteLocked(denproto.Message{ID: f.MessageID, ChannelID: f.ChannelID, Attachments: []denproto.File{f.File}})
	}
}

// ownerOf says what a file is on, as far as this service knows: a message,
// in a channel or a DM, or a member's picture.
func (c *conn) ownerOf(fileID string) cache.Owner {
	c.mu.Lock()
	defer c.mu.Unlock()
	if df, ok := c.dmFiles[fileID]; ok && df.message != 0 {
		return cache.Owner{Message: df.message, Channel: df.channel}
	}
	if o, ok := c.owners[fileID]; ok {
		return cache.Owner{Message: o.message, Channel: o.channel}
	}
	if c.den != nil {
		for _, m := range c.den.members {
			if m.Avatar.ID == fileID || m.Banner.ID == fileID {
				id, _ := denproto.ParseID(m.ID)
				return cache.Owner{Member: id}
			}
		}
	}
	return cache.Owner{}
}

// openCached opens a file of another den through the cache.
func (c *conn) openCached(ctx context.Context, fileID string, thumb bool) (*OpenedFile, error) {
	id, err := denproto.ParseID(fileID)
	if err != nil {
		return nil, inputError(errors.New("invalid file ID"))
	}
	h, err := c.m.Cache.File(c.j.denID, id, thumb, c.ownerOf(fileID), func() (cache.Source, error) {
		return c.fileSource(ctx, fileID, thumb)
	})
	if err != nil {
		return nil, err
	}
	if h.Size() > denproto.MaxFileSize {
		h.Close()
		return nil, errors.New("the den's answer is malformed")
	}
	head := make([]byte, min(int64(media.SniffLen), h.Size()))
	if _, err := h.ReadAt(head, 0); err != nil && !errors.Is(err, io.EOF) {
		h.Close()
		return nil, err
	}
	return &OpenedFile{SectionReader: io.NewSectionReader(h, 0, h.Size()), Kind: media.Sniff(head), Head: head, close: h.Close}, nil
}

// cached reports whether the cache follows this den.
func (c *conn) cached() bool { return c.m.Cache != nil && !c.own }

// connected reports whether the den's socket is open.
func (c *conn) connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ws != nil
}

// dropFiles takes files the den deleted out of the cache.
func (c *conn) dropFiles(ids []string) {
	if !c.cached() || len(ids) == 0 {
		return
	}
	var files []int64
	for _, s := range ids {
		if id, err := denproto.ParseID(s); err == nil {
			files = append(files, id)
		}
	}
	c.cacheErr(c.m.Cache.DropFiles(c.j.denID, files...))
}

// cacheEvent takes what an event says went out of the cache: a deleted
// channel's messages and files, or those a retention pass deleted.
func (c *conn) cacheEvent(e denproto.Event) {
	if !c.cached() {
		return
	}
	switch e.T {
	case denproto.EventChannelDeleted:
		var d denproto.ChannelDeleted
		if json.Unmarshal(e.D, &d) == nil {
			if id, err := denproto.ParseID(d.ID); err == nil {
				c.cacheErr(c.m.Cache.DropChannel(c.j.denID, id))
			}
		}
	case denproto.EventMessagesExpired:
		var x denproto.MessagesExpired
		if json.Unmarshal(e.D, &x) == nil {
			if id, err := denproto.ParseID(x.Through); err == nil {
				c.cacheErr(c.m.Cache.DropThrough(c.j.denID, id))
				c.cacheErr(c.m.Cache.Expire(c.j.denID, id))
			}
		}
	}
}

// cacheMessageEvent keeps what an event says of a message (M6.2): a new
// one joins its channel's run, which reaches the newest while the
// connection lasts; an edit replaces the cache's copy; a delete takes it.
func (c *conn) cacheMessageEvent(e denproto.Event, m *denproto.Message) {
	if !c.cached() {
		return
	}
	switch e.T {
	case denproto.EventMessageCreated:
		if m != nil {
			c.cacheErr(c.m.Cache.AppendTail(c.j.denID, *m, c.gen.Load()))
		}
	case denproto.EventMessageUpdated:
		if m != nil {
			c.cacheErr(c.m.Cache.UpdateMessage(c.j.denID, *m))
		}
	case denproto.EventMessageDeleted:
		var d denproto.MessageDeleted
		if json.Unmarshal(e.D, &d) == nil {
			if id, err := denproto.ParseID(d.ID); err == nil {
				c.cacheErr(c.m.Cache.DeleteMessage(c.j.denID, id))
			}
		}
	}
}

func (c *conn) cacheErr(err error) {
	if err != nil {
		c.m.log.Warnf("Update the cache: %v", err)
	}
}

// pendingSync is a snapshot's say on what the cache may keep, which the
// cache's worker takes in turn. fresh says the cache started again from
// it, so there are no changes to ask for.
type pendingSync struct {
	sync     denproto.Sync
	channels []int64
	pictures map[int64]bool
	fresh    bool
}

// cacheReady checks what the cache holds against a fresh snapshot. A
// cache of another epoch, or from further back than the den keeps its
// changes, starts again at once, before the page loads anything from it;
// the runs that reach their channels' newest messages do so in this
// connection; and the worker drops the rest of what went.
func (c *conn) cacheReady(r denproto.Ready) {
	if !c.cached() {
		return
	}
	cs, den := c.m.Cache, c.j.denID
	p := &pendingSync{sync: r.Sync, pictures: map[int64]bool{}}
	synced, ok, err := cs.Synced(den)
	if err != nil {
		c.cacheErr(err)
		return
	}
	s := r.Sync
	if !ok || !bytes.Equal(synced.Epoch, s.Epoch) || synced.Version < s.Floor || synced.Version > s.Version {
		if ok {
			c.m.log.Infof("A den can't say what changed since the client last synced, so its cache starts again")
		}
		c.cacheErr(cs.DropDen(den))
		c.cacheErr(cs.SetSynced(den, cache.Synced{Epoch: s.Epoch, Version: s.Version}))
		p.fresh = true
	}
	newest := map[int64]int64{}
	for _, rs := range r.ReadStates {
		channel, err := denproto.ParseID(rs.ChannelID)
		if err != nil {
			continue
		}
		last, _ := denproto.ParseID(rs.LastMessage)
		newest[channel] = last
	}
	c.cacheErr(cs.Reach(den, newest, c.gen.Load()))
	for _, ch := range r.Channels {
		if id, err := denproto.ParseID(ch.ID); err == nil {
			p.channels = append(p.channels, id)
		}
	}
	for _, m := range r.Members {
		for _, pic := range []denproto.Image{m.Avatar, m.Banner} {
			if id, err := denproto.ParseID(pic.ID); err == nil {
				p.pictures[id] = true
			}
		}
	}
	// A snapshot of only the members that changed can't say which
	// pictures went: the changes to members say it as their records come.
	if r.Sync.MembersSince != nil {
		p.pictures = nil
	}
	c.mu.Lock()
	c.readySync = p
	c.mu.Unlock()
	c.kickSync()
}

func (c *conn) kickSync() {
	select {
	case c.syncDue <- struct{}{}:
	default:
	}
}

// cacheWork keeps the cache of a den in step with it: after each snapshot,
// and daily while it stays connected. It's the one goroutine that moves
// where the den's changes stood, so a snapshot and the changes after it
// can't cross. It also keeps the den's state as it changes, and once more
// as the service stops.
func (c *conn) cacheWork(ctx context.Context) {
	if !c.cached() {
		return
	}
	defer c.saveKept()
	daily := time.NewTicker(syncEvery)
	defer daily.Stop()
	saving := time.NewTicker(saveEvery)
	defer saving.Stop()
	var retry <-chan time.Time
	wait := syncRetry
	for {
		select {
		case <-ctx.Done():
			return
		case <-saving.C:
			c.saveKept()
			continue
		case <-c.syncDue:
			wait = syncRetry
		case <-daily.C:
		case <-retry:
		}
		retry = nil
		err := c.syncCache(ctx)
		c.saveKept()
		if err == nil {
			wait = syncRetry
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if wait == syncRetry {
			c.m.log.Infof("Couldn't check the cache against a den's changes: %v", logged(err))
		}
		retry = time.After(wait)
		wait = min(2*wait, syncRetryMax)
	}
}

// syncCache drops what the latest snapshot, if one came, says went from
// the cache, and then asks the den what changed since the client last
// synced.
func (c *conn) syncCache(ctx context.Context) error {
	cs, den := c.m.Cache, c.j.denID
	c.mu.Lock()
	p := c.readySync
	c.readySync = nil
	c.mu.Unlock()
	if p != nil && p.fresh {
		return nil
	}
	synced, ok, err := cs.Synced(den)
	if err != nil || !ok {
		return err // nothing to sync from until a snapshot says where
	}
	if p != nil {
		if err := cs.DropChannelsExcept(den, p.channels); err != nil {
			return err
		}
		if s := p.sync; s.Expired != "" {
			through, _ := denproto.ParseID(s.Expired)
			if err := cs.DropThrough(den, through); err != nil {
				return err
			}
			if err := cs.Expire(den, through); err != nil {
				return err
			}
			// The page may have loaded them from the cache meanwhile.
			if e, err := denproto.NewEvent(denproto.EventMessagesExpired, 0, denproto.MessagesExpired{Through: s.Expired}); err == nil {
				c.m.publish(PageEvent{DenID: den.String(), Events: []denproto.Event{e}})
			}
		}
		if p.pictures != nil {
			pictures, err := cs.Pictures(den)
			if err != nil {
				return err
			}
			var replaced []int64
			for id := range pictures {
				if !p.pictures[id] {
					replaced = append(replaced, id)
				}
			}
			if err := cs.DropFiles(den, replaced...); err != nil {
				return err
			}
		}
	}
	// A den that isn't connected is asked once it is: its snapshot wakes
	// this worker again.
	if !c.connected() {
		return nil
	}
	return c.applyChanges(ctx, synced)
}

// applyChanges asks the den what changed since the client last synced,
// and applies it to the cache: messages edited take their new revision,
// and files edits took off their messages go; messages deleted go with
// their files. The page hears of each, as it would of an event, since it
// may have loaded them from the cache meanwhile.
func (c *conn) applyChanges(ctx context.Context, synced cache.Synced) error {
	cs, den := c.m.Cache, c.j.denID
	upto, err := cs.Newest(den)
	if err != nil {
		return err
	}
	since := synced.Version
	for {
		q := url.Values{"since": {strconv.FormatInt(since, 10)}}
		if upto > 0 {
			q.Set("upto", strconv.FormatInt(upto, 10))
		}
		var page denproto.Changes
		err := c.call(ctx, http.MethodGet, "/api/changes?"+q.Encode(), nil, &page)
		if denproto.IsCode(err, denproto.CodeChangesGone) {
			// Pruned since the last snapshot: what the cache holds goes,
			// and the next snapshot says where to start.
			c.m.log.Infof("A den no longer has the changes since the client last synced, so its cache starts again")
			return cs.DropDen(den)
		}
		if err != nil {
			return err
		}
		// A page that says there's more moves on, so a den can't keep a
		// client asking for ever.
		if page.Version < since || (page.More && page.Version == since) ||
			len(page.Messages)+len(page.Deleted) > denproto.ChangesPage {
			return errMalformed
		}
		var events []denproto.Event
		for _, m := range page.Messages {
			if denproto.CheckMessage(m) != nil {
				return errMalformed
			}
			id, _ := denproto.ParseID(m.ID)
			// A message this device can't open loses every file it had in
			// the cache: there's no telling which it still has.
			keep, _ := c.filesOf(m)
			if err := cs.DropMessage(den, id, keep...); err != nil {
				return err
			}
			if err := cs.UpdateMessage(den, m); err != nil {
				return err
			}
			if e, err := denproto.NewEvent(denproto.EventMessageUpdated, 0, c.open(m)); err == nil {
				events = append(events, e)
			}
		}
		for _, d := range page.Deleted {
			id, err := denproto.ParseID(d.ID)
			if err != nil || !validID(d.ChannelID) {
				return errMalformed
			}
			if err := cs.DropMessage(den, id); err != nil {
				return err
			}
			if err := cs.DeleteMessage(den, id); err != nil {
				return err
			}
			if e, err := denproto.NewEvent(denproto.EventMessageDeleted, 0, denproto.MessageDeleted{ID: d.ID, ChannelID: d.ChannelID}); err == nil {
				events = append(events, e)
			}
		}
		if len(events) > 0 {
			c.m.publish(PageEvent{DenID: den.String(), Events: events})
		}
		since = page.Version
		if !page.More {
			break
		}
	}
	return cs.SetSynced(den, cache.Synced{Epoch: synced.Epoch, Version: since})
}

// filesOf lists the files a message has now: a channel's attachments, or
// what a DM's sealed text lists, which this service opens, and reports
// false for a DM's that doesn't open on this device.
func (c *conn) filesOf(m denproto.Message) ([]int64, bool) {
	var out []int64
	add := func(s string) {
		if id, err := denproto.ParseID(s); err == nil {
			out = append(out, id)
		}
	}
	if m.Sealed == nil {
		for _, f := range m.Attachments {
			add(f.ID)
		}
		return out, true
	}
	pm := c.open(m)
	if pm.Locked != "" {
		return nil, false
	}
	for _, f := range pm.Attachments {
		add(f.ID)
	}
	return out, true
}

// saveKept keeps the den's state, and its members when they changed, in
// the cache, for the page to show while the den is away (M6.2).
func (c *conn) saveKept() {
	if !c.cached() {
		return
	}
	c.mu.Lock()
	if c.den == nil || (!c.stateDirty && !c.membersDirty) {
		c.mu.Unlock()
		return
	}
	k, members := c.den.kept()
	cursor, withMembers := c.members, c.membersDirty
	c.stateDirty, c.membersDirty = false, false
	c.mu.Unlock()
	state, err := json.Marshal(k)
	if err == nil {
		err = c.m.Cache.SaveState(c.j.denID, state)
	}
	if err == nil && withMembers && cursor != nil {
		var list []byte
		if list, err = json.Marshal(members); err == nil {
			err = c.m.Cache.SaveMembers(c.j.denID, cache.Members{List: list, Epoch: cursor.epoch, Version: cursor.version})
		}
	}
	c.cacheErr(err)
}

// restoreKept gives the connection the den's state as the cache kept it,
// for the page to show until the den sends a snapshot, or while it's
// away, with the members and where their list stands. Something kept that
// doesn't check goes from the cache with the rest of the den's.
func (c *conn) restoreKept() {
	if !c.cached() {
		return
	}
	cs, den := c.m.Cache, c.j.denID
	state, err := cs.State(den)
	if err != nil || state == nil {
		c.cacheErr(err)
		return
	}
	var k keptState
	var members []denproto.Member
	kept, withMembers, err := cs.Members(den)
	if err == nil {
		err = json.Unmarshal(state, &k)
	}
	if err == nil && withMembers {
		err = json.Unmarshal(kept.List, &members)
	}
	var s *denState
	if err == nil {
		s, err = restore(k, members)
	}
	if err != nil {
		c.m.log.Warnf("A den's state in the cache doesn't read, so its cache starts again: %v", err)
		c.cacheErr(cs.DropDen(den))
		return
	}
	c.mu.Lock()
	c.den = s
	if withMembers {
		c.members = &membersCursor{epoch: kept.Epoch, version: kept.Version}
	}
	c.mu.Unlock()
}

// cachedHistory answers a page of history from the cache where it holds
// the messages, and otherwise from the den, keeping what the den sends
// (M6.2). A channel whose newest messages came while the client was away
// asks for those after the newest it holds, a page or two, rather than
// the whole newest page. Without the den, the cache answers what it can.
func (c *conn) cachedHistory(ctx context.Context, channelID string, q HistoryQuery) (denproto.History, error) {
	channel, err := denproto.ParseID(channelID)
	if err != nil {
		return denproto.History{}, inputError(errors.New("invalid channel ID"))
	}
	cq := cache.Query{Limit: q.Limit}
	for _, f := range []struct {
		s  string
		at *int64
	}{{q.Before, &cq.Before}, {q.After, &cq.After}, {q.Around, &cq.Around}} {
		if f.s != "" {
			if *f.at, err = denproto.ParseID(f.s); err != nil {
				return denproto.History{}, inputError(errors.New("invalid message ID"))
			}
		}
	}
	gen := c.gen.Load()
	offline := !c.connected()
	if p, ok, err := c.fromCache(channel, cq, gen, offline); err != nil || ok {
		return p, err
	}
	// A den that failed to connect would only keep the page waiting.
	if c.status().State == StateOffline {
		return denproto.History{}, ErrOffline
	}
	newest := q.Before == "" && q.After == "" && q.Around == ""
	if newest && !offline {
		if ok := c.catchUp(ctx, channelID, channel, gen); ok {
			if p, ok, err := c.fromCache(channel, cq, gen, false); err != nil || ok {
				return p, err
			}
		}
	}
	h, err := c.history(ctx, channelID, q)
	if err != nil {
		return h, err
	}
	c.keepPage(channel, q, h, gen)
	return h, nil
}

// fromCache answers a page from the cache, if it can.
func (c *conn) fromCache(channel int64, q cache.Query, gen int64, offline bool) (denproto.History, bool, error) {
	empty := false
	c.mu.Lock()
	if c.den != nil {
		r, ok := c.den.reads[denproto.FormatID(channel)]
		empty = ok && r.LastMessage == ""
	}
	c.mu.Unlock()
	p, ok, err := c.m.Cache.Page(c.j.denID, channel, q, gen, offline, empty)
	if err != nil || !ok {
		return denproto.History{}, false, err
	}
	return denproto.History{Messages: p.Messages, HasOlder: p.HasOlder, HasNewer: p.HasNewer}, true, nil
}

// catchUp fetches the messages after the newest a channel's cache holds,
// a page at a time, up to catchUpPages, and reports whether that reached
// the newest.
func (c *conn) catchUp(ctx context.Context, channelID string, channel, gen int64) bool {
	runs, err := c.m.Cache.Runs(c.j.denID, channel)
	if err != nil || len(runs) == 0 {
		return false
	}
	after := runs[len(runs)-1].Hi
	for range catchUpPages {
		q := HistoryQuery{After: denproto.FormatID(after), Limit: denproto.MaxHistory}
		h, err := c.history(ctx, channelID, q)
		if err != nil {
			return false
		}
		c.keepPage(channel, q, h, gen)
		if !h.HasNewer {
			return true
		}
		if len(h.Messages) == 0 {
			return false
		}
		after, _ = denproto.ParseID(h.Messages[len(h.Messages)-1].ID)
	}
	return false
}

// keepPage keeps a page of history from the den, with the range of IDs it
// covers: from the anchor to its last message, or the other way, and from
// the channel's start when nothing comes before it. A page that reaches
// the newest does so in the connection it was fetched in, if that's still
// the one.
func (c *conn) keepPage(channel int64, q HistoryQuery, h denproto.History, gen int64) {
	ms := h.Messages
	var lo, hi int64
	if len(ms) > 0 {
		lo, _ = denproto.ParseID(ms[0].ID)
		hi, _ = denproto.ParseID(ms[len(ms)-1].ID)
	}
	switch {
	case q.Before != "":
		x, _ := denproto.ParseID(q.Before)
		hi = x - 1
		if len(ms) == 0 {
			lo = hi + 1
		}
	case q.After != "":
		x, _ := denproto.ParseID(q.After)
		lo = x + 1
		if len(ms) == 0 {
			hi = x
		}
	case len(ms) == 0:
		return // an empty channel: the client knows it from read state
	}
	first := !h.HasOlder
	if first {
		lo = 1
	}
	var reach int64
	if !h.HasNewer && gen != 0 && gen == c.gen.Load() {
		reach = gen
	}
	c.cacheErr(c.m.Cache.AddRun(c.j.denID, channel, lo, hi, first, reach, ms))
}

// dropCache forgets what the cache holds of the den, as when the member
// leaves it, is removed or banned, or the device is signed out.
func (c *conn) dropCache() {
	if c.cached() {
		c.cacheErr(c.m.Cache.DropDen(c.j.denID))
	}
}

// CacheUsage is how much the cache holds, and how much it may.
type CacheUsage struct {
	Used  int64 `json:"used"`
	Limit int64 `json:"limit"`
}

// CacheUsage says how much of its size the cache holds.
func (m *Manager) CacheUsage() CacheUsage {
	if m.Cache == nil {
		return CacheUsage{}
	}
	return CacheUsage{Used: m.Cache.Used(), Limit: m.Cache.Limit()}
}

// ClearCache empties the cache of files and messages. The dens' state
// stays, as the page shows it while a den is away.
func (m *Manager) ClearCache() error {
	if m.Cache == nil {
		return nil
	}
	return m.Cache.Clear()
}
