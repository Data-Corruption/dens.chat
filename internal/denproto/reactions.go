package denproto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"golang.org/x/crypto/chacha20poly1305"
)

// Reactions (M15.3): a member's emoji on a message, one of Unicode's, from
// the list the page's picker shows (IsEmoji), or one of the den's. In a
// channel the den keeps each reaction, checks it against the list or its
// own emoji, by ID, so a reaction carries no text, and sends each change
// as one member's one emoji, which applies to a set, so applying it twice
// changes nothing. In a DM the den can't read them, and reactions in the
// clear would tell it how one member answered the other: each member's
// reactions on a message are one set, which their client seals with the
// DM key, padded to one length, and the den keeps whole against its
// revision, as an edit is kept. A set names the den's emoji by name, as a
// DM's text writes them.

// Events for reactions.
const (
	EventReactionAdded   = "reaction.added"
	EventReactionRemoved = "reaction.removed"
	EventReactionCleared = "reaction.cleared"
	EventDMReactions     = "dm.reactions"
)

const (
	// MaxReactionEmoji is how many different emoji a message holds.
	MaxReactionEmoji = 20
	// MaxReactions is how many reactions a channel's message holds in all,
	// so it stays well within a frame, and a history page holds a few of
	// the most reacted to: at a member ID's longest, about 200 KiB.
	MaxReactions = 10_000
	// reactionsPlain is a DM's set before it's sealed, padded to one
	// length, so its size doesn't say how many it holds: room for
	// MaxReactionEmoji of the longest emoji, 37 bytes as JSON, or names,
	// 34, with headroom for emoji to come.
	reactionsPlain = 1024
	// SealedReactionsSize is a sealed set's length: its nonce, the padded
	// set and its tag.
	SealedReactionsSize = chacha20poly1305.NonceSizeX + reactionsPlain + chacha20poly1305.Overhead
)

const dmReactionsContext = "dens-dm-reactions-v1"

// Reaction is one emoji's reactions on a channel's message: the emoji, one
// of Unicode's, or one of the den's by its ID, and the members who reacted
// with it, in the order they did.
type Reaction struct {
	Emoji   string   `json:"emoji,omitempty"`
	EmojiID string   `json:"emoji_id,omitempty"`
	Members []string `json:"members"`
}

// ReactionRequest names the emoji a member reacts with or takes back, or
// whose reactions staff take off a message: one of Unicode's, or one of
// the den's by its ID.
type ReactionRequest struct {
	Emoji   string `json:"emoji,omitempty"`
	EmojiID string `json:"emoji_id,omitempty"`
}

// Check checks that a request names one emoji: one of Unicode's from the
// list, or an ID.
func (r ReactionRequest) Check() error {
	switch {
	case r.Emoji != "" && r.EmojiID != "":
		return errors.New("a reaction is one emoji, Unicode's or the den's")
	case r.Emoji != "":
		if !IsEmoji(r.Emoji) {
			return errors.New("not one of Unicode's emoji")
		}
	case r.EmojiID != "":
		if _, err := ParseID(r.EmojiID); err != nil {
			return errors.New("invalid emoji ID")
		}
	default:
		return errors.New("a reaction names an emoji")
	}
	return nil
}

// ReactionChange is one member's reaction added to a channel's message, or
// taken back.
type ReactionChange struct {
	MessageID string `json:"message_id"`
	ChannelID string `json:"channel_id"`
	MemberID  string `json:"member_id"`
	Emoji     string `json:"emoji,omitempty"`
	EmojiID   string `json:"emoji_id,omitempty"`
}

// Check checks a change as a den sends it.
func (c ReactionChange) Check() error {
	for _, id := range []string{c.MessageID, c.ChannelID, c.MemberID} {
		if _, err := ParseID(id); err != nil {
			return errors.New("a reaction names an invalid ID")
		}
	}
	return ReactionRequest{Emoji: c.Emoji, EmojiID: c.EmojiID}.Check()
}

