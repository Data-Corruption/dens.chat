package denclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"

	"github.com/Data-Corruption/dens.chat/internal/cache"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Reactions (M15.3). A channel's pass through as the den sends them, each
// emoji with the members who reacted with it, and each change as one
// member's one emoji, which the cache and the page apply to a set. A DM's
// are each member's set, sealed here with the DM's key, padded to one
// length, and opened here for the page, keeping only Unicode's emoji and
// names the den's emoji may have; a change to one is made against its
// revision, as an edit is, and tried again on a conflict, so a member's
// two devices don't drop each other's reactions.

// PageReaction is a reaction the page adds, or with On false takes back:
// one of Unicode's emoji, one of the den's by ID, or in a DM by a name a
// set holds, which the den's emoji may no longer have.
type PageReaction struct {
	Emoji   string `json:"emoji,omitempty"`
	EmojiID string `json:"emoji_id,omitempty"`
	Name    string `json:"name,omitempty"`
	On      bool   `json:"on"`
}

// PageReactionSet is a member's set on a DM's message, opened for the
// page: Unicode's emoji, and the den's by name, in the order they came.
type PageReactionSet struct {
	MemberID string   `json:"member_id"`
	Revision int      `json:"revision"`
	Emoji    []string `json:"emoji"`
}

// PageDMReactions is a member's set on a DM's message as it changed, for
// the page.
type PageDMReactions struct {
	MessageID string `json:"message_id"`
	ChannelID string `json:"channel_id"`
	PageReactionSet
}

// openSetLocked opens a member's sealed set on a DM's message with the key
// it names, M1.7's or a signed one (M16.1), and reports false for one this
// device can't open. The caller holds c.mu.
func (c *conn) openSetLocked(channel, message string, r denproto.SealedReactions) (PageReactionSet, bool) {
	key, locked := c.dmKeyLocked(channel, r.KeyID)
	if locked != "" {
		return PageReactionSet{}, false
	}
	defer clear(key)
	ch, _ := denproto.ParseID(channel)
	mid, _ := denproto.ParseID(message)
	keyID, _ := denproto.ParseID(r.KeyID)
	who, _ := denproto.ParseID(r.MemberID)
	set, err := denproto.OpenReactions(key, denproto.ReactionsAD{DenID: c.j.denID, Channel: ch, Message: mid, Member: who,
		KeyID: keyID, Revision: r.Revision}, r.Sealed)
	if err != nil {
		return PageReactionSet{}, false
	}
	return PageReactionSet{MemberID: r.MemberID, Revision: r.Revision, Emoji: set.Emoji}, true
}

// openSetsLocked opens a DM message's sets for the page, leaving out those
// this device can't open. The caller holds c.mu.
func (c *conn) openSetsLocked(m denproto.Message) []PageReactionSet {
	var out []PageReactionSet
	for _, r := range m.DMReactions {
		if set, ok := c.openSetLocked(m.ChannelID, m.ID, r); ok {
			out = append(out, set)
		}
	}
	return out
}

// applyReaction applies a reaction's event to a message as the den sent
// it, and reports whether that changed it: a channel's reaction added,
// taken back or cleared, applied to its emoji's set, so applying one twice
// changes nothing; or a member's sealed set on a DM's message, where it's
// newer than the one the message holds. A change that would pass what a
// message holds is left for the den's next word on it.
func applyReaction(m *denproto.Message, e denproto.Event) bool {
	switch e.T {
	case denproto.EventReactionAdded, denproto.EventReactionRemoved:
		var ch denproto.ReactionChange
		if json.Unmarshal(e.D, &ch) != nil || ch.MessageID != m.ID {
			return false
		}
		return applyChange(m, ch.Emoji, ch.EmojiID, []string{ch.MemberID}, e.T == denproto.EventReactionAdded)
	case denproto.EventReactionCleared:
		var cl denproto.ReactionCleared
		if json.Unmarshal(e.D, &cl) != nil || cl.MessageID != m.ID {
			return false
		}
		return applyChange(m, cl.Emoji, cl.EmojiID, cl.Members, false)
	case denproto.EventDMReactions:
		var d denproto.DMReactions
		if json.Unmarshal(e.D, &d) != nil || d.MessageID != m.ID || m.Sealed == nil {
			return false
		}
		sets := cache.NewerSets(m.DMReactions, []denproto.SealedReactions{d.SealedReactions})
		if slices.EqualFunc(sets, m.DMReactions, func(a, b denproto.SealedReactions) bool {
			return a.MemberID == b.MemberID && a.Revision == b.Revision
		}) {
			return false
		}
		m.DMReactions = sets
		return true
	}
	return false
}

