package denproto

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Group DMs (M16.3).
//
// A group DM is a DM of three to ten members, which any of them adds
// others to, the one who made it removes them from, and anyone leaves.
// Each change is a statement signed by the member who made it, naming the
// group's members after it in the order they joined, and the hash of the
// change before it, so the den keeps them in order and can't make up an
// add: every member's Dens checks each change against the members it had,
// that its signer was one and could make it, and knows the group's members
// from them alone. The den's own changes are removals of members who left
// the den, which take nobody in.
//
// A group has a key for each membership, sealed to its members' identities
// and signed, as a DM's is (identity.go). Its messages, and each member's
// set of reactions, are signed inside what's sealed by their author's
// identity, since every member holds the key: a member and the den
// together could otherwise write as another. Its name is sealed under its
// key, so the den never reads it.

// KindGroupDM is a group DM's channel kind.
const KindGroupDM = "group_dm"

// Events for group DMs.
const (
	// EventGroupChange is a change to a group DM, sent to its members
	// after it.
	EventGroupChange = "group.change"
)

// Limits for group DMs.
const (
	// MinGroupMembers and MaxGroupMembers bound the members a group is made
	// with, and MaxGroupMembers those it holds.
	MinGroupMembers = 3
	MaxGroupMembers = MaxKeyMembers
	// MaxGroupDMs bounds the group DMs a member is in.
	MaxGroupDMs = 100
	// MaxGroupChanges bounds a group's changes, and GroupChangesPage those
	// one request returns.
	MaxGroupChanges  = 1000
	GroupChangesPage = 200
	// MaxReactionSets bounds the members' sets of reactions a DM's or a
	// group's message carries: a group's members, and those who left it.
	MaxReactionSets = 2 * MaxGroupMembers
	// groupNamePlain is a group's name before it's sealed, padded to one
	// length, so its size says nothing of it: room for MaxNameRunes of
	// four bytes each, and its length.
	groupNamePlain = 160
	// SealedGroupNameSize is a sealed name: its nonce, the padded name and
	// its tag.
	SealedGroupNameSize = 24 + groupNamePlain + 16
	// groupSigned is what heads a group's sealed message or set: its
	// signer's ID, their identity as the hash of its X-Wing key and its
	// signing key, then the signature.
	groupSigned = 8 + IdentityHashSize + ed25519.PublicKeySize + SignatureSize
)

// The kinds of change.
const (
	ChangeMade     = "made"
	ChangeAdded    = "added"
	ChangeRemoved  = "removed"
	ChangeLeft     = "left"
	ChangeDeparted = "departed" // the den's: a member who left the den
	ChangeNamed    = "named"
)

var changeKinds = map[string]byte{ChangeMade: 1, ChangeAdded: 2, ChangeRemoved: 3, ChangeLeft: 4, ChangeDeparted: 5, ChangeNamed: 6}

const (
	groupChangeContext     = "dens-group-change-v1"
	groupNameContext       = "dens-group-name-v1"
	groupMessageContext    = "dens-group-message-v1"
	groupSignedContext     = "dens-group-signed-v1"
	groupReactionsContext  = "dens-group-reactions-v1"
	groupReactedContext    = "dens-group-reacted-v1"
	groupChangeHashContext = "dens-group-change-hash-v1"
)

// SealedName is a group's name, sealed under the key whose maker's ID is
// KID.
type SealedName struct {
	KID    Bytes `json:"kid"`
	Sealed Bytes `json:"sealed"`
}

// Check checks a sealed name's shape.
func (n SealedName) Check() error {
	if len(n.KID) != KIDSize || len(n.Sealed) != SealedGroupNameSize {
		return errors.New("a group's name is sealed oddly")
	}
	return nil
}

// GroupChange is a change to a group DM: its number in the group's
// changes, from 1, and the hash of the one before; its kind; the member
// who made it, none for the den's; the members it adds or removes; the
// group's members after it, in the order they joined; a new name; and its
// maker's identity and signature. ID and MadeAt are the den's.
type GroupChange struct {
	ID        string      `json:"id,omitempty"`
	MadeAt    int64       `json:"made_at,omitempty"`
	N         int         `json:"n"`
	Prev      Bytes       `json:"prev"`
	Kind      string      `json:"kind"`
	By        string      `json:"by,omitempty"`
	Subjects  []string    `json:"subjects,omitempty"`
	After     []string    `json:"after"`
	Name      *SealedName `json:"name,omitempty"`
	Signer    *KeyMaker   `json:"signer,omitempty"`
	Signature Bytes       `json:"signature,omitempty"`
}

