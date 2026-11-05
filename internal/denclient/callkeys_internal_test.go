package denclient

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

// callConn is a member's connection as far as a call's keys need it: their
// seal, the den's members with the identities it gives, and those this
// member's Dens kept, which agree with the den's, so checking one keeps
// nothing new.
func callConn(t *testing.T, denID []byte, me string, seal byte, members map[string]denproto.Bytes) *conn {
	t.Helper()
	var secret *vault.Secret
	if seal != 0 {
		var err error
		if secret, err = vault.NewSecret(bytes.Repeat([]byte{seal}, denproto.SealSize)); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(secret.Close)
	}
	c := &conn{m: &Manager{Now: time.Now}, j: &joined{denID: denID, seal: secret}, den: newState(), ids: newKept(),
		profile: Profile{Member: denproto.Member{ID: me}}, verdicts: map[string]string{}, copies: map[string]denproto.Bytes{}}
	c.den.sealedCalls = true
	for id, hash := range members {
		c.den.members[id] = denproto.Member{ID: id, Identity: hash.String()}
		if id != me {
			c.ids.Record.Members[id] = &keptIdentity{Hash: hash, KeptAt: 1}
		}
	}
	return c
}

func identityOf(t *testing.T, denID []byte, seal byte) *denproto.IdentityKeys {
	t.Helper()
	ik, err := denproto.DeriveIdentity(bytes.Repeat([]byte{seal}, denproto.SealSize), denID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ik.Close)
	return ik
}

// A call's key made by one member's Dens opens on another's only once its
// statement names that member's own identity, its signature checks against
// the identity kept for its maker, and its copy opens to the key it
// commits to (M16.2).
func TestCallKeyChecks(t *testing.T) {
	ctx := context.Background()
	den := bytes.Repeat([]byte{9}, denproto.IDSize)
	alice, bob, carol := identityOf(t, den, 7), identityOf(t, den, 8), identityOf(t, den, 9)
	ids := map[string]denproto.Bytes{"1": alice.Public().Hash(), "2": bob.Public().Hash(), "3": carol.Public().Hash()}
	atAlice := callConn(t, den, "1", 7, ids)
	atBob := callConn(t, den, "2", 8, ids)

	// Bob's Dens makes his key for the call, sealed to alice and carol, and
	// alice's opens her copy.
	made, vk, err := atBob.makeCallKey("5", "2", []string{"1", "3"}, map[string]denproto.Identity{"1": alice.Public(), "3": carol.Public()}, -1)
	if err != nil {
		t.Fatal(err)
	}
	if !made.complete || !slices.Equal(made.sealedTo, []string{"1", "3"}) || len(vk.Copies) != 2 || denproto.CheckVoiceKey(den, "2", vk) != nil {
		t.Fatalf("bob's key: %+v, %d copies", made, len(vk.Copies))
	}
	key, why := atAlice.openCallKey(ctx, vk.Event("2", vk.Copy("1")))
	if why != "" || !bytes.Equal(key, made.key) {
		t.Fatalf("alice opened bob's key to %x, %q", key, why)
	}
	// A key whose number a page would take for the last one's is made
	// again.
	for range 20 {
		next, _, err := atBob.makeCallKey("5", "2", []string{"1"}, map[string]denproto.Identity{"1": alice.Public()}, denproto.CallKeyNumber(made.kid))
		if err != nil || denproto.CallKeyNumber(next.kid) == denproto.CallKeyNumber(made.kid) {
			t.Fatalf("a new key took the last one's number: %v", err)
		}
	}
	// A key sealed to fewer than the call's members says so.
	partial, _, _ := atBob.makeCallKey("5", "2", []string{"1", "3"}, map[string]denproto.Identity{"1": alice.Public()}, -1)
	if partial.complete {
		t.Error("a key sealed to one of two members says it's complete")
	}

	// signed makes a key as bob's Dens would, with a change to the
	// statement before it's signed by signer.
	signed := func(signer *denproto.IdentityKeys, change func(*denproto.VoiceKey, *[]byte)) denproto.VoiceKeyEvent {
		key, kid := denproto.Random(denproto.CallKeySize), denproto.Random(denproto.KIDSize)
		sealedKey := []byte(key)
		k := denproto.VoiceKey{ChannelID: "5", KID: kid, Commit: denproto.CallKeyCommit(kid, key),
			Members: []denproto.KeyMember{{MemberID: "1", Identity: alice.Public().Hash()}},
			Maker:   denproto.KeyMaker{KEMHash: denproto.KEMHash(bob.Public().KEM), Sig: bob.Public().Sig}}
		if change != nil {
			change(&k, &sealedKey)
		}
		s, err := k.Statement(den, "2")
		if err != nil {
			t.Fatal(err)
		}
		k.Signature = signer.SignCallKey(s)
		copied, err := denproto.SealCallKeyCopy(alice.Public(), den, 5, kid, 1, sealedKey)
		if err != nil {
			t.Fatal(err)
		}
		return k.Event("2", copied)
	}
	if _, why := atAlice.openCallKey(ctx, signed(bob, nil)); why != "" {
		t.Fatalf("a key as bob's Dens makes it was refused: %s", why)
	}
	for _, c := range []struct {
		what string
		e    denproto.VoiceKeyEvent
		want string
	}{
		{"naming another identity for alice", signed(bob, func(k *denproto.VoiceKey, _ *[]byte) {
			k.Members[0].Identity = carol.Public().Hash()
		}), "names another identity for this member"},
		{"made by carol's identity as bob's", signed(carol, func(k *denproto.VoiceKey, _ *[]byte) {
			k.Maker = denproto.KeyMaker{KEMHash: denproto.KEMHash(carol.Public().KEM), Sig: carol.Public().Sig}
		}), "its maker isn't the identity kept for them"},
		{"signed by carol for bob", signed(carol, nil), "its signature doesn't check"},
		{"whose copy opens to another key", signed(bob, func(_ *denproto.VoiceKey, sealed *[]byte) {
			*sealed = denproto.Random(denproto.CallKeySize)
		}), "its copy doesn't open to the key it names"},
		{"for another call", func() denproto.VoiceKeyEvent {
			e := signed(bob, nil)
			e.ChannelID = "6"
			return e
		}(), "its signature doesn't check"},
	} {
		if _, why := atAlice.openCallKey(ctx, c.e); !strings.Contains(why, c.want) {
			t.Errorf("a key %s: %q, want %q", c.what, why, c.want)
		}
	}
	// A device without the seal opens nothing.
	sealless := callConn(t, den, "1", 0, ids)
	if _, why := sealless.openCallKey(ctx, signed(bob, nil)); !strings.Contains(why, "DM recovery key") {
		t.Errorf("a device without the seal: %q", why)
	}
}
