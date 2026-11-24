package denclient

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Private DMs. A DM's messages and files are sealed here, with the
// DM's key, before they leave for the den, and opened here when they come
// back; the page never holds a key, and the den never sees one.
//
// A DM's key is 32 random bytes this service or the other
// member's makes as the DM first needs one, sealed to both members'
// identities and signed by its maker's (see signedkeys.go), so a DM opens
// at once. The keys both members checked before v1.9.0, from an exchange
// whose check code each typed, stay live, and open with the member's copy
// sealed with their seal.

// Why a DM message can't be opened, for the page.
const (
	lockedNoSeal    = "no_seal"   // this device doesn't have the member's DM seal
	lockedUnchecked = "unchecked" // sealed before v1.9.0 with a key the member never checked
	lockedLost      = "lost"      // the member started over since; their copy of the key is gone
	lockedBroken    = "broken"    // it doesn't open: someone changed it
	lockedKeys      = "keys"      // its key names identities other than those this Dens kept
	// lockedAuthor is a group's message changed by someone its author
	// didn't sign for, which waits for its author to keep or delete it.
	lockedAuthor = "author"
)

var (
	errNoSeal = inputError(errors.New("this device doesn't have your DM recovery key for this den: enter it, or reset it"))
	// errDigits is digits typed to approve a device that don't match.
	errDigits = inputError(errors.New("those digits don't match. Check you typed what the other screen shows; " +
		"if they really differ, someone may be in the middle, so start the check again"))
)

// PageMessage is a message as the page gets it. A DM's comes opened, with
// its text and files, or says why it can't be; so does the preview of the
// message it replies to, which stands in for the den's.
type PageMessage struct {
	denproto.Message
	Locked string     `json:"locked,omitempty"`
	Reply  *PageReply `json:"reply,omitempty"`
	// DMReactions are a DM message's sets of reactions, opened, in place
	// of the den's sealed ones.
	DMReactions []PageReactionSet `json:"dm_reactions,omitempty"`
	// Waiting is, on its author's devices alone, a group's message that
	// waits for them (lockedAuthor): what it says, and who changed it.
	Waiting *PageWaiting `json:"waiting,omitempty"`
}

// PageWaiting is a group's message changed by someone its author didn't
// sign for, as its author's page shows it, for them to keep or delete.
type PageWaiting struct {
	By          string          `json:"by"`
	Text        string          `json:"text"`
	Attachments []denproto.File `json:"attachments"`
	// Shown is the digest of the revision this shows, which Keep gives
	// back.
	Shown denproto.Bytes `json:"shown"`
}

// shownSum is a digest of a message's revision as the den gave it: the
// key, nonce and revision it opens under, and what's sealed. Keep signs
// only the revision its author's page showed, and the den's word for
// which that is, its number, is no check: a member can seal two at one.
func shownSum(m denproto.Message) denproto.Bytes {
	h := sha256.New()
	for _, part := range [][]byte{[]byte(m.KeyID), m.Nonce, []byte(strconv.Itoa(m.Revision)), m.Sealed} {
		h.Write(binary.BigEndian.AppendUint64(nil, uint64(len(part))))
		h.Write(part)
	}
	return h.Sum(nil)
}

// PageReply previews the message another replies to, for the page.
type PageReply struct {
	AuthorID string `json:"author_id"`
	Text     string `json:"text"`
	Locked   bool   `json:"locked,omitempty"`
}

// PageHistory is a page of messages as the page gets it.
type PageHistory struct {
	Messages []PageMessage `json:"messages"`
	HasOlder bool          `json:"has_older"`
	HasNewer bool          `json:"has_newer"`
}

// dmFile is a DM's file as a message this service opened seals it, for
// the page: its key, size and digests, its preview's, and the DM it's in.
// mine is a message of this member's.
type dmFile struct {
	channel int64
	file    denproto.DMFile
	mine    bool
}

// maxDMFiles bounds the DM files a den connection remembers keys for, by
// the messages that name them; past it they're forgotten, and the page's
// next load of their messages brings them back.
const maxDMFiles = 20_000

