package denclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Group DMs. A group's members are what its changes make them,
// each change checked here as it comes: that it follows the one before by
// the group's rules, and that its signer, a member who could make it, is
// the identity this Dens kept for them. Someone the den lists without a
// member's signed add gets no key and shows nowhere. A change that doesn't
// check marks the group broken: this Dens makes no key for it, and sends
// nothing there.
//
// A group has a key for each membership: the member whose change makes
// one makes its key, sealed to each member's identity, as a DM's is
// (signedkeys.go); after a member leaves, or the den takes one out, the
// first remaining member's Dens to send makes it. This service sends under
// a group's key only where its statement names the group's members as its
// changes make them, with the identities kept now. Messages and each
// member's set of reactions are signed inside what's sealed by whoever
// wrote them, and the group's name is sealed under its key.

// maxShownChanges bounds the changes of each group the page gets, the
// latest, for the lines that show where its members changed.
const maxShownChanges = 100

// groupState is a group DM as this member's Dens knows it from its
// changes: the latest one's number and hash, its members in the order they
// joined, the changes the page shows, and why its changes don't check, if
// they don't.
type groupState struct {
	N       int            `json:"n"`
	Hash    denproto.Bytes `json:"hash"`
	Members []string       `json:"members"`
	Changes []shownChange  `json:"changes"`
	Broken  string         `json:"broken,omitempty"`
	// Retry says the change that broke it was signed by an identity this
	// Dens couldn't place: one a member had before they started over, which
	// the record their other devices write may bring, and with it the
	// group's changes are checked again from the start, as Recheck says.
	Retry   bool `json:"retry,omitempty"`
	Recheck bool `json:"recheck,omitempty"`
	// Name is the name the latest checked change that carries one gave, its
	// making or a naming, nil for none; Named says one came since this
	// member joined, which a member added since the last naming lacks, who
	// reads the name a key carries. Names says the state keeps them, which
	// one v1.15.0 kept doesn't, so its changes are checked again.
	Name  *denproto.SealedName `json:"name,omitempty"`
	Named bool                 `json:"named,omitempty"`
	Names bool                 `json:"names,omitempty"`
}

// groupMark is the latest change of a group this Dens checked, which it
// keeps for good, beside the kinds of channels, the group gone or not: a
// den that shows the group from before it, as from before a removal, is
// breaking the protocol.
type groupMark struct {
	N    int            `json:"n"`
	Hash denproto.Bytes `json:"hash"`
}

// whyRolledBack is a group the den showed from before the latest change
// this Dens checked.
const whyRolledBack = "the den showed the group's changes from before those this Dens checked"

// shownChange is a change the page shows, with its name as it came,
// sealed, which opens once its key is here.
type shownChange struct {
	PageChange
	Sealed *denproto.SealedName `json:"sealed,omitempty"`
}

func (g *groupState) state() denproto.GroupState {
	return denproto.GroupState{N: g.N, Hash: g.Hash, Members: g.Members}
}

func (g *groupState) clone() *groupState {
	c := *g
	c.Hash, c.Members, c.Changes = slices.Clone(g.Hash), slices.Clone(g.Members), slices.Clone(g.Changes)
	return &c
}

// PageChange is a change to a group as the page shows it: who made it,
// none for the den's, whom it took in or out, and a name, where this Dens
// could open it.
type PageChange struct {
	N        int      `json:"n"`
	ID       string   `json:"id"`
	At       int64    `json:"at"`
	Kind     string   `json:"kind"`
	By       string   `json:"by,omitempty"`
	Subjects []string `json:"subjects,omitempty"`
	Name     *string  `json:"name,omitempty"`
}

// PageGroup is a group DM as the page sees it: its members as its changes
// make them, in the order they joined, the first removing others; its name,
// "" for none; its latest changes; why its changes don't check, if they
// don't; and whether the den holds changes this Dens hasn't checked yet.
type PageGroup struct {
	Members []string     `json:"members"`
	Name    string       `json:"name,omitempty"`
	Changes []PageChange `json:"changes"`
	Broken  string       `json:"broken,omitempty"`
	Syncing bool         `json:"syncing,omitempty"`
}

// EventGroups is the page's event with the den's groups as this member's
// Dens knows them, whenever they change.
const EventGroups = "groups"

var (
	errGroupBroken = inputError(errors.New("this group's changes don't check out, so someone may be in the middle: nothing more is sent there"))
	errNotInGroup  = inputError(errors.New("you're not in this group"))
	errGroupsOff   = inputError(errors.New("this den runs an older Dens, which has no group DMs: its owner can update it"))
)

// isGroup reports whether a channel is one of this member's group DMs.
func (c *conn) isGroup(channel string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.den != nil && c.den.channels[channel].Kind == denproto.KindGroupDM
}

// private reports whether a channel is one of this member's DMs or group
// DMs, or their notes, whose messages and files are sealed here, or was:
// what's sent to one the den deleted fails for want of a key, where it
// would otherwise go unsealed.
func (c *conn) private(channel string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.privateLocked(channel)
}

// privateLocked is private, for a caller that holds c.mu.
func (c *conn) privateLocked(channel string) bool {
	return c.den != nil && (isSealed(c.den.channels[channel]) || isSealed(denproto.Channel{Kind: c.den.kinds[channel]}))
}

