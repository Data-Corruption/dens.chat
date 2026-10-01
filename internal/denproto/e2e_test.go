package denproto

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

var testDen = bytes.Repeat([]byte{7}, IDSize)

// exchange runs an exchange between two honest sides.
func exchange(t *testing.T, context []byte) (starter, other Result) {
	t.Helper()
	s, offer, err := StartExchange(context)
	if err != nil {
		t.Fatal(err)
	}
	r, ans, err := AnswerExchange(context, offer)
	if err != nil {
		t.Fatal(err)
	}
	rev, starter, err := s.Finish(ans)
	if err != nil {
		t.Fatal(err)
	}
	if other, err = r.Finish(rev); err != nil {
		t.Fatal(err)
	}
	return starter, other
}

func TestExchange(t *testing.T) {
	ctx := DMExchangeContext(testDen, 5, 2, 3)
	a, b := exchange(t, ctx)
	if !bytes.Equal(a.Key, b.Key) || a.Check != b.Check || len(a.Key) != DMKeySize {
		t.Fatalf("the two sides differ: %+v %+v", a, b)
	}
	if len(a.Check) != CheckDigits || strings.Trim(a.Check, "0123456789") != "" {
		t.Fatalf("check code %q", a.Check)
	}
	// Each side reads out its half and types the other's, in any grouping.
	mine, theirs := CheckHalf(a.Check, true), CheckHalf(b.Check, false)
	if !CheckTyped(b.Check, false, FormatCheckHalf(mine)) || !CheckTyped(a.Check, true, strings.ReplaceAll(FormatCheckHalf(theirs), " ", "-")) {
		t.Fatal("typed halves don't check")
	}
	if CheckTyped(a.Check, true, mine) || CheckTyped(a.Check, true, "") || CheckTyped(a.Check, true, theirs[:15]) {
		t.Fatal("a side checked its own half, nothing, or too little")
	}
	if a2, _ := exchange(t, ctx); a2.Check == a.Check || bytes.Equal(a2.Key, a.Key) {
		t.Fatal("two exchanges made the same key or code")
	}
}