// errNotOpened is a file asked for in a DM, a group or notes under a
// message this service hasn't opened, or that doesn't name it, or in a
// channel it never held: a sealed file's key and digests are its
// message's, so it opens under no other, and never as the den's bytes.
var errNotOpened = &denproto.Error{Status: http.StatusNotFound, Code: denproto.CodeNotFound,
	Message: "This file's message isn't open here; load it again."}

// place is where the page shows a file: a message, in its channel, or
// neither, for a picture or an upload waiting to be sent.
type place struct{ channel, message int64 }

// keepDMFileLocked remembers a file a message names, by the message. The
// caller holds c.mu.
func (c *conn) keepDMFileLocked(id string, message int64, df dmFile) {
	byMessage := c.dmFiles[id]
	if _, ok := byMessage[message]; !ok {
		if c.dmFileN >= maxDMFiles {
			clear(c.dmFiles)
			c.dmFileN, byMessage = 0, nil
		}
		c.dmFileN++
	}
	if byMessage == nil {
		byMessage = map[int64]dmFile{}
		c.dmFiles[id] = byMessage
	}
	byMessage[message] = df
}

// dropDMFilesLocked forgets files, under every message. The caller holds
// c.mu.
func (c *conn) dropDMFilesLocked(ids ...string) {
	for _, id := range ids {
		c.dmFileN -= len(c.dmFiles[id])
		delete(c.dmFiles, id)
	}
}

// sealedLocked finds the file the page asks for where it shows it: in a
// DM, a group or notes, by the kind this service keeps for good, as the
// message it's on seals it; under no message, as an upload of this
// member's waiting to be sent. A file in a channel isn't sealed, nor one
// under no message that no message this service opened names. The caller
// holds c.mu.
func (c *conn) sealedLocked(id string, at place) (dmFile, bool, error) {
	if at.message == 0 {
		if u, ok := c.uploads[id]; ok {
			ch, _ := denproto.ParseID(u.channel)
			return dmFile{channel: ch, file: u.file, mine: true}, true, nil
		}
		if _, ok := c.dmFiles[id]; ok {
			return dmFile{}, true, errNotOpened
		}
		return dmFile{}, false, nil
	}
	channel := denproto.FormatID(at.channel)
	if !c.privateLocked(channel) {
		if _, held := c.den.channels[channel]; !held && c.den.kinds[channel] == "" {
			return dmFile{}, false, errNotOpened
		}
		return dmFile{}, false, nil
	}
	df, ok := c.dmFiles[id][at.message]
	if !ok || df.channel != at.channel {
		return dmFile{}, true, errNotOpened
	}
	return df, true, nil
}

// me is this member's ID; the caller holds c.mu.
func (c *conn) meLocked() string { return c.profile.Member.ID }

// dmKeyLocked opens the key of a DM a message or set of reactions names,
// a legacy key or a signed one, or says why it can't; in the member's notes,
// the key their seal makes, which a message names by the notes'
// channel. The caller holds c.mu, and clears the key once it's used.
func (c *conn) dmKeyLocked(channel, keyID string) ([]byte, string) {
	if c.j.seal == nil {
		return nil, lockedNoSeal
	}
	if c.den.channels[channel].Kind == denproto.KindNotes {
		if keyID != channel {
			return nil, lockedBroken
		}
		key, err := c.notesKeyLocked()
		if err != nil {
			return nil, lockedBroken
		}
		return key, ""
	}
	if k, ok := c.den.keys[keyID]; ok {
		if k.ChannelID != channel {
			return nil, lockedBroken
		}
		me := c.meLocked()
		if k.Sealed == nil {
			if k.Retired == denproto.RetiredStartedOver && k.RetiredBy == me {
				return nil, lockedLost
			}
			return nil, lockedUnchecked
		}
		ch, _ := denproto.ParseID(channel)
		id, _ := denproto.ParseID(keyID)
		mine, _ := denproto.ParseID(me)
		var key []byte
		if err := c.j.seal.Use(func(seal []byte) error {
			var err error
			key, err = denproto.OpenDMKey(seal, c.j.denID, ch, id, mine, k.Sealed)
			return err
		}); err != nil {
			return nil, lockedBroken
		}
		return key, ""
	}
	if k, ok := c.den.signed[keyID]; ok && k.ChannelID == channel {
		return c.signedKeyLocked(k)
	}
	return nil, lockedBroken
}

