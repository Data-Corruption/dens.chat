package denproto

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// The audit log: staff's acts in the den, a line each, which moderators
// and the owner read newest first with GET /api/audit, a page at a time.

const (
	// AuditPage is the most lines a page of the log holds.
	AuditPage = 50
	// AuditKept is how long the den keeps a line.
	AuditKept = 90 * 24 * time.Hour
	// maxAuditChanges and maxAuditValueRunes bound what a line says
	// changed: the den's settings are the most one change makes, and a
	// den's address the longest value.
	maxAuditChanges    = 32
	maxAuditValueRunes = maxURLLength
)

// The kinds of line the den writes. A newer den may write others, which a
// client shows in words of its own.
const (
	AuditMemberRemoved         = "member.removed"
	AuditBanLifted             = "ban.lifted"
	AuditRoleChanged           = "role.changed"
	AuditInviteCreated         = "invite.created"
	AuditInviteRevoked         = "invite.revoked"
	AuditChannelCreated        = "channel.created"
	AuditChannelUpdated        = "channel.updated"
	AuditChannelDeleted        = "channel.deleted"
	AuditGroupCreated          = "group.created"
	AuditGroupUpdated          = "group.updated"
	AuditGroupDeleted          = "group.deleted"
	AuditMessageDeleted        = "message.deleted"
	AuditReactionsCleared      = "reactions.cleared"
	AuditCallDisconnected      = "call.disconnected"
	AuditCallMuted             = "call.muted"
	AuditCallUnmuted           = "call.unmuted"
	AuditCallMoved             = "call.moved"
	AuditRecordReset           = "record.reset"
	AuditEmojiAdded            = "emoji.added"
	AuditEmojiRenamed          = "emoji.renamed"
	AuditEmojiDeleted          = "emoji.deleted"
	AuditSoundAdded            = "sound.added"
	AuditSoundRenamed          = "sound.renamed"
	AuditSoundDeleted          = "sound.deleted"
	AuditEventUpdated          = "event.updated"
	AuditEventCanceled         = "event.canceled"
	AuditEventEnded            = "event.ended"
	AuditDenUpdated            = "den.updated"
	AuditRoleCreated           = "role.created"
	AuditRoleRenamed           = "role.renamed"
	AuditRoleDeleted           = "role.deleted"
	AuditMemberRolesChanged    = "member.roles"
	AuditCollectionCreated     = "collection.created"
	AuditCollectionUpdated     = "collection.updated"
	AuditCollectionDeleted     = "collection.deleted"
	AuditCollectionItemRemoved = "collection.item_removed"
)

// AuditKinds lists the kinds of line the den writes.
var AuditKinds = []string{
	AuditMemberRemoved, AuditBanLifted, AuditRoleChanged, AuditInviteCreated, AuditInviteRevoked,
	AuditChannelCreated, AuditChannelUpdated, AuditChannelDeleted, AuditGroupCreated, AuditGroupUpdated, AuditGroupDeleted,
	AuditMessageDeleted, AuditReactionsCleared, AuditCallDisconnected, AuditCallMuted, AuditCallUnmuted, AuditCallMoved,
	AuditRecordReset, AuditEmojiAdded, AuditEmojiRenamed, AuditEmojiDeleted, AuditSoundAdded, AuditSoundRenamed,
	AuditSoundDeleted, AuditEventUpdated, AuditEventCanceled, AuditEventEnded, AuditDenUpdated,
	AuditRoleCreated, AuditRoleRenamed, AuditRoleDeleted, AuditMemberRolesChanged,
	AuditCollectionCreated, AuditCollectionUpdated, AuditCollectionDeleted, AuditCollectionItemRemoved,
}

// AuditLine is one of staff's acts: when, what, who did it and on whom,
// and what the den kept of it. By is empty where the den's machine acted,
// as through its owner invite.
type AuditLine struct {
	ID       string `json:"id"`
	At       int64  `json:"at"`
	Kind     string `json:"kind"`
	By       string `json:"by,omitempty"`
	MemberID string `json:"member_id,omitempty"`
	AuditDetails
}

