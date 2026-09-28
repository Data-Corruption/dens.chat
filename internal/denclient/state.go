package denclient

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// denState is what a client knows about a den between events: its members,
// groups, channels and this member's read states. The messages themselves
// live in the page's windows, fetched from the den as needed.
type denState struct {
	members  map[string]denproto.Member
	groups   map[string]denproto.Group
	channels map[string]denproto.Channel
	reads    map[string]denproto.ReadState
}

// View is a joined den as the page sees it.
type View struct {
	Status
	Me         denproto.Member      `json:"me"`
	Members    []denproto.Member    `json:"members"`
	Groups     []denproto.Group     `json:"groups"`
	Channels   []denproto.Channel   `json:"channels"`
	ReadStates []denproto.ReadState `json:"read_states"`
}

func newState() *denState {
	return &denState{members: map[string]denproto.Member{}, groups: map[string]denproto.Group{},
		channels: map[string]denproto.Channel{}, reads: map[string]denproto.ReadState{}}
}

func (s *denState) view(status Status, me denproto.Member) View {
	v := View{Status: status, Me: me, Members: []denproto.Member{}, Groups: []denproto.Group{},
		Channels: []denproto.Channel{}, ReadStates: []denproto.ReadState{}}
	for _, m := range s.members {
		v.Members = append(v.Members, m)
	}
	for _, g := range s.groups {
		v.Groups = append(v.Groups, g)
	}
	for _, c := range s.channels {
		v.Channels = append(v.Channels, c)
	}
	for _, r := range s.reads {
		v.ReadStates = append(v.ReadStates, r)
	}
	slices.SortFunc(v.Members, func(a, b denproto.Member) int { return compareIDs(a.ID, b.ID) })
	slices.SortFunc(v.Groups, func(a, b denproto.Group) int { return a.Position - b.Position })
	slices.SortFunc(v.Channels, func(a, b denproto.Channel) int { return a.Position - b.Position })
	slices.SortFunc(v.ReadStates, func(a, b denproto.ReadState) int { return compareIDs(a.ChannelID, b.ChannelID) })
	return v
}

