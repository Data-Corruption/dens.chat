package denclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Private DMs (M1.7). A DM's messages and files are sealed here, with the
// DM's key, before they leave for the den, and opened here when they come
// back; the page never holds a key, and the den never sees one.
//
// Each DM key comes from an exchange of one-time keys between the two
// members' Dens, which the den relays: the starter's offer, the other's
// answer, and the starter's reveal. Each side's state waits on the den,
// sealed with the member's DM seal, so any of their devices that's online
// moves the exchange on (keyWork). The step no device takes alone is the
// check: the member reads out their half of the code and types the other
// half as the other member reads it out. Only then does this service store
// the member's copy of the key, sealed with their seal, which is what lets
// them send with it and their devices read with it.

// Why a DM message can't be opened, for the page.
const (
	lockedNoSeal    = "no_seal"   // this device doesn't have the member's DM seal
	lockedUnchecked = "unchecked" // the member hasn't compared the key's check code
	lockedLost      = "lost"      // the member started over since; their copy of the key is gone
	lockedBroken    = "broken"    // it doesn't open: someone changed it
)

var (
	errNoSeal = inputError(errors.New("this device doesn't have your DM seal for this den: type it in, or start over"))
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
}

// maxDMFiles bounds the DM files a den connection remembers keys for; past
// it they're forgotten, and the page's next load of their messages brings
// them back.
const maxDMFiles = 20_000

// me is this member's ID; the caller holds c.mu.
func (c *conn) meLocked() string { return c.profile.Member.ID }

// openPayloadLocked opens a DM message's sealed text and files with the
// key it names; the caller holds c.mu.
func (c *conn) openPayloadLocked(channel, author, keyID string, nonce []byte, revision int, sealed []byte) (denproto.DMPayload, string) {
	var p denproto.DMPayload
	if c.j.seal == nil {
		return p, lockedNoSeal
	}
	k, ok := c.den.keys[keyID]
	if !ok || k.ChannelID != channel {
		return p, lockedBroken
	}
	me := c.meLocked()
	if k.Sealed == nil {
		if k.Retired == denproto.RetiredStartedOver && k.RetiredBy == me {
			return p, lockedLost
		}
		return p, lockedUnchecked
	}
	ch, _ := denproto.ParseID(channel)
	id, _ := denproto.ParseID(keyID)
	who, _ := denproto.ParseID(author)
	mine, _ := denproto.ParseID(me)
	err := c.j.seal.Use(func(seal []byte) error {
		key, err := denproto.OpenDMKey(seal, c.j.denID, ch, id, mine, k.Sealed)
		if err != nil {
			return err
		}
		defer clear(key)
		p, err = denproto.OpenDMMessage(key, denproto.MessageAD{DenID: c.j.denID, Channel: ch, Author: who, Nonce: nonce, KeyID: id, Revision: revision}, sealed)
		return err
	})
	if err != nil {
		return denproto.DMPayload{}, lockedBroken
	}
	return p, ""
}

