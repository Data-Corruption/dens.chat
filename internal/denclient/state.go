package denclient

import (
	"cmp"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// denState is what a client knows about a den between events: its members,
// groups, channels and this member's read states. The messages themselves
// live in the page's windows, fetched from the den as needed.
type denState struct {
	members  map[string]denproto.Member
	groups   map[string]denproto.Group
	channels map[string]denproto.Channel
	// kinds holds the kind of every channel this Dens ever held, by ID,
	// those since deleted too (see keepsKind).
	kinds  map[string]string
	reads  map[string]denproto.ReadState
	online map[string]bool
	limits denproto.Limits
	// callLimits are the den's limits for calls and shares, and
	// retention the days it keeps a message, 0 for ever.
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
	// approval by ID; sealCheck identifies their DM seal.
	keys      map[string]denproto.DMKey
	requests  map[string]denproto.DeviceRequest
	sealCheck denproto.Bytes
	// signed are this member's DM keys sealed to identities, by ID, with
	// their own copies; identities says the den keeps identities at all,
	// which one older than v1.9.0 doesn't.
	signed     map[string]denproto.SignedKey
	identities bool
	// calls are who is in each voice channel's call, and starts when
	// each began on this computer's clock, as Unix milliseconds, for a den
	// that gives calls' lengths.
	calls  map[string]denproto.Call
	starts map[string]int64
	// sounds are the den's soundboard's, by ID, and soundLimits its limits
	// for them, none from a den older than the soundboard.
	sounds      map[string]denproto.Sound
	soundLimits denproto.SoundLimits
	// emoji are the den's emoji, by ID, and emojiLimits its limit for
	// them, nil from a den older than emoji.
	emoji       map[string]denproto.Emoji
	emojiLimits *denproto.EmojiLimits
	// sealedCalls says the den seals every call end to end, and takes no
	// join that doesn't.
	sealedCalls bool
	// disappearing says the den deletes a DM's messages at the time its
	// members set.
	disappearing bool
	// spoilers says the den keeps which of a channel's files went as
	// spoilers, moveMembers that staff move members between calls, away
	// that it keeps a member's Away in their call, and readAll that it
	// marks every channel read at once.
	spoilers, moveMembers, away, readAll bool
	// nameStyles says the den keeps a style for each member's name.
	nameStyles bool
	// relay says the den passes boxes between the member's devices,
	// auditLog that it keeps staff's acts for staff to read, membersSeen
	// that it tells staff when each member was last seen, forward that
	// it keeps a channel's message's forwarded mark, and forwardLinks that
	// it also keeps a channel forward's source. The cache keeps none
	// of them: they matter only while connected.
	relay, auditLog, membersSeen, forward, forwardLinks bool
	// deviceNames says a device may change its label there, and statuses
	// that members set a status.
	deviceNames, statuses bool
	// notes says the snapshot came on a socket speaking the protocol's
	// version with notes to self.
	notes bool
	// events are the den's events to come and live that the member sees,
	// by ID, each with their own mark, and eventLimits its limits for them,
	// nil from a den older than events.
	events      map[string]denproto.DenEvent
	eventLimits *denproto.EventLimits
	// roles are the den's custom roles visible to this member, by ID, and
	// hasRoles says the den supports custom roles at all.
	roles    map[string]denproto.Role
	hasRoles bool
	// collections are this member's personal collections and the shared
	// collections in channels they see, by ID, and hasCollections says the
	// den supports collections at all.
	collections    map[string]denproto.Collection
	hasCollections bool
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
	// DMKeys are the keys of this member's DMs, legacy ones and those
	// sealed to identities, without anything secret.
	DMKeys []PageKey `json:"dm_keys"`
	// Identities are the identities this member's Dens kept here, by
	// member, and KeepsIdentities whether the den keeps identities at all.
	Identities      map[string]PageIdentity `json:"identities"`
	KeepsIdentities bool                    `json:"keeps_identities"`
	// Calls are the calls with someone in them.
	Calls []PageCall `json:"calls"`
	// Sounds are the den's soundboard's, by name, and SoundLimits its
	// limits for them.
	Sounds      []denproto.Sound     `json:"sounds"`
	SoundLimits denproto.SoundLimits `json:"sound_limits"`
	// Emoji are the den's emoji, by name, and EmojiLimits its limit for
	// them, null from a den older than emoji.
	Emoji       []denproto.Emoji      `json:"emoji"`
	EmojiLimits *denproto.EmojiLimits `json:"emoji_limits"`
	// SealedCalls says the den seals its calls end to end, so the page
	// joins one only sealing what it sends.
	SealedCalls bool `json:"sealed_calls"`
	// Disappearing says the den deletes a DM's messages at the time its
	// members set, which the page offers only then.
	Disappearing bool `json:"disappearing"`
	// Spoilers says the den keeps which of a channel's files went as
	// spoilers, MoveMembers that staff move members between calls, and
	// Away that it keeps a member's Away in their call, which the page
	// offers only then.
	Spoilers    bool `json:"spoilers"`
	MoveMembers bool `json:"move_members"`
	Away        bool `json:"away"`
	// ReadAll says the den marks every channel and DM read at once, which
	// the page offers only then.
	ReadAll bool `json:"read_all"`
	// NameStyles says the den keeps a style for each member's name, which
	// the page offers only then.
	NameStyles bool `json:"name_styles"`
	// AuditLog says the den keeps staff's acts, and MembersSeen that it
	// tells staff when each member was last seen, which Manage offers
	// only then.
	AuditLog    bool `json:"audit_log"`
	MembersSeen bool `json:"members_seen"`
	// Forward says the den keeps a channel's message's forwarded mark,
	// which Forward offers its channels only then; ForwardLinks says it
	// also keeps a channel forward's source.
	Forward      bool `json:"forward"`
	ForwardLinks bool `json:"forward_links"`
	// DeviceNames says a device may change its label, which Devices offers
	// only then, and Statuses that members set a status, which their own
	// card offers only then.
	DeviceNames bool `json:"device_names"`
	Statuses    bool `json:"statuses"`
	// HasNotes says the den has notes to self for this Dens, which the page
	// offers only then.
	HasNotes bool `json:"has_notes"`
	// Events are the den's events to come and live that the member sees,
	// live first and then by their start, each with their own mark, and
	// EventLimits its limits for them, null from a den older than events,
	// which the page offers them by.
	Events      []denproto.DenEvent   `json:"events"`
	EventLimits *denproto.EventLimits `json:"event_limits"`
	// Roles are the custom roles visible to this member, by name, and
	// HasRoles says the den supports custom roles at all.
	Roles    []denproto.Role `json:"roles"`
	HasRoles bool            `json:"has_roles"`
	// Collections are this member's personal collections and the shared
	// collections in channels they see, and HasCollections says the den
	// supports collections at all.
	Collections    []denproto.Collection `json:"collections"`
	HasCollections bool                  `json:"has_collections"`
}

// PageCall is a call as the page sees it: the den's, its length given as
// StartedAt, when it began on this computer's clock, which the page's timer
// counts from, so the den's clock and this one may disagree. A den
// older than v1.13.0 gives no length, and its calls none.
type PageCall struct {
	denproto.Call
	StartedAt int64 `json:"started_at,omitempty"`
}

// pageVoiceState is voice.state as the page gets it.
type pageVoiceState struct {
	Calls []PageCall `json:"calls"`
	Full  bool       `json:"full,omitempty"`
}

func (s *denState) pageCall(c denproto.Call) PageCall {
	c.ElapsedMS = nil
	return PageCall{Call: c, StartedAt: s.starts[c.ChannelID]}
}

// startSlack is how much later than the start kept a frame's may put a
// call's: each frame's start is its arrival less its elapsed_ms, late by
// however long it took to come, so the earliest is the nearest, and the
// start kept moves only earlier, so a page's timer never steps back. One
// later by more is a new call, or a clock that was set.
const startSlack = 5 * time.Second

// timeCall keeps when a call began, as a frame carrying it arrives at now,
// against the start kept before it in kept.
func (s *denState) timeCall(c denproto.Call, kept map[string]int64, now time.Time) {
	if c.ElapsedMS == nil {
		delete(s.starts, c.ChannelID)
		return
	}
	at := now.UnixMilli() - *c.ElapsedMS
	if was, ok := kept[c.ChannelID]; ok && at >= was && at-was <= startSlack.Milliseconds() {
		at = was
	}
	s.starts[c.ChannelID] = at
}

func newState() *denState {
	return &denState{members: map[string]denproto.Member{}, groups: map[string]denproto.Group{},
		channels: map[string]denproto.Channel{}, kinds: map[string]string{}, reads: map[string]denproto.ReadState{}, online: map[string]bool{},
		touched: map[string]bool{}, keys: map[string]denproto.DMKey{}, requests: map[string]denproto.DeviceRequest{},
		signed: map[string]denproto.SignedKey{}, calls: map[string]denproto.Call{}, starts: map[string]int64{}, sounds: map[string]denproto.Sound{},
		emoji: map[string]denproto.Emoji{}, events: map[string]denproto.DenEvent{}, roles: map[string]denproto.Role{},
		collections: map[string]denproto.Collection{}}
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
		Disappearing: s.disappearing, Spoilers: s.spoilers, MoveMembers: s.moveMembers, Away: s.away, ReadAll: s.readAll, NameStyles: s.nameStyles, Events: sortedEvents(s.events),
		EventLimits: s.eventLimits, AuditLog: s.auditLog, MembersSeen: s.membersSeen, Forward: s.forward, ForwardLinks: s.forwardLinks,
		DeviceNames: s.deviceNames, Statuses: s.statuses, Roles: sortedRoles(s.roles), HasRoles: s.hasRoles,
		Collections: sortedCollections(s.collections), HasCollections: s.hasCollections}
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
	v.Calls = []PageCall{}
	for _, c := range s.calls {
		v.Calls = append(v.Calls, s.pageCall(c))
	}
	slices.SortFunc(v.Calls, func(a, b PageCall) int { return compareIDs(a.ChannelID, b.ChannelID) })
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

// sortedRoles lists the den's custom roles by name, in any case, as the den
// lists them.
func sortedRoles(roles map[string]denproto.Role) []denproto.Role {
	out := slices.Collect(maps.Values(roles))
	slices.SortFunc(out, func(a, b denproto.Role) int {
		return cmp.Or(strings.Compare(denproto.FoldName(a.Name), denproto.FoldName(b.Name)), compareIDs(a.ID, b.ID))
	})
	if out == nil {
		out = []denproto.Role{}
	}
	return out
}

// sortedCollections lists collections in creation order, breaking ties by ID.
func sortedCollections(cols map[string]denproto.Collection) []denproto.Collection {
	out := slices.Collect(maps.Values(cols))
	slices.SortFunc(out, func(a, b denproto.Collection) int {
		return cmp.Or(cmp.Compare(a.CreatedAt, b.CreatedAt), compareIDs(a.ID, b.ID))
	})
	if out == nil {
		out = []denproto.Collection{}
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
			c.GroupID == nil && c.Name == "" && c.Description == "" && !c.StaffOnly && len(c.RoleIDs) == 0 && c.Bitrate == 0
		return c, ok && cleanExpiry(c)
	}
	if c.Kind == denproto.KindNotes {
		// A member's notes name them alone, whom the state checks.
		ok := len(c.Members) == 1 && validID(c.Members[0]) && c.GroupID == nil && c.Name == "" && c.Description == "" && !c.StaffOnly &&
			len(c.RoleIDs) == 0 && c.Bitrate == 0 && c.ExpiresAfter == 0 && c.ExpiresSet == nil
		return c, ok
	}
	if c.ExpiresAfter != 0 || c.ExpiresSet != nil {
		return c, false
	}
	name, err := denproto.CleanName(c.Name, denproto.MaxNameRunes)
	if err != nil || len(c.Members) != 0 || (c.GroupID != nil && !validID(*c.GroupID)) ||
		(c.Kind != denproto.KindText && c.Kind != denproto.KindVoice) || denproto.CheckDescription(c.Description) != nil {
		return c, false
	}
	if denproto.CheckRoleIDs(c.RoleIDs) != nil || (c.StaffOnly && len(c.RoleIDs) > 0) {
		return c, false
	}
	if len(c.RoleIDs) == 0 {
		c.RoleIDs = nil
	} else {
		c.RoleIDs = slices.Clone(c.RoleIDs)
	}
	// A voice channel's bitrate is one staff could set, and a text
	// channel has none.
	if (c.Kind == denproto.KindVoice) != (denproto.CheckVoiceBitrate(c.Bitrate) == nil) {
		return c, false
	}
	c.Name = name
	return c, true
}

// keepsKind reports whether a channel has the kind this Dens ever held
// its ID as, and holds it to that kind from here on. A den never gives an
// ID out twice, and a DM taken for a text channel, relabeled or deleted
// and made again, would have the member's next message there go unsealed.
func (s *denState) keepsKind(c denproto.Channel) bool {
	if kind, ok := s.kinds[c.ID]; ok && kind != c.Kind {
		return false
	}
	s.kinds[c.ID] = c.Kind
	return true
}

// olderGroup reports whether a channel is a group DM, which an older den
// lists and this Dens drops unread, its ID keeping the kind (see
// keepsKind); ok is false where its ID was held as another kind.
func (s *denState) olderGroup(c denproto.Channel) (group, ok bool) {
	if c.Kind != denproto.KindGroupDM {
		return false, true
	}
	return true, validID(c.ID) && s.keepsKind(c)
}

// dropped reports whether a channel's ID was a group DM's, whose events,
// keys and messages an older den sends this Dens drops unread.
func (s *denState) dropped(channel string) bool { return s.kinds[channel] == denproto.KindGroupDM }

// cleanExpiry checks a DM's time: up to four weeks, in any whole number of
// seconds, which a development instance's den takes, and who set it, and
// when.
func cleanExpiry(c denproto.Channel) bool {
	return c.ExpiresAfter >= 0 && c.ExpiresAfter <= denproto.MaxExpiresAfter &&
		(c.ExpiresSet == nil || validID(c.ExpiresSet.By) && c.ExpiresSet.At > 0)
}

// hasMessages reports whether a channel holds messages, as text channels,
// DMs and notes do.
func hasMessages(c denproto.Channel) bool {
	return c.Kind == denproto.KindText || isSealed(c)
}

// isSealed reports whether a channel's messages are sealed by its
// members' Dens: a DM's, or the member's notes; or were, as an older den's
// group DM's, whose ID nothing goes to unsealed.
func isSealed(c denproto.Channel) bool {
	return c.Kind == denproto.KindDM || c.Kind == denproto.KindGroupDM || c.Kind == denproto.KindNotes
}

// ownNotes checks the notes among a den's channels: those of the member
// whose ID is me, once at most. With me empty, as the cache's
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
// retention period, its soundboard's limits and its limits for emoji and
// events, which a den older than the soundboard, emoji or
// events doesn't send. The den checked them when they were set, so
// any that don't check are the den misbehaving.
func cleanLimits(d denproto.Den) bool {
	return denproto.CheckLimits(d.Limits) == nil && denproto.CheckCallLimits(d.CallLimits) == nil &&
		denproto.CheckRetention(d.Retention) == nil &&
		(d.SoundLimits == denproto.SoundLimits{} || denproto.CheckSoundLimits(d.SoundLimits) == nil) &&
		(d.EmojiLimits == nil || denproto.CheckEmojiLimits(*d.EmojiLimits) == nil) &&
		(d.EventLimits == nil || denproto.CheckEventLimits(*d.EventLimits) == nil)
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

// cleanRoles checks a den's custom roles: at most MaxRoles, each once and
// with a case-insensitively unique name.
func cleanRoles(list []denproto.Role) (map[string]denproto.Role, bool) {
	if len(list) > denproto.MaxRoles {
		return nil, false
	}
	out := map[string]denproto.Role{}
	names := map[string]bool{}
	for _, r := range list {
		if denproto.CheckRole(r) != nil || out[r.ID] != (denproto.Role{}) {
			return nil, false
		}
		key := denproto.FoldName(r.Name)
		if names[key] {
			return nil, false
		}
		out[r.ID], names[key] = r, true
	}
	return out, true
}

func pruneRoleIDs(ids []string, roles map[string]denproto.Role) []string {
	if len(ids) == 0 || len(roles) == 0 {
		return nil
	}
	var out []string
	for _, id := range ids {
		if _, ok := roles[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

func knownRoleIDs(ids []string, roles map[string]denproto.Role) bool {
	for _, id := range ids {
		if _, ok := roles[id]; !ok {
			return false
		}
	}
	return true
}

func maxCollectionsInScope(channelID string) int {
	if channelID == "" {
		return denproto.MaxPersonalCollections
	}
	return denproto.MaxChannelCollections
}

// cleanCollections checks a den's collections against its visible text
// channels: valid fields, unique IDs, case-insensitively unique names per
// scope and within per-scope caps.
func cleanCollections(list []denproto.Collection, channels map[string]denproto.Channel) (map[string]denproto.Collection, bool) {
	out := map[string]denproto.Collection{}
	counts := map[string]int{}
	names := map[string]map[string]bool{}
	for _, c := range list {
		if denproto.CheckCollection(c) != nil || out[c.ID].ID != "" {
			return nil, false
		}
		if c.ChannelID != "" {
			ch, ok := channels[c.ChannelID]
			if !ok || ch.Kind != denproto.KindText {
				return nil, false
			}
		}
		counts[c.ChannelID]++
		if counts[c.ChannelID] > maxCollectionsInScope(c.ChannelID) {
			return nil, false
		}
		if names[c.ChannelID] == nil {
			names[c.ChannelID] = map[string]bool{}
		}
		key := denproto.FoldName(c.Name)
		if names[c.ChannelID][key] {
			return nil, false
		}
		names[c.ChannelID][key] = true
		out[c.ID] = c
	}
	return out, true
}

func (s *denState) collectionScopeCount(channelID string) int {
	n := 0
	for _, c := range s.collections {
		if c.ChannelID == channelID {
			n++
		}
	}
	return n
}

// load replaces the state with a snapshot of the den with denID, which
// arrived at now. Anything malformed in it is a protocol violation. A
// snapshot that sends only the members changed since the client's list
// changes that list, prev, which the client named on the socket's
// upgrade; kinds are those of the channels this Dens held before, which
// hold; starts are when the calls it held began, which a call going on
// keeps.
func (s *denState) load(denID []byte, r denproto.Ready, prev map[string]denproto.Member, kinds map[string]string, starts map[string]int64,
	now time.Time) error {
	next := newState()
	maps.Copy(next.kinds, kinds)
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
	next.spoilers, next.moveMembers, next.away, next.nameStyles = r.Den.Spoilers, r.Den.MoveMembers, r.Den.Away, r.Den.NameStyles
	next.readAll = r.Den.ReadAll
	next.relay, next.auditLog, next.membersSeen, next.forward, next.forwardLinks = r.Den.Relay, r.Den.AuditLog, r.Den.MembersSeen, r.Den.Forward, r.Den.ForwardLinks
	next.deviceNames, next.statuses = r.Den.DeviceNames, r.Den.Status
	next.hasRoles, next.hasCollections = r.Den.Roles, r.Den.Collections
	roles, ok := cleanRoles(r.Roles)
	if !ok {
		return errMalformed
	}
	next.roles = roles
	if r.Sync.MembersSince != nil {
		for id, m := range next.members {
			m.RoleIDs = pruneRoleIDs(m.RoleIDs, next.roles)
			next.members[id] = m
		}
	}
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
	events, ok := cleanEvents(r.Events, true)
	if !ok {
		return errMalformed
	}
	next.events, next.eventLimits = events, r.Den.EventLimits
	for _, m := range r.Members {
		m, ok := cleanMember(m)
		if !ok || !validID(m.ID) || !knownRoleIDs(m.RoleIDs, next.roles) {
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
		if group, ok := next.olderGroup(c); !ok {
			return errMalformed
		} else if group {
			continue
		}
		c, ok := cleanChannel(c)
		if !ok || !next.keepsKind(c) || !knownRoleIDs(c.RoleIDs, next.roles) {
			return errMalformed
		}
		next.channels[c.ID] = c
	}
	cols, ok := cleanCollections(r.Collections, next.channels)
	if !ok {
		return errMalformed
	}
	next.collections = cols
	for _, rs := range r.ReadStates {
		rs, ok := cleanRead(rs)
		if !ok {
			return errMalformed
		}
		if !next.dropped(rs.ChannelID) {
			next.reads[rs.ChannelID] = rs
		}
	}
	for _, id := range r.Online {
		if !validID(id) {
			return errMalformed
		}
		next.online[id] = true
	}
	channels := 0
	for _, c := range next.channels {
		if c.Kind != denproto.KindDM && c.Kind != denproto.KindNotes {
			channels++
		}
	}
	if !ownNotes(next.channels, r.Me.ID) {
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
	// A den older than v1.9.0 sends no signed keys at all.
	next.identities = r.SignedKeys != nil
	for _, k := range r.SignedKeys {
		if next.dropped(k.ChannelID) {
			continue
		}
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
			next.timeCall(c, starts, now)
		}
	}
	*s = *next
	return nil
}

// keptState is a den's state as the cache keeps it between runs of the
// service, for the page to show while the den is away: all of it
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
	Sounds    []denproto.Sound     `json:"sounds,omitempty"`
	Emoji     []denproto.Emoji     `json:"emoji,omitempty"`
	// The DM keys sealed to identities, and whether the den keeps
	// identities.
	SignedKeys []denproto.SignedKey `json:"signed_keys,omitempty"`
	Identities bool                 `json:"identities,omitempty"`
	// Bare says this holds only what the Dens keeps for good, Kinds, kept
	// alone where the rest of a state didn't read.
	Bare bool `json:"bare,omitempty"`
	// Whether the den has notes to self for this Dens.
	Notes bool `json:"notes,omitempty"`
	// The kind of every channel this Dens ever held, by ID, which a cache
	// from before v1.14.1 lacks.
	Kinds map[string]string `json:"kinds,omitempty"`
	// The den's events to come and live that the member saw, each
	// with their own mark, as the connection last knew them.
	Events []denproto.DenEvent `json:"events,omitempty"`
	// The custom roles visible to this member.
	Roles []denproto.Role `json:"roles,omitempty"`
	// This member's personal collections and shared collections in visible
	// channels.
	Collections []denproto.Collection `json:"collections,omitempty"`
}

// kept returns the state as the cache keeps it, and its members.
func (s *denState) kept() (keptState, []denproto.Member) {
	k := keptState{Den: denproto.Den{Limits: s.limits, CallLimits: s.callLimits, Retention: s.retention, SoundLimits: s.soundLimits,
		EmojiLimits: s.emojiLimits, EventLimits: s.eventLimits, SealedCalls: s.sealedCalls, Disappearing: s.disappearing, Spoilers: s.spoilers,
		MoveMembers: s.moveMembers, Away: s.away, ReadAll: s.readAll, NameStyles: s.nameStyles, Roles: s.hasRoles, Collections: s.hasCollections},
		Groups: slices.Collect(maps.Values(s.groups)), Channels: slices.Collect(maps.Values(s.channels)),
		Reads: slices.Collect(maps.Values(s.reads)), Keys: slices.Collect(maps.Values(s.keys)), SealCheck: s.sealCheck,
		Sounds: slices.Collect(maps.Values(s.sounds)), Emoji: slices.Collect(maps.Values(s.emoji)),
		SignedKeys: slices.Collect(maps.Values(s.signed)), Identities: s.identities, Notes: s.notes,
		Events: sortedEvents(s.events), Roles: sortedRoles(s.roles), Collections: sortedCollections(s.collections), Kinds: maps.Clone(s.kinds)}
	return k, slices.Collect(maps.Values(s.members))
}

// forGood picks out of a kept state what this Dens keeps for good, the
// kinds of its channels, entry by entry: one that doesn't check, which a
// member with the den can have kept, takes none of the others with it,
// whatever the rest of the state does.
func (k keptState) forGood() map[string]string {
	kinds := map[string]string{}
	for id, kind := range k.Kinds {
		if validID(id) {
			kinds[id] = kind
		}
	}
	return kinds
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
	s.spoilers, s.moveMembers, s.away, s.nameStyles = k.Den.Spoilers, k.Den.MoveMembers, k.Den.Away, k.Den.NameStyles
	s.readAll, s.hasRoles, s.hasCollections = k.Den.ReadAll, k.Den.Roles, k.Den.Collections
	roles, ok := cleanRoles(k.Roles)
	if !ok {
		return nil, errMalformed
	}
	s.roles = roles
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
	events, ok := cleanEvents(k.Events, false)
	if !ok {
		return nil, errMalformed
	}
	s.events, s.eventLimits = events, k.Den.EventLimits
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
	for id, kind := range k.Kinds {
		if !validID(id) {
			return nil, errMalformed
		}
		s.kinds[id] = kind
	}
	for _, c := range k.Channels {
		if group, ok := s.olderGroup(c); !ok {
			return nil, errMalformed
		} else if group {
			continue
		}
		c, ok := cleanChannel(c)
		if !ok || !s.keepsKind(c) {
			return nil, errMalformed
		}
		s.channels[c.ID] = c
	}
	if !ownNotes(s.channels, "") {
		return nil, errMalformed
	}
	cols, ok := cleanCollections(k.Collections, s.channels)
	if !ok {
		return nil, errMalformed
	}
	s.collections = cols
	for _, r := range k.Reads {
		r, ok := cleanRead(r)
		if !ok {
			return nil, errMalformed
		}
		if !s.dropped(r.ChannelID) {
			s.reads[r.ChannelID] = r
		}
	}
	for _, key := range k.Keys {
		if denproto.CheckDMKey(key) != nil {
			return nil, errMalformed
		}
		s.keys[key.ID] = key
	}
	s.identities, s.notes = k.Identities, k.Notes
	for _, key := range k.SignedKeys {
		if s.dropped(key.ChannelID) {
			continue
		}
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
	if !validID(c.ChannelID) || len(c.Members) > maxCallMembers || c.RecordMS < 0 || c.RecordMS > denproto.MaxCallMS ||
		(c.ElapsedMS != nil && (*c.ElapsedMS < 0 || *c.ElapsedMS > denproto.MaxCallMS)) {
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
func (s *denState) applyEvent(denID []byte, e denproto.Event, me denproto.Member, now time.Time) (denproto.Event, bool, error) {
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
			// member.updated goes to Everyone without RoleIDs; keep the visible ones.
			if e.T == denproto.EventMemberUpdated && m.RoleIDs == nil {
				m.RoleIDs = slices.Clone(old.RoleIDs)
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
			m.LeftAt, m.Role, m.Avatar, m.Banner, m.RoleIDs = l.LeftAt, denproto.RoleMember, denproto.Image{}, denproto.Image{}, nil
			s.members[l.ID] = m
		}
		delete(s.online, l.ID)
		data = l
	case denproto.EventRoleCreated, denproto.EventRoleUpdated:
		var role denproto.Role
		if json.Unmarshal(e.D, &role) != nil || denproto.CheckRole(role) != nil {
			return e, false, errMalformed
		}
		if _, ok := s.roles[role.ID]; !ok && len(s.roles) >= denproto.MaxRoles {
			return e, false, errMalformed
		}
		s.roles[role.ID] = role
		data = role
	case denproto.EventRoleDeleted:
		var d denproto.RoleDeleted
		if json.Unmarshal(e.D, &d) != nil || !validID(d.ID) {
			return e, false, errMalformed
		}
		delete(s.roles, d.ID)
		for id, m := range s.members {
			if slices.Contains(m.RoleIDs, d.ID) {
				m.RoleIDs = slices.DeleteFunc(slices.Clone(m.RoleIDs), func(r string) bool { return r == d.ID })
				if len(m.RoleIDs) == 0 {
					m.RoleIDs = nil
				}
				s.members[id] = m
			}
		}
		for id, ch := range s.channels {
			if slices.Contains(ch.RoleIDs, d.ID) {
				ch.RoleIDs = slices.DeleteFunc(slices.Clone(ch.RoleIDs), func(r string) bool { return r == d.ID })
				if len(ch.RoleIDs) == 0 {
					ch.RoleIDs = nil
				}
				s.channels[id] = ch
			}
		}
		data = d
	case denproto.EventMemberRolesUpdated:
		var u denproto.MemberRoles
		if json.Unmarshal(e.D, &u) != nil || !validID(u.MemberID) || denproto.CheckRoleIDs(u.RoleIDs) != nil {
			return e, false, errMalformed
		}
		if m, ok := s.members[u.MemberID]; ok {
			if len(u.RoleIDs) == 0 {
				m.RoleIDs = nil
			} else {
				m.RoleIDs = slices.Clone(u.RoleIDs)
			}
			s.members[u.MemberID] = m
		}
		data = u
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
		kept := s.starts
		if v.Full {
			clear(s.calls)
			s.starts = map[string]int64{}
		}
		page := pageVoiceState{Calls: []PageCall{}, Full: v.Full}
		for _, c := range v.Calls {
			if len(c.Members) == 0 {
				delete(s.calls, c.ChannelID)
				delete(s.starts, c.ChannelID)
			} else {
				s.calls[c.ChannelID] = c
				s.timeCall(c, kept, now)
			}
			page.Calls = append(page.Calls, s.pageCall(c))
		}
		data = page
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
		if group, ok := s.olderGroup(c); !ok {
			return e, false, errMalformed
		} else if group {
			return e, false, nil
		}
		c, ok := cleanChannel(c)
		if !ok || !s.keepsKind(c) {
			return e, false, errMalformed
		}
		// The member's own notes alone, and one.
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
		if e.T == denproto.EventChannelDeleted && s.dropped(d.ID) {
			return e, false, nil
		}
		if e.T == denproto.EventChannelDeleted {
			delete(s.channels, d.ID)
			delete(s.reads, d.ID)
			for cid, col := range s.collections {
				if col.ChannelID == d.ID {
					delete(s.collections, cid)
				}
			}
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
		if json.Unmarshal(e.D, &u) != nil {
			return e, false, errMalformed
		}
		if s.dropped(u.Message.ChannelID) {
			return e, false, nil
		}
		if denproto.CheckMessage(u.Message) != nil || len(u.Files) > denproto.MaxDMFiles {
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
			// The files an edit took off its message are gone.
			s.gone = append(s.gone, u.Files...)
		}
		data = m
	case denproto.EventMessageDeleted:
		var d denproto.MessageDeleted
		// A DM's message holds a preview beside each file (MaxDMFiles).
		if json.Unmarshal(e.D, &d) != nil || !validID(d.ID) || !validID(d.ChannelID) || len(d.Files) > denproto.MaxDMFiles {
			return e, false, errMalformed
		}
		if s.dropped(d.ChannelID) {
			return e, false, nil
		}
		for _, f := range d.Files {
			if !validID(f) {
				return e, false, errMalformed
			}
		}
		if d.LastMessage != nil && *d.LastMessage != "" && !validID(*d.LastMessage) {
			return e, false, errMalformed
		}
		s.gone = append(s.gone, d.Files...)
		if d.LastMessage != nil {
			r, ok := s.reads[d.ChannelID]
			if !ok {
				if c, known := s.channels[d.ChannelID]; known && hasMessages(c) {
					r, ok = denproto.ReadState{ChannelID: d.ChannelID}, true
				}
			}
			// Guard against an out-of-order delete regressing a newer message.
			if ok && *d.LastMessage != r.LastMessage &&
				(compareIDs(d.ID, r.LastMessage) >= 0 || compareIDs(*d.LastMessage, r.LastMessage) > 0) {
				r.LastMessage = *d.LastMessage
				s.reads[d.ChannelID] = r
				s.touched[d.ChannelID] = true
			}
		}
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
		// A DM's set is one of its members'.
		if c := s.channels[d.ChannelID]; c.Kind == denproto.KindDM && !slices.Contains(c.Members, d.MemberID) {
			return e, false, errMalformed
		}
		data = d
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
		// The page hears of it as of any of a DM's keys. One of a
		// channel it doesn't know is passed over.
		var k denproto.SignedKey
		if json.Unmarshal(e.D, &k) != nil {
			return e, false, errMalformed
		}
		if s.dropped(k.ChannelID) {
			return e, false, nil
		}
		if denproto.CheckSignedKey(denID, k) != nil {
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
	case denproto.EventEventCreated, denproto.EventEventUpdated, denproto.EventEventStarted:
		var ev denproto.DenEvent
		if json.Unmarshal(e.D, &ev) != nil || denproto.CheckDenEvent(ev) != nil || ev.Ended {
			return e, false, errMalformed
		}
		old, known := s.events[ev.ID]
		if !known && len(s.events) >= denproto.MaxEvents {
			return e, false, errMalformed
		}
		// The member's own mark comes only to their own sessions.
		if ev.Mine == nil {
			mine := known && old.Mine != nil && *old.Mine
			ev.Mine = &mine
		}
		if known && old.Banner.ID != "" && old.Banner.ID != ev.Banner.ID {
			s.gone = append(s.gone, old.Banner.ID) // replaced, so deleted
		}
		// So is a badge's picture replaced, or the badge taken off.
		if known && old.Badge != nil && (ev.Badge == nil || old.Badge.Image.ID != ev.Badge.Image.ID) {
			s.gone = append(s.gone, old.Badge.Image.ID)
		}
		s.events[ev.ID] = ev
		data = ev
	case denproto.EventEventCanceled, denproto.EventEventEnded:
		var x denproto.EventID
		if json.Unmarshal(e.D, &x) != nil || !validID(x.ID) {
			return e, false, errMalformed
		}
		// A canceled event's banner and badge go with it; an ended one's stay
		// on the den, for its card and its badge.
		if old, ok := s.events[x.ID]; ok && e.T == denproto.EventEventCanceled {
			if old.Banner.ID != "" {
				s.gone = append(s.gone, old.Banner.ID)
			}
			if old.Badge != nil {
				s.gone = append(s.gone, old.Badge.Image.ID)
			}
		}
		delete(s.events, x.ID)
		data = x
	case denproto.EventEventInterest:
		var in denproto.EventInterest
		if json.Unmarshal(e.D, &in) != nil || in.Check() != nil {
			return e, false, errMalformed
		}
		ev, ok := s.events[in.EventID]
		if !ok {
			return e, false, nil
		}
		ev.Interested = in.Interested
		if in.Mine != nil {
			ev.Mine = in.Mine
		}
		s.events[in.EventID] = ev
		data = in
	case denproto.EventCollectionCreated, denproto.EventCollectionUpdated:
		var col denproto.Collection
		if json.Unmarshal(e.D, &col) != nil || denproto.CheckCollection(col) != nil {
			return e, false, errMalformed
		}
		if col.ChannelID != "" {
			if s.dropped(col.ChannelID) {
				return e, false, nil
			}
			ch, known := s.channels[col.ChannelID]
			if !known {
				return e, false, nil
			}
			if ch.Kind != denproto.KindText {
				return e, false, errMalformed
			}
		}
		if old, known := s.collections[col.ID]; !known {
			if s.collectionScopeCount(col.ChannelID) >= maxCollectionsInScope(col.ChannelID) {
				return e, false, errMalformed
			}
		} else if old.ChannelID != col.ChannelID {
			return e, false, errMalformed
		}
		s.collections[col.ID] = col
		data = col
	case denproto.EventCollectionDeleted:
		var d denproto.CollectionDeleted
		if json.Unmarshal(e.D, &d) != nil || !validID(d.ID) || (d.ChannelID != "" && !validID(d.ChannelID)) {
			return e, false, errMalformed
		}
		delete(s.collections, d.ID)
		data = d
	case denproto.EventCollectionItemAdded, denproto.EventCollectionItemRemoved:
		var ev denproto.CollectionItemEvent
		if json.Unmarshal(e.D, &ev) != nil || denproto.CheckCollectionItemEvent(ev) != nil {
			return e, false, errMalformed
		}
		if col, ok := s.collections[ev.CollectionID]; ok {
			if ev.ChannelID != col.ChannelID {
				return e, false, errMalformed
			}
			col.Count = ev.Count
			s.collections[ev.CollectionID] = col
		}
		data = ev
	case denproto.EventReadStateUpdated:
		var r denproto.ReadState
		if json.Unmarshal(e.D, &r) != nil {
			return e, false, errMalformed
		}
		if s.dropped(r.ChannelID) {
			return e, false, nil
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
// sealed set of reactions, a text channel for the rest, reactions'
// and polls'. One it doesn't know, as one deleted while the event
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
// mentions waiting in them.
type marks struct {
	unread, mentions int
}

// marks counts what the member hasn't read, as the page's list marks it:
// text channels and DMs, the DMs they closed aside, and nothing in their
// notes, which are all their own.
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
// their own message, staff's @everyone among them; in a DM, as the
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
	// Everything in the member's notes is their own.
	if m.AuthorID == me.ID || s.channels[m.ChannelID].Kind == denproto.KindNotes {
		r.ReadPosition = m.ID
	} else if dm || m.Everyone || slices.Contains(denproto.Mentions(m.Text), strings.ToLower(me.Username)) {
		r.MentionCount++
	}
	s.reads[m.ChannelID] = r
	s.touched[m.ChannelID] = true
}