func compareIDs(a, b string) int {
	x, _ := denproto.ParseID(a)
	y, _ := denproto.ParseID(b)
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

var errMalformed = errors.New("the den sent a malformed event")

func validID(id string) bool {
	_, err := denproto.ParseID(id)
	return err == nil
}

func cleanChannel(c denproto.Channel) (denproto.Channel, bool) {
	name, err := denproto.CleanName(c.Name, denproto.MaxNameRunes)
	if err != nil || !validID(c.ID) || (c.GroupID != nil && !validID(*c.GroupID)) ||
		(c.Kind != denproto.KindText && c.Kind != denproto.KindVoice) || denproto.CheckDescription(c.Description) != nil || c.Position < 0 {
		return c, false
	}
	c.Name = name
	return c, true
}

func cleanGroup(g denproto.Group) (denproto.Group, bool) {
	name, err := denproto.CleanName(g.Name, denproto.MaxNameRunes)
	if err != nil || !validID(g.ID) || g.Position < 0 {
		return g, false
	}
	g.Name = name
	return g, true
}

func cleanRead(r denproto.ReadState) (denproto.ReadState, bool) {
	ok := validID(r.ChannelID) && (r.LastMessage == "" || validID(r.LastMessage)) &&
		(r.ReadPosition == "" || validID(r.ReadPosition)) && r.MentionCount >= 0
	return r, ok
}

// load replaces the state with a snapshot. Anything malformed in it is a
// protocol violation.
func (s *denState) load(r denproto.Ready) error {
	next := newState()
	for _, m := range r.Members {
		m, ok := cleanMember(m)
		if !ok || !validID(m.ID) {
			return errMalformed
		}
		next.members[m.ID] = m
	}
	for _, g := range r.Groups {
		g, ok := cleanGroup(g)
		if !ok {
			return errMalformed
		}
		next.groups[g.ID] = g
	}
	for _, c := range r.Channels {
		c, ok := cleanChannel(c)
		if !ok {
			return errMalformed
		}
		next.channels[c.ID] = c
	}
	for _, rs := range r.ReadStates {
		rs, ok := cleanRead(rs)
		if !ok {
			return errMalformed
		}
		next.reads[rs.ChannelID] = rs
	}
	if len(next.channels) > denproto.MaxChannels || len(next.groups) > denproto.MaxGroups {
		return errMalformed
	}
	*s = *next
	return nil
}

// applyEvent updates the state for one event and returns the event as it
// may be forwarded to the page: decoded, checked and re-encoded, so no
// byte from a den reaches the page unchecked. ok is false for an event the
// client ignores.
func (s *denState) applyEvent(e denproto.Event, me denproto.Member) (denproto.Event, bool, error) {
	var data any
	switch e.T {
	case denproto.EventMemberJoined:
		var m denproto.Member
		if json.Unmarshal(e.D, &m) != nil {
			return e, false, errMalformed
		}
		m, ok := cleanMember(m)
		if !ok || !validID(m.ID) {
			return e, false, errMalformed
		}
		s.members[m.ID] = m
		data = m
	case denproto.EventChannelCreated, denproto.EventChannelUpdated:
		var c denproto.Channel
		if json.Unmarshal(e.D, &c) != nil {
			return e, false, errMalformed
		}
		c, ok := cleanChannel(c)
		if !ok {
			return e, false, errMalformed
		}
		s.channels[c.ID] = c
		if _, ok := s.reads[c.ID]; !ok && c.Kind == denproto.KindText {
			s.reads[c.ID] = denproto.ReadState{ChannelID: c.ID}
		}
		data = c
	case denproto.EventChannelDeleted, denproto.EventGroupDeleted:
		var d denproto.ChannelDeleted
		if json.Unmarshal(e.D, &d) != nil || !validID(d.ID) {
			return e, false, errMalformed
		}
		if e.T == denproto.EventChannelDeleted {
			delete(s.channels, d.ID)
			delete(s.reads, d.ID)
		} else {
			delete(s.groups, d.ID)
		}
		data = d
	case denproto.EventChannelsReordered:
		var r denproto.ChannelsReordered
		if json.Unmarshal(e.D, &r) != nil || (r.GroupID != nil && !validID(*r.GroupID)) {
			return e, false, errMalformed
		}
		for i, id := range r.ChannelIDs {
			if c, ok := s.channels[id]; ok {
				c.GroupID, c.Position = r.GroupID, i
				s.channels[id] = c
			}
		}
		data = r
	case denproto.EventGroupCreated, denproto.EventGroupUpdated:
		var g denproto.Group
		if json.Unmarshal(e.D, &g) != nil {
			return e, false, errMalformed
		}
		g, ok := cleanGroup(g)
		if !ok {
			return e, false, errMalformed
		}
		s.groups[g.ID] = g
		data = g
	case denproto.EventGroupsReordered:
		var r denproto.GroupsReordered
		if json.Unmarshal(e.D, &r) != nil {
			return e, false, errMalformed
		}
		for i, id := range r.GroupIDs {
			if g, ok := s.groups[id]; ok {
				g.Position = i
				s.groups[id] = g
			}
		}
		data = r
	case denproto.EventMessageCreated, denproto.EventMessageUpdated:
		var m denproto.Message
		if json.Unmarshal(e.D, &m) != nil || denproto.CheckMessage(m) != nil {
			return e, false, errMalformed
		}
		if e.T == denproto.EventMessageCreated {
			s.noteMessage(m, me)
		}
		data = m
	case denproto.EventMessageDeleted:
		var d denproto.MessageDeleted
		if json.Unmarshal(e.D, &d) != nil || !validID(d.ID) || !validID(d.ChannelID) {
			return e, false, errMalformed
		}
		data = d
	case denproto.EventReadStateUpdated:
		var r denproto.ReadState
		if json.Unmarshal(e.D, &r) != nil {
			return e, false, errMalformed
		}
		r, ok := cleanRead(r)
		if !ok {
			return e, false, errMalformed
		}
		if old, ok := s.reads[r.ChannelID]; ok {
			r.LastMessage = old.LastMessage
			s.reads[r.ChannelID] = r
		}
		data = r
	default:
		return e, false, nil
	}
	out, err := denproto.NewEvent(e.T, e.Seq, data)
	return out, err == nil, err
}

// noteMessage keeps unread state current as messages arrive: the channel's
// last message moves, and a mention of this member counts unless it's
// their own message. A page showing the channel marks it read, and the
// den's read_state.updated resets the count.
func (s *denState) noteMessage(m denproto.Message, me denproto.Member) {
	r, ok := s.reads[m.ChannelID]
	if !ok {
		return
	}
	if compareIDs(m.ID, r.LastMessage) > 0 {
		r.LastMessage = m.ID
	}
	if m.AuthorID == me.ID {
		r.ReadPosition = m.ID
	} else if slices.Contains(denproto.Mentions(m.Text), strings.ToLower(me.Username)) {
		r.MentionCount++
	}
	s.reads[m.ChannelID] = r
}
