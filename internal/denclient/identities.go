package denclient

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// Identities (M16.1). A member's identity at a den comes from their DM
// seal, so this service makes it from the seal whenever it needs it, and
// publishes its public keys as it signs in, where the den lacks them or
// holds others.
//
// It keeps the identity the den gives for someone the first time it
// needs it, as SSH keeps a host's key, and checks each one it meets after
// against what it kept: one that differs is a change, which every DM with
// that member shows where it happened, and where the member had verified
// them, the DM holds until they compare again or send anyway. Comparing
// codes sends nothing: the code comes from the identity kept here.
//
// What it kept, and which the member verified, is their record: kept here
// sealed with the data key, and on the den sealed with their seal, which
// every connected device of theirs writes every hour whether anything
// changed or not, merging what the others wrote. So a comparison reaches
// their other devices within the hour, while the den sees each device
// write once an hour whatever happened.

// DefaultRecordEvery is how often each device writes the member's record.
const DefaultRecordEvery = time.Hour

const (
	// maxPast and maxChanges bound what the record keeps of a member's
	// identities before their current one, and when it changed.
	maxPast    = 8
	maxChanges = 16
)

var errWrongCode = inputError(errors.New("those digits don't match. Check you typed what they read out to you; " +
	"if they really differ, someone may be in the middle, so compare somewhere else"))

// keptIdentity is a member's identity as this member's Dens kept it.
// KeptAt is when a device of theirs kept it, by its clock; Held means it
// changed since they verified the member, until they compare again or
// send anyway. Past are the identities kept before, oldest first, whose
// keys still open what was sealed under them, and Changes when it
// changed.
type keptIdentity struct {
	Hash     denproto.Bytes   `json:"h"`
	KeptAt   int64            `json:"t"`
	Verified bool             `json:"v,omitempty"`
	Held     bool             `json:"held,omitempty"`
	Past     []denproto.Bytes `json:"p,omitempty"`
	Changes  []int64          `json:"c,omitempty"`
}

// identityRecord is the member's record at a den: what their Dens kept for
// each member, by ID; and the member's own identities there before their
// current one, oldest first, which signed the changes to their groups they
// made before they started over (M16.3).
type identityRecord struct {
	Members map[string]*keptIdentity `json:"members"`
	Own     []denproto.Bytes         `json:"own,omitempty"`
}

// keptIdentities is the record as this device holds it, with the den's
// revision of it this device last read or wrote.
type keptIdentities struct {
	Record   identityRecord `json:"record"`
	Revision int64          `json:"revision"`
}

func newKept() *keptIdentities {
	return &keptIdentities{Record: identityRecord{Members: map[string]*keptIdentity{}}}
}

func (k *keptIdentity) clone() *keptIdentity {
	c := *k
	c.Past, c.Changes = slices.Clone(k.Past), slices.Clone(k.Changes)
	return &c
}

// withPast adds identities to those kept before, the current one aside,
// each once, keeping the latest maxPast.
func withPast(past []denproto.Bytes, current []byte, more ...denproto.Bytes) []denproto.Bytes {
	out := slices.Clone(past)
	for _, h := range more {
		if !bytes.Equal(h, current) && !slices.ContainsFunc(out, func(o denproto.Bytes) bool { return bytes.Equal(o, h) }) {
			out = append(out, h)
		}
	}
	out = slices.DeleteFunc(out, func(o denproto.Bytes) bool { return bytes.Equal(o, current) })
	if len(out) > maxPast {
		out = out[len(out)-maxPast:]
	}
	return out
}

// withChanges joins times a member's identity changed, in order, each
// once, keeping the latest maxChanges.
func withChanges(a, b []int64) []int64 {
	out := slices.Compact(slices.Sorted(slices.Values(append(slices.Clone(a), b...))))
	if len(out) > maxChanges {
		out = out[len(out)-maxChanges:]
	}
	return out
}