// applyChange adds members to an emoji's reactions on a channel's message,
// or takes them off.
func applyChange(m *denproto.Message, emoji, emojiID string, members []string, add bool) bool {
	i := slices.IndexFunc(m.Reactions, func(r denproto.Reaction) bool { return r.Emoji == emoji && r.EmojiID == emojiID })
	changed := false
	if !add {
		if i < 0 {
			return false
		}
		r := &m.Reactions[i]
		kept := slices.DeleteFunc(slices.Clone(r.Members), func(id string) bool { return slices.Contains(members, id) })
		if len(kept) == len(r.Members) {
			return false
		}
		if len(kept) == 0 {
			m.Reactions = slices.Delete(slices.Clone(m.Reactions), i, i+1)
		} else {
			m.Reactions = slices.Clone(m.Reactions)
			m.Reactions[i].Members = kept
		}
		return true
	}
	total := 0
	for _, r := range m.Reactions {
		total += len(r.Members)
	}
	if i < 0 {
		if len(m.Reactions) >= denproto.MaxReactionEmoji {
			return false
		}
		m.Reactions = append(slices.Clone(m.Reactions), denproto.Reaction{Emoji: emoji, EmojiID: emojiID})
		i = len(m.Reactions) - 1
	} else {
		m.Reactions = slices.Clone(m.Reactions)
	}
	r := &m.Reactions[i]
	r.Members = slices.Clone(r.Members)
	for _, id := range members {
		if !slices.Contains(r.Members, id) && total < denproto.MaxReactions {
			r.Members = append(r.Members, id)
			total++
			changed = true
		}
	}
	if len(r.Members) == 0 {
		m.Reactions = slices.Delete(m.Reactions, i, i+1)
	}
	return changed
}

// errReactionsMoved is a DM's set that changed meanwhile, three times
// over: someone, as another of the member's devices, keeps changing it.
var errReactionsMoved = inputError(errors.New("your reactions on that message keep changing elsewhere; try again"))

// React adds the member's reaction to a message, or takes it back. In a
// channel the den keeps it; in a DM this service seals the member's set
// again with the reaction in it or out, against the set's revision, and
// returns the set as it now is.
func (m *Manager) React(ctx context.Context, denID, channelID, messageID string, req PageReaction) (*PageReactionSet, error) {
	c, err := m.find(denID)
	if err != nil {
		return nil, err
	}
	if err := checkID("message", messageID); err != nil {
		return nil, err
	}
	if err := checkID("channel", channelID); err != nil {
		return nil, err
	}
	if _, dm := c.isDM(channelID); dm {
		return c.reactDM(ctx, channelID, messageID, req)
	}
	r := denproto.ReactionRequest{Emoji: req.Emoji, EmojiID: req.EmojiID}
	if req.Name != "" || r.Check() != nil {
		return nil, inputError(errors.New("a reaction is one of Unicode's emoji or one of the den's"))
	}
	path := "/api/messages/" + messageID + "/reactions"
	if !req.On {
		path += "/remove"
	}
	return nil, c.call(ctx, http.MethodPost, path, r, nil)
}

// ClearReaction takes one emoji's reactions off a channel's message, those
// of the members this one outranks: staff's to do.
func (m *Manager) ClearReaction(ctx context.Context, denID, messageID string, req denproto.ReactionRequest) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if err := checkID("message", messageID); err != nil {
		return err
	}
	if err := req.Check(); err != nil {
		return inputError(err)
	}
	return c.call(ctx, http.MethodPost, "/api/messages/"+messageID+"/reactions/clear", req, nil)
}