// holdKind keeps the kind of a channel the den just answered with, a DM
// opened, the notes or a group reserved, ahead of its event, and refuses
// one this Dens held as another kind (see keepsKind).
func (c *conn) holdKind(id, kind string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.den == nil {
		return ErrNotStarted
	}
	if !c.den.keepsKind(denproto.Channel{ID: id, Kind: kind}) {
		return errMalformed
	}
	c.stateDirty = true
	return nil
}

// isNotes reports whether a channel is this member's notes.
func (c *conn) isNotes(channel string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.den != nil && c.den.channels[channel].Kind == denproto.KindNotes
}

// groupsOnLocked reports whether the den has group DMs for this Dens: its
// socket speaks the protocol's version with them, and it keeps
// identities. The caller holds c.mu.
func (c *conn) groupsOnLocked() bool { return c.den != nil && c.den.groupDMs && c.den.identities }

// signerLocked checks a change's signer is the identity this Dens kept for
// the member it names as its maker, the current one or one before, or for
// this member, their own; keeping, the first time this Dens needs one,
// the identity the den publishes for them, or where they left the den and
// publish none, the one the change carries. It reports whether the change
// checks, and whether what was kept changed. The caller holds c.mu.
func (c *conn) signerLocked(channel int64, ch denproto.GroupChange) (ok, kept bool) {
	if ch.Kind == denproto.ChangeDeparted {
		return true, false
	}
	if ch.Signer == nil {
		return false, false
	}
	signer := denproto.IdentityHash(ch.Signer.KEMHash, ch.Signer.Sig)
	var hashes []denproto.Bytes
	if ch.By == c.meLocked() {
		hashes = append(append(hashes, c.ownHashLocked()), c.ids.Record.Own...)
	} else {
		k := c.ids.Record.Members[ch.By]
		if k == nil {
			published := c.den.members[ch.By].IdentityHash()
			if published == nil {
				published = signer
			}
			kept = c.ids.Record.meet(ch.By, published, c.m.wall().UnixMilli(), true)
			k = c.ids.Record.Members[ch.By]
		}
		if k != nil {
			hashes = append(append(hashes, k.Hash), k.Past...)
		}
	}
	for _, h := range hashes {
		if bytes.Equal(h, signer) {
			return denproto.VerifyChange(c.j.denID, channel, ch, h), kept
		}
	}
	return false, kept
}

// applyChangeLocked takes a change to a group: the one that took this
// member in, where this Dens knows nothing of the group yet, or the one
// after the latest it checked. One already taken is passed over, and one
// further on waits for those between, which syncing fetches. A change that
// doesn't check marks the group broken. A group that starts from before
// the latest change this Dens checked of it waits to be checked from its
// start. It reports whether the group changed, and whether the identities
// kept did. The caller holds c.mu.
func (c *conn) applyChangeLocked(channel string, ch denproto.GroupChange) (changed, kept bool) {
	cid, err := denproto.ParseID(channel)
	if err != nil {
		return false, false
	}
	g := c.groups[channel]
	if g != nil && (g.Broken != "" || g.N == 0 || ch.N <= g.N || ch.N > g.N+1) {
		return false, false
	}
	// A group this Dens holds a mark for and no state, as one listed again,
	// or one whose kept state didn't read, starts from before the mark: its
	// changes are checked from the start on the side, and stand only once
	// they reach the mark, the same.
	if mark, ok := c.marks[channel]; g == nil && ok && ch.N <= mark.N {
		c.groups[channel] = &groupState{Recheck: true, Names: true}
		c.stateDirty = true
		c.wakeGroups()
		return true, false
	}
	g, kept = c.takeLocked(cid, g, ch)
	c.groups[channel] = g
	c.markLocked(channel, g)
	// What a key may be used for depends on the members now, whose
	// identities this Dens needs, since the group's keys are sealed to them.
	clear(c.verdicts)
	c.stateDirty = true
	if g.Broken == "" && c.meetLocked(g.Members...) {
		kept = true
	}
	return true, kept
}

// takeLocked checks a change against g, a group's state as this Dens
// checked it, nil for the change that took this member in, and returns the
// state after it, g itself where there is one: the change taken, or the
// group marked broken where it doesn't check. It reports whether the
// identities kept changed. The caller holds c.mu.
func (c *conn) takeLocked(cid int64, g *groupState, ch denproto.GroupChange) (*groupState, bool) {
	var next denproto.GroupState
	var err error
	if g == nil {
		next, err = denproto.CheckStart(c.j.denID, cid, c.meLocked(), ch)
		g = &groupState{Names: true}
	} else {
		prev := g.state()
		next, err = denproto.CheckChange(c.j.denID, cid, &prev, ch)
	}
	signed, kept := false, false
	if err == nil {
		signed, kept = c.signerLocked(cid, ch)
	}
	if err != nil || !signed {
		why := "a change to the group isn't one its members could make"
		if err == nil {
			why = "a change to the group isn't signed by the member it names"
			g.Retry = true
		}
		g.Broken = why
		c.m.log.Warnf("A group DM's change %d doesn't check: %s", ch.N, why)
		return g, kept
	}
	g.N, g.Hash, g.Members = next.N, next.Hash, slices.Clone(next.Members)
	if ch.Kind == denproto.ChangeMade || ch.Kind == denproto.ChangeNamed {
		g.Name, g.Named = ch.Name, true
	}
	shown := shownChange{PageChange: PageChange{N: ch.N, ID: ch.ID, At: ch.MadeAt, Kind: ch.Kind, By: ch.By, Subjects: slices.Clone(ch.Subjects)},
		Sealed: ch.Name}
	g.Changes = append(g.Changes, shown)
	if len(g.Changes) > maxShownChanges {
		g.Changes = slices.Clone(g.Changes[len(g.Changes)-maxShownChanges:])
	}
	return g, kept
}