// mergeIdentity merges what another device kept for a member into what
// this one kept, either nil. Two that kept the same identity agree on it:
// verified where either compared, held only while both are. Two that kept
// different ones show it as a change: the one kept later stands, the other
// joins the past, and a member verified with the other is held; a change
// neither recorded since the older was kept counts from the newer.
func mergeIdentity(ours, theirs *keptIdentity) *keptIdentity {
	switch {
	case theirs == nil:
		return ours
	case ours == nil:
		return theirs.clone()
	case bytes.Equal(ours.Hash, theirs.Hash):
		out := ours.clone()
		out.KeptAt = min(ours.KeptAt, theirs.KeptAt)
		out.Verified = ours.Verified || theirs.Verified
		out.Held = ours.Held && theirs.Held && !out.Verified
		out.Past = withPast(ours.Past, ours.Hash, theirs.Past...)
		out.Changes = withChanges(ours.Changes, theirs.Changes)
		return out
	}
	newer, older := ours, theirs
	if theirs.KeptAt > ours.KeptAt || (theirs.KeptAt == ours.KeptAt && bytes.Compare(theirs.Hash, ours.Hash) > 0) {
		newer, older = theirs, ours
	}
	out := newer.clone()
	out.Past = withPast(newer.Past, newer.Hash, append(slices.Clone(older.Past), older.Hash)...)
	out.Changes = withChanges(newer.Changes, older.Changes)
	if !slices.ContainsFunc(out.Changes, func(at int64) bool { return at >= older.KeptAt }) {
		out.Changes = withChanges(out.Changes, []int64{newer.KeptAt})
	}
	out.Verified = newer.Verified
	out.Held = (newer.Held || older.Verified) && !newer.Verified
	return out
}

// merge merges another device's record into this one, and reports
// whether this one changed.
func (r *identityRecord) merge(theirs identityRecord) bool {
	changed := false
	if own := withPast(r.Own, nil, theirs.Own...); !slices.EqualFunc(own, r.Own, func(a, b denproto.Bytes) bool { return bytes.Equal(a, b) }) {
		r.Own, changed = own, true
	}
	for id, t := range theirs.Members {
		if t == nil || len(t.Hash) != denproto.IdentityHashSize || !validID(id) {
			continue
		}
		before := r.Members[id]
		after := mergeIdentity(before, t)
		if !sameKept(before, after) {
			r.Members[id] = after
			changed = true
		}
	}
	return changed
}

func sameKept(a, b *keptIdentity) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// meet checks a member's identity, as the den gives it now, against what
// was kept for them: keeping it the first time, when keep says the Dens
// needs it, and seeing a change when it differs, which drops their mark
// and holds the DM where they were verified. It reports whether what was
// kept changed.
func (r *identityRecord) meet(member string, hash []byte, now int64, keep bool) bool {
	if hash == nil {
		return false // none published: nothing to check against
	}
	k := r.Members[member]
	if k == nil {
		if !keep {
			return false
		}
		r.Members[member] = &keptIdentity{Hash: hash, KeptAt: now}
		return true
	}
	if bytes.Equal(k.Hash, hash) {
		return false
	}
	k.Past = withPast(k.Past, hash, k.Hash)
	k.Hash, k.KeptAt = hash, now
	k.Held, k.Verified = k.Held || k.Verified, false
	k.Changes = withChanges(k.Changes, []int64{now})
	return true
}

// encode writes the record for the den, within the most a record holds:
// past the most, the oldest of what it kept before go, then the oldest
// changes, then the members kept longest ago who aren't verified.
func (r *identityRecord) encode() ([]byte, error) {
	out, err := json.Marshal(r)
	if err != nil || len(out) <= denproto.MaxRecord {
		return out, err
	}
	trimmed := identityRecord{Members: map[string]*keptIdentity{}}
	for id, k := range r.Members {
		t := k.clone()
		if len(t.Past) > 1 {
			t.Past = t.Past[len(t.Past)-1:]
		}
		if len(t.Changes) > 2 {
			t.Changes = t.Changes[len(t.Changes)-2:]
		}
		trimmed.Members[id] = t
	}
	// Those kept longest ago go first, the unverified before the verified,
	// each taking what it encodes to, its ID and punctuation with it.
	ids := slices.SortedFunc(maps.Keys(trimmed.Members), func(a, b string) int {
		x, y := trimmed.Members[a], trimmed.Members[b]
		if x.Verified != y.Verified {
			if x.Verified {
				return 1
			}
			return -1
		}
		return cmp.Compare(x.KeptAt, y.KeptAt)
	})
	for {
		out, err = json.Marshal(trimmed)
		if err != nil || len(out) <= denproto.MaxRecord {
			return out, err
		}
		over := len(out) - denproto.MaxRecord
		for over > 0 && len(ids) > 0 {
			entry, _ := json.Marshal(trimmed.Members[ids[0]])
			over -= len(entry) + len(ids[0]) + 4
			delete(trimmed.Members, ids[0])
			ids = ids[1:]
		}
	}
}