// openLocked opens a DM message for the page; other messages pass as they
// are. The caller holds c.mu.
func (c *conn) openLocked(m denproto.Message) PageMessage {
	var reply *PageReply
	if r := m.Reply; r != nil {
		reply = &PageReply{AuthorID: r.AuthorID, Text: r.Text}
		if r.Sealed != nil {
			p, locked := c.openPayloadLocked(m.ChannelID, r.AuthorID, r.KeyID, r.Nonce, r.Revision, r.Sealed)
			reply.Text, reply.Locked = denproto.Excerpt(p.Text), locked != ""
		}
	}
	out := m
	out.Reply = nil
	if m.Sealed == nil {
		return PageMessage{Message: out, Reply: reply}
	}
	out.Sealed = nil
	p, locked := c.openPayloadLocked(m.ChannelID, m.AuthorID, m.KeyID, m.Nonce, m.Revision, m.Sealed)
	if locked != "" {
		return PageMessage{Message: out, Locked: locked, Reply: reply}
	}
	out.Text = p.Text
	channel, _ := denproto.ParseID(m.ChannelID)
	for _, f := range p.Files {
		file := denproto.File{ID: f.ID, Name: f.Name, Type: f.Type, Size: f.Size, Width: f.Width, Height: f.Height, Animated: f.Animated}
		df := dmFile{channel: channel, key: f.Key}
		if f.Thumb != nil {
			file.Thumb = &denproto.Thumb{Width: f.Thumb.Width, Height: f.Thumb.Height}
			df.thumb = f.Thumb.ID
		}
		out.Attachments = append(out.Attachments, file)
		if len(c.dmFiles) >= maxDMFiles {
			clear(c.dmFiles)
		}
		c.dmFiles[f.ID] = df
	}
	return PageMessage{Message: out, Reply: reply}
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
// which they checked.
func (c *conn) sendKey(channel string) (string, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.den == nil {
		return "", nil, ErrNotStarted
	}
	if c.j.seal == nil {
		return "", nil, errNoSeal
	}
	k, ok := c.den.liveKey(channel)
	if !ok || k.Stage != denproto.StageRevealed || k.Sealed == nil {
		return "", nil, inputError(errors.New("compare check codes in this DM before sending in it"))
	}
	ch, _ := denproto.ParseID(channel)
	id, _ := denproto.ParseID(k.ID)
	me, _ := denproto.ParseID(c.meLocked())
	var key []byte
	err := c.j.seal.Use(func(seal []byte) error {
		var err error
		key, err = denproto.OpenDMKey(seal, c.j.denID, ch, id, me, k.Sealed)
		return err
	})
	if err != nil {
		return "", nil, fmt.Errorf("open the DM's key: %w", err)
	}
	return k.ID, key, nil
}

// sealMessage seals a DM message's text and files for the den, as its
// author wrote it at the given revision, with the DM's live key.
func (c *conn) sealMessage(channel, author string, nonce []byte, revision int, p denproto.DMPayload) (string, denproto.Bytes, error) {
	keyID, key, err := c.sendKey(channel)
	if err != nil {
		return "", nil, err
	}
	defer clear(key)
	ch, _ := denproto.ParseID(channel)
	id, _ := denproto.ParseID(keyID)
	who, _ := denproto.ParseID(author)
	sealed, err := denproto.SealDMMessage(key, denproto.MessageAD{DenID: c.j.denID, Channel: ch, Author: who, Nonce: nonce, KeyID: id, Revision: revision}, p)
	if err != nil {
		return "", nil, inputError(errors.New("this message is too long to send"))
	}
	return keyID, sealed, nil
}

// needsMe reports whether this member's Dens can move a DM key's exchange
// on: answer the other's offer, or reveal once they answered.
func needsMe(k denproto.DMKey, me string) bool {
	if k.Retired != "" {
		return false
	}
	return k.Stage == denproto.StageOffered && k.StartedBy != me || k.Stage == denproto.StageAnswered && k.StartedBy == me
}

// markDueLocked queues DMs for keyWork; the caller holds c.mu.
func (c *conn) markDueLocked(channels ...string) {
	for _, ch := range channels {
		c.due[ch] = true
	}
	if len(channels) > 0 {
		select {
		case c.keysDue <- struct{}{}:
		default:
		}
	}
}

// dueKeysLocked lists the DMs whose exchange waits on this member; the
// caller holds c.mu.
func (c *conn) dueKeysLocked() []string {
	var out []string
	me := c.meLocked()
	for _, k := range c.den.keys {
		if needsMe(k, me) {
			out = append(out, k.ChannelID)
		}
	}
	return out
}

// keyWork moves this member's side of DM exchanges on as soon as this
// device can. Every device of theirs that's online tries; the den takes
// the first, and the rest find the step done.
func (c *conn) keyWork(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.keysDue:
		}
		c.mu.Lock()
		due := c.due
		c.due = map[string]bool{}
		c.mu.Unlock()
		for channel := range due {
			if err := c.moveKey(ctx, channel); err != nil && ctx.Err() == nil {
				c.m.log.Warnf("move a DM key's exchange on: %v", err)
			}
		}
	}
}

// liveExchange fetches a DM's live key with its exchange, as this member
// sees it.
func (c *conn) liveExchange(ctx context.Context, channel string) (denproto.DMKey, bool, error) {
	var list denproto.DMKeyList
	if err := c.call(ctx, http.MethodGet, "/api/dms/"+channel+"/keys", nil, &list); err != nil {
		return denproto.DMKey{}, false, err
	}
	for _, k := range list.Keys {
		if denproto.CheckDMKey(k) != nil || k.ChannelID != channel {
			return denproto.DMKey{}, false, errMalformed
		}
		if k.Retired == "" {
			return k, true, nil
		}
	}
	return denproto.DMKey{}, false, nil
}