// markLocked keeps a group's latest checked change where it's past the
// mark. The caller holds c.mu.
func (c *conn) markLocked(channel string, g *groupState) {
	if mark := c.marks[channel]; g.N > mark.N {
		c.marks[channel] = groupMark{N: g.N, Hash: slices.Clone(g.Hash)}
	}
}

// markKeptLocked marks each group the cache kept at its latest checked
// change, which a cache from before v1.16.0, keeping no marks, needs. The
// caller holds c.mu.
func (c *conn) markKeptLocked() {
	for id, g := range c.groups {
		if g.N > 0 {
			c.markLocked(id, g)
		}
	}
}

// openNameLocked opens a group's name with the key it's sealed under, by
// that key's ID. The caller holds c.mu.
func (c *conn) openNameLocked(channel string, n denproto.SealedName) (string, bool) {
	for _, k := range c.den.signed {
		if k.ChannelID != channel || !bytes.Equal(k.KID, n.KID) {
			continue
		}
		key, locked := c.signedKeyLocked(k)
		if locked != "" {
			return "", false
		}
		defer clear(key)
		cid, _ := denproto.ParseID(channel)
		name, err := denproto.OpenGroupName(key, c.j.denID, cid, n)
		return name, err == nil
	}
	return "", false
}

// groupNameLocked is a group's name, opened, as its latest checked naming
// gave it, or for a member added since, as a key carries it, signed by the
// identity the key names for its maker; "" where it has none, or this Dens
// can't open it or hasn't checked it yet. The caller holds c.mu.
func (c *conn) groupNameLocked(channel string) string {
	g := c.groups[channel]
	if g == nil || !g.Names {
		return ""
	}
	n := g.Name
	if !g.Named {
		carried := c.den.channels[channel].SealedName
		if carried == nil || !c.signedNameLocked(channel, *carried) {
			return ""
		}
		n = carried
	}
	if n == nil {
		return ""
	}
	name, _ := c.openNameLocked(channel, *n)
	return name
}

// signedNameLocked reports whether a name a key carries is signed by the
// identity that key, one this member holds, names for its maker. The
// caller holds c.mu.
func (c *conn) signedNameLocked(channel string, n denproto.SealedName) bool {
	cid, _ := denproto.ParseID(channel)
	for _, k := range c.den.signed {
		if k.ChannelID == channel && bytes.Equal(k.KID, n.KID) {
			// The key's own check holds its maker's keys to the identity kept.
			return c.checkSignedLocked(k, false) == "" && denproto.VerifyGroupName(c.j.denID, cid, n, k.Maker.Sig)
		}
	}
	return false
}

// groupTitleLocked is what a group is called where a line of text names
// it: its name, or its other members'. The caller holds c.mu.
func (c *conn) groupTitleLocked(channel string) string {
	if name := c.groupNameLocked(channel); name != "" {
		return name
	}
	var names []string
	if g := c.groups[channel]; g != nil {
		for _, m := range g.Members {
			if m != c.meLocked() {
				names = append(names, c.den.members[m].DisplayName)
			}
		}
	}
	if len(names) == 0 {
		return "a group"
	}
	return strings.Join(names, ", ")
}

// pageGroupsLocked lists the den's groups as this member's Dens knows
// them, for the page. The caller holds c.mu.
func (c *conn) pageGroupsLocked() map[string]PageGroup {
	out := map[string]PageGroup{}
	if c.den == nil {
		return out
	}
	for id, ch := range c.den.channels {
		if ch.Kind != denproto.KindGroupDM {
			continue
		}
		pg := PageGroup{Members: []string{}, Changes: []PageChange{}, Name: c.groupNameLocked(id)}
		if g := c.groups[id]; g != nil {
			pg.Members, pg.Broken = slices.Clone(g.Members), g.Broken
			pg.Syncing = g.Broken == "" && (g.N < ch.Changes || g.Recheck || !g.Names)
			for _, sc := range g.Changes {
				pc := sc.PageChange
				if sc.Sealed != nil {
					if name, ok := c.openNameLocked(id, *sc.Sealed); ok {
						pc.Name = &name
					}
				}
				pg.Changes = append(pg.Changes, pc)
			}
		} else {
			pg.Syncing = true
		}
		out[id] = pg
	}
	return out
}

// groupsChanged tells the pages of the den's groups as they now stand,
// and keeps what this Dens kept of identities where that changed.
func (c *conn) groupsChanged(ctx context.Context, kept bool) {
	if kept {
		c.identitiesChanged(ctx)
	}
	c.mu.Lock()
	page := c.pageGroupsLocked()
	c.mu.Unlock()
	if e, err := denproto.NewEvent(EventGroups, 0, page); err == nil {
		c.m.publish(PageEvent{DenID: c.j.denID.String(), Events: []denproto.Event{e}})
	}
}