// ReactionCleared names the members whose reactions with one emoji staff
// took off a channel's message: those of a lower rank than theirs.
type ReactionCleared struct {
	MessageID string   `json:"message_id"`
	ChannelID string   `json:"channel_id"`
	Emoji     string   `json:"emoji,omitempty"`
	EmojiID   string   `json:"emoji_id,omitempty"`
	Members   []string `json:"members"`
}

// Check checks a clearing as a den sends it.
func (c ReactionCleared) Check() error {
	for _, id := range []string{c.MessageID, c.ChannelID} {
		if _, err := ParseID(id); err != nil {
			return errors.New("a reaction names an invalid ID")
		}
	}
	if len(c.Members) == 0 || len(c.Members) > MaxReactions {
		return errors.New("a reaction's members are out of range")
	}
	for i, id := range c.Members {
		if _, err := ParseID(id); err != nil || slices.Contains(c.Members[:i], id) {
			return errors.New("a reaction names an invalid member")
		}
	}
	return ReactionRequest{Emoji: c.Emoji, EmojiID: c.EmojiID}.Check()
}

// SealedReactions is one member's reactions on a DM's message: a set their
// client sealed with the DM key KeyID names, as their set's Revision.
type SealedReactions struct {
	MemberID string `json:"member_id"`
	KeyID    string `json:"key_id"`
	Revision int    `json:"revision"`
	Sealed   Bytes  `json:"sealed"`
}

// Check checks a sealed set as a den sends it: what opening it takes.
func (r SealedReactions) Check() error {
	for _, id := range []string{r.MemberID, r.KeyID} {
		if _, err := ParseID(id); err != nil {
			return errors.New("a sealed set names an invalid ID")
		}
	}
	if r.Revision < 1 || len(r.Sealed) != SealedReactionsSize {
		return errors.New("a sealed set has an invalid revision or size")
	}
	return nil
}

// DMReactions is a member's set on a DM's message, as it changed.
type DMReactions struct {
	MessageID string `json:"message_id"`
	ChannelID string `json:"channel_id"`
	SealedReactions
}

// DMReactionsRequest sets the member's reactions on a DM's message: their
// set, sealed with the DM's live key as the revision after Revision, the
// one it was made against, 0 while they have none.
type DMReactionsRequest struct {
	Revision int    `json:"revision"`
	KeyID    string `json:"key_id"`
	Sealed   Bytes  `json:"sealed"`
}

// ReactionSet is what a member's sealed set on a DM's message holds:
// Unicode's emoji, and the den's by name, in the order the member added
// them.
type ReactionSet struct {
	Emoji []string `json:"emoji"`
}

// ReactionsAD binds a DM's sealed set to the den, the DM, the message, its
// member, the key and the set's revision, so the den can't move it to
// another message, member or DM, or pass an old set off as the newest.
type ReactionsAD struct {
	DenID    []byte
	Channel  int64
	Message  int64
	Member   int64
	KeyID    int64
	Revision int
}

func (a ReactionsAD) bytes() []byte {
	return join([]byte(dmReactionsContext), a.DenID, u64(a.Channel), u64(a.Message), u64(a.Member), u64(a.KeyID), u64(int64(a.Revision)))
}

// IsReactionName reports whether s is a name one of the den's emoji may
// have, as a DM's set writes it.
func IsReactionName(s string) bool {
	name, err := NormalizeEmojiName(s)
	return err == nil && name == s
}

// SealReactions seals a member's set on a DM's message with the DM key,
// padded to one length. Each must be one of Unicode's emoji or a name the
// den's emoji may have, once, and at most MaxReactionEmoji.
func SealReactions(key []byte, ad ReactionsAD, set ReactionSet) (Bytes, error) {
	plain, err := padSet(set, reactionsPlain)
	if err != nil {
		return nil, err
	}
	defer clear(plain)
	return sealWith(key, plain, ad.bytes())
}