func identitiesAD(denID []byte) []byte { return append([]byte("joined_dens.identities:"), denID...) }

// loadIdentities opens what this device kept of a den's record.
func loadIdentities(v *vault.Vault, denID, sealed []byte) (*keptIdentities, error) {
	k := newKept()
	if sealed == nil {
		return k, nil
	}
	plain, err := v.Open(sealed, identitiesAD(denID))
	if err != nil {
		return nil, fmt.Errorf("open the identities kept for a den: %w", err)
	}
	if err := json.Unmarshal(plain, k); err != nil {
		return nil, fmt.Errorf("read the identities kept for a den: %w", err)
	}
	if k.Record.Members == nil {
		k.Record.Members = map[string]*keptIdentity{}
	}
	return k, nil
}

// storeIdentities keeps a den's record on this device, as encoded.
func storeIdentities(ctx context.Context, db *sql.DB, v *vault.Vault, denID []byte, plain []byte) error {
	sealed, err := v.Seal(plain, identitiesAD(denID))
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `UPDATE joined_dens SET identities = ? WHERE den_id = ?`, sealed, denID); err != nil {
		return fmt.Errorf("keep the identities kept for a den: %w", err)
	}
	return nil
}

// PageIdentity is a member's identity as the page sees it: whether this
// member verified them, whether the DMs with them hold since they
// changed, and when they changed.
type PageIdentity struct {
	Verified bool    `json:"verified,omitempty"`
	Held     bool    `json:"held,omitempty"`
	Changes  []int64 `json:"changes,omitempty"`
}

// pageIdentitiesLocked lists what this member's Dens kept here for the
// page. The caller holds c.mu.
func (c *conn) pageIdentitiesLocked() map[string]PageIdentity {
	out := map[string]PageIdentity{}
	for id, k := range c.ids.Record.Members {
		out[id] = PageIdentity{Verified: k.Verified, Held: k.Held, Changes: slices.Clone(k.Changes)}
	}
	return out
}

// EventIdentities is the page's event with the identities kept at a den,
// whenever they change.
const EventIdentities = "identities"

// identitiesChanged keeps the record on this device after a change, and
// tells the pages. Keys checked against what was kept are checked again.
func (c *conn) identitiesChanged(ctx context.Context) {
	c.mu.Lock()
	clear(c.verdicts)
	page := c.pageIdentitiesLocked()
	c.mu.Unlock()
	c.keepRevision(ctx)
	if e, err := denproto.NewEvent(EventIdentities, 0, page); err == nil {
		c.m.publish(PageEvent{DenID: c.j.denID.String(), Events: []denproto.Event{e}})
	}
}

// meetLocked checks the identities the den gives against what was kept:
// keeping those of the members whose DMs this member is in, which the
// Dens needs, and seeing changes in any kept. It reports whether what was
// kept changed. The caller holds c.mu.
func (c *conn) meetLocked(members ...string) bool {
	if c.den == nil || !c.den.identities {
		return false
	}
	needed := map[string]bool{}
	me := c.meLocked()
	for _, ch := range c.den.channels {
		if ch.Kind == denproto.KindDM {
			for _, id := range ch.Members {
				if id != me {
					needed[id] = true
				}
			}
		}
	}
	// And the members of the groups this member is in, as their changes make
	// them (M16.3).
	for _, g := range c.groups {
		for _, id := range g.Members {
			if id != me {
				needed[id] = true
			}
		}
	}
	if members == nil {
		members = slices.Collect(maps.Keys(c.den.members))
	}
	now := c.m.wall().UnixMilli()
	changed := false
	for _, id := range members {
		if id == me {
			continue
		}
		if m, ok := c.den.members[id]; ok && c.ids.Record.meet(id, m.IdentityHash(), now, needed[id]) {
			changed = true
		}
	}
	return changed
}

