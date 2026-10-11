package denproto

import (
	"fmt"
	"strconv"
	"strings"
)

// What changed while away (M6): a den logs each change to a message, by ID
// only, and a client that caches what the den sends asks, after a ready,
// for the changes since the version it last synced at.

const (
	// SyncEpochSize is the length of a den's sync epoch, which a restore
	// from a backup replaces.
	SyncEpochSize = 16
	// ChangeLogDays is how long a den keeps a change, and so how long a
	// client can be away and still ask what changed.
	ChangeLogDays = 30
	// ChangesPage is the most versions one page of changes covers.
	ChangesPage = 1000
)

// CodeChangesGone refuses changes since a version the den no longer has
// them from: older than its floor, or newer than its own.
const CodeChangesGone = "changes_gone"

// Sync is where a den's changes stand, in ready. A client keeps what it
// cached only when it last synced in the same Epoch, at a version no older
// than Floor and no newer than Version. Expired is the last message
// retention deleted, once it has deleted any. MembersSince, from M6.2, is
// set when ready's members are only those changed since that version, the
// one the client named on the socket's upgrade.
type Sync struct {
	Epoch        Bytes  `json:"epoch"`
	Version      int64  `json:"version"`
	Floor        int64  `json:"floor"`
	Expired      string `json:"expired,omitempty"`
	MembersSince *int64 `json:"members_since,omitempty"`
}

// CheckSync checks a den's sync block.
func CheckSync(s Sync) error {
	if len(s.Epoch) != SyncEpochSize {
		return fmt.Errorf("sync epoch: want %d bytes", SyncEpochSize)
	}
	if s.Floor < 0 || s.Version < s.Floor {
		return fmt.Errorf("sync: a floor of %d past version %d", s.Floor, s.Version)
	}
	if s.Expired != "" {
		if _, err := ParseID(s.Expired); err != nil {
			return fmt.Errorf("sync expired: %w", err)
		}
	}
	if m := s.MembersSince; m != nil && (*m < s.Floor || *m > s.Version) {
		return fmt.Errorf("sync: members since %d, outside %d to %d", *m, s.Floor, s.Version)
	}
	return nil
}

// MembersCursor is the socket upgrade's members parameter (M6.2): the
// epoch and version a client's list of members is current as of, so the
// den's ready sends only the members changed since.
func MembersCursor(epoch Bytes, version int64) string {
	return epoch.String() + "." + strconv.FormatInt(version, 10)
}

// ParseMembersCursor reads a members parameter, and reports false for one
// that isn't.
func ParseMembersCursor(v string) (Bytes, int64, bool) {
	e, n, ok := strings.Cut(v, ".")
	if !ok {
		return nil, 0, false
	}
	epoch, err := ParseBytes(e, SyncEpochSize)
	if err != nil {
		return nil, 0, false
	}
	version, err := strconv.ParseInt(n, 10, 64)
	if err != nil || version < 0 {
		return nil, 0, false
	}
	return epoch, version, true
}

// Changes is a page of what changed since a version: the messages edited
// since, as they are now, and those deleted. More says the log goes on
// past Version, from which the client asks again.
type Changes struct {
	Version  int64            `json:"version"`
	Messages []Message        `json:"messages"`
	Deleted  []MessageDeleted `json:"deleted"`
	More     bool             `json:"more,omitempty"`
}
