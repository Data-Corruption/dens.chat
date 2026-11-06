package denclient

import (
	"cmp"
	"encoding/json"
	"errors"
	"maps"
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
	limits   denproto.Limits
	// callLimits are the den's limits for calls and shares (M4.2), and
	// retention the days it keeps a message, 0 for ever (M5).
	callLimits denproto.CallLimits
	retention  int
	// touched holds the channels whose read state changed since the page
	// was last told, so the page shows the counts counted here.
	touched map[string]bool
	// gone holds files the den deleted since it was last taken, so they
	// leave the cache.
	gone []string
	// keys are this member's DM keys by ID, with their own sealed copies
	// and without exchanges; requests are sign-ins waiting for their
	// approval by ID; sealCheck identifies their DM seal (M1.7).
	keys      map[string]denproto.DMKey
	requests  map[string]denproto.DeviceRequest
	sealCheck denproto.Bytes
	// signed are this member's DM keys sealed to identities, by ID, with
	// their own copies; identities says the den keeps identities at all,
	// which one older than M16.1 doesn't.
	signed     map[string]denproto.SignedKey
	identities bool
	// calls are who is in each voice channel's call (M2).
	calls map[string]denproto.Call
	// sounds are the den's soundboard's, by ID, and soundLimits its limits
	// for them, none from a den older than the soundboard (M14.3).
	sounds      map[string]denproto.Sound
	soundLimits denproto.SoundLimits
	// emoji are the den's emoji, by ID, and emojiLimits its limit for
	// them, nil from a den older than emoji (M15.2).
	emoji       map[string]denproto.Emoji
	emojiLimits *denproto.EmojiLimits
	// sealedCalls says the den seals every call end to end, and takes no
	// join that doesn't (M16.2).
	sealedCalls bool
	// disappearing says the den deletes a DM's and a group's messages at
	// the time their members set (M16.4).
	disappearing bool
	// spoilers says the den keeps which of a channel's files went as
	// spoilers, and moveMembers that staff move members between calls
	// (M17.2).
	spoilers, moveMembers bool
	// groupDMs says the snapshot came on a socket speaking the protocol's
	// version with group DMs (M16.3), and notes the one with notes to self
	// (M17.3).
	groupDMs, notes bool
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
	Limits     denproto.Limits      `json:"limits"`
	CallLimits denproto.CallLimits  `json:"call_limits"`
	Retention  int                  `json:"retention"`
	// DMKeys are the keys of this member's DMs, M1.7's and those sealed to
	// identities (M16.1), without anything secret.
	DMKeys []PageKey `json:"dm_keys"`
	// Identities are the identities this member's Dens kept here, by
	// member, and KeepsIdentities whether the den keeps identities at all
	// (M16.1).
	Identities      map[string]PageIdentity `json:"identities"`
	KeepsIdentities bool                    `json:"keeps_identities"`
	// Calls are the calls with someone in them.
	Calls []denproto.Call `json:"calls"`
	// Sounds are the den's soundboard's, by name, and SoundLimits its
	// limits for them (M14.3).
	Sounds      []denproto.Sound     `json:"sounds"`
	SoundLimits denproto.SoundLimits `json:"sound_limits"`
	// Emoji are the den's emoji, by name, and EmojiLimits its limit for
	// them, null from a den older than emoji (M15.2).
	Emoji       []denproto.Emoji      `json:"emoji"`
	EmojiLimits *denproto.EmojiLimits `json:"emoji_limits"`
	// SealedCalls says the den seals its calls end to end, so the page
	// joins one only sealing what it sends (M16.2).
	SealedCalls bool `json:"sealed_calls"`
	// Disappearing says the den deletes a DM's and a group's messages at
	// the time their members set, which the page offers only then (M16.4).
	Disappearing bool `json:"disappearing"`
	// Spoilers says the den keeps which of a channel's files went as
	// spoilers, and MoveMembers that staff move members between calls,
	// which the page offers only then (M17.2).
	Spoilers    bool `json:"spoilers"`
	MoveMembers bool `json:"move_members"`
	// HasGroupDMs says the den has group DMs for this Dens, and GroupDMs
	// are this member's, as their changes make them (M16.3).
	HasGroupDMs bool                 `json:"has_group_dms"`
	GroupDMs    map[string]PageGroup `json:"group_dms"`
	// HasNotes says the den has notes to self for this Dens, which the page
	// offers only then (M17.3).
	HasNotes bool `json:"has_notes"`
}