// ownIdentity makes this member's identity here from their seal, for fn.
func (c *conn) ownIdentity(fn func(*denproto.IdentityKeys) error) error {
	return c.useSeal(func(seal []byte) error {
		k, err := denproto.DeriveIdentity(seal, c.j.denID)
		if err != nil {
			return err
		}
		defer k.Close()
		return fn(k)
	})
}

// ownHashLocked returns the hash of this member's identity here, which
// the seal makes, nil without the seal. The caller holds c.mu.
func (c *conn) ownHashLocked() denproto.Bytes {
	if c.ownHash == nil && c.j.seal != nil {
		_ = c.j.seal.Use(func(seal []byte) error {
			k, err := denproto.DeriveIdentity(seal, c.j.denID)
			if err != nil {
				return err
			}
			c.ownHash = k.Public().Hash()
			k.Close()
			return nil
		})
	}
	return c.ownHash
}

// publishIdentity publishes this member's identity where the den lacks it
// or holds another, once this device has the seal.
func (c *conn) publishIdentity(ctx context.Context) error {
	c.mu.Lock()
	ready := c.den != nil && c.den.identities && c.j.seal != nil
	own := c.ownHashLocked()
	published := c.profile.Member.Identity
	c.mu.Unlock()
	if !ready || own == nil || published == denproto.Bytes(own).String() {
		return nil
	}
	var pub denproto.Identity
	if err := c.ownIdentity(func(k *denproto.IdentityKeys) error {
		pub = k.Public()
		return nil
	}); err != nil {
		return err
	}
	var m denproto.Member
	if err := c.call(ctx, http.MethodPut, "/api/me/identity", pub, &m); err != nil {
		return err
	}
	c.mu.Lock()
	me := c.meLocked()
	c.mu.Unlock()
	// The den's member.updated says so too; this keeps the next look from
	// publishing it again meanwhile.
	if m, ok := cleanMember(m); ok && m.ID == me {
		return c.updateMe(ctx, m)
	}
	return nil
}

// pullRecord reads the member's record from the den, and merges it when
// it's newer than the one this device last read.
func (c *conn) pullRecord(ctx context.Context) (denproto.Record, error) {
	var rec denproto.Record
	if err := c.call(ctx, http.MethodGet, "/api/me/record", nil, &rec); err != nil {
		return rec, err
	}
	c.mu.Lock()
	revision := c.ids.Revision
	me, _ := denproto.ParseID(c.meLocked())
	c.pulled = true
	c.mu.Unlock()
	if rec.Revision <= revision || rec.Sealed == nil {
		if rec.Revision != revision {
			// Starting over took it, and only this device of theirs is
			// signed in to start it again; or the den sent one older than
			// this device read, which holds nothing new.
			c.mu.Lock()
			c.ids.Revision = rec.Revision
			c.mu.Unlock()
		}
		return rec, nil
	}
	var theirs keptIdentities
	err := c.useSeal(func(seal []byte) error {
		plain, err := denproto.OpenRecord(seal, c.j.denID, me, rec.Revision, rec.Sealed)
		if err != nil {
			return err
		}
		return json.Unmarshal(plain, &theirs.Record)
	})
	if errors.Is(err, errNoSeal) {
		return rec, err
	}
	if err != nil {
		// Not this seal's, or not a record: nothing in it can be merged,
		// so this device's goes over it at its next write.
		c.m.log.Warnf("A den holds a record of this member's that doesn't open: %v", err)
		c.mu.Lock()
		c.ids.Revision = rec.Revision
		c.mu.Unlock()
		return rec, nil
	}
	c.mu.Lock()
	changed := c.ids.Record.merge(theirs.Record)
	c.ids.Revision = rec.Revision
	// What another device kept may differ from what the den gives now.
	if c.meetLocked() {
		changed = true
	}
	// Groups whose changes were signed by identities this Dens couldn't
	// place are checked again with what the other devices kept (M16.3).
	retry := changed && c.retryGroupsLocked()
	c.mu.Unlock()
	if changed {
		c.identitiesChanged(ctx)
	} else {
		c.keepRevision(ctx)
	}
	if retry {
		c.wakeGroups()
	}
	return rec, nil
}