// exchangeContext names a DM's exchange, started by starter.
func (c *conn) exchangeContext(channel, starter string) ([]byte, error) {
	other, ok := c.isDM(channel)
	c.mu.Lock()
	me := c.meLocked()
	c.mu.Unlock()
	if !ok {
		return nil, ErrUnknownChannel
	}
	answerer := other
	if starter == other {
		answerer = me
	}
	ch, _ := denproto.ParseID(channel)
	s, _ := denproto.ParseID(starter)
	a, _ := denproto.ParseID(answerer)
	return denproto.DMExchangeContext(c.j.denID, ch, s, a), nil
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

// moveKey answers a DM key's offer from the other member, or reveals once
// they answered this member's. A device without the member's seal leaves
// it to their others.
func (c *conn) moveKey(ctx context.Context, channel string) error {
	c.mu.Lock()
	hasSeal := c.j.seal != nil
	c.mu.Unlock()
	if !hasSeal {
		return nil
	}
	k, ok, err := c.liveExchange(ctx, channel)
	if err != nil || !ok || k.Exchange == nil {
		return err
	}
	c.mu.Lock()
	me := c.meLocked()
	c.mu.Unlock()
	if !needsMe(k, me) {
		return nil
	}
	xctx, err := c.exchangeContext(channel, k.StartedBy)
	if err != nil {
		return err
	}
	ch, _ := denproto.ParseID(channel)
	mine, _ := denproto.ParseID(me)
	x := k.Exchange
	path := "/api/dms/" + channel + "/keys/" + k.ID
	if k.StartedBy != me {
		r, answer, err := denproto.AnswerExchange(xctx, x.Offer)
		if err != nil {
			return err
		}
		var state denproto.Bytes
		if err := c.useSeal(func(seal []byte) error {
			state, err = denproto.SealExchangeState(seal, c.j.denID, ch, mine, x.Offer.Commit, false, r)
			return err
		}); err != nil {
			return err
		}
		err = c.call(ctx, http.MethodPost, path+"/answer", denproto.KeyAnswerRequest{Answer: answer, State: state}, nil)
		if denproto.IsCode(err, denproto.CodeKeyExists) {
			return nil // another of this member's devices answered first
		}
		return err
	}
	var s denproto.Starter
	if err := c.openState(ch, mine, x, true, &s); err != nil {
		return err
	}
	if !denproto.Equal(s.Context, xctx) {
		return errors.New("a DM exchange's state is for another exchange")
	}
	if offer, err := s.Offer(); err != nil || !denproto.Equal(offer.Commit, x.Offer.Commit) || !denproto.Equal(offer.EK, x.Offer.EK) {
		return errors.New("a DM exchange's offer isn't the one this member made")
	}
	reveal, _, err := s.Finish(*x.Answer)
	if err != nil {
		return err
	}
	err = c.call(ctx, http.MethodPost, path+"/reveal", denproto.KeyRevealRequest{Reveal: reveal}, nil)
	if denproto.IsCode(err, denproto.CodeKeyExists) {
		return nil
	}
	return err
}

// openState opens this member's side of an exchange from the den.
func (c *conn) openState(channel, me int64, x *denproto.DMExchange, starter bool, state any) error {
	if x.State == nil {
		return errors.New("the den has no state of this member's for the exchange")
	}
	return c.useSeal(func(seal []byte) error {
		return denproto.OpenExchangeState(seal, c.j.denID, channel, me, x.Offer.Commit, starter, x.State, state)
	})
}

// StartDMKey starts a key for a DM, when it has none this member can use:
// the first, one after a member started over, or, with restart, another
// after a check that didn't match.
func (m *Manager) StartDMKey(ctx context.Context, denID, channelID string, restart bool) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if _, ok := c.isDM(channelID); !ok {
		return ErrUnknownChannel
	}
	c.mu.Lock()
	me := c.meLocked()
	c.mu.Unlock()
	xctx, err := c.exchangeContext(channelID, me)
	if err != nil {
		return err
	}
	s, offer, err := denproto.StartExchange(xctx)
	if err != nil {
		return err
	}
	ch, _ := denproto.ParseID(channelID)
	mine, _ := denproto.ParseID(me)
	var state denproto.Bytes
	if err := c.useSeal(func(seal []byte) error {
		state, err = denproto.SealExchangeState(seal, c.j.denID, ch, mine, offer.Commit, true, s)
		return err
	}); err != nil {
		return err
	}
	err = c.call(ctx, http.MethodPost, "/api/dms/"+channelID+"/keys", denproto.KeyStartRequest{Offer: offer, State: state, Restart: restart}, nil)
	if !restart && denproto.IsCode(err, denproto.CodeKeyExists) {
		return nil // one is under way already
	}
	if denproto.IsCode(err, denproto.CodeKeyExists) {
		return inputError(errors.New("you both compared this DM's code already; it doesn't need another check"))
	}
	return err
}