// GroupChanges is a page of a group's changes, oldest first.
type GroupChanges struct {
	Changes []GroupChange `json:"changes"`
	More    bool          `json:"more"`
}

// GroupChanged is a change to a group DM as the den sends it.
type GroupChanged struct {
	ChannelID string      `json:"channel_id"`
	Change    GroupChange `json:"change"`
}

// GroupChangeRequest makes a change to a group DM: the change, signed by
// the session's member, and, for a change that makes a membership, its
// key.
type GroupChangeRequest struct {
	Change GroupChange       `json:"change"`
	Key    *SignedKeyRequest `json:"key,omitempty"`
}

// GroupCreated is a group DM reserved for its maker, which their first
// change makes.
type GroupCreated struct {
	ID string `json:"id"`
}

// bytes is the change as signed: "dens-group-change-v1" ‖ den_id ‖
// channel ‖ n ‖ prev ‖ kind ‖ by ‖ len(subjects) ‖ subject… ‖ len(after) ‖
// member… ‖ name, the name as 1 ‖ kid ‖ SHA-256(sealed), or 0.
func (c GroupChange) bytes(denID []byte, channel int64) ([]byte, error) {
	kind, ok := changeKinds[c.Kind]
	if !ok {
		return nil, errors.New("a group's change is of no known kind")
	}
	if len(denID) != IDSize || len(c.Prev) != sha256.Size || c.N < 1 {
		return nil, errors.New("a group's change is malformed")
	}
	by := int64(0)
	if c.By != "" {
		var err error
		if by, err = ParseID(c.By); err != nil {
			return nil, errors.New("a group's change names its maker oddly")
		}
	}
	b := join([]byte(groupChangeContext), denID, u64(channel), u64(int64(c.N)), c.Prev, []byte{kind}, u64(by))
	for _, list := range [][]string{c.Subjects, c.After} {
		b = append(b, u64(int64(len(list)))...)
		for _, s := range list {
			id, err := ParseID(s)
			if err != nil {
				return nil, errors.New("a group's change names a member oddly")
			}
			b = append(b, u64(id)...)
		}
	}
	if c.Name == nil {
		return append(b, 0), nil
	}
	if err := c.Name.Check(); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(c.Name.Sealed)
	return append(append(append(b, 1), c.Name.KID...), sum[:]...), nil
}

// Hash is a change's hash, which the change after it names as its prev.
func (c GroupChange) Hash(denID []byte, channel int64) (Bytes, error) {
	b, err := c.bytes(denID, channel)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	h.Write([]byte(groupChangeHashContext))
	h.Write(b)
	return h.Sum(nil), nil
}

// FirstPrev is what a group's first change names as the change before.
func FirstPrev() Bytes { return make(Bytes, sha256.Size) }

// SignChange signs a change with the identity of the member who made it.
func (k *IdentityKeys) SignChange(denID []byte, channel int64, c GroupChange) (Bytes, error) {
	b, err := c.bytes(denID, channel)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(k.sig, b), nil
}

// VerifyChange reports whether a change's signature checks against its
// signer's keys, which must be the identity whose hash is identity.
func VerifyChange(denID []byte, channel int64, c GroupChange, identity []byte) bool {
	if c.Signer == nil || len(c.Signer.KEMHash) != IdentityHashSize || len(c.Signer.Sig) != ed25519.PublicKeySize ||
		len(c.Signature) != SignatureSize || !Equal(IdentityHash(c.Signer.KEMHash, c.Signer.Sig), identity) {
		return false
	}
	b, err := c.bytes(denID, channel)
	return err == nil && ed25519.Verify(ed25519.PublicKey(c.Signer.Sig), b, c.Signature)
}

// GroupState is a group as its changes made it: the number and hash of
// its latest change, and its members in the order they joined, the first
// of whom removes members.
type GroupState struct {
	N       int
	Hash    Bytes
	Members []string
}

