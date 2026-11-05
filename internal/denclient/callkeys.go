package denclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// Call keys (M16.2). On a den that seals its calls, this member's page
// seals each frame it sends under a key of the member's own, and opens
// each it receives under its sender's. This service makes the member's key
// as they join, 16 random bytes, and a new one whenever anyone joins or
// leaves, so one who joins can't open what the den might have kept from
// before, nor one who left what comes after. It seals the key to each
// other member of the call, as the den shows it, as a DM's key is sealed:
// to their identity, checked against the one kept for them. It signs a
// statement naming each member it sealed to, with their identity's hash,
// and sends it over the den socket; the den passes each member their copy
// alone, and keeps the latest from each while the call lasts.
//
// Another member's key is used only once its statement names this
// member's own identity, its signature checks against the identity kept
// for its maker, and its copy opens to what the statement commits to.
//
// The page gets the call's keys whole each time one changes, since its
// stream drops what it can't take, and asks again when a frame names a key
// it lacks. The page holds a call's keys only while it lasts, as this
// service does. Keys go to the log by their IDs alone.

const (
	// rekeyGap is the least time between two keys this member's Dens makes
	// for a call: a burst of joins and leaves makes one, well within the
	// den's limit.
	rekeyGap = 500 * time.Millisecond
	// rekeyRetry is how soon a key that couldn't be sealed to every member
	// of the call, as when a member's identity couldn't be had, is made
	// again.
	rekeyRetry = 5 * time.Second
)

// CallKeys are the keys of the install's call as its page gets them: the
// member's own, and each other member's that opened here, by member.
type CallKeys struct {
	Own     *PageCallKey           `json:"own,omitempty"`
	Members map[string]PageCallKey `json:"members"`
}

// PageCallKey is a key of a call as its page gets it: the number its
// frames name it by (denproto.CallKeyNumber), and the key.
type PageCallKey struct {
	N   int            `json:"n"`
	Key denproto.Bytes `json:"key"`
}

// callKeys are the keys of the install's call while it's sealed, and what
// wakes the work that makes them.
type callKeys struct {
	mu      sync.Mutex
	own     *madeKey
	members map[string]heldKey
	// identities are those this call's keys were sealed to, by member,
	// which a key made after reuses while the den gives the same.
	identities map[string]denproto.Identity
	due        chan struct{}
	stop       chan struct{}
	stopOnce   sync.Once
}

// madeKey is this member's key for the call: whom it was sealed to, by ID
// and in order, with the identity each copy went to, and whether that was
// every other member of the call.
type madeKey struct {
	kid, key denproto.Bytes
	sealedTo []string
	hashes   map[string]denproto.Bytes
	complete bool
}

// heldKey is another member's key for the call, opened here.
type heldKey struct {
	kid, key denproto.Bytes
}

func newCallKeys() *callKeys {
	return &callKeys{members: map[string]heldKey{}, identities: map[string]denproto.Identity{},
		due: make(chan struct{}, 1), stop: make(chan struct{})}
}

// end stops the call's key work and forgets its keys.
func (k *callKeys) end() {
	k.stopOnce.Do(func() { close(k.stop) })
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.own != nil {
		clear(k.own.key)
		k.own = nil
	}
	for id, h := range k.members {
		clear(h.key)
		delete(k.members, id)
	}
}

// wake has the call's key work look at the call again.
func (k *callKeys) wake() {
	select {
	case k.due <- struct{}{}:
	default:
	}
}

// keysDue wakes the key work of the install's call on a den, as the call's
// members may have changed.
func (m *Manager) keysDue(denID string) {
	m.callMu.Lock()
	call := m.call
	m.callMu.Unlock()
	if call != nil && call.den == denID && call.keys != nil {
		call.keys.wake()
	}
}

