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
type DenEvent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Banner      Image  `json:"banner,omitzero"`
	StartsAt    int64  `json:"starts_at"`
	EndsAt      int64  `json:"ends_at"`
	ChannelID   string `json:"channel_id,omitempty"`
	Place       string `json:"place,omitempty"`
	MadeBy      string `json:"made_by"`
	Live        bool   `json:"live,omitempty"`
	Interested  int    `json:"interested"`
	Mine        *bool  `json:"mine,omitempty"`
}

// EventRequest makes an event, or changes one: each field it carries, the
// rest staying as they are. Banner names an upload of the member's,
// waiting to be used, cut three times as wide as it's high, or "" to take
// the banner off. A place is a voice channel, or a line of text: giving
// one takes the other off. Making one takes a name, a start, an end and a
// place. A live event's start stays where it was.
type EventRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Banner      *string `json:"banner,omitempty"`
	StartsAt    *int64  `json:"starts_at,omitempty"`
	EndsAt      *int64  `json:"ends_at,omitempty"`
	ChannelID   *string `json:"channel_id,omitempty"`
	Place       *string `json:"place,omitempty"`
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

// CheckDenEvent checks an event to come or live as a den sends it: its
// name and place as cleaning leaves them, one place, an end after its
// start and no further than the longest an event lasts, its banner's
// size, and how many are interested.
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
	case e.ChannelID != "" && e.Place != "", e.ChannelID == "" && e.Place == "":
		return errors.New("an event has one place")
	case e.ChannelID != "":
		if _, err := ParseID(e.ChannelID); err != nil {
			return errors.New("an event names an invalid channel")
		}
	default:
		if place, err := CleanEventPlace(e.Place); err != nil || place != e.Place {
			return errors.New("an event has an invalid place")
		}
	}
	if e.StartsAt <= 0 || e.EndsAt <= e.StartsAt || e.EndsAt-e.StartsAt > MaxEventLength.Milliseconds() {
		return errors.New("an event's times are out of range")
	}
	if CheckImage(e.Banner) != nil {
		return errors.New("an event has an invalid banner")
	}
	if e.Interested < 0 {
		return errors.New("an event's interest is out of range")
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