// Remover is the member who removes others: the one who made the group,
// and once they've left, the one who's been in it longest.
func (s GroupState) Remover() string {
	if len(s.Members) == 0 {
		return ""
	}
	return s.Members[0]
}

// Has reports whether a member is in the group.
func (s GroupState) Has(member string) bool { return slices.Contains(s.Members, member) }

// checkMembers checks a list of members: each an ID, once.
func checkMembers(ids []string) error {
	for i, id := range ids {
		if _, err := ParseID(id); err != nil || slices.Contains(ids[:i], id) {
			return errors.New("a group's change names a member oddly")
		}
	}
	return nil
}

// CheckChange checks a change's shape and that it follows from the group
// as it was, prev, by the group's rules: its number and the hash of the
// one before; a group made with three to ten members, its maker first; any
// member adding others, up to ten in all; the first of the members, the
// remover, removing another; a member leaving; the den taking out one who
// left it; and a member naming the group. Its signature is the caller's to
// check, by the identity of the member it names as its maker; a change of
// the den's has none. A group's first change follows from nothing.
func CheckChange(denID []byte, channel int64, prev *GroupState, c GroupChange) (GroupState, error) {
	if _, err := c.bytes(denID, channel); err != nil {
		return GroupState{}, err
	}
	if err := checkMembers(c.Subjects); err != nil {
		return GroupState{}, err
	}
	if err := checkMembers(c.After); err != nil {
		return GroupState{}, err
	}
	if len(c.After) > MaxGroupMembers {
		return GroupState{}, errors.New("a group holds at most ten members")
	}
	if (c.Kind == ChangeDeparted) != (c.By == "") || (c.Kind == ChangeDeparted) != (c.Signer == nil && c.Signature == nil) {
		return GroupState{}, errors.New("a group's change is signed by its maker, and the den's by nobody")
	}
	if (c.Kind == ChangeNamed) != (c.Name != nil) && c.Kind != ChangeMade {
		return GroupState{}, errors.New("only a group's naming, and its making, carry a name")
	}
	next := GroupState{N: c.N, Members: c.After}
	var err error
	if next.Hash, err = c.Hash(denID, channel); err != nil {
		return GroupState{}, err
	}
	if c.Kind == ChangeMade {
		if c.N != 1 || !Equal(c.Prev, FirstPrev()) || prev != nil {
			return GroupState{}, errors.New("a group is made by its first change")
		}
		if len(c.After) < MinGroupMembers || c.After[0] != c.By || !slices.Equal(c.Subjects, c.After[1:]) {
			return GroupState{}, errors.New("a group is made with three to ten members, its maker first")
		}
		return next, nil
	}
	if prev == nil || c.N != prev.N+1 || !Equal(c.Prev, prev.Hash) {
		return GroupState{}, errors.New("a group's change doesn't follow the one before")
	}
	without := func(gone string) []string {
		return slices.DeleteFunc(slices.Clone(prev.Members), func(m string) bool { return m == gone })
	}
	var want []string
	switch c.Kind {
	case ChangeAdded:
		if !prev.Has(c.By) || len(c.Subjects) == 0 || slices.ContainsFunc(c.Subjects, prev.Has) {
			return GroupState{}, errors.New("a member adds others who aren't in the group")
		}
		want = append(slices.Clone(prev.Members), c.Subjects...)
	case ChangeRemoved:
		if len(c.Subjects) != 1 || c.By != prev.Remover() || c.Subjects[0] == c.By || !prev.Has(c.Subjects[0]) {
			return GroupState{}, errors.New("only the group's first member removes another")
		}
		want = without(c.Subjects[0])
	case ChangeLeft:
		if len(c.Subjects) != 0 || !prev.Has(c.By) {
			return GroupState{}, errors.New("a member leaves a group they're in")
		}
		want = without(c.By)
	case ChangeDeparted:
		if len(c.Subjects) != 1 || !prev.Has(c.Subjects[0]) {
			return GroupState{}, errors.New("the den takes out one of the group's members")
		}
		want = without(c.Subjects[0])
	case ChangeNamed:
		if len(c.Subjects) != 0 || !prev.Has(c.By) {
			return GroupState{}, errors.New("a member names a group they're in")
		}
		want = prev.Members
	}
	if !slices.Equal(c.After, want) {
		return GroupState{}, errors.New("a group's change names other members after it than it makes")
	}
	return next, nil
}

