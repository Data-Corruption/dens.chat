package denclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Signed DM keys (M16.1). A DM's key is made by either member's Dens when
// the DM has no live key both can use: before its first message, after a
// member started over, or after an identity changed. It's 32 random bytes,
// sealed with HPKE to each member's identity, with a statement naming
// both identities and committing to the key, signed by its maker's.
//
// This service uses a key only once the statement checks against its
// maker's identity, the one it kept for them or its own, and names the
// identity it kept for each member: for sending, the one it keeps now;
// for reading, any it kept, since what was sealed before a change still
// opens. A key that names another identity for this member says someone
// may be in the middle. Two made at once see 409 key_exists, and the
// slower takes the live one.
//
// Opening a copy takes X-Wing, so once one opened here its key is kept in
// memory sealed with the seal, as M1.7's copies are, and opened from that
// as it's used.

var errCommit = errors.New("a key's copy opened to another key than its statement names")

// PageKey is a DM key as the page sees it, M1.7's or a signed one,
// without its copies, exchange or statement, which are this service's.
// Signed keys need no checks, and come with their maker as StartedBy.
type PageKey struct {
	ID        string                `json:"id"`
	ChannelID string                `json:"channel_id"`
	StartedBy string                `json:"started_by"`
	StartedAt int64                 `json:"started_at"`
	Stage     string                `json:"stage,omitempty"`
	Checks    []denproto.DMKeyCheck `json:"checks,omitempty"`
	RetiredAt int64                 `json:"retired_at,omitempty"`
	RetiredBy string                `json:"retired_by,omitempty"`
	Retired   string                `json:"retired,omitempty"`
	Signed    bool                  `json:"signed,omitempty"`
}

// pageKey is an M1.7 key as the page sees it.
func pageKey(k denproto.DMKey) PageKey {
	return PageKey{ID: k.ID, ChannelID: k.ChannelID, StartedBy: k.StartedBy, StartedAt: k.StartedAt, Stage: k.Stage, Checks: k.Checks,
		RetiredAt: k.RetiredAt, RetiredBy: k.RetiredBy, Retired: k.Retired}
}

// pageSignedKey is a signed key as the page sees it.
func pageSignedKey(k denproto.SignedKey) PageKey {
	return PageKey{ID: k.ID, ChannelID: k.ChannelID, StartedBy: k.MadeBy, StartedAt: k.MadeAt, RetiredAt: k.RetiredAt,
		RetiredBy: k.RetiredBy, Retired: k.Retired, Signed: true}
}

// cleanSigned checks a signed key from the den: its shape, and that it's
// the key of one of this member's DMs, naming its two members.
func (s *denState) cleanSigned(denID []byte, k denproto.SignedKey) bool {
	if denproto.CheckSignedKey(denID, k) != nil {
		return false
	}
	ch, ok := s.channels[k.ChannelID]
	if !ok || ch.Kind != denproto.KindDM || len(k.Members) != 2 {
		return false
	}
	for _, m := range k.Members {
		if !slices.Contains(ch.Members, m.MemberID) {
			return false
		}
	}
	return true
}

// liveSigned returns a DM's live signed key, if it has one.
func (s *denState) liveSigned(channel string) (denproto.SignedKey, bool) {
	for _, k := range s.signed {
		if k.ChannelID == channel && k.Retired == "" {
			return k, true
		}
	}
	return denproto.SignedKey{}, false
}

// checkSignedLocked says why a signed key can't be used, or "" when it
// can: for sending, only with the identities this Dens keeps now. A key's
// verdict for reading is kept until what was kept changes. The caller
// holds c.mu.
func (c *conn) checkSignedLocked(k denproto.SignedKey, sending bool) string {
	if !sending {
		if v, ok := c.verdicts[k.ID]; ok {
			return v
		}
	}
	v := c.verdictLocked(k, sending)
	if !sending {
		c.verdicts[k.ID] = v
	}
	return v
}