// retryGroupsLocked has the groups whose changes were signed by
// identities this Dens couldn't place checked again from the start, as the
// record brings more of them, and reports whether there were any. The
// caller holds c.mu.
func (c *conn) retryGroupsLocked() bool {
	retried := false
	for _, g := range c.groups {
		if g.Retry && !g.Recheck {
			g.Recheck, retried = true, true
		}
	}
	if retried {
		c.stateDirty = true
	}
	return retried
}

// recheck reports whether a group's changes are to be checked again from
// the start: as the record may place a signer this Dens couldn't, or as its
// state comes from before it kept names.
func (g *groupState) recheck() bool { return g.Recheck || !g.Names && g.Broken == "" }

// wakeGroups has idWork fetch the changes of groups this Dens hasn't
// checked yet.
func (c *conn) wakeGroups() {
	select {
	case c.groupDue <- struct{}{}:
	default:
	}
}

// staleGroupsLocked lists the groups whose changes the den holds more of
// than this Dens checked, or that it checks again, and forgets those this
// member isn't in any more, keeping their marks. The caller holds c.mu.
func (c *conn) staleGroupsLocked() []string {
	var out []string
	if c.den == nil {
		return nil
	}
	for id := range c.groups {
		if c.den.channels[id].Kind != denproto.KindGroupDM {
			delete(c.groups, id)
			c.stateDirty = true
		}
	}
	for id, ch := range c.den.channels {
		if ch.Kind != denproto.KindGroupDM {
			continue
		}
		if g := c.groups[id]; g == nil || g.Broken == "" && g.N < ch.Changes || g.recheck() {
			out = append(out, id)
		}
	}
	slices.SortFunc(out, compareIDs)
	return out
}

// syncGroups fetches and checks the changes of every group this Dens is
// behind on, and tells the pages if any changed.
func (c *conn) syncGroups(ctx context.Context) {
	c.mu.Lock()
	stale := c.staleGroupsLocked()
	c.mu.Unlock()
	changed, kept := false, false
	for _, id := range stale {
		c.mu.Lock()
		g := c.groups[id]
		recheck := g != nil && g.recheck()
		c.mu.Unlock()
		check := c.syncGroup
		if recheck {
			check = c.recheckGroup
		}
		ch, k, err := check(ctx, id)
		changed, kept = changed || ch, kept || k
		if err != nil && ctx.Err() == nil {
			c.m.log.Warnf("fetch a group DM's changes: %v", logged(err))
		}
	}
	if changed {
		c.groupsChanged(ctx, kept)
	}
}

// syncGroup fetches and checks a group's changes after the latest this
// Dens checked, or from the one that took this member in.
func (c *conn) syncGroup(ctx context.Context, channel string) (changed, kept bool, err error) {
	for range denproto.MaxGroupChanges/denproto.GroupChangesPage + 1 {
		c.mu.Lock()
		after := 0
		if g := c.groups[channel]; g != nil {
			if g.Broken != "" {
				c.mu.Unlock()
				return changed, kept, nil
			}
			after = g.N
		}
		c.mu.Unlock()
		var page denproto.GroupChanges
		if err := c.call(ctx, http.MethodGet, fmt.Sprintf("/api/group-dms/%s/changes?after=%d", channel, after), nil, &page); err != nil {
			return changed, kept, err
		}
		if len(page.Changes) > denproto.GroupChangesPage {
			return changed, kept, errMalformed
		}
		c.mu.Lock()
		// The den answers from the change that took this member in last: one
		// past where this Dens stands means it holds an earlier membership,
		// as one removed and added again while away would, which it starts
		// over from. One from before is the den showing the group as it was.
		if g := c.groups[channel]; g != nil && after > 0 && len(page.Changes) > 0 && page.Changes[0].N != after+1 {
			changed = true
			if page.Changes[0].N <= after {
				g.Broken = whyRolledBack
				c.m.log.Warnf("A group DM's changes come from %d, before the latest this Dens checked, %d", page.Changes[0].N, after)
				c.stateDirty = true
				c.mu.Unlock()
				return changed, kept, nil
			}
			delete(c.groups, channel)
		}
		for _, ch := range page.Changes {
			if _, err := denproto.ParseID(ch.ID); err != nil {
				c.mu.Unlock()
				return changed, kept, errMalformed
			}
			ok, k := c.applyChangeLocked(channel, ch)
			changed, kept = changed || ok, kept || k
		}
		c.mu.Unlock()
		if !page.More || len(page.Changes) == 0 {
			return changed, kept, nil
		}
	}
	return changed, kept, nil
}