func newState() *denState {
	return &denState{members: map[string]denproto.Member{}, groups: map[string]denproto.Group{},
		channels: map[string]denproto.Channel{}, reads: map[string]denproto.ReadState{}, online: map[string]bool{},
		touched: map[string]bool{}, keys: map[string]denproto.DMKey{}, requests: map[string]denproto.DeviceRequest{},
		signed: map[string]denproto.SignedKey{}, calls: map[string]denproto.Call{}, sounds: map[string]denproto.Sound{},
		emoji: map[string]denproto.Emoji{}}
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
		Channels: []denproto.Channel{}, ReadStates: []denproto.ReadState{}, Online: []string{}, Limits: s.limits,
		CallLimits: s.callLimits, Retention: s.retention, Sounds: sortedSounds(s.sounds), SoundLimits: s.soundLimits,
		Emoji: sortedEmoji(s.emoji), EmojiLimits: s.emojiLimits, KeepsIdentities: s.identities, SealedCalls: s.sealedCalls,
		Disappearing: s.disappearing, Spoilers: s.spoilers, MoveMembers: s.moveMembers}
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
	v.DMKeys = []PageKey{}
	for _, k := range s.keys {
		v.DMKeys = append(v.DMKeys, pageKey(k))
	}
	for _, k := range s.signed {
		v.DMKeys = append(v.DMKeys, pageSignedKey(k))
	}
	slices.SortFunc(v.DMKeys, func(a, b PageKey) int { return compareIDs(a.ID, b.ID) })
	v.Calls = []denproto.Call{}
	for _, c := range s.calls {
		v.Calls = append(v.Calls, c)
	}
	slices.SortFunc(v.Calls, func(a, b denproto.Call) int { return compareIDs(a.ChannelID, b.ChannelID) })
	slices.SortFunc(v.Members, func(a, b denproto.Member) int { return compareIDs(a.ID, b.ID) })
	// Positions count within a group, so IDs break ties between groups.
	slices.SortFunc(v.Groups, func(a, b denproto.Group) int { return cmp.Or(a.Position-b.Position, compareIDs(a.ID, b.ID)) })
	slices.SortFunc(v.Channels, func(a, b denproto.Channel) int { return cmp.Or(a.Position-b.Position, compareIDs(a.ID, b.ID)) })
	slices.SortFunc(v.ReadStates, func(a, b denproto.ReadState) int { return compareIDs(a.ChannelID, b.ChannelID) })
	return v
}

// sortedSounds lists the den's sounds by name, in any case, as the den
// lists them.
func sortedSounds(sounds map[string]denproto.Sound) []denproto.Sound {
	out := slices.Collect(maps.Values(sounds))
	slices.SortFunc(out, func(a, b denproto.Sound) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), compareIDs(a.ID, b.ID))
	})
	if out == nil {
		out = []denproto.Sound{}
	}
	return out
}

// sortedEmoji lists the den's emoji by name, as the den lists them.
func sortedEmoji(emoji map[string]denproto.Emoji) []denproto.Emoji {
	out := slices.Collect(maps.Values(emoji))
	slices.SortFunc(out, func(a, b denproto.Emoji) int { return cmp.Or(strings.Compare(a.Name, b.Name), compareIDs(a.ID, b.ID)) })
	if out == nil {
		out = []denproto.Emoji{}
	}
	return out
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
			c.GroupID == nil && c.Name == "" && c.Description == "" && !c.StaffOnly && c.Bitrate == 0 && c.Changes == 0 && c.SealedName == nil
		return c, ok && cleanExpiry(c)
	}
	if c.Kind == denproto.KindNotes {
		// A member's notes name them alone (M17.3), whom the state checks.
		ok := len(c.Members) == 1 && validID(c.Members[0]) && c.GroupID == nil && c.Name == "" && c.Description == "" && !c.StaffOnly &&
			c.Bitrate == 0 && c.Changes == 0 && c.SealedName == nil && c.ExpiresAfter == 0 && c.ExpiresSet == nil
		return c, ok
	}
	if c.Kind == denproto.KindGroupDM {
		// Its members as the den holds them, which only its changes, checked
		// here, make (M16.3).
		ok := len(c.Members) <= denproto.MaxGroupMembers && c.GroupID == nil && c.Name == "" && c.Description == "" && !c.StaffOnly &&
			c.Bitrate == 0 && c.Changes >= 1 && c.Changes <= denproto.MaxGroupChanges && (c.SealedName == nil || c.SealedName.Check() == nil)
		for i, m := range c.Members {
			ok = ok && validID(m) && !slices.Contains(c.Members[:i], m)
		}
		return c, ok && cleanExpiry(c)
	}
	if c.Changes != 0 || c.SealedName != nil || c.ExpiresAfter != 0 || c.ExpiresSet != nil {
		return c, false
	}
	name, err := denproto.CleanName(c.Name, denproto.MaxNameRunes)
	if err != nil || len(c.Members) != 0 || (c.GroupID != nil && !validID(*c.GroupID)) ||
		(c.Kind != denproto.KindText && c.Kind != denproto.KindVoice) || denproto.CheckDescription(c.Description) != nil {
		return c, false
	}
	// A voice channel's bitrate is one staff could set (M4.2), and a text
	// channel has none.
	if (c.Kind == denproto.KindVoice) != (denproto.CheckVoiceBitrate(c.Bitrate) == nil) {
		return c, false
	}
	c.Name = name
	return c, true
}