// DMCheck is where a DM's check stands, for the page. Half is the digits
// this member reads out, from when both sides can work them out until
// both have typed the other's: whoever checks first still has theirs to
// read out.
type DMCheck struct {
	KeyID   string `json:"key_id,omitempty"`
	Stage   string `json:"stage"` // none, or the live key's stage
	Starter bool   `json:"starter"`
	Half    string `json:"half,omitempty"`
	Checked bool   `json:"checked"` // this member typed the other's digits
	Partner bool   `json:"partner"` // the other member typed this member's
	Seal    bool   `json:"seal"`    // this device holds the member's seal
}

// keyResult works out what a DM's live exchange made: its key, and the
// check code, from this member's state and the exchange's messages.
func (c *conn) keyResult(ctx context.Context, channel string) (denproto.DMKey, denproto.Result, bool, error) {
	k, ok, err := c.liveExchange(ctx, channel)
	if err != nil {
		return k, denproto.Result{}, false, err
	}
	if !ok || k.Stage != denproto.StageRevealed || k.Exchange == nil || k.Exchange.Answer == nil || k.Exchange.Reveal == nil {
		return k, denproto.Result{}, false, inputError(errors.New("the DM's check isn't ready yet: both Dens need to be online once first"))
	}
	c.mu.Lock()
	me := c.meLocked()
	c.mu.Unlock()
	xctx, err := c.exchangeContext(channel, k.StartedBy)
	if err != nil {
		return k, denproto.Result{}, false, err
	}
	ch, _ := denproto.ParseID(channel)
	mine, _ := denproto.ParseID(me)
	x := k.Exchange
	starter := k.StartedBy == me
	var res denproto.Result
	if starter {
		var s denproto.Starter
		if err := c.openState(ch, mine, x, true, &s); err != nil {
			return k, res, starter, err
		}
		if offer, err := s.Offer(); err != nil || !denproto.Equal(s.Context, xctx) || !denproto.Equal(offer.Commit, x.Offer.Commit) {
			return k, res, starter, errors.New("a DM exchange's state is for another exchange")
		}
		_, res, err = s.Finish(*x.Answer)
	} else {
		var r denproto.Responder
		if err := c.openState(ch, mine, x, false, &r); err != nil {
			return k, res, starter, err
		}
		if !denproto.Equal(r.Context, xctx) || !denproto.Equal(r.Offer.Commit, x.Offer.Commit) {
			return k, res, starter, errors.New("a DM exchange's state is for another exchange")
		}
		res, err = r.Finish(*x.Reveal)
	}
	return k, res, starter, err
}

// CheckState says where a DM's check stands.
func (m *Manager) CheckState(ctx context.Context, denID, channelID string) (DMCheck, error) {
	c, err := m.find(denID)
	if err != nil {
		return DMCheck{}, err
	}
	other, ok := c.isDM(channelID)
	if !ok {
		return DMCheck{}, ErrUnknownChannel
	}
	c.mu.Lock()
	me := c.meLocked()
	st := DMCheck{Stage: "none", Seal: c.j.seal != nil}
	k, live := c.den.liveKey(channelID)
	c.mu.Unlock()
	if !live {
		return st, nil
	}
	st.KeyID, st.Stage, st.Starter = k.ID, k.Stage, k.StartedBy == me
	st.Checked, st.Partner = k.Checked(me), k.Checked(other)
	if k.Stage == denproto.StageRevealed && !(st.Checked && st.Partner) && st.Seal {
		_, res, starter, err := c.keyResult(ctx, channelID)
		if err != nil {
			return st, err
		}
		st.Half = denproto.FormatCheckHalf(denproto.CheckHalf(res.Check, starter))
		clear(res.Key)
	}
	return st, nil
}

// CheckDM takes the digits the member typed as the other member read
// them out. If they match, this member's copy of the DM's key goes to the
// den, sealed with their seal, and they can send and read with it.
func (m *Manager) CheckDM(ctx context.Context, denID, channelID, digits string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if _, ok := c.isDM(channelID); !ok {
		return ErrUnknownChannel
	}
	k, res, starter, err := c.keyResult(ctx, channelID)
	if err != nil {
		return err
	}
	defer clear(res.Key)
	if !denproto.CheckTyped(res.Check, starter, digits) {
		return errDigits
	}
	c.mu.Lock()
	me, _ := denproto.ParseID(c.meLocked())
	c.mu.Unlock()
	ch, _ := denproto.ParseID(channelID)
	id, _ := denproto.ParseID(k.ID)
	var sealed denproto.Bytes
	if err := c.useSeal(func(seal []byte) error {
		sealed, err = denproto.SealDMKey(seal, c.j.denID, ch, id, me, res.Key)
		return err
	}); err != nil {
		return err
	}
	return c.call(ctx, http.MethodPut, "/api/dms/"+channelID+"/keys/"+k.ID+"/sealed", denproto.KeySealRequest{Sealed: sealed}, nil)
}