// openPayloadLocked opens a DM message's sealed text and files with the
// key it names, and says who sealed it: in a DM its author, in a group
// whoever its signature says, which must be a member its key names: its
// author at its first revision, and after it, its author or a member they
// signed for. The payload's grant is one that checks, in a group alone.
// One another member changed with no grant waits for its author
// (lockedAuthor), whose devices alone get what it says. The caller holds
// c.mu.
func (c *conn) openPayloadLocked(channel, author, keyID string, nonce []byte, revision int, sealed []byte) (denproto.DMPayload, string, string) {
	key, locked := c.dmKeyLocked(channel, keyID)
	if locked != "" {
		return denproto.DMPayload{}, "", locked
	}
	defer clear(key)
	ch, _ := denproto.ParseID(channel)
	id, _ := denproto.ParseID(keyID)
	who, _ := denproto.ParseID(author)
	ad := denproto.MessageAD{DenID: c.j.denID, Channel: ch, Author: who, Nonce: nonce, KeyID: id, Revision: revision}
	if kind := c.den.channels[channel].Kind; kind != denproto.KindGroupDM {
		p, err := denproto.OpenDMMessage(key, ad, sealed)
		switch {
		case err != nil && kind == denproto.KindNotes:
			// A note that doesn't open under the key the seal makes was
			// sealed under the seal before the member started over, as far
			// as anyone can tell, and is gone for good either way.
			return denproto.DMPayload{}, "", lockedLost
		case err != nil:
			return denproto.DMPayload{}, "", lockedBroken
		}
		p.Grant = nil
		return p, author, ""
	}
	k, ok := c.den.signed[keyID]
	if !ok {
		return denproto.DMPayload{}, "", lockedBroken
	}
	stmt, err := k.Statement(c.j.denID)
	if err != nil {
		return denproto.DMPayload{}, "", lockedBroken
	}
	p, signer, err := denproto.OpenGroupMessage(key, ad, stmt.Named, sealed)
	if err != nil || revision == 1 && signer != who {
		return denproto.DMPayload{}, "", lockedBroken
	}
	if p.Grant != nil && !denproto.VerifyGrant(c.j.denID, ch, who, nonce, revision, *p.Grant, c.keptHashesLocked(author)) {
		if signer != who {
			return denproto.DMPayload{}, "", lockedBroken
		}
		p.Grant = nil
	}
	by := denproto.FormatID(signer)
	switch {
	case signer == who:
	case p.Grant == nil:
		// It can't be told from one whose grant was left out, so it opens as
		// nobody's, but for its author to keep or delete.
		if author != c.meLocked() {
			return denproto.DMPayload{}, by, lockedAuthor
		}
		return p, by, lockedAuthor
	case !slices.Contains(p.Grant.Editors, by):
		return denproto.DMPayload{}, "", lockedBroken
	}
	return p, by, ""
}

// keptHashesLocked lists the identities this Dens kept for a member, by
// their hashes, the current one and those before: for this member, their
// own. The caller holds c.mu.
func (c *conn) keptHashesLocked(member string) []denproto.Bytes {
	if member == c.meLocked() {
		own := c.ownHashLocked()
		if own == nil {
			return slices.Clone(c.ids.Record.Own)
		}
		return append([]denproto.Bytes{own}, c.ids.Record.Own...)
	}
	k := c.ids.Record.Members[member]
	if k == nil {
		return nil
	}
	return append([]denproto.Bytes{k.Hash}, k.Past...)
}