// cleanExpiry checks a DM's or a group's time (M16.4): up to four weeks, in
// any whole number of seconds, which a development instance's den takes,
// and who set it, and when.
func cleanExpiry(c denproto.Channel) bool {
	return c.ExpiresAfter >= 0 && c.ExpiresAfter <= denproto.MaxExpiresAfter &&
		(c.ExpiresSet == nil || validID(c.ExpiresSet.By) && c.ExpiresSet.At > 0)
}

// hasMessages reports whether a channel holds messages, as text channels,
// DMs, group DMs (M16.3) and notes (M17.3) do.
func hasMessages(c denproto.Channel) bool {
	return c.Kind == denproto.KindText || isSealed(c)
}

// isSealed reports whether a channel's messages are sealed by its
// members' Dens: a DM's, a group's (M16.3), or the member's notes (M17.3).
func isSealed(c denproto.Channel) bool {
	return c.Kind == denproto.KindDM || c.Kind == denproto.KindGroupDM || c.Kind == denproto.KindNotes
}

// ownNotes checks the notes among a den's channels: those of the member
// whose ID is me, once at most (M17.3). With me empty, as the cache's
// state restores before the connection knows, only that there's one.
func ownNotes(channels map[string]denproto.Channel, me string) bool {
	n := 0
	for _, c := range channels {
		if c.Kind == denproto.KindNotes {
			n++
			if me != "" && c.Members[0] != me {
				return false
			}
		}
	}
	return n <= 1
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

// cleanLimits checks a den's upload limits, its limits for calls, its
// retention period, its soundboard's limits and its limit for emoji, which
// a den older than the soundboard (M14.3) or emoji (M15.2) doesn't send.
// The den checked them when they were set, so any that don't check are the
// den misbehaving.
func cleanLimits(d denproto.Den) bool {
	return denproto.CheckLimits(d.Limits) == nil && denproto.CheckCallLimits(d.CallLimits) == nil &&
		denproto.CheckRetention(d.Retention) == nil &&
		(d.SoundLimits == denproto.SoundLimits{} || denproto.CheckSoundLimits(d.SoundLimits) == nil) &&
		(d.EmojiLimits == nil || denproto.CheckEmojiLimits(*d.EmojiLimits) == nil)
}

// cleanEmoji checks a den's emoji, as many as a den keeps at most, each
// once and by a name of its own.
func cleanEmoji(list []denproto.Emoji) (map[string]denproto.Emoji, bool) {
	if len(list) > denproto.MaxEmojis {
		return nil, false
	}
	out := map[string]denproto.Emoji{}
	names := map[string]bool{}
	for _, e := range list {
		if denproto.CheckEmoji(e) != nil || out[e.ID] != (denproto.Emoji{}) || names[e.Name] {
			return nil, false
		}
		out[e.ID], names[e.Name] = e, true
	}
	return out, true
}

// cleanSounds checks a den's sounds, as many as a den keeps at most, each
// once and by a name of its own.
func cleanSounds(list []denproto.Sound) (map[string]denproto.Sound, bool) {
	if len(list) > denproto.MaxSounds {
		return nil, false
	}
	out := map[string]denproto.Sound{}
	for _, s := range list {
		if denproto.CheckSound(s) != nil || out[s.ID] != (denproto.Sound{}) {
			return nil, false
		}
		out[s.ID] = s
	}
	return out, true
}

// load replaces the state with a snapshot of the den with denID. Anything
// malformed in it is a protocol violation. A snapshot that sends only the
// members changed since the client's list (M6.2) changes that list, prev,
// which the client named on the socket's upgrade.
func (s *denState) load(denID []byte, r denproto.Ready, prev map[string]denproto.Member) error {
	next := newState()
	if !cleanLimits(r.Den) || denproto.CheckSync(r.Sync) != nil {
		return errMalformed
	}
	if r.Sync.MembersSince != nil {
		if prev == nil {
			return errMalformed
		}
		maps.Copy(next.members, prev)
	}
	next.limits, next.callLimits, next.retention, next.soundLimits = r.Den.Limits, r.Den.CallLimits, r.Den.Retention, r.Den.SoundLimits
	next.sealedCalls, next.disappearing = r.Den.SealedCalls, r.Den.Disappearing
	next.spoilers, next.moveMembers = r.Den.Spoilers, r.Den.MoveMembers
	sounds, ok := cleanSounds(r.Sounds)
	if !ok {
		return errMalformed
	}
	next.sounds = sounds
	emoji, ok := cleanEmoji(r.Emoji)
	if !ok {
		return errMalformed
	}
	next.emoji, next.emojiLimits = emoji, r.Den.EmojiLimits
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
	channels, groups := 0, 0
	for _, c := range next.channels {
		switch c.Kind {
		case denproto.KindDM, denproto.KindNotes:
		case denproto.KindGroupDM:
			groups++
		default:
			channels++
		}
	}
	if !ownNotes(next.channels, r.Me.ID) {
		return errMalformed
	}
	if groups > denproto.MaxGroupDMs {
		return errMalformed
	}
	if channels > denproto.MaxChannels || len(next.groups) > denproto.MaxGroups {
		return errMalformed
	}
	if len(r.SealCheck) != denproto.SealCheckSize {
		return errMalformed
	}
	next.sealCheck = r.SealCheck
	for _, k := range r.DMKeys {
		if denproto.CheckDMKey(k) != nil || k.Exchange != nil || next.channels[k.ChannelID].Kind != denproto.KindDM {
			return errMalformed
		}
		next.keys[k.ID] = k
	}
	// A den older than M16.1 sends no signed keys at all.
	next.identities = r.SignedKeys != nil
	for _, k := range r.SignedKeys {
		if !next.cleanSigned(denID, k) || next.keys[k.ID].ID != "" {
			return errMalformed
		}
		next.signed[k.ID] = k
	}
	if len(r.DeviceRequests) > maxRequests {
		return errMalformed
	}
	for _, q := range r.DeviceRequests {
		if denproto.CheckDeviceRequest(q) != nil {
			return errMalformed
		}
		next.requests[q.ID.String()] = q
	}
	if len(r.Calls) > denproto.MaxChannels {
		return errMalformed
	}
	for _, c := range r.Calls {
		if !cleanCall(c) {
			return errMalformed
		}
		if len(c.Members) > 0 {
			next.calls[c.ChannelID] = c
		}
	}
	*s = *next
	return nil
}

// keptState is a den's state as the cache keeps it between runs of the
// service (M6.2), for the page to show while the den is away: all of it
// but who's online, in calls and asking to sign in, which only a
// connection knows. The members are kept apart, since they change least
// and take most of the room.
type keptState struct {
	Den       denproto.Den         `json:"den"`
	Groups    []denproto.Group     `json:"groups"`
	Channels  []denproto.Channel   `json:"channels"`
	Reads     []denproto.ReadState `json:"reads"`
	Keys      []denproto.DMKey     `json:"keys"`
	SealCheck denproto.Bytes       `json:"seal_check"`
	Sounds    []denproto.Sound     `json:"sounds,omitempty"` // M14.3
	Emoji     []denproto.Emoji     `json:"emoji,omitempty"`  // M15.2
	// M16.1: the DM keys sealed to identities, and whether the den keeps
	// identities.
	SignedKeys []denproto.SignedKey `json:"signed_keys,omitempty"`
	Identities bool                 `json:"identities,omitempty"`
	// M16.3: the member's group DMs as their changes, checked, made them,
	// and whether the den has them for this Dens.
	GroupStates map[string]*groupState `json:"group_states,omitempty"`
	GroupDMs    bool                   `json:"group_dms,omitempty"`
	// M17.3: whether the den has notes to self for this Dens.
	Notes bool `json:"notes,omitempty"`
}

// kept returns the state as the cache keeps it, and its members.
func (s *denState) kept() (keptState, []denproto.Member) {
	k := keptState{Den: denproto.Den{Limits: s.limits, CallLimits: s.callLimits, Retention: s.retention, SoundLimits: s.soundLimits,
		EmojiLimits: s.emojiLimits, SealedCalls: s.sealedCalls, Disappearing: s.disappearing, Spoilers: s.spoilers, MoveMembers: s.moveMembers},
		Groups: slices.Collect(maps.Values(s.groups)), Channels: slices.Collect(maps.Values(s.channels)),
		Reads: slices.Collect(maps.Values(s.reads)), Keys: slices.Collect(maps.Values(s.keys)), SealCheck: s.sealCheck,
		Sounds: slices.Collect(maps.Values(s.sounds)), Emoji: slices.Collect(maps.Values(s.emoji)),
		SignedKeys: slices.Collect(maps.Values(s.signed)), Identities: s.identities, GroupDMs: s.groupDMs, Notes: s.notes}
	return k, slices.Collect(maps.Values(s.members))
}

// cleanGroups checks the groups the cache kept, as checked here: each one
// of the den's group channels, its members IDs, at most a group's, and its
// changes no more than the page gets.
func cleanGroups(channels map[string]denproto.Channel, groups map[string]*groupState) bool {
	for id, g := range groups {
		if g == nil || channels[id].Kind != denproto.KindGroupDM || g.N < 0 || len(g.Members) > denproto.MaxGroupMembers ||
			len(g.Changes) > maxShownChanges || (g.N > 0 && len(g.Hash) != 32) {
			return false
		}
		for _, c := range g.Changes {
			if c.Sealed != nil && c.Sealed.Check() != nil {
				return false
			}
		}
		for i, m := range g.Members {
			if !validID(m) || slices.Contains(g.Members[:i], m) {
				return false
			}
		}
	}
	return true
}

// restore makes a state of what the cache kept of the den with denID. The
// cache sealed it with the data key, but it's checked as a den's answer is
// all the same.
func restore(denID []byte, k keptState, members []denproto.Member) (*denState, error) {
	s := newState()
	if !cleanLimits(k.Den) {
		return nil, errMalformed
	}
	s.limits, s.callLimits, s.retention, s.sealCheck = k.Den.Limits, k.Den.CallLimits, k.Den.Retention, k.SealCheck
	s.soundLimits, s.sealedCalls, s.disappearing = k.Den.SoundLimits, k.Den.SealedCalls, k.Den.Disappearing
	s.spoilers, s.moveMembers = k.Den.Spoilers, k.Den.MoveMembers
	sounds, ok := cleanSounds(k.Sounds)
	if !ok {
		return nil, errMalformed
	}
	s.sounds = sounds
	emoji, ok := cleanEmoji(k.Emoji)
	if !ok {
		return nil, errMalformed
	}
	s.emoji, s.emojiLimits = emoji, k.Den.EmojiLimits
	for _, m := range members {
		m, ok := cleanMember(m)
		if !ok {
			return nil, errMalformed
		}
		s.members[m.ID] = m
	}
	for _, g := range k.Groups {
		g, ok := cleanGroup(g)
		if !ok {
			return nil, errMalformed
		}
		s.groups[g.ID] = g
	}
	for _, c := range k.Channels {
		c, ok := cleanChannel(c)
		if !ok {
			return nil, errMalformed
		}
		s.channels[c.ID] = c
	}
	if !ownNotes(s.channels, "") {
		return nil, errMalformed
	}
	for _, r := range k.Reads {
		r, ok := cleanRead(r)
		if !ok {
			return nil, errMalformed
		}
		s.reads[r.ChannelID] = r
	}
	for _, key := range k.Keys {
		if denproto.CheckDMKey(key) != nil {
			return nil, errMalformed
		}
		s.keys[key.ID] = key
	}
	s.identities, s.groupDMs, s.notes = k.Identities, k.GroupDMs, k.Notes
	for _, key := range k.SignedKeys {
		if !s.cleanSigned(denID, key) {
			return nil, errMalformed
		}
		s.signed[key.ID] = key
	}
	return s, nil
}

// maxCallMembers bounds the members a den says are in one call: more than
// a den lets in today, so owner settings that raise the limit later don't
// make clients refuse it.
const maxCallMembers = 1000

// cleanCall checks who a den says is in a call.
func cleanCall(c denproto.Call) bool {
	if !validID(c.ChannelID) || len(c.Members) > maxCallMembers {
		return false
	}
	seen := map[string]bool{}
	for _, m := range c.Members {
		if !validID(m.ID) || seen[m.ID] || (m.Sound && !m.Sharing) || len(m.Watching) > denproto.MaxWatching {
			return false
		}
		seen[m.ID] = true
		for i, w := range m.Watching {
			if !validID(w) || w == m.ID || slices.Contains(m.Watching[:i], w) {
				return false
			}
		}
	}
	return true
}

// maxRequests bounds the sign-ins a den says wait for approval.
const maxRequests = 16

// applyEvent updates the state for one event and returns the event as it
// may be forwarded to the page: decoded, checked and re-encoded, so no
// byte from a den reaches the page unchecked. ok is false for an event the
// client ignores.
func (s *denState) applyEvent(denID []byte, e denproto.Event, me denproto.Member) (denproto.Event, bool, error) {
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
		if old, ok := s.members[m.ID]; ok {
			for _, pic := range [][2]denproto.Image{{old.Avatar, m.Avatar}, {old.Banner, m.Banner}} {
				if pic[0].ID != "" && pic[0].ID != pic[1].ID {
					s.gone = append(s.gone, pic[0].ID) // replaced, so deleted
				}
			}
		}
		s.members[m.ID] = m
		data = m
	case denproto.EventMemberLeft:
		var l denproto.MemberLeft
		if json.Unmarshal(e.D, &l) != nil || !validID(l.ID) || l.LeftAt <= 0 {
			return e, false, errMalformed
		}
		if m, ok := s.members[l.ID]; ok {
			// Leaving takes the pictures on a profile with it.
			for _, pic := range []denproto.Image{m.Avatar, m.Banner} {
				if pic.ID != "" {
					s.gone = append(s.gone, pic.ID)
				}
			}
			m.LeftAt, m.Role, m.Avatar, m.Banner = l.LeftAt, denproto.RoleMember, denproto.Image{}, denproto.Image{}
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
	case denproto.EventVoiceState:
		var v denproto.VoiceState
		if json.Unmarshal(e.D, &v) != nil || len(v.Calls) > denproto.MaxChannels {
			return e, false, errMalformed
		}
		for _, c := range v.Calls {
			if !cleanCall(c) {
				return e, false, errMalformed
			}
		}
		if v.Full {
			clear(s.calls)
		}
		for _, c := range v.Calls {
			if len(c.Members) == 0 {
				delete(s.calls, c.ChannelID)
			} else {
				s.calls[c.ChannelID] = c
			}
		}
		if v.Calls == nil {
			v.Calls = []denproto.Call{}
		}
		data = v
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
		// The member's own notes alone, and one (M17.3).
		if c.Kind == denproto.KindNotes {
			next := maps.Clone(s.channels)
			next[c.ID] = c
			if !ownNotes(next, me.ID) {
				return e, false, errMalformed
			}
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
		var u denproto.MessageUpdated
		if json.Unmarshal(e.D, &u) != nil || denproto.CheckMessage(u.Message) != nil || len(u.Files) > denproto.MaxDMFiles {
			return e, false, errMalformed
		}
		for _, f := range u.Files {
			if !validID(f) {
				return e, false, errMalformed
			}
		}
		m := u.Message
		if e.T == denproto.EventMessageCreated {
			s.noteMessage(m, me)
		} else {
			// The files an edit took off its message are gone (M5).
			s.gone = append(s.gone, u.Files...)
		}
		data = m
	case denproto.EventMessageDeleted:
		var d denproto.MessageDeleted
		// A DM's message holds a preview beside each file (MaxDMFiles).
		if json.Unmarshal(e.D, &d) != nil || !validID(d.ID) || !validID(d.ChannelID) || len(d.Files) > denproto.MaxDMFiles {
			return e, false, errMalformed
		}
		for _, f := range d.Files {
			if !validID(f) {
				return e, false, errMalformed
			}
		}
		s.gone = append(s.gone, d.Files...)
		data = d
	case denproto.EventReactionAdded, denproto.EventReactionRemoved:
		var ch denproto.ReactionChange
		if json.Unmarshal(e.D, &ch) != nil || ch.Check() != nil {
			return e, false, errMalformed
		}
		if known, err := s.talliedIn(ch.ChannelID, false); !known || err != nil {
			return e, false, err
		}
		data = ch
	case denproto.EventReactionCleared:
		var cl denproto.ReactionCleared
		if json.Unmarshal(e.D, &cl) != nil || cl.Check() != nil {
			return e, false, errMalformed
		}
		if known, err := s.talliedIn(cl.ChannelID, false); !known || err != nil {
			return e, false, err
		}
		data = cl
	case denproto.EventPollUpdated:
		var u denproto.PollUpdated
		if json.Unmarshal(e.D, &u) != nil || u.Check() != nil {
			return e, false, errMalformed
		}
		if known, err := s.talliedIn(u.ChannelID, false); !known || err != nil {
			return e, false, err
		}
		data = u
	case denproto.EventPollEnded:
		var x denproto.PollEnded
		if json.Unmarshal(e.D, &x) != nil || x.Check() != nil {
			return e, false, errMalformed
		}
		if known, err := s.talliedIn(x.ChannelID, false); !known || err != nil {
			return e, false, err
		}
		data = x
	case denproto.EventDMReactions:
		// The connection opens it for the page.
		var d denproto.DMReactions
		if json.Unmarshal(e.D, &d) != nil || d.SealedReactions.Check() != nil || !validID(d.MessageID) {
			return e, false, errMalformed
		}
		if known, err := s.talliedIn(d.ChannelID, true); !known || err != nil {
			return e, false, err
		}
		// A group's member may have left it as their set came (M16.3).
		if c := s.channels[d.ChannelID]; c.Kind == denproto.KindDM && !slices.Contains(c.Members, d.MemberID) {
			return e, false, errMalformed
		}
		data = d
	case denproto.EventGroupChange:
		// The connection checks it, against the group as it knew it (M16.3).
		var g denproto.GroupChanged
		if json.Unmarshal(e.D, &g) != nil || !validID(g.ChannelID) || !validID(g.Change.ID) {
			return e, false, errMalformed
		}
		if c, ok := s.channels[g.ChannelID]; ok && c.Kind != denproto.KindGroupDM {
			return e, false, errMalformed
		}
		return e, false, nil
	case denproto.EventMessagesExpired:
		var x denproto.MessagesExpired
		if json.Unmarshal(e.D, &x) != nil || !validID(x.Through) {
			return e, false, errMalformed
		}
		// Every message up to it is gone, so a channel whose last message
		// was among them holds none now, and nothing in it is unread.
		for id, r := range s.reads {
			if r.LastMessage != "" && compareIDs(r.LastMessage, x.Through) <= 0 {
				r.LastMessage, r.MentionCount = "", 0
				s.reads[id] = r
				s.touched[id] = true
			}
		}
		data = x
	case denproto.EventDeviceAdded:
		var d denproto.Device
		if json.Unmarshal(e.D, &d) != nil || denproto.CheckDevice(d) != nil {
			return e, false, errMalformed
		}
		d.Current = false
		data = d
	case denproto.EventDeviceRemoved:
		var r denproto.DeviceRemoved
		if json.Unmarshal(e.D, &r) != nil || len(r.KeyID) != denproto.IDSize {
			return e, false, errMalformed
		}
		data = r
	case denproto.EventDMKey:
		var k denproto.DMKey
		if json.Unmarshal(e.D, &k) != nil || denproto.CheckDMKey(k) != nil || s.channels[k.ChannelID].Kind != denproto.KindDM {
			return e, false, errMalformed
		}
		k.Exchange = nil
		s.keys[k.ID] = k
		data = pageKey(k)
	case denproto.EventDMSignedKey:
		// The page hears of it as of any of a DM's keys (M16.1). One of a
		// channel it doesn't know, as a group this member just left, is
		// passed over (M16.3).
		var k denproto.SignedKey
		if json.Unmarshal(e.D, &k) != nil || denproto.CheckSignedKey(denID, k) != nil {
			return e, false, errMalformed
		}
		if _, known := s.channels[k.ChannelID]; !known {
			return e, false, nil
		}
		if !s.cleanSigned(denID, k) || s.keys[k.ID].ID != "" {
			return e, false, errMalformed
		}
		s.signed[k.ID] = k
		out, err := denproto.NewEvent(denproto.EventDMKey, e.Seq, pageSignedKey(k))
		return out, err == nil, err
	case denproto.EventDeviceRequest:
		var q denproto.DeviceRequest
		if json.Unmarshal(e.D, &q) != nil || denproto.CheckDeviceRequest(q) != nil {
			return e, false, errMalformed
		}
		if _, ok := s.requests[q.ID.String()]; !ok && len(s.requests) >= maxRequests {
			return e, false, errMalformed
		}
		s.requests[q.ID.String()] = q
		// The page hears of it through the den's status.
		return e, false, nil
	case denproto.EventDeviceRequestEnded:
		var end denproto.DeviceRequestEnded
		if json.Unmarshal(e.D, &end) != nil || len(end.ID) != denproto.RequestIDSize {
			return e, false, errMalformed
		}
		delete(s.requests, end.ID.String())
		return e, false, nil
	case denproto.EventSoundCreated, denproto.EventSoundUpdated:
		var sound denproto.Sound
		if json.Unmarshal(e.D, &sound) != nil || denproto.CheckSound(sound) != nil {
			return e, false, errMalformed
		}
		if _, ok := s.sounds[sound.ID]; !ok && len(s.sounds) >= denproto.MaxSounds {
			return e, false, errMalformed
		}
		s.sounds[sound.ID] = sound
		data = sound
	case denproto.EventSoundDeleted:
		var d denproto.SoundDeleted
		if json.Unmarshal(e.D, &d) != nil || !validID(d.ID) {
			return e, false, errMalformed
		}
		delete(s.sounds, d.ID)
		data = d
	case denproto.EventEmojiCreated, denproto.EventEmojiUpdated:
		var emoji denproto.Emoji
		if json.Unmarshal(e.D, &emoji) != nil || denproto.CheckEmoji(emoji) != nil {
			return e, false, errMalformed
		}
		// Two renames' events may cross, so a name may name two emoji for a
		// moment, which the page takes as it comes.
		old, ok := s.emoji[emoji.ID]
		if !ok && len(s.emoji) >= denproto.MaxEmojis {
			return e, false, errMalformed
		}
		if ok && old.Image.ID != emoji.Image.ID {
			s.gone = append(s.gone, old.Image.ID)
		}
		s.emoji[emoji.ID] = emoji
		data = emoji
	case denproto.EventEmojiDeleted:
		var d denproto.EmojiDeleted
		if json.Unmarshal(e.D, &d) != nil || !validID(d.ID) {
			return e, false, errMalformed
		}
		// Its image goes with it.
		if old, ok := s.emoji[d.ID]; ok {
			s.gone = append(s.gone, old.Image.ID)
		}
		delete(s.emoji, d.ID)
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

// talliedIn says whether the channel of an event that changes a message's
// tallies is one the member sees, of the kind its event is for: a DM for a
// sealed set of reactions, a text channel for the rest, reactions' (M15.3)
// and polls' (M15.4). One it doesn't know, as one deleted while the event
// was on its way, is passed over.
func (s *denState) talliedIn(channel string, dm bool) (bool, error) {
	c, ok := s.channels[channel]
	if !ok {
		return false, nil
	}
	if isSealed(c) != dm || c.Kind == denproto.KindVoice {
		return false, errMalformed
	}
	return true, nil
}

// marks are a den's channels and DMs with messages unread, and the
// mentions waiting in them (M13.1).
type marks struct {
	unread, mentions int
}

// marks counts what the member hasn't read, as the page's list marks it:
// text channels and DMs, the DMs they closed aside, and nothing in their
// notes, which are all their own (M17.3).
func (s *denState) marks() marks {
	var m marks
	for id, r := range s.reads {
		if c := s.channels[id]; !hasMessages(c) || r.Closed || c.Kind == denproto.KindNotes {
			continue
		}
		if compareIDs(r.LastMessage, r.ReadPosition) > 0 {
			m.unread++
		}
		m.mentions += r.MentionCount
	}
	return m
}

// liveKey returns a DM's live key, if it has one.
func (s *denState) liveKey(channel string) (denproto.DMKey, bool) {
	for _, k := range s.keys {
		if k.ChannelID == channel && k.Retired == "" {
			return k, true
		}
	}
	return denproto.DMKey{}, false
}

// noteMessage keeps unread state current as messages arrive: the channel's
// last message moves, and a mention of this member counts unless it's
// their own message, staff's @everyone among them (M15.4); in a DM, as the
// den counts, every message does. A page showing the channel marks it
// read, and the den's read_state.updated resets the count.
func (s *denState) noteMessage(m denproto.Message, me denproto.Member) {
	r, ok := s.reads[m.ChannelID]
	if !ok {
		return
	}
	if compareIDs(m.ID, r.LastMessage) > 0 {
		r.LastMessage = m.ID
	}
	dm := isSealed(s.channels[m.ChannelID])
	if dm {
		r.Closed = false // a new message reopens a closed DM, as on the den
	}
	// Everything in the member's notes is their own (M17.3).
	if m.AuthorID == me.ID || s.channels[m.ChannelID].Kind == denproto.KindNotes {
		r.ReadPosition = m.ID
	} else if dm || m.Everyone || slices.Contains(denproto.Mentions(m.Text), strings.ToLower(me.Username)) {
		r.MentionCount++
	}
	s.reads[m.ChannelID] = r
	s.touched[m.ChannelID] = true
}