// keepRevision keeps the record, and its revision, on this device.
func (c *conn) keepRevision(ctx context.Context) {
	c.mu.Lock()
	plain, err := json.Marshal(c.ids)
	c.mu.Unlock()
	if err == nil {
		err = storeIdentities(ctx, c.m.db, c.m.v, c.j.denID, plain)
	}
	if err != nil {
		c.m.log.Errorf("keep the identities kept for a den: %v", err)
	}
}

// writeRecord writes the member's record to the den, merging what their
// other devices wrote, over the revision it merged; one written meanwhile
// is merged and written over in turn.
func (c *conn) writeRecord(ctx context.Context) error {
	for range 3 {
		rec, err := c.pullRecord(ctx)
		if err != nil {
			return err
		}
		c.mu.Lock()
		plain, err := c.ids.Record.encode()
		me, _ := denproto.ParseID(c.meLocked())
		c.mu.Unlock()
		if err != nil {
			return err
		}
		var sealed denproto.Bytes
		if err := c.useSeal(func(seal []byte) error {
			sealed, err = denproto.SealRecord(seal, c.j.denID, me, rec.Revision+1, plain)
			return err
		}); err != nil {
			return err
		}
		var wrote denproto.Record
		err = c.call(ctx, http.MethodPut, "/api/me/record", denproto.RecordWrite{Revision: rec.Revision, Sealed: sealed}, &wrote)
		if denproto.IsCode(err, denproto.CodeEditConflict) {
			continue
		}
		if err != nil {
			return err
		}
		c.mu.Lock()
		c.ids.Revision = wrote.Revision
		c.mu.Unlock()
		c.keepRevision(ctx)
		return nil
	}
	return errors.New("the member's record kept changing")
}

// idWork publishes this member's identity and reads their record as each
// snapshot comes, or a new seal does, and their other devices' writes as
// they're made; and writes the record every RecordEvery while connected,
// from a moment picked at random, so the den learns nothing from when.
func (c *conn) idWork(ctx context.Context) {
	every := c.m.RecordEvery
	timer := time.NewTimer(jitter(0, every))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.idDue:
			if !c.connectedWithIdentities() {
				continue
			}
			if err := c.publishIdentity(ctx); err != nil && ctx.Err() == nil {
				c.m.log.Warnf("publish this member's identity at a den: %v", logged(err))
			}
			if c.hasSeal() {
				if _, err := c.pullRecord(ctx); err != nil && ctx.Err() == nil {
					c.m.log.Warnf("read this member's record at a den: %v", logged(err))
				}
			}
		case <-c.groupDue:
			// Group DMs whose changes this Dens is behind on (M16.3), checked
			// once the member's record was read, which holds their own
			// identities before they started over.
			if !c.connectedWithIdentities() {
				continue
			}
			c.mu.Lock()
			pulled := c.pulled
			c.mu.Unlock()
			if !pulled && c.hasSeal() {
				if _, err := c.pullRecord(ctx); err != nil && ctx.Err() == nil {
					c.m.log.Warnf("read this member's record at a den: %v", logged(err))
				}
			}
			c.syncGroups(ctx)
		case <-timer.C:
			timer.Reset(every)
			if !c.connectedWithIdentities() || !c.hasSeal() {
				continue
			}
			if err := c.writeRecord(ctx); err != nil && ctx.Err() == nil {
				c.m.log.Warnf("write this member's record at a den: %v", logged(err))
			}
		}
	}
}

// wakeIdentities has idWork publish the identity and read the record.
func (c *conn) wakeIdentities() {
	select {
	case c.idDue <- struct{}{}:
	default:
	}
}

// connectedWithIdentities reports whether the den is connected, its
// socket open, and keeps identities.
func (c *conn) connectedWithIdentities() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ws != nil && c.den != nil && c.den.identities
}