// CheckStart checks the change that took a member into a group, from
// which their Dens knows it: the group made with them, or them added.
func CheckStart(denID []byte, channel int64, member string, c GroupChange) (GroupState, error) {
	if c.Kind != ChangeMade && c.Kind != ChangeAdded || !slices.Contains(c.After, member) ||
		c.Kind == ChangeAdded && !slices.Contains(c.Subjects, member) {
		return GroupState{}, errors.New("a member's first change of a group is the one that took them in")
	}
	if c.Kind == ChangeMade {
		return CheckChange(denID, channel, nil, c)
	}
	// What came before is the members who added them.
	before := slices.DeleteFunc(slices.Clone(c.After), func(m string) bool { return slices.Contains(c.Subjects, m) })
	return CheckChange(denID, channel, &GroupState{N: c.N - 1, Hash: c.Prev, Members: before}, c)
}

// groupNameAD binds a group's sealed name to the den, the group and the
// key it's sealed under.
func groupNameAD(denID []byte, channel int64, kid []byte) []byte {
	return join([]byte(groupNameContext), denID, u64(channel), kid)
}

// SealGroupName seals a group's name under its key, padded to one length:
// its length as two bytes, then the name.
func SealGroupName(key, denID []byte, channel int64, kid []byte, name string) (SealedName, error) {
	if len(name) > groupNamePlain-2 || len(kid) != KIDSize {
		return SealedName{}, errors.New("a group's name is too long")
	}
	plain := make([]byte, groupNamePlain)
	defer clear(plain)
	plain[0], plain[1] = byte(len(name)>>8), byte(len(name))
	copy(plain[2:], name)
	sealed, err := sealWith(key, plain, groupNameAD(denID, channel, kid))
	if err != nil {
		return SealedName{}, err
	}
	return SealedName{KID: append(Bytes(nil), kid...), Sealed: sealed}, nil
}

// OpenGroupName opens a group's name with the key it names, and cleans
// it, as any member wrote it. An empty name is none.
func OpenGroupName(key, denID []byte, channel int64, n SealedName) (string, error) {
	if n.Check() != nil {
		return "", ErrSealedOpen
	}
	plain, err := openWith(key, n.Sealed, groupNameAD(denID, channel, n.KID))
	if err != nil || len(plain) != groupNamePlain {
		return "", ErrSealedOpen
	}
	defer clear(plain)
	size := int(plain[0])<<8 | int(plain[1])
	if size > groupNamePlain-2 {
		return "", ErrSealedOpen
	}
	if size == 0 {
		return "", nil
	}
	name, err := CleanName(string(plain[2:2+size]), MaxNameRunes)
	if err != nil {
		return "", ErrSealedOpen
	}
	return name, nil
}

func (a MessageAD) groupBytes() []byte {
	return join([]byte(groupMessageContext), a.DenID, u64(a.Channel), u64(a.Author), u64(int64(len(a.Nonce))), a.Nonce,
		u64(a.KeyID), u64(int64(a.Revision)))
}

// signedHeader signs what's sealed, under a context, by a member's
// identity: their ID, the identity's keys, then the signature over
// context ‖ ad ‖ signer ‖ SHA-256(body).
func signedHeader(k *IdentityKeys, context string, ad []byte, signer int64, body []byte) []byte {
	sum := sha256.Sum256(body)
	sig := ed25519.Sign(k.sig, join([]byte(context), ad, u64(signer), sum[:]))
	return join(u64(signer), KEMHash(k.public.KEM), k.public.Sig, sig)
}

// checkHeader checks a signed header against what it signed, by the
// member it names, whose identity's hash identity returns, nil for one who
// can't have signed it; and returns who that is.
func checkHeader(header []byte, context string, ad []byte, body []byte, identity func(signer int64) []byte) (int64, bool) {
	if len(header) != groupSigned {
		return 0, false
	}
	signer := int64(binary.BigEndian.Uint64(header[:8]))
	rest := header[8:]
	kemHash, sigKey, sig := rest[:IdentityHashSize], rest[IdentityHashSize:IdentityHashSize+ed25519.PublicKeySize], rest[IdentityHashSize+ed25519.PublicKeySize:]
	want := identity(signer)
	if signer <= 0 || want == nil || !Equal(IdentityHash(kemHash, sigKey), want) {
		return signer, false
	}
	sum := sha256.Sum256(body)
	return signer, ed25519.Verify(ed25519.PublicKey(sigKey), join([]byte(context), ad, u64(signer), sum[:]), sig)
}