// AuditDetails is what a line keeps beyond who and when, which the den
// seals with its data key: the channel and its name, the ID of what else
// the act was on and its name, each as it was then, and what changed. A
// removal says whether it banned, how many seconds of the member's
// messages it deleted and whether it revoked their invite; Emoji is the
// Unicode emoji whose reactions came off, where a den emoji goes by its
// name; and Owner is who took the den over by its owner invite. No line
// holds a message's text.
type AuditDetails struct {
	ChannelID      string        `json:"channel_id,omitempty"`
	Channel        string        `json:"channel,omitempty"`
	Ref            string        `json:"ref,omitempty"`
	Name           string        `json:"name,omitempty"`
	Changes        []AuditChange `json:"changes,omitempty"`
	Ban            bool          `json:"ban,omitempty"`
	DeleteMessages int64         `json:"delete_messages,omitempty"`
	RevokeInvite   bool          `json:"revoke_invite,omitempty"`
	Emoji          string        `json:"emoji,omitempty"`
	Owner          string        `json:"owner,omitempty"`
}

// AuditChange is a setting an act changed, from one value to another,
// each as text; a description's change carries neither.
type AuditChange struct {
	Field string `json:"field"`
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
}

// AuditLog is a page of the log, and Next the cursor for the page after
// it, until the last.
type AuditLog struct {
	Lines []AuditLine `json:"lines"`
	Next  string      `json:"next,omitempty"`
}

// CheckAuditLine checks a line from the den: its IDs, a kind's shape, and
// names and values as cleaning leaves them.
func CheckAuditLine(l AuditLine) error {
	if _, err := ParseID(l.ID); err != nil {
		return errors.New("invalid line ID")
	}
	if l.At <= 0 {
		return errors.New("invalid time")
	}
	if !auditKind(l.Kind) {
		return errors.New("invalid kind")
	}
	for _, id := range []string{l.By, l.MemberID, l.ChannelID, l.Ref, l.Owner} {
		if _, err := ParseID(id); id != "" && err != nil {
			return errors.New("invalid ID")
		}
	}
	for _, name := range []string{l.Channel, l.Name} {
		if !cleanAs(name, MaxEventNameRunes) {
			return errors.New("invalid name")
		}
	}
	if len(l.Changes) > maxAuditChanges {
		return fmt.Errorf("more than %d changes", maxAuditChanges)
	}
	for _, c := range l.Changes {
		if !auditField(c.Field) || !cleanAs(c.From, maxAuditValueRunes) || !cleanAs(c.To, maxAuditValueRunes) {
			return errors.New("invalid change")
		}
	}
	if l.DeleteMessages < 0 {
		return errors.New("invalid window")
	}
	if l.Emoji != "" && !IsEmoji(l.Emoji) {
		return errors.New("not one of Unicode's emoji")
	}
	return nil
}

// cleanAs reports whether s is empty or a name as cleaning leaves it, up
// to max characters, each of Unicode's emoji whole, as an event's name
// keeps them.
func cleanAs(s string, max int) bool {
	if s == "" {
		return true
	}
	clean, err := cleanText(s, max, true, "name")
	return err == nil && clean == s
}

// auditKind reports whether s has a kind's shape: up to 32 of a-z and _,
// in dotted parts.
func auditKind(s string) bool {
	if len(s) == 0 || len(s) > 32 || s[0] == '.' || s[len(s)-1] == '.' {
		return false
	}
	for i := range len(s) {
		if c := s[i]; (c < 'a' || c > 'z') && c != '_' && (c != '.' || s[i-1] == '.') {
			return false
		}
	}
	return true
}

// auditField reports whether s has a setting's shape: up to 32 of a-z
// and _.
func auditField(s string) bool { return auditKind(s) && !strings.Contains(s, ".") }