func (c *conn) verdictLocked(k denproto.SignedKey, sending bool) string {
	me := c.meLocked()
	if k.Copy == nil {
		if k.Retired == denproto.RetiredStartedOver && k.RetiredBy == me {
			return lockedLost
		}
		return lockedBroken
	}
	own := c.ownHashLocked()
	if own == nil {
		return lockedNoSeal
	}
	s, err := k.Statement(c.j.denID)
	if err != nil {
		return lockedBroken
	}
	for _, m := range k.Members {
		if m.MemberID == me {
			if !bytes.Equal(m.Identity, own) {
				return lockedKeys
			}
			continue
		}
		kept := c.ids.Record.Members[m.MemberID]
		switch {
		case kept == nil:
			return lockedKeys
		case bytes.Equal(m.Identity, kept.Hash):
		case !sending && slices.ContainsFunc(kept.Past, func(h denproto.Bytes) bool { return bytes.Equal(h, m.Identity) }):
		default:
			return lockedKeys
		}
	}
	// The den sends the maker's keys with the key, which CheckSignedKey
	// found hash to the identity the statement names for them, checked
	// above against what was kept.
	if !denproto.VerifyStatement(k.Maker.Sig, s, k.Signature) {
		return lockedBroken
	}
	return ""
}

// signedKeyLocked opens this member's copy of a signed key once it checks
// out, and says why it can't otherwise. The caller holds c.mu, has the
// seal, and clears the key once it's used.
func (c *conn) signedKeyLocked(k denproto.SignedKey) ([]byte, string) {
	if v := c.checkSignedLocked(k, false); v != "" {
		return nil, v
	}
	if c.j.seal == nil {
		return nil, lockedNoSeal
	}
	ch, _ := denproto.ParseID(k.ChannelID)
	id, _ := denproto.ParseID(k.ID)
	me, _ := denproto.ParseID(c.meLocked())
	var key []byte
	err := c.j.seal.Use(func(seal []byte) error {
		if local := c.copies[k.ID]; local != nil {
			var err error
			key, err = denproto.OpenDMKey(seal, c.j.denID, ch, id, me, local)
			return err
		}
		ik, err := denproto.DeriveIdentity(seal, c.j.denID)
		if err != nil {
			return err
		}
		defer ik.Close()
		opened, err := ik.OpenKeyCopy(c.j.denID, ch, k.KID, me, k.Copy)
		if err != nil {
			return err
		}
		if !denproto.Equal(denproto.KeyCommit(k.KID, opened), k.Commit) {
			clear(opened)
			return errCommit
		}
		if local, err := denproto.SealDMKey(seal, c.j.denID, ch, id, me, opened); err == nil {
			c.copies[k.ID] = local
		}
		key = opened
		return nil
	})
	if err != nil {
		clear(key)
		return nil, lockedBroken
	}
	return key, ""
}