// A den in the middle runs one exchange with each member, honestly on
// each side. Both finish, but their keys and codes differ, so typing the
// other's digits fails.
func TestExchangeInTheMiddle(t *testing.T) {
	ctx := DMExchangeContext(testDen, 5, 2, 3)
	alice, offer, err := StartExchange(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Toward Alice the den answers as Bob; toward Bob it starts as Alice.
	towardAlice, ansToAlice, err := AnswerExchange(ctx, offer)
	if err != nil {
		t.Fatal(err)
	}
	towardBob, offerToBob, err := StartExchange(ctx)
	if err != nil {
		t.Fatal(err)
	}
	bob, ansFromBob, err := AnswerExchange(ctx, offerToBob)
	if err != nil {
		t.Fatal(err)
	}
	revFromAlice, aliceSees, err := alice.Finish(ansToAlice)
	if err != nil {
		t.Fatal(err)
	}
	denWithAlice, err := towardAlice.Finish(revFromAlice)
	if err != nil {
		t.Fatal(err)
	}
	revToBob, denWithBob, err := towardBob.Finish(ansFromBob)
	if err != nil {
		t.Fatal(err)
	}
	bobSees, err := bob.Finish(revToBob)
	if err != nil {
		t.Fatal(err)
	}
	if aliceSees.Check != denWithAlice.Check || bobSees.Check != denWithBob.Check {
		t.Fatal("each leg should agree with itself")
	}
	if aliceSees.Check == bobSees.Check || bytes.Equal(aliceSees.Key, bobSees.Key) {
		t.Fatal("the members' codes match through a den in the middle")
	}
	if CheckTyped(aliceSees.Check, true, CheckHalf(bobSees.Check, false)) || CheckTyped(bobSees.Check, false, CheckHalf(aliceSees.Check, true)) {
		t.Fatal("a member's typed digits checked through a den in the middle")
	}
	// Relaying Bob's answer to Alice unchanged doesn't help the den: the
	// reveal she then sends matches only her own commitment, not the one
	// the den made to Bob.
	revToAlice, _, err := alice.Finish(ansFromBob)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bob.Finish(revToAlice); !errors.Is(err, ErrExchange) {
		t.Fatalf("Bob took a reveal against another commitment: %v", err)
	}
}

func TestExchangeRefusesWhatDoesntFit(t *testing.T) {
	ctx := DMExchangeContext(testDen, 5, 2, 3)
	s, offer, _ := StartExchange(ctx)
	r, ans, _ := AnswerExchange(ctx, offer)
	rev, _, err := s.Finish(ans)
	if err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]ExchangeReveal{
		"another key":   {X: append(Bytes{}, ans.X...), Nonce: rev.Nonce},
		"another nonce": {X: rev.X, Nonce: Random(ExchangeNonce)},
		"too short":     {X: rev.X[:31], Nonce: rev.Nonce},
	} {
		if _, err := r.Finish(bad); !errors.Is(err, ErrExchange) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A reveal for one DM doesn't finish an exchange answered for another.
	other, _, _ := AnswerExchange(DMExchangeContext(testDen, 6, 2, 3), offer)
	if _, err := other.Finish(rev); !errors.Is(err, ErrExchange) {
		t.Errorf("another context: %v", err)
	}
	if _, _, err := AnswerExchange(ctx, ExchangeOffer{EK: offer.EK[:100], Commit: offer.Commit}); !errors.Is(err, ErrExchange) {
		t.Errorf("a short offer: %v", err)
	}
	if _, _, err := s.Finish(ExchangeAnswer{X: ans.X, Nonce: ans.Nonce, CT: ans.CT[:10]}); !errors.Is(err, ErrExchange) {
		t.Errorf("a short answer: %v", err)
	}
}

// Both sides' states survive being stored, since an exchange can wait days
// for the other member.
func TestExchangeStateKeeps(t *testing.T) {
	ctx := DeviceExchangeContext(testDen, "alice", Random(IDSize))
	s, offer, _ := StartExchange(ctx)
	if again, err := s.Offer(); err != nil || !bytes.Equal(again.EK, offer.EK) || !bytes.Equal(again.Commit, offer.Commit) {
		t.Fatalf("the starter's offer again: %v", err)
	}
	var s2 Starter
	roundTrip(t, s, &s2)
	r, ans, _ := AnswerExchange(ctx, offer)
	var r2 Responder
	roundTrip(t, r, &r2)
	rev, a, err := s2.Finish(ans)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r2.Finish(rev)
	if err != nil || a.Check != b.Check || !bytes.Equal(a.Key, b.Key) {
		t.Fatalf("after storing: %v", err)
	}
}

func roundTrip(t *testing.T, in, out any) {
	t.Helper()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatal(err)
	}
}

