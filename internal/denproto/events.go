package denproto

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

// Events (M17.4): a den's events, each a name, a description in the
// markdown subset, an optional banner cut three times as wide as it's
// high, a start, an end, and a place: one of the den's voice channels, or
// a line of text. Staff make them, and members too as far as the owner's
// limit allows; members mark the ones they're interested in, which the
// den keeps only to tell them as each starts, showing everyone how many
// and nobody who. An event in a staff-only channel is staff's alone, and
// follows its channel.
//
// An event is to come until its start, when the den's timer makes it
// live and tells the interested, then live until its end, which its maker
// or staff bring forward or put back, and then ended, which takes it off
// the list and forgets who was interested. One to come can be canceled,
// which deletes it; one live can only be ended.
//
// From M17.5 an event's link shows as a card where it's posted, which asks
// the den for an event not among those to come, an ended one included; and
// staff attach a badge to an event in a voice channel, a picture of an
// emoji's size and a title, which a member earns by a minute at a stretch
// in its call while it's live. A member's profile carries the badges they
// earned, as it carries their bio, only those whose events the viewer
// sees.

// Events for the den's events: one made, changed, canceled, started and
// ended, to everyone who sees it, and how many are interested, to them
// too, with the member's own mark to their own sessions. As an event
// starts the den sends event.started to its interested members alone,
// numbered as every durable event is, so a socket that resumes soon after
// still hears it, and event.updated, live, to the rest.
const (
	EventEventCreated  = "event.created"
	EventEventUpdated  = "event.updated"
	EventEventCanceled = "event.canceled"
	EventEventStarted  = "event.started"
	EventEventEnded    = "event.ended"
	EventEventInterest = "event.interest"
)

const (
	// An event's name is 1 to MaxEventNameRunes characters, its place's
	// line of text as many, and its description at most
	// MaxEventDescriptionRunes, and maxEventDescriptionBytes, in the
	// markdown subset.
	MaxEventNameRunes        = 100
	MaxEventPlaceRunes       = 100
	MaxEventDescriptionRunes = 2000
	maxEventDescriptionBytes = 8 << 10
	// maxEventLineBytes bounds a name or a place before it's cleaned, so
	// cleaning a hostile one costs little.
	maxEventLineBytes = 1 << 10

	// An event lasts MinEventLength to MaxEventLength, and starts at most
	// MaxEventAhead from now. A start up to EventStartSlack past counts as
	// now, so a member picking this minute in a form of whole minutes
	// makes one that starts at once.
	MinEventLength  = 5 * time.Minute
	MaxEventLength  = 7 * 24 * time.Hour
	MaxEventAhead   = 365 * 24 * time.Hour
	EventStartSlack = time.Minute

	// A den holds MaxEvents that haven't ended, to come and live, and a
	// member who isn't staff as many of their own as the owner allows, up
	// to MaxMemberEvents.
	MaxEvents       = 50
	MaxMemberEvents = 10

	// A badge's title is 1 to MaxBadgeTitleRunes characters, cleaned as an
	// event's name is (M17.5).
	MaxBadgeTitleRunes = 40
	// BadgeStretch is how long a member stays in a live event's call, at a
	// stretch, to earn its badge.
	BadgeStretch = time.Minute
	// MaxProfileBadges is how many badges a profile carries at most, the
	// latest a member earned.
	MaxProfileBadges = 500
)

// EventLimits are a den's limits for its events, which its owner sets: how
// many events that haven't ended each member who isn't staff may have, 0
// leaving events to staff. A den sends them in its description, which
// says it has events; a den older than events sends none.
type EventLimits struct {
	MemberEvents int `json:"member_events"`
}

// DefaultEventLimits are a new den's, and an older one's as it migrates:
// events are staff's until the owner says otherwise.
var DefaultEventLimits = EventLimits{}

// CheckEventLimits checks limits an owner sets, or a den sends.
func CheckEventLimits(l EventLimits) error {
	if l.MemberEvents < 0 || l.MemberEvents > MaxMemberEvents {
		return fmt.Errorf("a member may have 0 to %d events", MaxMemberEvents)
	}
	return nil
}

