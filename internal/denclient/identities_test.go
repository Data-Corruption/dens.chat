package denclient_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// shortRecord has a Manager write the member's record every moment rather
// than every hour, as a development instance writes it often.
func shortRecord(m *denclient.Manager) { m.RecordEvery = 300 * time.Millisecond }

// codeOf returns what a member's Dens shows to compare codes with another.
func codeOf(t *testing.T, m *denclient.Manager, denID, member string) denclient.CodeView {
	t.Helper()
	v, err := m.Code(context.Background(), denID, member)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Two members who start a DM write at once, neither typing a digit: each
// Dens makes the DM a key as it sends, the den keeps one, and the slower
// takes it, so both read both.
func TestDMsOpenAtOnce(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t)
	dm := openDM(t, owner, member, denID)
	readyDM(t, owner, member, denID, dm)
	var wg sync.WaitGroup
	sent := make([]denclient.PageMessage, 2)
	errs := make([]error, 2)
	for i, m := range []*denclient.Manager{owner, member} {
		wg.Go(func() {
			sent[i], errs[i] = m.Send(context.Background(), denID, dm, denproto.SendRequest{Nonce: denproto.Random(16), Text: "hi from " + string(rune('a'+i))})
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil || sent[i].Locked != "" {
			t.Fatalf("send %d: %+v %v", i, sent[i], err)
		}
	}
	if sent[0].KeyID != sent[1].KeyID {
		t.Fatalf("the two sent under keys %s and %s", sent[0].KeyID, sent[1].KeyID)
	}
	for _, m := range []*denclient.Manager{owner, member} {
		got := history(t, m, denID, dm)
		if len(got) != 2 || got[0].Locked != "" || got[1].Locked != "" || !strings.HasPrefix(got[0].Text, "hi from") {
			t.Fatalf("history: %+v", got)
		}
	}
	var live, all int
	if err := h.s.db.QueryRow(`SELECT count(*) FILTER (WHERE retired IS NULL), count(*) FROM den_dm_keys WHERE channel_id = ?`, dm).Scan(&live, &all); err != nil ||
		live != 1 || all != 1 {
		t.Fatalf("the DM has %d keys, %d live: %v", all, live, err)
	}
}

// Comparing codes: each member's own to read out, and the other's typed
// as they read it out; own digits and wrong ones refused; a match marks
// the other verified, asking nothing of the den, and reaches the member's
// other device with the record's next write.
func TestComparingCodes(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t, shortRecord)
	ctx := context.Background()
	dm := openDM(t, owner, member, denID)
	readyDM(t, owner, member, denID, dm)
	alice, bob := me(t, owner, denID).ID, me(t, member, denID).ID
	aliceSees, bobSees := codeOf(t, owner, denID, bob), codeOf(t, member, denID, alice)
	if aliceSees.Verified || len(strings.ReplaceAll(aliceSees.Mine, " ", "")) != denproto.CodeDigits || strings.Count(aliceSees.Mine, " ") != 4 {
		t.Fatalf("alice's view: %+v", aliceSees)
	}
	if own := codeOf(t, owner, denID, alice); own.Mine != aliceSees.Mine {
		t.Fatalf("alice's own code %q, beside bob %q", own.Mine, aliceSees.Mine)
	}

	before := h.requests.Load()
	if _, err := owner.Compare(ctx, denID, bob, aliceSees.Mine); !isInput(err) {
		t.Fatalf("alice typing her own digits: %v", err)
	}
	if _, err := owner.Compare(ctx, denID, bob, "0000 0000 0000 0000 0000"); !isInput(err) {
		t.Fatalf("the wrong digits: %v", err)
	}
	// Bob reads out his own code, which alice types with dashes.
	got, err := owner.Compare(ctx, denID, bob, strings.ReplaceAll(bobSees.Mine, " ", "-"))
	if err != nil || !got.Verified {
		t.Fatalf("alice's comparison: %+v %v", got, err)
	}
	// Bob's devices write his record meanwhile, every moment here, as
	// alice's would every hour, whatever she did.
	time.Sleep(200 * time.Millisecond)
	if after := h.requests.Load(); after != before {
		t.Fatalf("comparing asked the den %d times", after-before)
	}
	if v, _ := owner.View(denID); !v.Identities[bob].Verified {
		t.Fatalf("bob as alice's Dens kept him: %+v", v.Identities[bob])
	}

	// Bob's second device sees his comparison once his record's written.
	laptop := h.client(t, noOwnDen, shortRecord)
	signInApproved(t, laptop, member, denID, denclient.SignInRequest{Den: "https://example.com", Username: "bob", Password: "correct horse"}, false)
	waitFor(t, laptop, "the laptop connected", connected)
	if _, err := member.Compare(ctx, denID, alice, aliceSees.Mine); err != nil {
		t.Fatal(err)
	}
	viewOf(t, laptop, denID, "alice verified on bob's laptop", func(v denclient.View) bool { return v.Identities[alice].Verified })
}

// A member who starts over shows as changed wherever others meet them,
// and their mark goes; where the other had verified them, the DM holds
// until they compare again or send anyway.
func TestKeysChangedHold(t *testing.T) {
	_, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	dm := openDM(t, owner, member, denID)
	readyDM(t, owner, member, denID, dm)
	alice, bob := me(t, owner, denID).ID, me(t, member, denID).ID
	sendDM(t, member, denID, dm, "before")
	if _, err := owner.Compare(ctx, denID, bob, codeOf(t, member, denID, alice).Mine); err != nil {
		t.Fatal(err)
	}
	if _, err := member.StartOver(ctx, denID, "correct horse"); err != nil {
		t.Fatal(err)
	}
	v := viewOf(t, owner, denID, "bob's keys changed", func(v denclient.View) bool { return len(v.Identities[bob].Changes) == 1 })
	if k := v.Identities[bob]; k.Verified || !k.Held {
		t.Fatalf("bob as alice's Dens kept him after the change: %+v", k)
	}
	if code := codeOf(t, owner, denID, bob); code.Verified || !code.Held {
		t.Fatalf("comparing with bob after the change: %+v", code)
	}
	if _, err := owner.Send(ctx, denID, dm, denproto.SendRequest{Nonce: denproto.Random(16), Text: "held"}); !isInput(err) ||
		!strings.Contains(err.Error(), "keys changed") {
		t.Fatalf("sending while the DM holds: %v", err)
	}
	// Send anyway: nothing compared, but the DM takes messages, under a
	// key sealed to bob's new identity.
	if err := owner.SendAnyway(ctx, denID, bob); err != nil {
		t.Fatal(err)
	}
	after := sendDM(t, owner, denID, dm, "after")
	if got := history(t, member, denID, dm); len(got) != 2 || got[1].Text != "after" || got[0].Locked != "lost" || got[1].KeyID != after.KeyID {
		t.Fatalf("bob's history: %+v", got)
	}
	if code := codeOf(t, owner, denID, bob); code.Verified || code.Held {
		t.Fatalf("after sending anyway: %+v", code)
	}
	// Comparing his new code marks him again.
	if got, err := owner.Compare(ctx, denID, bob, codeOf(t, member, denID, alice).Mine); err != nil || !got.Verified {
		t.Fatalf("comparing again: %+v %v", got, err)
	}
}

// A DM whose key both members checked before M16.1 reads and sends as it
// did: messages go out under that key, sealed with each member's copy
// under their seal, and no new key is made for it.
func TestLegacyDMs(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t)
	dm := openDM(t, owner, member, denID)
	readyDM(t, owner, member, denID, dm)
	info, _ := h.d.Info()
	channel, _ := denproto.ParseID(dm)
	res, err := h.s.db.Exec(`INSERT INTO den_dm_keys (channel_id, started_by, started_at, stage) VALUES (?, ?, 1, 'revealed')`, channel, me(t, owner, denID).ID)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	key := denproto.Random(denproto.DMKeySize)
	var checks []denproto.DMKeyCheck
	copies := map[string]denproto.Bytes{}
	for _, m := range []*denclient.Manager{owner, member} {
		shown, err := m.ShowSeal(denID)
		if err != nil {
			t.Fatal(err)
		}
		seal, _ := denproto.ParseSeal(shown)
		who, _ := denproto.ParseID(me(t, m, denID).ID)
		sealed, err := denproto.SealDMKey(seal, info.ID, channel, id, who, key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.s.db.Exec(`INSERT INTO den_dm_key_members (key_id, member_id, sealed, checked_at) VALUES (?, ?, ?, 2)`, id, who, sealed); err != nil {
			t.Fatal(err)
		}
		copies[denproto.FormatID(who)] = sealed
		checks = append(checks, denproto.DMKeyCheck{MemberID: denproto.FormatID(who), At: 2})
	}
	// Each member's Dens hears of it as the den sent it before M16.1.
	for who, sealed := range copies {
		k := denproto.DMKey{ID: denproto.FormatID(id), ChannelID: dm, StartedBy: me(t, owner, denID).ID, StartedAt: 1,
			Stage: denproto.StageRevealed, Checks: checks, Sealed: sealed}
		whoID, _ := denproto.ParseID(who)
		if err := h.d.Hub.Publish(denproto.EventDMKey, k, den.OnlyMember(whoID)); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []*denclient.Manager{owner, member} {
		viewOf(t, m, denID, "the key from before", func(v denclient.View) bool {
			for _, k := range v.DMKeys {
				if k.ID == denproto.FormatID(id) && len(k.Checks) == 2 && !k.Signed {
					return true
				}
			}
			return false
		})
	}
	a := sendDM(t, owner, denID, dm, "as it did")
	b := sendDM(t, member, denID, dm, "and back")
	if a.KeyID != denproto.FormatID(id) || b.KeyID != a.KeyID {
		t.Fatalf("sent under %s and %s, not the key from before", a.KeyID, b.KeyID)
	}
	for _, m := range []*denclient.Manager{owner, member} {
		if got := history(t, m, denID, dm); len(got) != 2 || got[0].Text != "as it did" || got[1].Text != "and back" {
			t.Fatalf("history: %+v", got)
		}
	}
	var signed int
	h.s.db.QueryRow(`SELECT count(*) FROM den_dm_keys WHERE statement IS NOT NULL`).Scan(&signed)
	if signed != 0 {
		t.Fatalf("%d keys were made for a DM that has one", signed)
	}
}

// A record on the den that doesn't open with the member's seal, as junk a
// den could hold, is written over with what this device kept, rather than
// stopping every write after it.
func TestRecordThatDoesntOpen(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t, shortRecord)
	dm := openDM(t, owner, member, denID)
	readyDM(t, owner, member, denID, dm)
	alice, bob := me(t, owner, denID).ID, me(t, member, denID).ID
	id, _ := denproto.ParseID(bob)
	s := &den.Session{MemberID: id}
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	var rec denproto.Record
	for {
		var err error
		if rec, err = h.d.Record(ctx, s); err != nil {
			t.Fatal(err)
		}
		if rec.Revision > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if rec.Revision == 0 {
		t.Fatal("bob's Dens never wrote his record")
	}
	junk, err := h.d.WriteRecord(ctx, s, denproto.RecordWrite{Revision: rec.Revision, Sealed: denproto.Random(16<<10 + 40)})
	if err != nil {
		t.Fatal(err)
	}
	shown, err := member.ShowSeal(denID)
	if err != nil {
		t.Fatal(err)
	}
	seal, _ := denproto.ParseSeal(shown)
	info, _ := h.d.Info()
	for deadline = time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		rec, err = h.d.Record(ctx, s)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Revision > junk.Revision {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the junk record stays at revision %d", rec.Revision)
		}
	}
	plain, err := denproto.OpenRecord(seal, info.ID, id, rec.Revision, rec.Sealed)
	if err != nil || !strings.Contains(string(plain), `"`+alice+`"`) {
		t.Fatalf("the record written over the junk: %q %v", plain, err)
	}
}