// openLocked opens a DM message for the page; other messages pass as they
// are. The caller holds c.mu.
func (c *conn) openLocked(m denproto.Message) PageMessage {
	var reply *PageReply
	if r := m.Reply; r != nil {
		reply = &PageReply{AuthorID: r.AuthorID, Text: r.Text}
		if r.Sealed != nil {
			p, _, locked := c.openPayloadLocked(m.ChannelID, r.AuthorID, r.KeyID, r.Nonce, r.Revision, r.Sealed)
			reply.Text, reply.Locked = "", locked != ""
			if locked == "" {
				reply.Text = denproto.Excerpt(p.Text)
			}
		}
	}
	out := m
	out.Reply = nil
	if m.Sealed == nil {
		// Every message of a DM, a group or notes is sealed by its author's
		// Dens, so one there in the clear is the den's own, and keeps
		// nothing the den wrote.
		if c.privateLocked(m.ChannelID) {
			return PageMessage{Message: denproto.Message{ID: m.ID, ChannelID: m.ChannelID, AuthorID: m.AuthorID, CreatedAt: m.CreatedAt,
				Revision: m.Revision}, Locked: lockedBroken}
		}
		c.noteLocked(m)
		return PageMessage{Message: out, Reply: reply}
	}
	// Its members' sets of reactions open with the keys they name, which
	// may be newer than the message's. Its mark is its seal's alone.
	sets := c.openSetsLocked(m)
	out.Sealed, out.DMReactions, out.Forwarded = nil, nil, false
	p, signer, locked := c.openPayloadLocked(m.ChannelID, m.AuthorID, m.KeyID, m.Nonce, m.Revision, m.Sealed)
	// In a group, who may edit and who did are what its author signed for
	// and who signed, never the den's word.
	if c.den.channels[m.ChannelID].Kind == denproto.KindGroupDM {
		out.Editors, out.EditedBy = nil, ""
		if p.Grant != nil && locked == "" {
			out.Editors = slices.Clone(p.Grant.Editors)
		}
		if signer != m.AuthorID {
			out.EditedBy = signer
		}
	}
	if locked != "" {
		pm := PageMessage{Message: out, Locked: locked, Reply: reply, DMReactions: sets}
		if locked == lockedAuthor && m.AuthorID == c.meLocked() {
			pm.Waiting = &PageWaiting{By: signer, Text: p.Text, Attachments: c.dmFilesLocked(m, p), Shown: shownSum(m)}
		}
		return pm
	}
	out.Text, out.Forwarded = p.Text, p.Forwarded
	out.Attachments = append(out.Attachments, c.dmFilesLocked(m, p)...)
	return PageMessage{Message: out, Reply: reply, DMReactions: sets}
}

// dmFilesLocked lists a DM message's files for the page, keeping their
// keys for when the page asks for them. The caller holds c.mu.
func (c *conn) dmFilesLocked(m denproto.Message, p denproto.DMPayload) []denproto.File {
	channel, _ := denproto.ParseID(m.ChannelID)
	message, _ := denproto.ParseID(m.ID)
	mine := m.AuthorID == c.meLocked()
	out := []denproto.File{}
	for _, f := range p.Files {
		out = append(out, dmUploaded(f).File)
		c.keepDMFileLocked(f.ID, message, dmFile{channel: channel, file: f, mine: mine})
	}
	return out
}

// open opens a DM message for the page.
func (c *conn) open(m denproto.Message) PageMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.den == nil {
		return PageMessage{Message: m, Locked: lockedBroken}
	}
	return c.openLocked(m)
}

// isDM reports whether a channel is one of this member's DMs, and who the
// other member is.
func (c *conn) isDM(channel string) (other string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.den == nil {
		return "", false
	}
	ch, found := c.den.channels[channel]
	if !found || ch.Kind != denproto.KindDM {
		return "", false
	}
	me := c.meLocked()
	for _, id := range ch.Members {
		if id != me {
			return id, true
		}
	}
	return "", false
}