// DenEvent is one of a den's events as a member sees it: its name,
// description and banner; its start and end, as Unix milliseconds; its
// place, a voice channel's ID or a line of text; who made it; whether it's
// live; and how many members are interested. Mine is whether the member is
// one of them, which only ready and the events to their own sessions
// carry: elsewhere it's absent, and the member's mark stays as it was.
// Badge is its badge, if staff attached one (M17.5). Ended says it ended,
// which only an event the den sends by its ID can, for a link's card; an
// ended event has nobody interested any more.
type DenEvent struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Banner      Image       `json:"banner,omitzero"`
	StartsAt    int64       `json:"starts_at"`
	EndsAt      int64       `json:"ends_at"`
	ChannelID   string      `json:"channel_id,omitempty"`
	Place       string      `json:"place,omitempty"`
	MadeBy      string      `json:"made_by"`
	Live        bool        `json:"live,omitempty"`
	Interested  int         `json:"interested"`
	Mine        *bool       `json:"mine,omitempty"`
	Badge       *EventBadge `json:"badge,omitempty"`
	Ended       bool        `json:"ended,omitempty"`
}

// EventBadge is an event's badge (M17.5): its title, and its picture, of
// an emoji's size, which every member who sees the event opens.
type EventBadge struct {
	Title string `json:"title"`
	Image Image  `json:"image"`
}

// EventRequest makes an event, or changes one: each field it carries, the
// rest staying as they are. Banner names an upload of the member's,
// waiting to be used, cut three times as wide as it's high, or "" to take
// the banner off. A place is a voice channel, or a line of text: giving
// one takes the other off. Making one takes a name, a start, an end and a
// place. A live event's start stays where it was.
//
// Badge and BadgeTitle, which staff alone give, attach a badge to an event
// in a voice channel or change it (M17.5): Badge names an upload of the
// staff member's, waiting to be used, as its picture, checked as an
// emoji's is, or "" to take the badge off; BadgeTitle is its title.
// Attaching one takes both.
type EventRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Banner      *string `json:"banner,omitempty"`
	StartsAt    *int64  `json:"starts_at,omitempty"`
	EndsAt      *int64  `json:"ends_at,omitempty"`
	ChannelID   *string `json:"channel_id,omitempty"`
	Place       *string `json:"place,omitempty"`
	Badge       *string `json:"badge,omitempty"`
	BadgeTitle  *string `json:"badge_title,omitempty"`
}

// MemberBadge is a badge a member earned, as their profile carries it
// (M17.5): its event's ID, its title and picture, its event's name and
// start, which say what it was for once the event left the list, and when
// the member earned it.
type MemberBadge struct {
	EventID  string `json:"event_id"`
	Title    string `json:"title"`
	Image    Image  `json:"image"`
	Event    string `json:"event"`
	StartsAt int64  `json:"starts_at"`
	EarnedAt int64  `json:"earned_at"`
}

// EventID names an event the den canceled or ended.
type EventID struct {
	ID string `json:"id"`
}

// EventInterest is how many members are interested in an event, as it
// changes, and to the member's own sessions, and in answer to them,
// whether they are.
type EventInterest struct {
	EventID    string `json:"event_id"`
	Interested int    `json:"interested"`
	Mine       *bool  `json:"mine,omitempty"`
}

// CleanEventName cleans an event's name as a poll's choice is cleaned,
// keeping emoji whole.
func CleanEventName(s string) (string, error) {
	if len(s) > maxEventLineBytes {
		return "", fmt.Errorf("a name is at most %d characters", MaxEventNameRunes)
	}
	return cleanText(s, MaxEventNameRunes, true, "name")
}

// CleanEventPlace cleans an event's place given as a line of text, as a
// name is.
func CleanEventPlace(s string) (string, error) {
	if len(s) > maxEventLineBytes {
		return "", fmt.Errorf("a place is at most %d characters", MaxEventPlaceRunes)
	}
	return cleanText(s, MaxEventPlaceRunes, true, "place")
}

// CheckEventDescription checks an event's description, which may be empty.
func CheckEventDescription(s string) error {
	if !utf8.ValidString(s) || len(s) > maxEventDescriptionBytes || utf8.RuneCountInString(s) > MaxEventDescriptionRunes {
		return fmt.Errorf("a description is at most %d characters of valid UTF-8", MaxEventDescriptionRunes)
	}
	return nil
}

