package denproto

import "fmt"

// Disappearing messages (M16.4): any member of a DM or a group sets its
// messages to disappear a while after they're sent, which its den does as
// their author's delete would, and each member's Dens with what it holds
// of them. A time counts from sending, which the den knows and every
// member sees alike, and applies to the messages sent after it's set.

// The times a DM's or a group's messages disappear after, in seconds: an
// hour, a day, a week or four weeks. 0 keeps them.
const (
	ExpiresHour     = 3600
	ExpiresDay      = 24 * ExpiresHour
	ExpiresWeek     = 7 * ExpiresDay
	MaxExpiresAfter = 4 * ExpiresWeek
)

// ExpiryTimes are the times a den takes, besides 0.
var ExpiryTimes = []int{ExpiresHour, ExpiresDay, ExpiresWeek, MaxExpiresAfter}

// CheckExpiresAfter checks a time a member sets: 0, or one of ExpiryTimes;
// with seconds, as a development instance's den takes for the browser
// checks, any whole number of seconds up to the longest.
func CheckExpiresAfter(after int, seconds bool) error {
	switch {
	case after == 0:
		return nil
	case seconds && after > 0 && after <= MaxExpiresAfter:
		return nil
	}
	for _, t := range ExpiryTimes {
		if after == t {
			return nil
		}
	}
	return fmt.Errorf("expires_after is 0, to keep messages, or one of %v seconds", ExpiryTimes)
}

// ExpirySet says who last set a DM's or a group's time, and when, for the
// line in it that says so.
type ExpirySet struct {
	By string `json:"by"`
	At int64  `json:"at"`
}

// ExpiryRequest sets a DM's or a group's time.
type ExpiryRequest struct {
	ExpiresAfter int `json:"expires_after"`
}