// reactDM changes this member's set on a DM's message: one of Unicode's
// emoji, or one of the den's by name, as a DM's text writes it. Another
// device's change meanwhile is a conflict, which it takes and tries again
// on.
func (c *conn) reactDM(ctx context.Context, channelID, messageID string, req PageReaction) (*PageReactionSet, error) {
	item, err := c.setItem(req)
	if err != nil {
		return nil, err
	}
	current, err := c.message(ctx, channelID, messageID)
	if err != nil {
		return nil, err
	}
	sets := current.DMReactions
	for range 3 {
		set, err := c.nextSet(ctx, channelID, messageID, sets, item, req.On)
		if err != nil {
			return nil, err
		}
		if !set.changed {
			return &PageReactionSet{MemberID: c.memberID(), Revision: set.req.Revision, Emoji: set.emoji}, nil
		}
		var held denproto.SealedReactions
		err = c.call(ctx, http.MethodPut, "/api/messages/"+messageID+"/reactions/sealed", set.req, &held)
		var perr *denproto.Error
		if errors.As(err, &perr) && perr.Code == denproto.CodeEditConflict {
			var body struct {
				Reactions *denproto.SealedReactions `json:"reactions"`
			}
			if json.Unmarshal(perr.Body, &body) != nil || body.Reactions != nil && (body.Reactions.Check() != nil || body.Reactions.MemberID != c.memberID()) {
				return nil, errMalformed
			}
			sets = slices.DeleteFunc(slices.Clone(sets), func(r denproto.SealedReactions) bool { return r.MemberID == c.memberID() })
			if body.Reactions != nil {
				sets = append(sets, *body.Reactions)
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		if held.Check() != nil || held.MemberID != c.memberID() || held.Revision != set.req.Revision+1 {
			return nil, errMalformed
		}
		opened := PageReactionSet{MemberID: held.MemberID, Revision: held.Revision, Emoji: set.emoji}
		return &opened, nil
	}
	return nil, errReactionsMoved
}

// setItem is what a DM's set holds for a reaction: one of Unicode's emoji,
// or one of the den's by its name, as the den has it now.
func (c *conn) setItem(req PageReaction) (string, error) {
	switch {
	case req.Emoji != "" && req.EmojiID == "" && req.Name == "":
		if denproto.IsEmoji(req.Emoji) {
			return req.Emoji, nil
		}
	case req.EmojiID != "" && req.Emoji == "" && req.Name == "":
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.den != nil {
			if e, ok := c.den.emoji[req.EmojiID]; ok {
				return e.Name, nil
			}
		}
		return "", inputError(errors.New("the den has no such emoji"))
	case req.Name != "" && req.Emoji == "" && req.EmojiID == "":
		if denproto.IsReactionName(req.Name) {
			return req.Name, nil
		}
	}
	return "", inputError(errors.New("a reaction is one of Unicode's emoji or one of the den's"))
}

// sealedSet is a member's set sealed for the den, and what it holds; or,
// not changed, the set they have, at the revision req names.
type sealedSet struct {
	req     denproto.DMReactionsRequest
	emoji   []string
	changed bool
}

// nextSet is the member's set with an item in it or out, sealed against
// the revision of the set they have in sets, or that set where it holds
// the item already, or doesn't. A message holds at most MaxReactionEmoji
// different emoji, both members' together.
func (c *conn) nextSet(ctx context.Context, channelID, messageID string, sets []denproto.SealedReactions, item string, on bool) (*sealedSet, error) {
	me := c.memberID()
	mine := []string{}
	revision := 0
	others := map[string]bool{}
	c.mu.Lock()
	if c.den == nil {
		c.mu.Unlock()
		return nil, ErrNotStarted
	}
	for _, r := range sets {
		set, ok := c.openSetLocked(channelID, messageID, r)
		if r.MemberID == me {
			revision = r.Revision
			if !ok {
				// A set of their own that doesn't open here, as one sealed
				// with a key this device lost, starts again from none.
				continue
			}
			mine = set.Emoji
		} else if ok {
			for _, e := range set.Emoji {
				others[e] = true
			}
		}
	}
	c.mu.Unlock()
	if slices.Contains(mine, item) == on {
		return &sealedSet{req: denproto.DMReactionsRequest{Revision: revision}, emoji: mine}, nil
	}
	next := slices.Clone(mine)
	if on {
		all := maps.Clone(others)
		for _, e := range mine {
			all[e] = true
		}
		if !all[item] && len(all) >= denproto.MaxReactionEmoji {
			return nil, inputError(fmt.Errorf("a message holds at most %d different emoji", denproto.MaxReactionEmoji))
		}
		next = append(next, item)
	} else {
		next = slices.DeleteFunc(next, func(e string) bool { return e == item })
	}
	keyID, key, err := c.sendKey(ctx, channelID)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	ch, _ := denproto.ParseID(channelID)
	mid, _ := denproto.ParseID(messageID)
	kid, _ := denproto.ParseID(keyID)
	who, _ := denproto.ParseID(me)
	sealed, err := denproto.SealReactions(key, denproto.ReactionsAD{DenID: c.j.denID, Channel: ch, Message: mid, Member: who, KeyID: kid,
		Revision: revision + 1}, denproto.ReactionSet{Emoji: next})
	if err != nil {
		return nil, inputError(err)
	}
	return &sealedSet{req: denproto.DMReactionsRequest{Revision: revision, KeyID: keyID, Sealed: sealed}, emoji: next, changed: true}, nil
}
