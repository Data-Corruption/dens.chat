package denproto

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits on what members write.
const (
	MaxTextRunes   = 4000
	MaxTextBytes   = 16 << 10
	MaxHistory     = 100
	DefaultHistory = 50
	NonceBytes     = 16 // message idempotency key
	MaxChannels    = 500
	MaxGroups      = 100
	ReplyExcerpt   = 100 // characters of a replied-to message a reply carries
)

// Channel kinds.
const (
	KindText  = "text"
	KindVoice = "voice"
	KindDM    = "dm" // a conversation between two members
)

// Event types for chat.
const (
	EventMemberJoined      = "member.joined"
	EventChannelCreated    = "channel.created"
	EventChannelUpdated    = "channel.updated"
	EventChannelDeleted    = "channel.deleted"
	EventChannelsReordered = "channels.reordered"
	EventGroupCreated      = "group.created"
	EventGroupUpdated      = "group.updated"
	EventGroupDeleted      = "group.deleted"
	EventGroupsReordered   = "groups.reordered"
	EventMessageCreated    = "message.created"
	EventMessageUpdated    = "message.updated"
	EventMessageDeleted    = "message.deleted"
	EventReadStateUpdated  = "read_state.updated"
)

// Channel is a text or voice channel, or a DM. A DM has no name, group or
// position, and names its two members, lower ID first.
type Channel struct {
	ID          string   `json:"id"`
	GroupID     *string  `json:"group_id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Kind        string   `json:"kind"`
	Position    int      `json:"position"`
	StaffOnly   bool     `json:"staff_only"`
	Members     []string `json:"members,omitempty"`
}

type Group struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

type Message struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	AuthorID  string `json:"author_id"`
	CreatedAt int64  `json:"created_at"`
	Revision  int    `json:"revision"`
	EditedAt  int64  `json:"edited_at,omitempty"`
	EditedBy  string `json:"edited_by,omitempty"`
	Text      string `json:"text"`
	ReplyTo   string `json:"reply_to,omitempty"`
	Reply     *Reply `json:"reply,omitempty"`
	Nonce     Bytes  `json:"nonce,omitempty"`
}

// Reply previews the message another one replies to, so a reply reads
// without that message loaded. It's left out once that message is deleted.
type Reply struct {
	AuthorID string `json:"author_id"`
	Text     string `json:"text"`
}

// Excerpt is the start of a text for a reply's preview: from the first
// character that isn't a space, at most ReplyExcerpt characters.
func Excerpt(text string) string {
	text = strings.TrimLeftFunc(text, unicode.IsSpace)
	n := 0
	for i := range text {
		if n == ReplyExcerpt {
			return text[:i]
		}
		n++
	}
	return text
}

// ReadState is one channel's unread state for a member. Closed marks a DM
// they closed, which stays out of their list until it has a new message.
type ReadState struct {
	ChannelID    string `json:"channel_id"`
	LastMessage  string `json:"last_message_id,omitempty"`
	ReadPosition string `json:"message_id,omitempty"`
	MentionCount int    `json:"mention_count"`
	Closed       bool   `json:"closed,omitempty"`
}

type MessageDeleted struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
}

type ChannelDeleted struct {
	ID string `json:"id"`
}

type ChannelsReordered struct {
	GroupID    *string  `json:"group_id"`
	ChannelIDs []string `json:"channel_ids"`
}

type GroupsReordered struct {
	GroupIDs []string `json:"group_ids"`
}

type History struct {
	Messages []Message `json:"messages"`
	HasOlder bool      `json:"has_older"`
	HasNewer bool      `json:"has_newer"`
}

type SendRequest struct {
	Nonce   Bytes  `json:"nonce"`
	Text    string `json:"text"`
	ReplyTo string `json:"reply_to,omitempty"`
}

type EditRequest struct {
	Revision int    `json:"revision"`
	Text     string `json:"text"`
}

type ReadRequest struct {
	MessageID string `json:"message_id"`
}

// ChannelRequest creates or changes a channel. On a change, nil fields stay
// as they are; GroupID "" moves the channel out of any group.
type ChannelRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Kind        string  `json:"kind,omitempty"`
	GroupID     *string `json:"group_id,omitempty"`
	Position    *int    `json:"position,omitempty"`
	StaffOnly   *bool   `json:"staff_only,omitempty"`
}

type GroupRequest struct {
	Name     *string `json:"name,omitempty"`
	Position *int    `json:"position,omitempty"`
}

// ParseID reads a den-issued ID: a positive decimal number.
func ParseID(s string) (int64, error) {
	if s == "" || len(s) > 19 || s[0] == '0' {
		return 0, errors.New("invalid ID")
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid ID")
	}
	return id, nil
}

// FormatID writes an ID.
func FormatID(id int64) string { return strconv.FormatInt(id, 10) }

// CheckText checks a message's text: valid UTF-8, not blank, and within
// the length limits. It is stored as sent.
func CheckText(text string) error {
	if !utf8.ValidString(text) {
		return errors.New("text must be valid UTF-8")
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("a message can't be empty")
	}
	if len(text) > MaxTextBytes || utf8.RuneCountInString(text) > MaxTextRunes {
		return fmt.Errorf("a message is at most %d characters", MaxTextRunes)
	}
	return nil
}

// CheckDescription checks a channel description, which may be empty.
func CheckDescription(text string) error {
	if text == "" {
		return nil
	}
	if !utf8.ValidString(text) || len(text) > MaxTextBytes || utf8.RuneCountInString(text) > MaxTextRunes {
		return fmt.Errorf("a description is at most %d characters of valid UTF-8", MaxTextRunes)
	}
	return nil
}

// CheckMessage checks a message from a den before a client keeps it.
func CheckMessage(m Message) error {
	for _, id := range []string{m.ID, m.ChannelID, m.AuthorID} {
		if _, err := ParseID(id); err != nil {
			return errors.New("message has an invalid ID")
		}
	}
	for _, id := range []string{m.EditedBy, m.ReplyTo} {
		if id != "" {
			if _, err := ParseID(id); err != nil {
				return errors.New("message has an invalid ID")
			}
		}
	}
	if m.Revision < 1 || len(m.Nonce) > NonceBytes {
		return errors.New("message has an invalid revision or nonce")
	}
	if r := m.Reply; r != nil {
		if _, err := ParseID(r.AuthorID); err != nil || m.ReplyTo == "" {
			return errors.New("message has an invalid reply")
		}
		if CheckText(r.Text) != nil || utf8.RuneCountInString(r.Text) > ReplyExcerpt {
			return errors.New("message has an invalid reply")
		}
	}
	return CheckText(m.Text)
}

// Mentions returns the usernames a text mentions, lowercased and without
// repeats. The page's renderer highlights exactly these: "@" at the start
// of a line or after a character that is neither part of a name nor "@",
// then 2 to 32 letters, digits or _, then anything else. Mentions in code
// (a ``` block, or a span from a backtick to the next with something in
// between) and in links don't count.
func Mentions(text string) []string {
	var out []string
	seen := map[string]bool{}
	inBlock := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inBlock = !inBlock
			continue
		}
		if inBlock {
			continue
		}
		for i := 0; i < len(line); i++ {
			// Formatting marks like * and ~ aren't name characters, so
			// "**@alice**" mentions alice, as the renderer shows it.
			free := i == 0 || !isNameByte(line[i-1])
			switch c := line[i]; {
			case c == '`':
				if end := strings.IndexByte(line[i+1:], '`'); end > 0 {
					i += end + 1
				}
			case c == 'h' && free:
				if n := linkLength(line[i:]); n > 0 {
					i += n - 1
				}
			case c == '@' && free && (i == 0 || line[i-1] != '@'):
				j := i + 1
				for j < len(line) && isNameByte(line[j]) {
					j++
				}
				if n := j - i - 1; n >= 2 && n <= 32 {
					name := strings.ToLower(line[i+1 : j])
					if !seen[name] {
						seen[name] = true
						out = append(out, name)
					}
				}
				i = j - 1
			}
		}
	}
	return out
}

// linkLength returns the length of the link s starts with, as the page's
// renderer finds links: http:// or https://, then everything up to a space
// or one of <>"'`. It returns 0 if s doesn't start with one.
func linkLength(s string) int {
	var rest string
	switch {
	case strings.HasPrefix(s, "https://"):
		rest = s[len("https://"):]
	case strings.HasPrefix(s, "http://"):
		rest = s[len("http://"):]
	default:
		return 0
	}
	n := strings.IndexFunc(rest, endsLink)
	if n < 0 {
		n = len(rest)
	}
	if n == 0 {
		return 0
	}
	return len(s) - len(rest) + n
}

// endsLink reports whether r ends a link: a space as JavaScript's \s
// matches one, or one of <>"'`.
func endsLink(r rune) bool {
	switch r {
	case '<', '>', '"', '\'', '`', '\t', '\n', '\v', '\f', '\r', ' ',
		0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func isNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}