func (c *conn) hasSeal() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.j.seal != nil
}

// CodeView is what the page shows to compare codes with a member: this
// member's own code, to read out, and whether they verified the other, or
// the DMs with them hold since their keys changed. The other's code never
// reaches the page, so a check can't pass by copying one's own screen.
type CodeView struct {
	Mine     string `json:"mine"`
	Verified bool   `json:"verified,omitempty"`
	Held     bool   `json:"held,omitempty"`
}

// codeConn finds a den and the member to compare codes with, keeping
// their identity if this is the first time it's needed.
func (m *Manager) codeConn(ctx context.Context, denID, memberID string) (*conn, CodeView, error) {
	c, err := m.find(denID)
	if err != nil {
		return nil, CodeView{}, err
	}
	c.mu.Lock()
	if c.den == nil {
		c.mu.Unlock()
		return nil, CodeView{}, ErrNotStarted
	}
	own := c.ownHashLocked()
	if own == nil {
		c.mu.Unlock()
		return nil, CodeView{}, errNoSeal
	}
	me, _ := denproto.ParseID(c.meLocked())
	view := CodeView{Mine: denproto.FormatCode(denproto.Code(own, c.j.denID, me))}
	if memberID == c.meLocked() {
		c.mu.Unlock()
		return c, view, nil
	}
	if !c.den.identities {
		c.mu.Unlock()
		return nil, view, inputError(errors.New("this den runs an older Dens, which keeps no identities to compare"))
	}
	member, ok := c.den.members[memberID]
	if !ok {
		c.mu.Unlock()
		return nil, view, inputError(errors.New("no such member"))
	}
	if member.IdentityHash() == nil {
		c.mu.Unlock()
		return nil, view, inputError(fmt.Errorf("%s's Dens needs an update before you can compare codes", member.DisplayName))
	}
	changed := c.ids.Record.meet(memberID, member.IdentityHash(), c.m.wall().UnixMilli(), true)
	k := c.ids.Record.Members[memberID]
	view.Verified, view.Held = k.Verified, k.Held
	c.mu.Unlock()
	if changed {
		c.identitiesChanged(ctx)
	}
	return c, view, nil
}

// Code returns what the page shows to compare codes with a member at a
// den, or this member's own code there.
func (m *Manager) Code(ctx context.Context, denID, memberID string) (CodeView, error) {
	_, view, err := m.codeConn(ctx, denID, memberID)
	return view, err
}

// Compare takes the digits the member typed as the other read out their
// code. A match marks them verified until their identity changes. It asks
// nothing of the den: the code comes from the identity kept here, and the
// mark reaches the member's other devices with the record's next write.
func (m *Manager) Compare(ctx context.Context, denID, memberID, digits string) (CodeView, error) {
	c, view, err := m.codeConn(ctx, denID, memberID)
	if err != nil {
		return view, err
	}
	if denproto.CodeTyped(clean(view.Mine), digits) {
		return view, inputError(errors.New("those are your own digits: type the ones they read out to you"))
	}
	id, err := denproto.ParseID(memberID)
	if err != nil {
		return view, inputError(errors.New("no such member"))
	}
	c.mu.Lock()
	k := c.ids.Record.Members[memberID]
	if k == nil || !denproto.CodeTyped(denproto.Code(k.Hash, c.j.denID, id), digits) {
		c.mu.Unlock()
		return view, errWrongCode
	}
	k.Verified, k.Held = true, false
	view.Verified, view.Held = true, false
	c.mu.Unlock()
	c.identitiesChanged(ctx)
	return view, nil
}

// SendAnyway lets the DMs with a member whose keys changed since this
// member verified them take messages again, without comparing.
func (m *Manager) SendAnyway(ctx context.Context, denID, memberID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	k := c.ids.Record.Members[memberID]
	held := k != nil && k.Held
	if held {
		k.Held = false
	}
	c.mu.Unlock()
	if held {
		c.identitiesChanged(ctx)
	}
	return nil
}

// clean takes a code's spaces out.
func clean(code string) string { return string(bytes.ReplaceAll([]byte(code), []byte(" "), nil)) }