// padSet checks a set and writes it as JSON, padded to size with spaces,
// which JSON reads past, so the padding needs no length.
func padSet(set ReactionSet, size int) ([]byte, error) {
	if len(set.Emoji) > MaxReactionEmoji {
		return nil, fmt.Errorf("a message holds at most %d different emoji", MaxReactionEmoji)
	}
	for i, e := range set.Emoji {
		if !IsEmoji(e) && !IsReactionName(e) || slices.Contains(set.Emoji[:i], e) {
			return nil, errors.New("a reaction is one of Unicode's emoji or the den's, once")
		}
	}
	if set.Emoji == nil {
		set.Emoji = []string{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(set); err != nil {
		return nil, err
	}
	if buf.Len() > size {
		return nil, errors.New("a set of reactions is too long")
	}
	return append(buf.Bytes(), bytes.Repeat([]byte{' '}, size-buf.Len())...), nil
}

// OpenReactions opens a member's set on a DM's message. The other member's
// client wrote it, so it keeps only Unicode's emoji from the list and
// names the den's emoji may have, each once, at most MaxReactionEmoji, and
// drops anything else.
func OpenReactions(key []byte, ad ReactionsAD, sealed []byte) (ReactionSet, error) {
	if len(sealed) != SealedReactionsSize {
		return ReactionSet{}, ErrSealedOpen
	}
	plain, err := openWith(key, sealed, ad.bytes())
	if err != nil {
		return ReactionSet{}, ErrSealedOpen
	}
	defer clear(plain)
	return readSet(plain)
}

// readSet reads a padded set, keeping only what a set may hold.
func readSet(plain []byte) (ReactionSet, error) {
	var set ReactionSet
	if err := json.Unmarshal(plain, &set); err != nil {
		return ReactionSet{}, ErrSealedOpen
	}
	out := ReactionSet{Emoji: []string{}}
	for _, e := range set.Emoji {
		if len(out.Emoji) == MaxReactionEmoji {
			break
		}
		if (IsEmoji(e) || IsReactionName(e)) && !slices.Contains(out.Emoji, e) {
			out.Emoji = append(out.Emoji, e)
		}
	}
	return out, nil
}

// checkReactions checks a message's reactions as a den sends them: a
// channel's, each emoji once, at most MaxReactionEmoji and MaxReactions in
// all, each member once an emoji; and a DM's or a group's sealed sets, one
// a member, at most MaxReactionSets (M16.3).
func checkReactions(m Message) error {
	if m.Sealed != nil {
		if len(m.Reactions) > 0 || len(m.DMReactions) > MaxReactionSets {
			return errors.New("a DM's message carries its reactions sealed, a set a member")
		}
		for i, r := range m.DMReactions {
			if err := r.Check(); err != nil {
				return err
			}
			for _, o := range m.DMReactions[:i] {
				if o.MemberID == r.MemberID {
					return errors.New("a DM's message carries a member's set twice")
				}
			}
		}
		return nil
	}
	if len(m.DMReactions) > 0 {
		return errors.New("only a DM's message carries sealed reactions")
	}
	if len(m.Reactions) > MaxReactionEmoji {
		return errors.New("a message has too many different reactions")
	}
	total := 0
	for i, r := range m.Reactions {
		if err := (ReactionRequest{Emoji: r.Emoji, EmojiID: r.EmojiID}).Check(); err != nil {
			return fmt.Errorf("a message's reaction: %w", err)
		}
		for _, o := range m.Reactions[:i] {
			if o.Emoji == r.Emoji && o.EmojiID == r.EmojiID {
				return errors.New("a message has a reaction twice")
			}
		}
		if total += len(r.Members); len(r.Members) == 0 || total > MaxReactions {
			return errors.New("a message's reactions are out of range")
		}
		for j, id := range r.Members {
			if _, err := ParseID(id); err != nil || slices.Contains(r.Members[:j], id) {
				return errors.New("a message's reaction names an invalid member")
			}
		}
	}
	return nil
}