// keyWork makes the member's keys for a sealed call as its members change,
// at most one every rekeyGap, until the call ends.
func (m *Manager) keyWork(call *activeCall) {
	k := call.keys
	var last time.Time
	retry := time.NewTimer(time.Hour)
	retry.Stop()
	defer retry.Stop()
	for {
		select {
		case <-k.stop:
			return
		case <-k.due:
		case <-retry.C:
		}
		if wait := rekeyGap - time.Since(last); wait > 0 {
			select {
			case <-k.stop:
				return
			case <-time.After(wait):
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		made, again := m.rekey(ctx, call)
		cancel()
		if made {
			last = time.Now()
		}
		if again {
			retry.Reset(rekeyRetry)
		}
	}
}

// rekey makes the member a new key for the call where it has none, or the
// one it has wasn't sealed to every other member as the den shows them
// now, and reports whether it made one, and whether to try again.
func (m *Manager) rekey(ctx context.Context, call *activeCall) (made, again bool) {
	c, err := m.find(call.den)
	if err != nil {
		return false, false
	}
	k := call.keys
	c.mu.Lock()
	if c.den == nil || !c.den.sealedCalls {
		c.mu.Unlock()
		return false, false
	}
	me := c.meLocked()
	in := false
	var others []string
	for _, x := range c.den.calls[call.channel].Members {
		if x.ID == me {
			in = true
		} else {
			others = append(others, x.ID)
		}
	}
	slices.SortFunc(others, compareIDs)
	hashes := map[string]denproto.Bytes{}
	for _, id := range others {
		hashes[id] = c.den.members[id].IdentityHash()
	}
	own := slices.Clone(c.ownHashLocked())
	c.mu.Unlock()
	if !in {
		return false, false
	}
	if own == nil {
		m.note(call, "made no key: this device doesn't have the member's DM recovery key")
		return false, false
	}
	k.mu.Lock()
	current := k.own
	var need []string
	for _, id := range others {
		if have, ok := k.identities[id]; hashes[id] != nil && (!ok || !denproto.Equal(have.Hash(), hashes[id])) {
			need = append(need, id)
		}
	}
	// The keys of members who left go: the page keeps each a while itself.
	left := false
	for id, h := range k.members {
		if !slices.Contains(others, id) {
			clear(h.key)
			delete(k.members, id)
			left = true
		}
	}
	k.mu.Unlock()
	// A key sealed to every member as the den shows them now, each at the
	// identity they have now, stands.
	if current != nil && current.complete && slices.Equal(current.sealedTo, others) &&
		!slices.ContainsFunc(others, func(id string) bool { return !denproto.Equal(current.hashes[id], hashes[id]) }) {
		if left {
			m.deliverKeys(call)
		}
		return false, false
	}
	if len(need) > 0 {
		fetched, err := c.fetchIdentities(ctx, need)
		if err != nil {
			m.log.Warnf("Den %s, call in channel %s: couldn't fetch its members' identities: %v", call.fp, call.channel, logged(err))
			return false, true
		}
		k.mu.Lock()
		for id, i := range fetched {
			if denproto.Equal(i.Hash(), hashes[id]) {
				k.identities[id] = i
			}
		}
		k.mu.Unlock()
	}
	// Each identity sealed to is kept for its member, the first time as on
	// first use, and checked against what was kept after, as for a DM.
	c.mu.Lock()
	changed := false
	now := c.m.wall().UnixMilli()
	for _, id := range others {
		if hashes[id] != nil && c.ids.Record.meet(id, hashes[id], now, true) {
			changed = true
		}
	}
	c.mu.Unlock()
	if changed {
		c.identitiesChanged(ctx)
	}
	k.mu.Lock()
	to := map[string]denproto.Identity{}
	for _, id := range others {
		if i, ok := k.identities[id]; ok && denproto.Equal(i.Hash(), hashes[id]) {
			to[id] = i
		}
	}
	prev := -1
	if current != nil {
		prev = denproto.CallKeyNumber(current.kid)
	}
	k.mu.Unlock()
	next, vk, err := c.makeCallKey(call.channel, me, others, to, prev)
	if err != nil {
		m.log.Warnf("Den %s, call in channel %s: couldn't make the member's key: %v", call.fp, call.channel, err)
		return false, true
	}
	if len(vk.Copies) > 0 {
		if err := c.sendFrame(denproto.EventVoiceKey, vk); err != nil {
			m.note(call, "couldn't send key %s: %v", next.kid, err)
		}
	}
	k.mu.Lock()
	select {
	case <-k.stop:
		k.mu.Unlock()
		clear(next.key)
		return false, false
	default:
	}
	if k.own != nil {
		clear(k.own.key)
	}
	k.own = next
	k.mu.Unlock()
	m.note(call, "made key %s, sealed to %d of its %d other members", next.kid, len(vk.Copies), len(others))
	m.deliverKeys(call)
	return true, !next.complete
}

// fetchIdentities fetches members' identities from the den, those that
// check out, by member.
func (c *conn) fetchIdentities(ctx context.Context, members []string) (map[string]denproto.Identity, error) {
	out := map[string]denproto.Identity{}
	for len(members) > 0 {
		n := min(len(members), denproto.MaxIdentities)
		var list denproto.IdentityList
		if err := c.call(ctx, http.MethodGet, "/api/identities?members="+strings.Join(members[:n], ","), nil, &list); err != nil {
			return nil, err
		}
		for _, x := range list.Identities {
			if i := x.Identity(); slices.Contains(members[:n], x.MemberID) && i.Check() == nil {
				out[x.MemberID] = i
			}
		}
		members = members[n:]
	}
	return out, nil
}

// makeCallKey makes the member's key for a call, sealed to each identity
// given, its number unlike the last key's, prev, so the page can't take
// one for the other; and the frame that sends it.
func (c *conn) makeCallKey(channel, me string, others []string, to map[string]denproto.Identity, prev int) (*madeKey, denproto.VoiceKey, error) {
	ch, err := denproto.ParseID(channel)
	if err != nil {
		return nil, denproto.VoiceKey{}, err
	}
	key := denproto.Random(denproto.CallKeySize)
	kid := denproto.Random(denproto.KIDSize)
	for denproto.CallKeyNumber(kid) == prev {
		kid = denproto.Random(denproto.KIDSize)
	}
	made := &madeKey{kid: kid, key: key, hashes: map[string]denproto.Bytes{}, complete: len(to) == len(others)}
	vk := denproto.VoiceKey{ChannelID: channel, KID: kid, Commit: denproto.CallKeyCommit(kid, key)}
	for _, id := range others {
		if i, ok := to[id]; ok {
			made.sealedTo = append(made.sealedTo, id)
			made.hashes[id] = i.Hash()
			vk.Members = append(vk.Members, denproto.KeyMember{MemberID: id, Identity: made.hashes[id]})
		}
	}
	if len(vk.Members) == 0 {
		return made, vk, nil
	}
	err = c.ownIdentity(func(ik *denproto.IdentityKeys) error {
		for _, x := range vk.Members {
			id, err := denproto.ParseID(x.MemberID)
			if err != nil {
				return err
			}
			copied, err := denproto.SealCallKeyCopy(to[x.MemberID], c.j.denID, ch, kid, id, key)
			if err != nil {
				return err
			}
			vk.Copies = append(vk.Copies, denproto.KeyCopy{MemberID: x.MemberID, Copy: copied})
		}
		s, err := vk.Statement(c.j.denID, me)
		if err != nil {
			return err
		}
		vk.Signature = ik.SignCallKey(s)
		vk.Maker = denproto.KeyMaker{KEMHash: denproto.KEMHash(ik.Public().KEM), Sig: ik.Public().Sig}
		return nil
	})
	if err != nil {
		clear(key)
		return nil, denproto.VoiceKey{}, err
	}
	return made, vk, nil
}

var errCallCommit = errors.New("a call key's copy opened to another key than its statement names")

// openCallKey checks another member's key for the install's call, and
// opens this member's copy, or says why it can't be used: its statement
// must name this member's own identity, its signature check against the
// identity kept for its maker, and its copy open to the key it commits to.
func (c *conn) openCallKey(ctx context.Context, k denproto.VoiceKeyEvent) ([]byte, string) {
	s, err := k.Statement(c.j.denID)
	if err != nil {
		return nil, "its statement is malformed"
	}
	c.mu.Lock()
	if c.den == nil {
		c.mu.Unlock()
		return nil, "the den's state isn't here yet"
	}
	me, _ := denproto.ParseID(c.meLocked())
	own := c.ownHashLocked()
	if own == nil {
		c.mu.Unlock()
		return nil, "this device doesn't have the member's DM recovery key"
	}
	if !denproto.Equal(s.Named(me), own) {
		c.mu.Unlock()
		return nil, "it names another identity for this member, so someone may be in the middle"
	}
	changed := c.ids.Record.meet(k.MemberID, c.den.members[k.MemberID].IdentityHash(), c.m.wall().UnixMilli(), true)
	var kept denproto.Bytes
	if x := c.ids.Record.Members[k.MemberID]; x != nil {
		kept = slices.Clone(x.Hash)
	}
	c.mu.Unlock()
	if changed {
		c.identitiesChanged(ctx)
	}
	if kept == nil || !denproto.Equal(kept, denproto.IdentityHash(k.Maker.KEMHash, k.Maker.Sig)) {
		return nil, "its maker isn't the identity kept for them"
	}
	if !denproto.VerifyCallKey(k.Maker.Sig, s, k.Signature) {
		return nil, "its signature doesn't check"
	}
	var key []byte
	err = c.useSeal(func(seal []byte) error {
		ik, err := denproto.DeriveIdentity(seal, c.j.denID)
		if err != nil {
			return err
		}
		defer ik.Close()
		opened, err := ik.OpenCallKeyCopy(c.j.denID, s.Channel, k.KID, me, k.Copy)
		if err != nil {
			return err
		}
		if !denproto.Equal(denproto.CallKeyCommit(k.KID, opened), k.Commit) {
			clear(opened)
			return errCallCommit
		}
		key = opened
		return nil
	})
	if err != nil {
		return nil, "its copy doesn't open to the key it names"
	}
	return key, ""
}

// takeKey takes another member's key for the install's call, as the den
// passed it, and gives the page the call's keys once it opens.
func (c *conn) takeKey(ctx context.Context, k denproto.VoiceKeyEvent) {
	m := c.m
	m.callMu.Lock()
	call := m.call
	m.callMu.Unlock()
	if call == nil || call.den != c.j.denID.String() || call.channel != k.ChannelID || call.keys == nil {
		return
	}
	key, why := c.openCallKey(ctx, k)
	if why != "" {
		m.log.Warnf("Den %s, call in channel %s: key %s from member %s was refused: %s", call.fp, call.channel, k.KID, k.MemberID, why)
		return
	}
	call.keys.mu.Lock()
	select {
	case <-call.keys.stop:
		call.keys.mu.Unlock()
		clear(key)
		return
	default:
	}
	if old, ok := call.keys.members[k.MemberID]; ok {
		clear(old.key)
	}
	call.keys.members[k.MemberID] = heldKey{kid: k.KID, key: key}
	call.keys.mu.Unlock()
	m.note(call, "key %s from member %s opened", k.KID, k.MemberID)
	m.deliverKeys(call)
}

// deliverKeys gives the call's page its keys, whole: the member's own, and
// each other member's, but those a development build withholds. They go
// with the keys' lock held, which deliver doesn't block on, so the page
// gets each set in the order they changed.
func (m *Manager) deliverKeys(call *activeCall) {
	m.callMu.Lock()
	withheld := m.withheld
	m.callMu.Unlock()
	keys := CallKeys{Members: map[string]PageCallKey{}}
	call.keys.mu.Lock()
	defer call.keys.mu.Unlock()
	if own := call.keys.own; own != nil {
		keys.Own = &PageCallKey{N: denproto.CallKeyNumber(own.kid), Key: slices.Clone(own.key)}
	}
	for id, h := range call.keys.members {
		if !withheld[call.den+"/"+id] {
			keys.Members[id] = PageCallKey{N: denproto.CallKeyNumber(h.kid), Key: slices.Clone(h.key)}
		}
	}
	call.deliver(CallEvent{DenID: call.den, Channel: call.channel, Keys: &keys})
}

// CallKeysAgain gives the page that holds a sealed call its keys again, as
// when a frame names a key it lacks.
func (m *Manager) CallKeysAgain(page, denID string) {
	if call := m.currentCall(page, denID); call != nil && call.keys != nil {
		m.deliverKeys(call)
	}
}

// Withhold keeps a den's member's keys for calls from this install's
// pages, or gives them again, so a development build's browser checks can
// show a page denied a member's keys receives their frames and hears
// nothing. Only a development instance's client listener offers it.
func (m *Manager) Withhold(denID, member string, on bool) error {
	if _, err := m.find(denID); err != nil {
		return err
	}
	if !validID(member) {
		return inputError(fmt.Errorf("no such member"))
	}
	m.callMu.Lock()
	next := map[string]bool{}
	for k := range m.withheld {
		next[k] = true
	}
	if on {
		next[denID+"/"+member] = true
	} else {
		delete(next, denID+"/"+member)
	}
	m.withheld = next
	call := m.call
	m.callMu.Unlock()
	if call != nil && call.keys != nil {
		m.deliverKeys(call)
	}
	return nil
}
