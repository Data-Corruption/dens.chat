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
// they're read, and leave it as exactly what went: a deleted message's
// files, a file taken off one, a deleted channel's, those retention
// reached and a replaced picture's. After a ready, the den's changes since
// the client last synced say what went while it was away.

// syncEvery is how often a connected client asks a den for its changes,
// which keeps its version within the den's floor however long it stays.
const syncEvery = 24 * time.Hour

// syncRetry is how long a client first waits to ask again after a
// failure, doubling up to syncRetryMax while it keeps failing.
const (
	syncRetry    = time.Minute
	syncRetryMax = 30 * time.Minute
)

// maxOwners bounds the files a den connection remembers what they're on;
// past it they're forgotten, and the page's next load of their messages
// brings them back.
const maxOwners = 50_000

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
// channel's files, or those of the messages a retention pass deleted.
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
// cache's worker takes in turn.
type pendingSync struct {
	sync     denproto.Sync
	channels []int64
	pictures map[int64]bool
}

// cacheReady has the cache's worker check what the cache holds against a
// fresh snapshot.
func (c *conn) cacheReady(r denproto.Ready) {
	if !c.cached() {
		return
	}
	p := &pendingSync{sync: r.Sync, pictures: map[int64]bool{}}
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
// can't cross.
func (c *conn) cacheWork(ctx context.Context) {
	if !c.cached() {
		return
	}
	daily := time.NewTicker(syncEvery)
	defer daily.Stop()
	var retry <-chan time.Time
	wait := syncRetry
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.syncDue:
			wait = syncRetry
		case <-daily.C:
		case <-retry:
		}
		retry = nil
		err := c.syncCache(ctx)
		if err == nil {
			wait = syncRetry
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if wait == syncRetry {
			c.m.log.Infof("Couldn't check the cache against a den's changes: %v", err)
		}
		retry = time.After(wait)
		wait = min(2*wait, syncRetryMax)
	}
}

// syncCache checks the cache against the latest snapshot, if one came,
// and then asks the den what changed since the client last synced.
func (c *conn) syncCache(ctx context.Context) error {
	cs, den := c.m.Cache, c.j.denID
	c.mu.Lock()
	p := c.readySync
	c.readySync = nil
	c.mu.Unlock()
	synced, ok, err := cs.Synced(den)
	if err != nil {
		return err
	}
	if p != nil {
		s := p.sync
		if !ok || !bytes.Equal(synced.Epoch, s.Epoch) || synced.Version < s.Floor || synced.Version > s.Version {
			// The den can't say what changed since: what the cache holds
			// of it goes, and the cache starts again from the snapshot.
			if ok {
				c.m.log.Infof("A den can't say what changed since the client last synced, so its cache starts again")
			}
			if err := cs.DropDen(den); err != nil {
				return err
			}
			return cs.SetSynced(den, cache.Synced{Epoch: s.Epoch, Version: s.Version})
		}
		if err := cs.DropChannelsExcept(den, p.channels); err != nil {
			return err
		}
		if s.Expired != "" {
			through, _ := denproto.ParseID(s.Expired)
			if err := cs.DropThrough(den, through); err != nil {
				return err
			}
		}
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
	} else if !ok {
		return nil // nothing to sync from until a snapshot says where
	}
	// A den that isn't connected is asked once it is: its snapshot wakes
	// this worker again.
	c.mu.Lock()
	connected := c.ws != nil
	c.mu.Unlock()
	if !connected {
		return nil
	}
	return c.applyChanges(ctx, synced)
}

// applyChanges asks the den what changed since the client last synced,
// and takes what went out of the cache: the files of messages deleted, and
// those edits took off their messages.
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
			// Pruned, or another epoch, since the last snapshot: what the
			// cache holds goes, and the next snapshot says where to start.
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
		}
		for _, d := range page.Deleted {
			id, err := denproto.ParseID(d.ID)
			if err != nil || !validID(d.ChannelID) {
				return errMalformed
			}
			if err := cs.DropMessage(den, id); err != nil {
				return err
			}
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

// ClearCache empties the cache of files.
func (m *Manager) ClearCache() error {
	if m.Cache == nil {
		return nil
	}
	return m.Cache.Clear()
}