// ErrSignature is a group's message or set whose signature doesn't check
// against the identity its key names for whoever wrote it.
var ErrSignature = errors.New("its signature doesn't check")

// SealGroupMessage seals a group's message with the group's key, signed
// inside by its signer's identity: its author, or whoever changed it last,
// whose ID it names, since a tick changes it without marking it edited.
func SealGroupMessage(key []byte, ad MessageAD, signer *IdentityKeys, signerID int64, p DMPayload) (Bytes, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	body := buf.Bytes()
	plain := append(signedHeader(signer, groupSignedContext, ad.groupBytes(), signerID, body), body...)
	defer clear(plain)
	sealed, err := sealWith(key, plain, ad.groupBytes())
	if err != nil {
		return nil, err
	}
	if len(sealed) > MaxSealed {
		return nil, fmt.Errorf("a sealed message is at most %d bytes", MaxSealed)
	}
	return sealed, nil
}

// OpenGroupMessage opens a group's message, checks its signature by the
// member it names as its signer, whose identity's hash identity returns
// (the one the message's key names for them, nil for one it doesn't), and
// its payload, which another member wrote and so is untrusted. It returns
// who signed it, for the caller to check they could have.
func OpenGroupMessage(key []byte, ad MessageAD, identity func(signer int64) []byte, sealed []byte) (DMPayload, int64, error) {
	plain, err := openWith(key, sealed, ad.groupBytes())
	if err != nil || len(plain) < groupSigned {
		return DMPayload{}, 0, ErrSealedOpen
	}
	defer clear(plain)
	header, body := plain[:groupSigned], plain[groupSigned:]
	signer, ok := checkHeader(header, groupSignedContext, ad.groupBytes(), body, identity)
	if !ok {
		return DMPayload{}, signer, ErrSignature
	}
	var p DMPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return DMPayload{}, signer, ErrSealedOpen
	}
	if err := CheckDMPayload(p); err != nil {
		return DMPayload{}, signer, err
	}
	return p, signer, nil
}

func (a ReactionsAD) groupBytes() []byte {
	return join([]byte(groupReactionsContext), a.DenID, u64(a.Channel), u64(a.Message), u64(a.Member), u64(a.KeyID), u64(int64(a.Revision)))
}

// groupSetPlain is the padded set a group's sealed set holds after its
// signed header, which keeps a DM's sealed size.
const groupSetPlain = reactionsPlain - groupSigned

// SealGroupReactions seals a member's set on a group's message, signed
// inside by their identity, at a DM's sealed size.
func SealGroupReactions(key []byte, ad ReactionsAD, signer *IdentityKeys, set ReactionSet) (Bytes, error) {
	body, err := padSet(set, groupSetPlain)
	if err != nil {
		return nil, err
	}
	defer clear(body)
	plain := append(signedHeader(signer, groupReactedContext, ad.groupBytes(), ad.Member, body), body...)
	defer clear(plain)
	return sealWith(key, plain, ad.groupBytes())
}

// OpenGroupReactions opens a member's set on a group's message and checks
// its signature by that member, whose identity's hash the set's key names,
// keeping only what a set may hold, as OpenReactions does.
func OpenGroupReactions(key []byte, ad ReactionsAD, identity, sealed []byte) (ReactionSet, error) {
	if len(sealed) != SealedReactionsSize {
		return ReactionSet{}, ErrSealedOpen
	}
	plain, err := openWith(key, sealed, ad.groupBytes())
	if err != nil || len(plain) != reactionsPlain {
		return ReactionSet{}, ErrSealedOpen
	}
	defer clear(plain)
	header, body := plain[:groupSigned], plain[groupSigned:]
	if signer, ok := checkHeader(header, groupReactedContext, ad.groupBytes(), body, func(int64) []byte { return identity }); !ok || signer != ad.Member {
		return ReactionSet{}, ErrSignature
	}
	return readSet(body)
}
