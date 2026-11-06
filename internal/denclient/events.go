package denclient

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/notify"
)

// Events (M17.4): the den's events to come and live, which the state keeps
// with the member's own mark on each, the cache keeps for the page offline,
// and the page shows from the den's view. The member makes, changes,
// cancels and ends them, and marks Interested, through the den; what comes
// back as the den's events changes the state. An event that starts, which
// the den tells only the members interested in it, notifies the member.

// cleanEvents checks the events a den sent in ready, or the cache kept: as
// many as a den holds at most, each once. A den's carry the member's own
// mark on each, which the cache keeps too; one without is taken as none.
func cleanEvents(list []denproto.DenEvent, fromDen bool) (map[string]denproto.DenEvent, bool) {
	if len(list) > denproto.MaxEvents {
		return nil, false
	}
	out := map[string]denproto.DenEvent{}
	for _, e := range list {
		if denproto.CheckDenEvent(e) != nil {
			return nil, false
		}
		if _, ok := out[e.ID]; ok || fromDen && e.Mine == nil {
			return nil, false
		}
		if e.Mine == nil {
			e.Mine = new(bool)
		}
		out[e.ID] = e
	}
	return out, true
}

// sortedEvents lists events as the page shows them: those live first, then
// the rest by their start.
func sortedEvents(events map[string]denproto.DenEvent) []denproto.DenEvent {
	out := slices.Collect(maps.Values(events))
	slices.SortFunc(out, func(a, b denproto.DenEvent) int {
		if a.Live != b.Live {
			if a.Live {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(a.StartsAt, b.StartsAt), compareIDs(a.ID, b.ID))
	})
	if out == nil {
		out = []denproto.DenEvent{}
	}
	return out
}

// startNotice is what an event that started, which the den told this
// member of as one interested in it, may notify them of: the event as the
// state now holds it, its den's name, and its place. The caller holds
// c.mu.
func (c *conn) startNoticeLocked(id string) *notify.Start {
	if c.m.Notices == nil || c.den == nil {
		return nil
	}
	e, ok := c.den.events[id]
	if !ok {
		return nil
	}
	st := &notify.Start{Den: c.j.denID.String(), DenName: c.profile.Name, Event: e.ID, Name: e.Name, Place: e.Place}
	if e.ChannelID != "" {
		st.Channel = c.den.channels[e.ChannelID].Name
		if st.Channel == "" {
			st.Channel = "a voice channel"
		}
	}
	return st
}

// checkEventRequest checks what a member asks of an event before it goes
// to the den, which checks it again: IDs this side can read, and a place
// that's one of the two.
func checkEventRequest(req denproto.EventRequest) error {
	if req.ChannelID != nil && req.Place != nil {
		return inputError(errors.New("an event's place is a voice channel or a line of text"))
	}
	if req.ChannelID != nil {
		if err := checkID("channel", *req.ChannelID); err != nil {
			return err
		}
	}
	if req.Banner != nil && *req.Banner != "" {
		if err := checkID("upload", *req.Banner); err != nil {
			return err
		}
	}
	return nil
}

// CreateEvent makes an event in a den, and returns it as the den made it.
func (m *Manager) CreateEvent(ctx context.Context, denID string, req denproto.EventRequest) (denproto.DenEvent, error) {
	return m.eventCall(ctx, denID, http.MethodPost, "", req)
}

// UpdateEvent changes one of a den's events, and returns it as it now is.
func (m *Manager) UpdateEvent(ctx context.Context, denID, eventID string, req denproto.EventRequest) (denproto.DenEvent, error) {
	if err := checkID("event", eventID); err != nil {
		return denproto.DenEvent{}, err
	}
	return m.eventCall(ctx, denID, http.MethodPatch, "/"+eventID, req)
}

func (m *Manager) eventCall(ctx context.Context, denID, method, path string, req denproto.EventRequest) (denproto.DenEvent, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.DenEvent{}, err
	}
	if err := checkEventRequest(req); err != nil {
		return denproto.DenEvent{}, err
	}
	var e denproto.DenEvent
	if err := c.call(ctx, method, "/api/events"+path, req, &e); err != nil {
		return denproto.DenEvent{}, err
	}
	if denproto.CheckDenEvent(e) != nil || e.Mine == nil {
		return denproto.DenEvent{}, errMalformed
	}
	return e, nil
}

// CancelEvent cancels one of a den's events to come.
func (m *Manager) CancelEvent(ctx context.Context, denID, eventID string) error {
	return m.eventAction(ctx, denID, eventID, http.MethodDelete, "")
}

// EndEvent ends one of a den's live events now.
func (m *Manager) EndEvent(ctx context.Context, denID, eventID string) error {
	return m.eventAction(ctx, denID, eventID, http.MethodPost, "/end")
}

func (m *Manager) eventAction(ctx context.Context, denID, eventID, method, path string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if err := checkID("event", eventID); err != nil {
		return err
	}
	return c.call(ctx, method, "/api/events/"+eventID+path, nil, nil)
}

// SetInterest marks the member Interested in one of a den's events, or
// takes it back, and returns how many are, with the member's mark.
func (m *Manager) SetInterest(ctx context.Context, denID, eventID string, on bool) (denproto.EventInterest, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.EventInterest{}, err
	}
	if err := checkID("event", eventID); err != nil {
		return denproto.EventInterest{}, err
	}
	method := http.MethodPut
	if !on {
		method = http.MethodDelete
	}
	var in denproto.EventInterest
	if err := c.call(ctx, method, "/api/events/"+eventID+"/interest", nil, &in); err != nil {
		return denproto.EventInterest{}, err
	}
	if in.Check() != nil || in.EventID != eventID || in.Mine == nil {
		return denproto.EventInterest{}, errMalformed
	}
	return in, nil
}