// recheckGroup checks a group's changes again from the one that took this
// member in, on the side, while the group stands as this Dens held it.
// What they make takes its place only once they reach the latest change
// this Dens checked, the same, or start past it; otherwise the den showed
// the group from before it, and the group is broken.
func (c *conn) recheckGroup(ctx context.Context, channel string) (changed, kept bool, err error) {
	cid, err := denproto.ParseID(channel)
	if err != nil {
		return false, false, nil
	}
	var side *groupState
	start, hashes := 0, map[int]denproto.Bytes{}
	for range denproto.MaxGroupChanges/denproto.GroupChangesPage + 2 {
		after := 0
		if side != nil {
			after = side.N
		}
		var page denproto.GroupChanges
		if err := c.call(ctx, http.MethodGet, fmt.Sprintf("/api/group-dms/%s/changes?after=%d", channel, after), nil, &page); err != nil {
			return changed, kept, err
		}
		if len(page.Changes) > denproto.GroupChangesPage {
			return changed, kept, errMalformed
		}
		c.mu.Lock()
		for _, ch := range page.Changes {
			if _, err := denproto.ParseID(ch.ID); err != nil {
				c.mu.Unlock()
				return changed, kept, errMalformed
			}
			var k bool
			side, k = c.takeLocked(cid, side, ch)
			kept = kept || k
			if side.Broken != "" {
				break
			}
			hashes[side.N] = side.Hash
			if start == 0 {
				start = side.N
			}
		}
		// A change made meanwhile moves the mark on, which its next page
		// brings.
		if side == nil || side.Broken != "" || len(page.Changes) == 0 || !page.More && side.N >= c.marks[channel].N {
			changed = c.settleLocked(channel, side, start, hashes)
			if g := c.groups[channel]; changed && g != nil && g.Broken == "" && c.meetLocked(g.Members...) {
				kept = true
			}
			c.mu.Unlock()
			return changed, kept, nil
		}
		c.mu.Unlock()
	}
	return changed, kept, errMalformed
}

// settleLocked puts what a group's changes made, checked again, in its
// place where they reach the latest change this Dens checked, the same, or
// start past it, and otherwise marks the group as it stood broken. It
// reports whether the group changed. The caller holds c.mu.
func (c *conn) settleLocked(channel string, side *groupState, start int, hashes map[int]denproto.Bytes) bool {
	g := c.groups[channel]
	if g == nil || c.den == nil || c.den.channels[channel].Kind != denproto.KindGroupDM {
		return false
	}
	mark, marked := c.marks[channel]
	reached := !marked || start > mark.N || side != nil && side.N >= mark.N && bytes.Equal(hashes[mark.N], mark.Hash)
	switch {
	case side == nil && !marked:
		g.Recheck = false
	case side != nil && reached:
		side.Names, side.Recheck = true, false
		c.groups[channel] = side
		c.markLocked(channel, side)
	default:
		g.Recheck, g.Broken, g.Retry = false, whyRolledBack, false
		if side != nil && side.Broken != "" && side.N < mark.N {
			g.Broken, g.Retry = side.Broken, side.Retry
		}
		c.m.log.Warnf("A group DM's changes, checked again, don't reach the latest this Dens checked, %d", mark.N)
	}
	clear(c.verdicts)
	c.stateDirty = true
	return true
}

// groupLocked returns a group's state as this Dens checked it, for this
// member to change or write in. The caller holds c.mu.
func (c *conn) groupLocked(channel string) (*groupState, error) {
	if !c.groupsOnLocked() {
		return nil, errGroupsOff
	}
	g := c.groups[channel]
	if g == nil || c.den.channels[channel].Kind != denproto.KindGroupDM {
		return nil, errNotInGroup
	}
	if g.Broken != "" {
		return nil, errGroupBroken
	}
	if !slices.Contains(g.Members, c.meLocked()) {
		return nil, errNotInGroup
	}
	return g.clone(), nil
}