// partnerLocked returns the other member of one of this member's DMs.
// The caller holds c.mu.
func (c *conn) partnerLocked(channel string) (string, bool) {
	ch, ok := c.den.channels[channel]
	if !ok || ch.Kind != denproto.KindDM {
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

// canMakeKeyLocked says why a DM can't get a new key, if it can't: on a
// den older than M16.1, which keeps no identities; with a member whose
// Dens publishes none, being older; or while it holds, since the other's
// keys changed after this member verified them. The caller holds c.mu.
func (c *conn) canMakeKeyLocked(channel string) error {
	partner, ok := c.partnerLocked(channel)
	if !ok {
		return ErrUnknownChannel
	}
	if !c.den.identities {
		return inputError(errors.New("this den runs an older Dens, which can't set up this DM's encryption: its owner can update it"))
	}
	m := c.den.members[partner]
	if m.LeftAt != 0 {
		return inputError(errors.New("they're no longer in this den"))
	}
	if m.IdentityHash() == nil {
		return inputError(fmt.Errorf("%s's Dens needs an update before you can write here", m.DisplayName))
	}
	if k := c.ids.Record.Members[partner]; k != nil && k.Held {
		return inputError(fmt.Errorf("%s's keys changed since you compared codes: compare them again, or send anyway", m.DisplayName))
	}
	return nil
}

// makeKey makes a new key for a DM: this member's identity published
// first, the other's fetched and checked against the one kept, then the
// key sealed to both and signed. One the other member's Dens made at the
// same moment wins as the den's live one.
func (c *conn) makeKey(ctx context.Context, channel string) error {
	if err := c.publishIdentity(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	partner, ok := c.partnerLocked(channel)
	if !ok {
		c.mu.Unlock()
		return ErrUnknownChannel
	}
	me := c.meLocked()
	changed := c.ids.Record.meet(partner, c.den.members[partner].IdentityHash(), c.m.wall().UnixMilli(), true)
	var kept []byte
	if k := c.ids.Record.Members[partner]; k != nil {
		kept = slices.Clone(k.Hash)
	}
	own := slices.Clone(c.ownHashLocked())
	name := c.den.members[partner].DisplayName
	c.mu.Unlock()
	if changed {
		c.identitiesChanged(ctx)
	}
	if kept == nil || own == nil {
		return inputError(fmt.Errorf("%s's Dens needs an update before you can write here", name))
	}
	var list denproto.IdentityList
	if err := c.call(ctx, http.MethodGet, "/api/identities?members="+partner, nil, &list); err != nil {
		return err
	}
	i := slices.IndexFunc(list.Identities, func(m denproto.MemberIdentity) bool { return m.MemberID == partner })
	if i < 0 {
		return inputError(fmt.Errorf("%s's Dens needs an update before you can write here", name))
	}
	theirs := list.Identities[i].Identity()
	if theirs.Check() != nil || !denproto.Equal(theirs.Hash(), kept) {
		// Their identity changed as this was asked; the change is on its way.
		return inputError(fmt.Errorf("%s's keys just changed: try again in a moment", name))
	}
	ch, _ := denproto.ParseID(channel)
	mine, _ := denproto.ParseID(me)
	other, _ := denproto.ParseID(partner)
	key, kid := denproto.Random(denproto.DMKeySize), denproto.Random(denproto.KIDSize)
	defer clear(key)
	members := []denproto.KeyMember{{MemberID: me, Identity: own}, {MemberID: partner, Identity: kept}}
	if other < mine {
		members[0], members[1] = members[1], members[0]
	}
	req := denproto.SignedKeyRequest{KID: kid, Commit: denproto.KeyCommit(kid, key), Members: members}
	if err := c.ownIdentity(func(ik *denproto.IdentityKeys) error {
		ours, err := denproto.SealKeyCopy(ik.Public(), c.j.denID, ch, kid, mine, key)
		if err != nil {
			return err
		}
		their, err := denproto.SealKeyCopy(theirs, c.j.denID, ch, kid, other, key)
		if err != nil {
			return err
		}
		s, err := req.Statement(c.j.denID, channel, me)
		if err != nil {
			return err
		}
		req.Signature = ik.Sign(s)
		req.Copies = []denproto.KeyCopy{{MemberID: me, Copy: ours}, {MemberID: partner, Copy: their}}
		return nil
	}); err != nil {
		return err
	}
	var made denproto.SignedKey
	err := c.call(ctx, http.MethodPost, "/api/dms/"+channel+"/signed-keys", req, &made)
	if denproto.IsCode(err, denproto.CodeKeyExists) {
		// The other member's Dens made one at the same moment: take it.
		var keys denproto.SignedKeyList
		if err := c.call(ctx, http.MethodGet, "/api/dms/"+channel+"/signed-keys", nil, &keys); err != nil {
			return err
		}
		c.addSignedKeys(keys.Keys...)
		return nil
	}
	if err != nil {
		return err
	}
	c.addSignedKeys(made)
	return nil
}

// addSignedKeys keeps signed keys the den answered a request with, as its
// events would.
func (c *conn) addSignedKeys(keys ...denproto.SignedKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.den == nil {
		return
	}
	for _, k := range keys {
		if c.den.cleanSigned(c.j.denID, k) {
			c.den.signed[k.ID] = k
			delete(c.verdicts, k.ID)
		}
	}
}
