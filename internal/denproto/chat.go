package denproto

import (
	"errors"
	"fmt"
	"slices"
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
	MaxEditors     = 20  // members besides the author who may edit a message
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
// position, and names its two members, lower ID first. A voice channel has
// a bitrate, in bits a second, for its call's Opus (M4.2).
type Channel struct {
	ID          string   `json:"id"`
	GroupID     *string  `json:"group_id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Kind        string   `json:"kind"`
	Position    int      `json:"position"`
	StaffOnly   bool     `json:"staff_only"`
	Members     []string `json:"members,omitempty"`
	Bitrate     int      `json:"bitrate,omitempty"`
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
	// Attachments are the message's files, in the order they were sent.
	Attachments []File `json:"attachments,omitempty"`
	// Editors are the members besides the author who may edit the message
	// and tick its tasks.
	Editors []string `json:"editors,omitempty"`
	// Sealed is a DM message's text and files, sealed by its members'
	// clients with the DM key KeyID names (M1.7). Its Text is empty, its
	// files are listed only inside, and its Nonce comes everywhere,
	// history too, since opening it takes the nonce.
	Sealed Bytes  `json:"sealed,omitempty"`
	KeyID  string `json:"key_id,omitempty"`
}

// Reply previews the message another one replies to, so a reply reads
// without that message loaded. It's left out once that message is deleted.
// Its text is empty when that message has only files.
type Reply struct {
	AuthorID string `json:"author_id"`
	Text     string `json:"text"`
	// A reply in a DM carries the original sealed instead, for the client
	// to open and quote: its payload, key, nonce and revision.
	Sealed   Bytes  `json:"sealed,omitempty"`
	KeyID    string `json:"key_id,omitempty"`
	Nonce    Bytes  `json:"nonce,omitempty"`
	Revision int    `json:"revision,omitempty"`
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

// MessageDeleted names a deleted message, and the files that went with
// it, so clients drop them from their caches.
type MessageDeleted struct {
	ID        string   `json:"id"`
	ChannelID string   `json:"channel_id"`
	Files     []string `json:"files,omitempty"`
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
	// Attachments are uploads to send with the message.
	Attachments []string `json:"attachments,omitempty"`
	// Editors are members who may also edit the message.
	Editors []string `json:"editors,omitempty"`
	// Sealed and KeyID carry a DM message's text and files instead, with
	// Text empty; Attachments are then its sealed uploads.
	Sealed Bytes  `json:"sealed,omitempty"`
	KeyID  string `json:"key_id,omitempty"`
}

// EditRequest replaces a message's text, and its editors when set, which
// only the author changes. A DM message's edit carries it sealed instead,
// and since the den can't compare texts, Unedited marks one that only
// ticks a task or changes the editors, which doesn't mark it edited.
type EditRequest struct {
	Revision int       `json:"revision"`
	Text     string    `json:"text"`
	Editors  *[]string `json:"editors,omitempty"`
	Sealed   Bytes     `json:"sealed,omitempty"`
	KeyID    string    `json:"key_id,omitempty"`
	Unedited bool      `json:"unedited,omitempty"`
}

// TaskRequest checks or unchecks one of a message's tasks. Text is the
// task's text as the member saw it, so a tick doesn't land on another line
// when the message changed meanwhile.
type TaskRequest struct {
	Checked bool   `json:"checked"`
	Text    string `json:"text"`
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
	Bitrate     *int    `json:"bitrate,omitempty"` // a voice channel's (M4.2)
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
func CheckText(text string) error { return CheckMessageText(text, false) }

// CheckMessageText checks a message's text, which may be blank when the
// message has files.
func CheckMessageText(text string, files bool) error {
	if !utf8.ValidString(text) {
		return errors.New("text must be valid UTF-8")
	}
	if !files && strings.TrimSpace(text) == "" {
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
		// The text is empty when the original has only files.
		if (r.Text != "" && CheckText(r.Text) != nil) || utf8.RuneCountInString(r.Text) > ReplyExcerpt {
			return errors.New("message has an invalid reply")
		}
		if (r.Sealed != nil) != (m.Sealed != nil) || r.Sealed != nil && (r.Text != "" || checkSealed(r.Sealed, r.KeyID, r.Nonce, r.Revision) != nil) {
			return errors.New("message has an invalid reply")
		}
	}
	if m.Sealed != nil || m.KeyID != "" {
		if err := checkSealed(m.Sealed, m.KeyID, m.Nonce, m.Revision); err != nil || m.Text != "" || len(m.Attachments) > 0 {
			return errors.New("message has an invalid sealed text")
		}
		if len(m.Editors) > 1 {
			return errors.New("message has too many editors")
		}
	}
	if len(m.Editors) > MaxEditors {
		return errors.New("message has too many editors")
	}
	for i, id := range m.Editors {
		if _, err := ParseID(id); err != nil || id == m.AuthorID || slices.Contains(m.Editors[:i], id) {
			return errors.New("message has an invalid editor")
		}
	}
	if len(m.Attachments) > MaxAttachments {
		return errors.New("message has too many files")
	}
	seen := map[string]bool{}
	for _, f := range m.Attachments {
		if err := CheckFile(f); err != nil {
			return err
		}
		if seen[f.ID] {
			return errors.New("message has a file twice")
		}
		seen[f.ID] = true
	}
	if m.Sealed != nil {
		return nil
	}
	return CheckMessageText(m.Text, len(m.Attachments) > 0)
}

// checkSealed checks what a client needs to open a sealed DM message.
func checkSealed(sealed Bytes, keyID string, nonce Bytes, revision int) error {
	if len(sealed) == 0 || len(sealed) > MaxSealed || len(nonce) != NonceBytes || revision < 1 {
		return errors.New("invalid sealed message")
	}
	if _, err := ParseID(keyID); err != nil {
		return errors.New("invalid sealed message")
	}
	return nil
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
	return jsSpace(r) || strings.ContainsRune("<>\"'`", r)
}

func isNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// Task is a checkbox in a message: a line that starts with "[ ] " or
// "[x] ", outside a ``` block, as the page renders one. Text is the rest of
// the line. A message's tasks are numbered from 0 in order.
type Task struct {
	Checked bool   `json:"checked"`
	Text    string `json:"text"`
}

const (
	taskOpen = "[ ] "
	taskDone = "[x] "
)

// Tasks returns a text's tasks in order.
func Tasks(text string) []Task {
	var out []Task
	eachTask(text, func(_ int, t Task) bool {
		out = append(out, t)
		return true
	})
	return out
}

// SetTask returns text with task n checked or not, and false if it has no
// task n.
func SetTask(text string, n int, checked bool) (string, bool) {
	if n < 0 {
		return text, false
	}
	i := 0
	out, found := text, false
	eachTask(text, func(at int, _ Task) bool {
		if i < n {
			i++
			return true
		}
		mark := taskOpen
		if checked {
			mark = taskDone
		}
		out, found = text[:at]+mark+text[at+len(mark):], true
		return false
	})
	return out, found
}

// eachTask calls fn with each task and where its line starts, until fn
// returns false. Code blocks open and close as in Mentions.
func eachTask(text string, fn func(at int, t Task) bool) {
	inBlock := false
	at := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		body := strings.TrimSuffix(line, "\n")
		switch {
		case strings.HasPrefix(strings.TrimSpace(body), "```"):
			inBlock = !inBlock
		case inBlock:
		case strings.HasPrefix(body, taskOpen):
			if !fn(at, Task{Text: body[len(taskOpen):]}) {
				return
			}
		case strings.HasPrefix(body, taskDone):
			if !fn(at, Task{Checked: true, Text: body[len(taskDone):]}) {
				return
			}
		}
		at += len(line)
	}
}