// identitiesFor fetches the identities of members, checks each against
// the one kept for them, keeping it the first time, and returns them by
// member. A member whose Dens publishes none, being older, or whose keys
// changed since this member verified them, stops it, as a DM's would.
func (c *conn) identitiesFor(ctx context.Context, members []string) (map[string]denproto.Identity, error) {
	if err := c.publishIdentity(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	me := c.meLocked()
	var others []string
	changed := false
	now := c.m.wall().UnixMilli()
	for _, m := range members {
		if m == me {
			continue
		}
		member := c.den.members[m]
		if member.LeftAt != 0 {
			c.mu.Unlock()
			return nil, inputError(fmt.Errorf("%s is no longer in this den", member.DisplayName))
		}
		if member.IdentityHash() == nil {
			c.mu.Unlock()
			return nil, inputError(fmt.Errorf("%s's Dens needs an update before you can write to them", member.DisplayName))
		}
		if c.ids.Record.meet(m, member.IdentityHash(), now, true) {
			changed = true
		}
		if k := c.ids.Record.Members[m]; k != nil && k.Held {
			c.mu.Unlock()
			if changed {
				c.identitiesChanged(ctx)
			}
			return nil, inputError(fmt.Errorf("%s's keys changed since you compared codes: compare them again, or send anyway", member.DisplayName))
		}
		others = append(others, m)
	}
	kept := map[string]denproto.Bytes{}
	for _, m := range others {
		kept[m] = slices.Clone(c.ids.Record.Members[m].Hash)
	}
	c.mu.Unlock()
	if changed {
		c.identitiesChanged(ctx)
	}
	out := map[string]denproto.Identity{}
	if len(others) == 0 {
		return out, nil
	}
	var list denproto.IdentityList
	if err := c.call(ctx, http.MethodGet, "/api/identities?members="+strings.Join(others, ","), nil, &list); err != nil {
		return nil, err
	}
	for _, mi := range list.Identities {
		id := mi.Identity()
		if want, ok := kept[mi.MemberID]; ok && id.Check() == nil && denproto.Equal(id.Hash(), want) {
			out[mi.MemberID] = id
		}
	}
	for _, m := range others {
		if _, ok := out[m]; !ok {
			// Their identity changed as this was asked; the change is on its way.
			return nil, inputError(fmt.Errorf("%s's keys just changed: try again in a moment", c.memberName(m)))
		}
	}
	return out, nil
}

// memberName is a member's display name here.
func (c *conn) memberName(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.den == nil {
		return "a member"
	}
	return c.den.members[id].DisplayName
}

// groupKey makes a key for a group's members, sealed to each one's
// identity and signed by this member's, carrying the group's name sealed
// under it where it has one this Dens can open.
func (c *conn) groupKey(ctx context.Context, channel string, members []string) (*denproto.SignedKeyRequest, error) {
	ids, err := c.identitiesFor(ctx, members)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	me := c.meLocked()
	own := slices.Clone(c.ownHashLocked())
	// Only the name this Dens checked goes on, never one it can't place.
	name := c.groupNameLocked(channel)
	c.mu.Unlock()
	if own == nil {
		return nil, errNoSeal
	}
	cid, _ := denproto.ParseID(channel)
	sorted := slices.SortedFunc(slices.Values(members), compareIDs)
	key, kid := denproto.Random(denproto.DMKeySize), denproto.Random(denproto.KIDSize)
	defer clear(key)
	req := denproto.SignedKeyRequest{KID: kid, Commit: denproto.KeyCommit(kid, key)}
	err = c.ownIdentity(func(ik *denproto.IdentityKeys) error {
		for _, m := range sorted {
			id, hash := ids[m], denproto.Bytes(nil)
			if m == me {
				id, hash = ik.Public(), own
			} else {
				hash = id.Hash()
			}
			mid, _ := denproto.ParseID(m)
			copied, err := denproto.SealKeyCopy(id, c.j.denID, cid, kid, mid, key)
			if err != nil {
				return err
			}
			req.Members = append(req.Members, denproto.KeyMember{MemberID: m, Identity: hash})
			req.Copies = append(req.Copies, denproto.KeyCopy{MemberID: m, Copy: copied})
		}
		s, err := req.Statement(c.j.denID, channel, me)
		if err != nil {
			return err
		}
		req.Signature = ik.Sign(s)
		return nil
	})
	if err != nil {
		return nil, err
	}
	// A name this Dens can't open stays as the den holds it, under the key
	// it was sealed with. One it carries, it signs, for a member added
	// since its naming.
	if name != "" {
		sealed, err := denproto.SealGroupName(key, c.j.denID, cid, kid, name)
		if err != nil {
			return nil, err
		}
		if err := c.ownIdentity(func(ik *denproto.IdentityKeys) error {
			sealed.Sig = ik.SignGroupName(c.j.denID, cid, sealed)
			return nil
		}); err != nil {
			return nil, err
		}
		req.Name = &sealed
	}
	return &req, nil
}

// makeGroupKey makes a group's key for its members as this Dens checked
// them, for sending, as after a member left or an identity changed. One
// another member's Dens made at the same moment wins as the den's live one.
func (c *conn) makeGroupKey(ctx context.Context, channel string) error {
	if _, _, err := c.syncGroup(ctx, channel); err != nil {
		return err
	}
	c.mu.Lock()
	g, err := c.groupLocked(channel)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	req, err := c.groupKey(ctx, channel, g.Members)
	if err != nil {
		return err
	}
	var made denproto.SignedKey
	err = c.call(ctx, http.MethodPost, "/api/dms/"+channel+"/signed-keys", req, &made)
	if denproto.IsCode(err, denproto.CodeKeyExists) {
		var keys denproto.SignedKeyList
		if err := c.call(ctx, http.MethodGet, "/api/dms/"+channel+"/signed-keys", nil, &keys); err != nil {
			return err
		}
		c.addSignedKeys(keys.Keys...)
		return nil
	}
	if err != nil {
		return err
	}
	c.addSignedKeys(made)
	return nil
}

// groupChannelWait is how long a change waits for the den's word of its
// group's channel.
const groupChannelWait = 5 * time.Second

// awaitGroupChannel waits for a group's channel where this Dens checked
// the group's changes and doesn't know the channel yet: the den's word of
// it follows the group's first change by a moment, which a change made
// straight after can beat.
func (c *conn) awaitGroupChannel(ctx context.Context, channel string) {
	known := func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.groups[channel] == nil || (c.den != nil && c.den.channels[channel].Kind == denproto.KindGroupDM)
	}
	if known() {
		return
	}
	deadline := time.NewTimer(groupChannelWait)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for !known() {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-tick.C:
		}
	}
}

// changeGroup makes a change to a group, as build says from the group as
// this Dens checked it: signed by this member's identity, with a key for
// the members after it where it makes a membership. One the den finds made
// against an earlier change is made again on the group as it now is.
func (c *conn) changeGroup(ctx context.Context, channel string, build func(g *groupState, me string) (denproto.GroupChange, error)) (denproto.GroupChange, error) {
	cid, err := denproto.ParseID(channel)
	if err != nil {
		return denproto.GroupChange{}, ErrUnknownChannel
	}
	for range 3 {
		if _, _, err := c.syncGroup(ctx, channel); err != nil {
			return denproto.GroupChange{}, err
		}
		c.awaitGroupChannel(ctx, channel)
		c.mu.Lock()
		g, err := c.groupLocked(channel)
		me := c.meLocked()
		c.mu.Unlock()
		if err != nil {
			return denproto.GroupChange{}, err
		}
		ch, err := build(g, me)
		if err != nil {
			return denproto.GroupChange{}, err
		}
		made, err := c.postChange(ctx, channel, cid, ch)
		if denproto.IsCode(err, denproto.CodeEditConflict) {
			continue
		}
		return made, err
	}
	return denproto.GroupChange{}, inputError(errors.New("the group keeps changing; try again"))
}

