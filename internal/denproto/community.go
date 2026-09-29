package denproto

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// Limits for M1.3.
const (
	MaxBioRunes = 300
	MaxBioBytes = 4 * MaxBioRunes
	MaxFocus    = 10 // channels one connection can be viewing at once
)

// Events for members, presence and typing. Presence and typing are
// ephemeral: they carry no seq and are never replayed.
const (
	EventMemberUpdated = "member.updated"
	EventMemberLeft    = "member.left"
	EventPresence      = "presence"
	EventTyping        = "typing" // also a client frame
	EventFocus         = "focus"  // a client frame
)

// Reasons a den gives when it closes a member's sockets with CloseRevoked.
const (
	CloseReasonKeyRevoked      = "key revoked"
	CloseReasonPasswordChanged = "password changed" // the key was revoked by a new den password
	CloseReasonRemoved         = "removed"
	CloseReasonBanned          = "banned"
	CloseReasonLeft            = "left"
)

// DeleteWindows are the spans of a removed member's recent messages the den
// can delete along with them, in seconds.
var DeleteWindows = []int64{0, 3600, 86400, 7 * 86400}

type MemberLeft struct {
	ID     string `json:"id"`
	LeftAt int64  `json:"left_at"`
}

// Presence changes who is online. Full replaces the whole online set,
// otherwise it adds and removes members.
type Presence struct {
	Online  []string `json:"online,omitempty"`
	Offline []string `json:"offline,omitempty"`
	Full    bool     `json:"full,omitempty"`
}

// Typing says a member is typing in a channel. Clients send it without
// MemberID; the den fills it in.
type Typing struct {
	ChannelID string `json:"channel_id"`
	MemberID  string `json:"member_id,omitempty"`
}

// Focus names the channels a connection is showing, so typing reaches only
// the members who can see it happen.
type Focus struct {
	Channels []string `json:"channels"`
}

// ProfileRequest changes the member's own profile; nil fields stay as they
// are, and an empty bio clears it. Avatar and Banner name an upload to
// show, or "" for none.
type ProfileRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	Bio         *string `json:"bio,omitempty"`
	Avatar      *string `json:"avatar,omitempty"`
	Banner      *string `json:"banner,omitempty"`
}

type RoleRequest struct {
	Role string `json:"role"`
}

// RemoveRequest removes a member from the den. Ban keeps their username
// from coming back; DeleteMessages is how many seconds of their recent
// messages to delete (one of DeleteWindows); RevokeInvite revokes the
// invite they joined with, if it can still be used.
type RemoveRequest struct {
	Ban            bool  `json:"ban,omitempty"`
	DeleteMessages int64 `json:"delete_messages,omitempty"`
	RevokeInvite   bool  `json:"revoke_invite,omitempty"`
}

type Ban struct {
	Member   Member `json:"member"`
	BannedAt int64  `json:"banned_at"`
	BannedBy string `json:"banned_by,omitempty"`
}

type BanList struct {
	Bans []Ban `json:"bans"`
}

type DMRequest struct {
	MemberID string `json:"member_id"`
}

// CheckBio checks a profile bio: the markdown subset, possibly empty.
func CheckBio(text string) error {
	if !utf8.ValidString(text) {
		return errors.New("a bio must be valid UTF-8")
	}
	if len(text) > MaxBioBytes || utf8.RuneCountInString(text) > MaxBioRunes {
		return fmt.Errorf("a bio is at most %d characters", MaxBioRunes)
	}
	return nil
}

// Rank orders roles by what they may do to each other: a member may remove
// only members of a lower rank.
func Rank(role string) int {
	switch role {
	case RoleOwner:
		return 2
	case RoleModerator:
		return 1
	}
	return 0
}