func TestSeal(t *testing.T) {
	seal := NewSeal()
	shown := FormatSeal(seal)
	if len(strings.ReplaceAll(shown, "-", "")) != 52 {
		t.Fatalf("seal shown as %q", shown)
	}
	for _, typed := range []string{shown, strings.ToLower(shown), strings.ReplaceAll(shown, "-", " "), strings.ReplaceAll(shown, "-", "")} {
		if got, err := ParseSeal(typed); err != nil || !bytes.Equal(got, seal) {
			t.Errorf("ParseSeal(%q) = %v", typed, err)
		}
	}
	for _, bad := range []string{"", shown[:20], shown + "AAAA", "!" + shown[1:]} {
		if _, err := ParseSeal(bad); err == nil {
			t.Errorf("ParseSeal(%q) accepted", bad)
		}
	}

	other := bytes.Repeat([]byte{9}, IDSize)
	check, _ := SealCheck(seal, testDen)
	if again, _ := SealCheck(seal, testDen); !bytes.Equal(check, again) {
		t.Fatal("a seal's check changes")
	}
	if atOther, _ := SealCheck(seal, other); bytes.Equal(check, atOther) {
		t.Fatal("a seal's check is the same at two dens")
	}
	if another, _ := SealCheck(NewSeal(), testDen); bytes.Equal(check, another) {
		t.Fatal("two seals check the same")
	}

	key := Random(DMKeySize)
	sealed, err := SealDMKey(seal, testDen, 5, 2, 3, key)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := OpenDMKey(seal, testDen, 5, 2, 3, sealed); err != nil || !bytes.Equal(got, key) {
		t.Fatalf("OpenDMKey: %v", err)
	}
	for name, open := range map[string]func() ([]byte, error){
		"another seal":   func() ([]byte, error) { return OpenDMKey(NewSeal(), testDen, 5, 2, 3, sealed) },
		"another den":    func() ([]byte, error) { return OpenDMKey(seal, other, 5, 2, 3, sealed) },
		"another DM":     func() ([]byte, error) { return OpenDMKey(seal, testDen, 6, 2, 3, sealed) },
		"another key":    func() ([]byte, error) { return OpenDMKey(seal, testDen, 5, 3, 3, sealed) },
		"another member": func() ([]byte, error) { return OpenDMKey(seal, testDen, 5, 2, 4, sealed) },
		"cut short":      func() ([]byte, error) { return OpenDMKey(seal, testDen, 5, 2, 3, sealed[:20]) },
	} {
		if _, err := open(); !errors.Is(err, ErrSealedOpen) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestHandover(t *testing.T) {
	ctx := DeviceExchangeContext(testDen, "alice", Random(IDSize))
	a, b := exchange(t, ctx)
	seal := NewSeal()
	sealed, err := SealHandover(a.Key, ctx, seal)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := OpenHandover(b.Key, ctx, sealed); err != nil || !bytes.Equal(got, seal) {
		t.Fatalf("OpenHandover: %v", err)
	}
	if _, err := OpenHandover(Random(DMKeySize), ctx, sealed); !errors.Is(err, ErrSealedOpen) {
		t.Errorf("another key: %v", err)
	}
	if _, err := OpenHandover(b.Key, DeviceExchangeContext(testDen, "alice", Random(IDSize)), sealed); !errors.Is(err, ErrSealedOpen) {
		t.Errorf("another request: %v", err)
	}
}

func TestDMMessage(t *testing.T) {
	key := Random(DMKeySize)
	ad := MessageAD{DenID: testDen, Channel: 5, Author: 2, Nonce: Random(NonceBytes), KeyID: 1, Revision: 1}
	p := DMPayload{Text: "hi [ ] milk", Files: []DMFile{{
		ID: "11", Key: Random(32), Name: "cat.jpg", Type: "image/jpeg", Size: 1234, Width: 800, Height: 600,
		Thumb: &DMThumb{ID: "12", Width: 640, Height: 480},
	}}}
	sealed, err := SealDMMessage(key, ad, p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := OpenDMMessage(key, ad, sealed)
	if err != nil || got.Text != p.Text || len(got.Files) != 1 || got.Files[0].Thumb.ID != "12" {
		t.Fatalf("OpenDMMessage: %+v %v", got, err)
	}
	for name, change := range map[string]func(*MessageAD){
		"another author":   func(a *MessageAD) { a.Author = 3 },
		"another nonce":    func(a *MessageAD) { a.Nonce = Random(NonceBytes) },
		"another key":      func(a *MessageAD) { a.KeyID = 2 },
		"another revision": func(a *MessageAD) { a.Revision = 2 },
		"another DM":       func(a *MessageAD) { a.Channel = 6 },
	} {
		moved := ad
		change(&moved)
		if _, err := OpenDMMessage(key, moved, sealed); !errors.Is(err, ErrSealedOpen) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// What the other member sealed is checked like a den's message.
	for name, bad := range map[string]DMPayload{
		"no text or files": {Text: " "},
		"a short file key": {Files: []DMFile{{ID: "11", Key: Random(8), Name: "a", Type: "text/plain"}}},
		"a hostile name":   {Files: []DMFile{{ID: "11", Key: Random(32), Name: "../x", Type: "text/plain"}}},
		"a bad type":       {Files: []DMFile{{ID: "11", Key: Random(32), Name: "a", Type: "text/html; x"}}},
		"its own preview":  {Files: []DMFile{{ID: "11", Key: Random(32), Name: "a.png", Type: "image/png", Width: 1, Height: 1, Thumb: &DMThumb{ID: "11", Width: 1, Height: 1}}}},
	} {
		sealed, err := SealDMMessage(key, ad, bad)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := OpenDMMessage(key, ad, sealed); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// A side's state, sealed for the den to keep, opens only with the same
// seal, for the same member and side of the same exchange.
func TestExchangeStateSealed(t *testing.T) {
	seal := NewSeal()
	s, offer, err := StartExchange(DMExchangeContext(testDen, 5, 2, 3))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := SealExchangeState(seal, testDen, 5, 2, offer.Commit, true, s)
	if err != nil {
		t.Fatal(err)
	}
	var got Starter
	if err := OpenExchangeState(seal, testDen, 5, 2, offer.Commit, true, sealed, &got); err != nil {
		t.Fatal(err)
	}
	if again, err := got.Offer(); err != nil || !bytes.Equal(again.Commit, offer.Commit) {
		t.Fatalf("the opened state makes another offer: %v", err)
	}
	for name, open := range map[string]func() error{
		"another seal":     func() error { return OpenExchangeState(NewSeal(), testDen, 5, 2, offer.Commit, true, sealed, &got) },
		"another den":      func() error { return OpenExchangeState(seal, Random(IDSize), 5, 2, offer.Commit, true, sealed, &got) },
		"another DM":       func() error { return OpenExchangeState(seal, testDen, 6, 2, offer.Commit, true, sealed, &got) },
		"another member":   func() error { return OpenExchangeState(seal, testDen, 5, 3, offer.Commit, true, sealed, &got) },
		"another exchange": func() error { return OpenExchangeState(seal, testDen, 5, 2, Random(32), true, sealed, &got) },
		"the other side":   func() error { return OpenExchangeState(seal, testDen, 5, 2, offer.Commit, false, sealed, &got) },
	} {
		if err := open(); !errors.Is(err, ErrSealedOpen) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A device exchange's digits match only for the same username and new
// device key on both sides.
func TestDeviceExchangeContext(t *testing.T) {
	key := Random(IDSize)
	ctx := DeviceExchangeContext(testDen, "alice", key)
	for name, other := range map[string][]byte{
		"another username": DeviceExchangeContext(testDen, "alicf", key),
		"another device":   DeviceExchangeContext(testDen, "alice", Random(IDSize)),
		"another den":      DeviceExchangeContext(Random(IDSize), "alice", key),
		// The username's length keeps a name from running into the key.
		"a shifted name": DeviceExchangeContext(testDen, "alice"+string(key[:1]), append(append([]byte{}, key[1:]...), 0)),
	} {
		if bytes.Equal(ctx, other) {
			t.Errorf("%s gives the same context", name)
		}
	}
	s, offer, _ := StartExchange(ctx)
	r, ans, _ := AnswerExchange(DeviceExchangeContext(testDen, "alice", Random(IDSize)), offer)
	rev, a, err := s.Finish(ans)
	if err != nil {
		t.Fatal(err)
	}
	// The commitment names the context, so the answering side refuses a
	// reveal meant for another device.
	if _, err := r.Finish(rev); !errors.Is(err, ErrExchange) {
		t.Fatalf("another device's reveal: %v, check %s", err, a.Check)
	}
}