// postChange signs a change, with its key where it makes a membership,
// sends it, and takes the change the den kept.
func (c *conn) postChange(ctx context.Context, channel string, cid int64, ch denproto.GroupChange) (denproto.GroupChange, error) {
	req := denproto.GroupChangeRequest{}
	if ch.Kind == denproto.ChangeMade || ch.Kind == denproto.ChangeAdded || ch.Kind == denproto.ChangeRemoved {
		key, err := c.groupKey(ctx, channel, ch.After)
		if err != nil {
			return denproto.GroupChange{}, err
		}
		req.Key = key
	}
	if err := c.ownIdentity(func(ik *denproto.IdentityKeys) error {
		maker := ik.Maker()
		ch.Signer = &maker
		var err error
		ch.Signature, err = ik.SignChange(c.j.denID, cid, ch)
		return err
	}); err != nil {
		return denproto.GroupChange{}, err
	}
	req.Change = ch
	var made denproto.GroupChange
	if err := c.call(ctx, http.MethodPost, "/api/group-dms/"+channel+"/changes", req, &made); err != nil {
		return denproto.GroupChange{}, err
	}
	if req.Key != nil {
		var keys denproto.SignedKeyList
		if err := c.call(ctx, http.MethodGet, "/api/dms/"+channel+"/signed-keys", nil, &keys); err == nil {
			c.addSignedKeys(keys.Keys...)
		}
	}
	c.mu.Lock()
	changed, kept := false, false
	if c.den != nil && c.den.channels[channel].Kind == denproto.KindGroupDM {
		changed, kept = c.applyChangeLocked(channel, made)
	}
	c.mu.Unlock()
	if changed {
		c.groupsChanged(ctx, kept)
	}
	return made, nil
}

// cleanMembers checks members named for a group: IDs, each once, of the
// den's, none this member.
func (c *conn) cleanMembers(members []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	me := c.meLocked()
	for i, m := range members {
		if !validID(m) || m == me || slices.Contains(members[:i], m) {
			return inputError(errors.New("pick each member once, not yourself"))
		}
		if _, ok := c.den.members[m]; !ok {
			return inputError(errors.New("no such member"))
		}
	}
	return nil
}

// MakeGroup makes a group DM of this member and others, two to nine of
// them, with a name, "" for none, and returns its channel's ID.
func (m *Manager) MakeGroup(ctx context.Context, denID string, members []string, name string) (string, error) {
	c, err := m.find(denID)
	if err != nil {
		return "", err
	}
	if len(members) < denproto.MinGroupMembers-1 || len(members) > denproto.MaxGroupMembers-1 {
		return "", inputError(fmt.Errorf("a group has %d to %d members, you among them", denproto.MinGroupMembers, denproto.MaxGroupMembers))
	}
	if name != "" {
		if name, err = denproto.CleanName(name, denproto.MaxNameRunes); err != nil {
			return "", inputError(err)
		}
	}
	c.mu.Lock()
	on := c.groupsOnLocked()
	c.mu.Unlock()
	if !on {
		return "", errGroupsOff
	}
	if err := c.cleanMembers(members); err != nil {
		return "", err
	}
	var reserved denproto.GroupCreated
	if err := c.call(ctx, http.MethodPost, "/api/group-dms", nil, &reserved); err != nil {
		return "", err
	}
	cid, err := denproto.ParseID(reserved.ID)
	if err != nil {
		return "", errMalformed
	}
	if err := c.holdKind(reserved.ID, denproto.KindGroupDM); err != nil {
		return "", err
	}
	me := c.memberID()
	ch := denproto.GroupChange{N: 1, Prev: denproto.FirstPrev(), Kind: denproto.ChangeMade, By: me,
		Subjects: slices.Clone(members), After: append([]string{me}, members...)}
	if _, err := c.makeFirst(ctx, reserved.ID, cid, ch, name); err != nil {
		return "", err
	}
	return reserved.ID, nil
}

