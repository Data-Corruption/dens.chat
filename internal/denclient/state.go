package denclient

import (
	"cmp"
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
	online   map[string]bool
	// touched holds the channels whose read state changed since the page
	// was last told, so the page shows the counts counted here.
	touched map[string]bool
}

// View is a joined den as the page sees it.
type View struct {
	Status
	Me         denproto.Member      `json:"me"`
	Members    []denproto.Member    `json:"members"`
	Groups     []denproto.Group     `json:"groups"`
	Channels   []denproto.Channel   `json:"channels"`
	ReadStates []denproto.ReadState `json:"read_states"`
	Online     []string             `json:"online"`
}

func newState() *denState {
	return &denState{members: map[string]denproto.Member{}, groups: map[string]denproto.Group{},
		channels: map[string]denproto.Channel{}, reads: map[string]denproto.ReadState{}, online: map[string]bool{},
		touched: map[string]bool{}}
}

// takeTouched returns the read states that changed since the last call.
func (s *denState) takeTouched() []denproto.ReadState {
	var out []denproto.ReadState
	for id := range s.touched {
		if r, ok := s.reads[id]; ok {
			out = append(out, r)
		}
	}
	clear(s.touched)
	slices.SortFunc(out, func(a, b denproto.ReadState) int { return compareIDs(a.ChannelID, b.ChannelID) })
	return out
}

func (s *denState) view(status Status, me denproto.Member) View {
	v := View{Status: status, Me: me, Members: []denproto.Member{}, Groups: []denproto.Group{},
		Channels: []denproto.Channel{}, ReadStates: []denproto.ReadState{}, Online: []string{}}
	for id := range s.online {
		v.Online = append(v.Online, id)
	}
	slices.SortFunc(v.Online, compareIDs)
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
	// Positions count within a group, so IDs break ties between groups.
	slices.SortFunc(v.Groups, func(a, b denproto.Group) int { return cmp.Or(a.Position-b.Position, compareIDs(a.ID, b.ID)) })
	slices.SortFunc(v.Channels, func(a, b denproto.Channel) int { return cmp.Or(a.Position-b.Position, compareIDs(a.ID, b.ID)) })
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
	if !validID(c.ID) || c.Position < 0 {
		return c, false
	}
	if c.Kind == denproto.KindDM {
		ok := len(c.Members) == 2 && validID(c.Members[0]) && validID(c.Members[1]) && c.Members[0] != c.Members[1] &&
			c.GroupID == nil && c.Name == "" && c.Description == "" && !c.StaffOnly
		return c, ok
	}
	name, err := denproto.CleanName(c.Name, denproto.MaxNameRunes)
	if err != nil || len(c.Members) != 0 || (c.GroupID != nil && !validID(*c.GroupID)) ||
		(c.Kind != denproto.KindText && c.Kind != denproto.KindVoice) || denproto.CheckDescription(c.Description) != nil {
		return c, false
	}
	c.Name = name
	return c, true
}

// hasMessages reports whether a channel holds messages, as text channels
// and DMs do.
func hasMessages(c denproto.Channel) bool {
	return c.Kind == denproto.KindText || c.Kind == denproto.KindDM
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
	for _, id := range r.Online {
		if !validID(id) {
			return errMalformed
		}
		next.online[id] = true
	}
	channels := 0
	for _, c := range next.channels {
		if c.Kind != denproto.KindDM {
			channels++
		}
	}
	if channels > denproto.MaxChannels || len(next.groups) > denproto.MaxGroups {
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
	case denproto.EventMemberJoined, denproto.EventMemberUpdated:
		var m denproto.Member
		if json.Unmarshal(e.D, &m) != nil {
			return e, false, errMalformed
		}
		m, ok := cleanMember(m)
		if !ok {
			return e, false, errMalformed
		}
		s.members[m.ID] = m
		data = m
	case denproto.EventMemberLeft:
		var l denproto.MemberLeft
		if json.Unmarshal(e.D, &l) != nil || !validID(l.ID) || l.LeftAt <= 0 {
			return e, false, errMalformed
		}
		if m, ok := s.members[l.ID]; ok {
			m.LeftAt, m.Role = l.LeftAt, denproto.RoleMember
			s.members[l.ID] = m
		}
		delete(s.online, l.ID)
		data = l
	case denproto.EventPresence:
		var p denproto.Presence
		if json.Unmarshal(e.D, &p) != nil {
			return e, false, errMalformed
		}
		for _, id := range append(slices.Clip(p.Online), p.Offline...) {
			if !validID(id) {
				return e, false, errMalformed
			}
		}
		if p.Full {
			clear(s.online)
		}
		for _, id := range p.Online {
			s.online[id] = true
		}
		for _, id := range p.Offline {
			delete(s.online, id)
		}
		data = p
	case denproto.EventTyping:
		var t denproto.Typing
		if json.Unmarshal(e.D, &t) != nil || !validID(t.ChannelID) || !validID(t.MemberID) {
			return e, false, errMalformed
		}
		if _, ok := s.channels[t.ChannelID]; !ok {
			return e, false, nil
		}
		data = t
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
		if _, ok := s.reads[c.ID]; !ok && hasMessages(c) {
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
			s.touched[r.ChannelID] = true
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
// their own message; in a DM, as the den counts, every message does. A
// page showing the channel marks it read, and the den's read_state.updated
// resets the count.
func (s *denState) noteMessage(m denproto.Message, me denproto.Member) {
	r, ok := s.reads[m.ChannelID]
	if !ok {
		return
	}
	if compareIDs(m.ID, r.LastMessage) > 0 {
		r.LastMessage = m.ID
	}
	dm := s.channels[m.ChannelID].Kind == denproto.KindDM
	if m.AuthorID == me.ID {
		r.ReadPosition = m.ID
	} else if dm || slices.Contains(denproto.Mentions(m.Text), strings.ToLower(me.Username)) {
		r.MentionCount++
	}
	s.reads[m.ChannelID] = r
	s.touched[m.ChannelID] = true
}