// sendKey opens the key this member sends with in a DM: its live key,
// once it's one both members can use, which a legacy key is once both
// checked it, and a signed key once it names their identities as this
// Dens kept them. A DM without one gets a new key, sealed to both
// members' identities.
func (c *conn) sendKey(ctx context.Context, channel string) (string, []byte, error) {
	for range 3 {
		c.mu.Lock()
		if c.den == nil {
			c.mu.Unlock()
			return "", nil, ErrNotStarted
		}
		if c.j.seal == nil {
			c.mu.Unlock()
			return "", nil, errNoSeal
		}
		// The member's notes go under the key their seal makes, named by
		// the notes' channel, or by the kind kept from the den's answer
		// until the notes' event comes, since Forward makes and sends at once.
		if c.den.channels[channel].Kind == denproto.KindNotes || c.den.kinds[channel] == denproto.KindNotes {
			key, err := c.notesKeyLocked()
			c.mu.Unlock()
			return channel, key, err
		}
		if k, ok := c.den.liveKey(channel); ok && c.den.channels[channel].Kind == denproto.KindDM && len(k.Checks) == 2 && k.Sealed != nil {
			key, locked := c.dmKeyLocked(channel, k.ID)
			c.mu.Unlock()
			if locked != "" {
				return "", nil, fmt.Errorf("open the DM's key: it's %s", locked)
			}
			return k.ID, key, nil
		}
		if k, ok := c.den.liveSigned(channel); ok && c.checkSignedLocked(k, true) == "" {
			if err := c.heldLocked(channel); err != nil {
				c.mu.Unlock()
				return "", nil, err
			}
			key, locked := c.signedKeyLocked(k)
			c.mu.Unlock()
			if locked != "" {
				return "", nil, fmt.Errorf("open the DM's key: it's %s", locked)
			}
			return k.ID, key, nil
		}
		err := c.canMakeKeyLocked(channel)
		group := c.den.channels[channel].Kind == denproto.KindGroupDM
		c.mu.Unlock()
		if err != nil {
			return "", nil, err
		}
		makeKey := c.makeKey
		if group {
			makeKey = c.makeGroupKey
		}
		if err := makeKey(ctx, channel); err != nil {
			return "", nil, err
		}
	}
	return "", nil, errors.New("the DM's keys keep changing")
}

// sealMessage seals a DM message's text and files for the den, as its
// author wrote it at the given revision, with the DM's live key.
func (c *conn) sealMessage(ctx context.Context, channel, author string, nonce []byte, revision int, p denproto.DMPayload) (string, denproto.Bytes, error) {
	keyID, key, err := c.sendKey(ctx, channel)
	if err != nil {
		return "", nil, err
	}
	defer clear(key)
	ch, _ := denproto.ParseID(channel)
	id, _ := denproto.ParseID(keyID)
	who, _ := denproto.ParseID(author)
	ad := denproto.MessageAD{DenID: c.j.denID, Channel: ch, Author: who, Nonce: nonce, KeyID: id, Revision: revision}
	var sealed denproto.Bytes
	if c.isGroup(channel) {
		// A group's is signed inside by whoever wrote or changed it.
		me, _ := denproto.ParseID(c.memberID())
		err = c.ownIdentity(func(ik *denproto.IdentityKeys) error {
			var err error
			sealed, err = denproto.SealGroupMessage(key, ad, ik, me, p)
			return err
		})
		if errors.Is(err, errNoSeal) {
			return "", nil, err
		}
	} else {
		sealed, err = denproto.SealDMMessage(key, ad, p)
	}
	if err != nil {
		return "", nil, inputError(errors.New("this message is too long to send"))
	}
	return keyID, sealed, nil
}

// ErrUnknownChannel is a channel this member can't see, or not a DM.
var ErrUnknownChannel = inputError(errors.New("no such DM"))

// useSeal calls fn with this member's DM seal here.
func (c *conn) useSeal(fn func(seal []byte) error) error {
	c.mu.Lock()
	s := c.j.seal
	c.mu.Unlock()
	if s == nil {
		return errNoSeal
	}
	return s.Use(fn)
}

// notesKeyLocked makes the key the member's notes here are sealed with,
// from their seal. The caller holds c.mu, and clears the key once
// it's used.
func (c *conn) notesKeyLocked() ([]byte, error) {
	var key []byte
	err := c.j.seal.Use(func(seal []byte) error {
		var err error
		key, err = denproto.NotesKey(seal, c.j.denID)
		return err
	})
	return key, err
}