// makeFirst makes a group's first change, its key and its name, which
// nothing before it holds yet.
func (c *conn) makeFirst(ctx context.Context, channel string, cid int64, ch denproto.GroupChange, name string) (denproto.GroupChange, error) {
	ids, err := c.identitiesFor(ctx, ch.After)
	if err != nil {
		return denproto.GroupChange{}, err
	}
	c.mu.Lock()
	own := slices.Clone(c.ownHashLocked())
	me := c.meLocked()
	c.mu.Unlock()
	if own == nil {
		return denproto.GroupChange{}, errNoSeal
	}
	sorted := slices.SortedFunc(slices.Values(ch.After), compareIDs)
	key, kid := denproto.Random(denproto.DMKeySize), denproto.Random(denproto.KIDSize)
	defer clear(key)
	req := denproto.GroupChangeRequest{Key: &denproto.SignedKeyRequest{KID: kid, Commit: denproto.KeyCommit(kid, key)}}
	ch.Name = nil
	if name != "" {
		sealed, err := denproto.SealGroupName(key, c.j.denID, cid, kid, name)
		if err != nil {
			return denproto.GroupChange{}, err
		}
		ch.Name = &sealed
	}
	err = c.ownIdentity(func(ik *denproto.IdentityKeys) error {
		for _, m := range sorted {
			id, hash := ids[m], denproto.Bytes(nil)
			if m == me {
				id, hash = ik.Public(), own
			} else {
				hash = id.Hash()
			}
			mid, _ := denproto.ParseID(m)
			copied, err := denproto.SealKeyCopy(id, c.j.denID, cid, kid, mid, key)
			if err != nil {
				return err
			}
			req.Key.Members = append(req.Key.Members, denproto.KeyMember{MemberID: m, Identity: hash})
			req.Key.Copies = append(req.Key.Copies, denproto.KeyCopy{MemberID: m, Copy: copied})
		}
		s, err := req.Key.Statement(c.j.denID, channel, me)
		if err != nil {
			return err
		}
		req.Key.Signature = ik.Sign(s)
		maker := ik.Maker()
		ch.Signer = &maker
		ch.Signature, err = ik.SignChange(c.j.denID, cid, ch)
		return err
	})
	if err != nil {
		return denproto.GroupChange{}, err
	}
	req.Change = ch
	var made denproto.GroupChange
	if err := c.call(ctx, http.MethodPost, "/api/group-dms/"+channel+"/changes", req, &made); err != nil {
		return denproto.GroupChange{}, err
	}
	return made, nil
}

// AddToGroup adds members to a group DM, up to ten in all.
func (m *Manager) AddToGroup(ctx context.Context, denID, channel string, members []string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return inputError(errors.New("pick someone to add"))
	}
	if err := c.cleanMembers(members); err != nil {
		return err
	}
	_, err = c.changeGroup(ctx, channel, func(g *groupState, me string) (denproto.GroupChange, error) {
		for _, x := range members {
			if slices.Contains(g.Members, x) {
				return denproto.GroupChange{}, inputError(errors.New("someone you picked is in the group already"))
			}
		}
		if len(g.Members)+len(members) > denproto.MaxGroupMembers {
			return denproto.GroupChange{}, inputError(fmt.Errorf("a group holds at most %d members", denproto.MaxGroupMembers))
		}
		return denproto.GroupChange{N: g.N + 1, Prev: g.Hash, Kind: denproto.ChangeAdded, By: me, Subjects: slices.Clone(members),
			After: append(slices.Clone(g.Members), members...)}, nil
	})
	return err
}

// RemoveFromGroup removes a member from a group DM: the first of its
// members to, as the one who made it, or once they've left, the one who's
// been in it longest.
func (m *Manager) RemoveFromGroup(ctx context.Context, denID, channel, member string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	_, err = c.changeGroup(ctx, channel, func(g *groupState, me string) (denproto.GroupChange, error) {
		if g.state().Remover() != me {
			return denproto.GroupChange{}, inputError(errors.New("only the member who's been in the group longest removes others"))
		}
		if member == me || !slices.Contains(g.Members, member) {
			return denproto.GroupChange{}, inputError(errors.New("they're not in the group"))
		}
		return denproto.GroupChange{N: g.N + 1, Prev: g.Hash, Kind: denproto.ChangeRemoved, By: me, Subjects: []string{member},
			After: slices.DeleteFunc(slices.Clone(g.Members), func(x string) bool { return x == member })}, nil
	})
	return err
}

// LeaveGroup takes this member out of a group DM.
func (m *Manager) LeaveGroup(ctx context.Context, denID, channel string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	_, err = c.changeGroup(ctx, channel, func(g *groupState, me string) (denproto.GroupChange, error) {
		after := slices.DeleteFunc(slices.Clone(g.Members), func(x string) bool { return x == me })
		if after == nil {
			after = []string{}
		}
		return denproto.GroupChange{N: g.N + 1, Prev: g.Hash, Kind: denproto.ChangeLeft, By: me, After: after}, nil
	})
	return err
}

// NameGroup names a group DM, or with "" takes its name away: the name
// sealed under its live key, which this Dens makes first where it has
// none it may send under.
func (m *Manager) NameGroup(ctx context.Context, denID, channel, name string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if name != "" {
		if name, err = denproto.CleanName(name, denproto.MaxNameRunes); err != nil {
			return inputError(err)
		}
	}
	cid, err := denproto.ParseID(channel)
	if err != nil {
		return ErrUnknownChannel
	}
	_, err = c.changeGroup(ctx, channel, func(g *groupState, me string) (denproto.GroupChange, error) {
		keyID, key, err := c.sendKey(ctx, channel)
		if err != nil {
			return denproto.GroupChange{}, err
		}
		defer clear(key)
		c.mu.Lock()
		live := c.den.signed[keyID]
		c.mu.Unlock()
		sealed, err := denproto.SealGroupName(key, c.j.denID, cid, live.KID, name)
		if err != nil {
			return denproto.GroupChange{}, err
		}
		// The key may have come as the group changed.
		c.mu.Lock()
		now, err := c.groupLocked(channel)
		c.mu.Unlock()
		if err != nil {
			return denproto.GroupChange{}, err
		}
		return denproto.GroupChange{N: now.N + 1, Prev: now.Hash, Kind: denproto.ChangeNamed, By: me, After: slices.Clone(now.Members),
			Name: &sealed}, nil
	})
	return err
}
