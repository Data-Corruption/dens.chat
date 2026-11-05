package denclient

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Private DMs (M1.7). A DM's messages and files are sealed here, with the
// DM's key, before they leave for the den, and opened here when they come
// back; the page never holds a key, and the den never sees one.
//
// From M16.1 a DM's key is 32 random bytes this service or the other
// member's makes as the DM first needs one, sealed to both members'
// identities and signed by its maker's (see signedkeys.go), so a DM opens
// at once. The keys both members checked before M16.1, from an exchange
// whose check code each typed, stay live, and open with the member's copy
// sealed with their seal.

// Why a DM message can't be opened, for the page.
const (
	lockedNoSeal    = "no_seal"   // this device doesn't have the member's DM seal
	lockedUnchecked = "unchecked" // sealed before M16.1 with a key the member never checked
	lockedLost      = "lost"      // the member started over since; their copy of the key is gone
	lockedBroken    = "broken"    // it doesn't open: someone changed it
	lockedKeys      = "keys"      // its key names identities other than those this Dens kept
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
	// of the den's sealed ones (M15.3).
	DMReactions []PageReactionSet `json:"dm_reactions,omitempty"`
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

// dmFile is a DM's file this service can open for the page: its key, the
// blob of its preview, and the DM it's in.
type dmFile struct {
	channel int64
	key     []byte
	thumb   string
	// message is the message the file is on, which the cache drops it
	// with (M6); 0 for one waiting to be sent.
	message int64
}

// maxDMFiles bounds the DM files a den connection remembers keys for; past
// it they're forgotten, and the page's next load of their messages brings
// them back.
const maxDMFiles = 20_000

// me is this member's ID; the caller holds c.mu.
func (c *conn) meLocked() string { return c.profile.Member.ID }

// dmKeyLocked opens the key of a DM a message or set of reactions names,
// M1.7's or a signed one, or says why it can't. The caller holds c.mu,
// and clears the key once it's used.
func (c *conn) dmKeyLocked(channel, keyID string) ([]byte, string) {
	if c.j.seal == nil {
		return nil, lockedNoSeal
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
// (M16.3) whoever its signature says, which must be a member its key
// names, and its author at its first revision. The caller holds c.mu.
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
	if c.den.channels[channel].Kind != denproto.KindGroupDM {
		p, err := denproto.OpenDMMessage(key, ad, sealed)
		if err != nil {
			return denproto.DMPayload{}, "", lockedBroken
		}
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
	return p, denproto.FormatID(signer), ""
}

// openLocked opens a DM message for the page; other messages pass as they
// are. The caller holds c.mu.
func (c *conn) openLocked(m denproto.Message) PageMessage {
	var reply *PageReply
	if r := m.Reply; r != nil {
		reply = &PageReply{AuthorID: r.AuthorID, Text: r.Text}
		if r.Sealed != nil {
			p, _, locked := c.openPayloadLocked(m.ChannelID, r.AuthorID, r.KeyID, r.Nonce, r.Revision, r.Sealed)
			reply.Text, reply.Locked = denproto.Excerpt(p.Text), locked != ""
		}
	}
	out := m
	out.Reply = nil
	if m.Sealed == nil {
		c.noteLocked(m)
		return PageMessage{Message: out, Reply: reply}
	}
	// Its members' sets of reactions open with the keys they name, which
	// may be newer than the message's (M15.3).
	sets := c.openSetsLocked(m)
	out.Sealed, out.DMReactions = nil, nil
	p, signer, locked := c.openPayloadLocked(m.ChannelID, m.AuthorID, m.KeyID, m.Nonce, m.Revision, m.Sealed)
	// A group's message changed by someone the den doesn't say may edit it
	// isn't shown as its author's (M16.3).
	if locked == "" && signer != m.AuthorID && signer != m.EditedBy && !slices.Contains(m.Editors, signer) {
		locked = lockedBroken
	}
	if locked != "" {
		return PageMessage{Message: out, Locked: locked, Reply: reply, DMReactions: sets}
	}
	out.Text = p.Text
	channel, _ := denproto.ParseID(m.ChannelID)
	message, _ := denproto.ParseID(m.ID)
	for _, f := range p.Files {
		df := dmFile{channel: channel, key: f.Key, message: message}
		if f.Thumb != nil {
			df.thumb = f.Thumb.ID
		}
		out.Attachments = append(out.Attachments, dmUploaded(f).File)
		if len(c.dmFiles) >= maxDMFiles {
			clear(c.dmFiles)
		}
		c.dmFiles[f.ID] = df
	}
	return PageMessage{Message: out, Reply: reply, DMReactions: sets}
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
// once it's one both members can use, which an M1.7 key is once both
// checked it, and a signed key once it names their identities as this
// Dens kept them. A DM without one gets a new key, sealed to both
// members' identities (M16.1).
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
		if k, ok := c.den.liveKey(channel); ok && c.den.channels[channel].Kind == denproto.KindDM && len(k.Checks) == 2 && k.Sealed != nil {
			key, locked := c.dmKeyLocked(channel, k.ID)
			c.mu.Unlock()
			if locked != "" {
				return "", nil, fmt.Errorf("open the DM's key: it's %s", locked)
			}
			return k.ID, key, nil
		}
		if k, ok := c.den.liveSigned(channel); ok && c.checkSignedLocked(k, true) == "" {
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
		// A group's is signed inside by whoever wrote or changed it (M16.3).
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