// CleanBadgeTitle cleans a badge's title as an event's name is cleaned.
func CleanBadgeTitle(s string) (string, error) {
	if len(s) > maxEventLineBytes {
		return "", fmt.Errorf("a title is at most %d characters", MaxBadgeTitleRunes)
	}
	return cleanText(s, MaxBadgeTitleRunes, true, "title")
}

// checkBadge checks a badge's title, as cleaning leaves it, and its
// picture, as a den sends them: an image of an emoji's size.
func checkBadge(title string, image Image) error {
	if t, err := CleanBadgeTitle(title); err != nil || t != title {
		return errors.New("a badge has an invalid title")
	}
	if _, err := ParseID(image.ID); err != nil || image.Width < 1 || image.Height < 1 ||
		image.Width > MaxEmojiSide || image.Height > MaxEmojiSide {
		return errors.New("a badge has an invalid picture")
	}
	return nil
}

// CheckMemberBadge checks a badge on a member's profile as a den sends it:
// its event's ID, title and picture, the event's name as cleaning leaves
// it, and its times.
func CheckMemberBadge(b MemberBadge) error {
	if _, err := ParseID(b.EventID); err != nil {
		return errors.New("a badge names an invalid event")
	}
	if err := checkBadge(b.Title, b.Image); err != nil {
		return err
	}
	if name, err := CleanEventName(b.Event); err != nil || name != b.Event {
		return errors.New("a badge names its event invalidly")
	}
	if b.StartsAt <= 0 || b.EarnedAt <= 0 {
		return errors.New("a badge's times are out of range")
	}
	return nil
}

// CheckDenEvent checks an event as a den sends it: its name and place as
// cleaning leaves them, one place, an end after its start and no further
// than the longest an event lasts, its banner's size, how many are
// interested, and its badge, if it has one, which goes with a voice
// channel. An ended one isn't live and has nobody interested; it may have
// ended as it started, and its voice channel may be gone, leaving it no
// place, a badge and all.
func CheckDenEvent(e DenEvent) error {
	if _, err := ParseID(e.ID); err != nil {
		return errors.New("an event has an invalid ID")
	}
	if _, err := ParseID(e.MadeBy); err != nil {
		return errors.New("an event names an invalid maker")
	}
	if name, err := CleanEventName(e.Name); err != nil || name != e.Name {
		return errors.New("an event has an invalid name")
	}
	if CheckEventDescription(e.Description) != nil {
		return errors.New("an event has an invalid description")
	}
	switch {
	case e.ChannelID != "" && e.Place != "", e.ChannelID == "" && e.Place == "" && !e.Ended:
		return errors.New("an event has one place")
	case e.ChannelID != "":
		if _, err := ParseID(e.ChannelID); err != nil {
			return errors.New("an event names an invalid channel")
		}
	case e.Place != "":
		if place, err := CleanEventPlace(e.Place); err != nil || place != e.Place {
			return errors.New("an event has an invalid place")
		}
	}
	if e.StartsAt <= 0 || e.EndsAt < e.StartsAt || e.EndsAt == e.StartsAt && !e.Ended || e.EndsAt-e.StartsAt > MaxEventLength.Milliseconds() {
		return errors.New("an event's times are out of range")
	}
	if CheckImage(e.Banner) != nil {
		return errors.New("an event has an invalid banner")
	}
	if e.Interested < 0 || e.Ended && (e.Live || e.Interested != 0) {
		return errors.New("an event's interest is out of range")
	}
	if e.Badge != nil {
		if err := checkBadge(e.Badge.Title, e.Badge.Image); err != nil {
			return err
		}
		if e.Place != "" {
			return errors.New("an event's badge goes with a voice channel")
		}
	}
	return nil
}

// Check checks how many are interested in an event, as a den sends it.
func (i EventInterest) Check() error {
	if _, err := ParseID(i.EventID); err != nil || i.Interested < 0 {
		return errors.New("an event's interest is invalid")
	}
	return nil
}
