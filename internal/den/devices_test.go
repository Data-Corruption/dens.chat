package den

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func (f *fixture) recover(username, code, password string, dev device) (denproto.SignInResponse, error) {
	nonce, proof := f.proof(dev)
	info, _ := f.d.Info()
	return f.d.Recover(context.Background(), denproto.RecoverRequest{
		Username: username, RecoveryCode: code, NewVerifier: denproto.Verifier(password, info.ID, username),
		PublicKey: dev.pub(), DeviceLabel: "new phone", Nonce: nonce, Proof: proof,
	})
}

func (f *fixture) verifier(username, password string) denproto.Bytes {
	info, _ := f.d.Info()
	return denproto.Verifier(password, info.ID, username)
}

func TestPasswordSignIn(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	sub, _, _, _ := f.d.Hub.Subscribe(member.MemberID, false, "", 0)
	ownerSub, _, _, _ := f.d.Hub.Subscribe(owner.MemberID, true, "", 0)

	laptop := newDevice()
	resp, err := f.signIn(member, "BOB", "password", laptop)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := f.d.Info()
	if resp.Member.Username != "bob" || !denproto.Equal(resp.Den.ID, info.ID) || resp.RecoveryCodesLeft != denproto.RecoveryCodes {
		t.Fatalf("%+v", resp)
	}
	s, err := f.d.Authenticate(ctx, resp.Token)
	if err != nil || s.MemberID != member.MemberID || !denproto.Equal(s.KeyID, laptop.id()) {
		t.Fatalf("the new session: %+v %v", s, err)
	}
	// The member's other sessions hear about the request, and then the new
	// device; nobody else does.
	for _, want := range []string{denproto.EventDeviceRequest, denproto.EventDeviceRequest, denproto.EventDeviceRequest,
		denproto.EventDeviceAdded, denproto.EventDeviceRequestEnded} {
		e := <-sub.Events
		if e.T != want {
			t.Fatalf("event %s, want %s", e.T, want)
		}
		if e.T == denproto.EventDeviceAdded {
			var added denproto.Device
			json.Unmarshal(e.D, &added)
			if added.Label != "laptop" || !denproto.Equal(added.KeyID, laptop.id()) {
				t.Fatalf("added %+v", added)
			}
		}
	}
	select {
	case e := <-ownerSub.Events:
		t.Fatalf("the owner heard %s", e.T)
	default:
	}

	for name, try := range map[string]func() error{
		"a wrong password": func() error { _, err := f.askSignIn("bob", "not the password", newDevice()); return err },
		"nobody":           func() error { _, err := f.askSignIn("nobody", "password", newDevice()); return err },
		"a bad username":   func() error { _, err := f.askSignIn("!", "password", newDevice()); return err },
	} {
		if err := try(); !denproto.IsCode(err, denproto.CodeUnauthorized) {
			t.Errorf("%s: %v", name, err)
		}
	}
	_, err = f.askSignIn("bob", "password", laptop)
	wantCode(t, err, denproto.CodeInvalidField)

	// Only members in the den now sign in: not one who left, or was banned.
	carol := f.member(owner, "carol")
	if err := f.d.Leave(ctx, carol); err != nil {
		t.Fatal(err)
	}
	_, err = f.askSignIn("carol", "password", newDevice())
	wantCode(t, err, denproto.CodeUnauthorized)
	dave := f.member(owner, "dave")
	if err := f.d.Remove(ctx, owner, denproto.FormatID(dave.MemberID), denproto.RemoveRequest{Ban: true}); err != nil {
		t.Fatal(err)
	}
	_, err = f.askSignIn("dave", "password", newDevice())
	wantCode(t, err, denproto.CodeUnauthorized)
}

func TestPasswordGuessesAreLimited(t *testing.T) {
	f, _, _ := chatFixture(t)
	var err error
	for range 11 {
		_, err = f.askSignIn("bob", "guess", newDevice())
	}
	wantCode(t, err, denproto.CodeRateLimited)
	// The limit is the username's, not the device's or the address's.
	_, err = f.askSignIn("bob", "password", newDevice())
	wantCode(t, err, denproto.CodeRateLimited)
}

func TestRecovery(t *testing.T) {
	f, owner, _ := chatFixture(t)
	ctx := context.Background()
	joined, err := f.join(f.invite(owner, 1).Code, "erin", newDevice())
	if err != nil {
		t.Fatal(err)
	}
	code := joined.RecoveryCodes[3]
	resp, err := f.recover("erin", code, "a new password", newDevice())
	if err != nil || resp.RecoveryCodesLeft != denproto.RecoveryCodes-1 || resp.Member.Username != "erin" || resp.SignedOut != 1 {
		t.Fatalf("recover: %+v %v", resp, err)
	}
	// The new password signed out the device erin joined on.
	if _, err := f.d.Authenticate(ctx, joined.Token); err == nil {
		t.Fatal("the device erin joined on is still signed in")
	}
	// The code is spent, and the old password is gone with it.
	_, err = f.recover("erin", code, "another password", newDevice())
	wantCode(t, err, denproto.CodeUnauthorized)
	_, err = f.askSignIn("erin", "password", newDevice())
	wantCode(t, err, denproto.CodeUnauthorized)
	recovered, _ := f.d.Authenticate(ctx, resp.Token)
	if _, err := f.signIn(recovered, "erin", "a new password", newDevice()); err != nil {
		t.Fatalf("the new password: %v", err)
	}
	// A code works as typed: any case, without dashes.
	typed := ""
	for _, c := range joined.RecoveryCodes[4] {
		if c != '-' {
			typed += string(c | 0x20)
		}
	}
	if _, err := f.recover("erin", typed, "a third password", newDevice()); err != nil {
		t.Fatalf("a code typed loosely: %v", err)
	}
	// Someone else's code doesn't open this account.
	bobs, err := f.join(f.invite(owner, 1).Code, "frank", newDevice())
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.recover("erin", bobs.RecoveryCodes[0], "x password", newDevice())
	wantCode(t, err, denproto.CodeUnauthorized)
	_, err = f.recover("erin", "not a code", "x password", newDevice())
	wantCode(t, err, denproto.CodeUnauthorized)
}

func TestChangePasswordAndCodes(t *testing.T) {
	f, owner, _ := chatFixture(t)
	ctx := context.Background()
	joined, err := f.join(f.invite(owner, 1).Code, "gail", newDevice())
	if err != nil {
		t.Fatal(err)
	}
	s, _ := f.d.Authenticate(ctx, joined.Token)

	_, err = f.d.ChangePassword(ctx, s, denproto.PasswordChangeRequest{Verifier: f.verifier("gail", "wrong"), NewVerifier: f.verifier("gail", "second")})
	wantCode(t, err, denproto.CodeWrongPassword)
	_, err = f.d.ChangePassword(ctx, s, denproto.PasswordChangeRequest{NewVerifier: f.verifier("gail", "second")})
	wantCode(t, err, denproto.CodeInvalidField)
	laptop, err := f.signIn(s, "gail", "password", newDevice())
	if err != nil {
		t.Fatal(err)
	}
	sub, _, _, _ := f.d.Hub.Subscribe(s.MemberID, false, "", 0)
	changed, err := f.d.ChangePassword(ctx, s, denproto.PasswordChangeRequest{Verifier: f.verifier("gail", "password"), NewVerifier: f.verifier("gail", "second")})
	if err != nil || changed.RecoveryCodesLeft != denproto.RecoveryCodes || changed.SignedOut != 1 {
		t.Fatalf("change: %+v %v", changed, err)
	}
	// The device that changed it stays signed in, and the other is out.
	if _, err := f.d.Authenticate(ctx, joined.Token); err != nil {
		t.Fatalf("the session after the change: %v", err)
	}
	if _, err := f.d.Authenticate(ctx, laptop.Token); err == nil {
		t.Fatal("the other device is still signed in")
	}
	if e := <-sub.Events; e.T != denproto.EventDeviceRemoved {
		t.Fatalf("event %s", e.T)
	}
	changed, err = f.d.ChangePassword(ctx, s, denproto.PasswordChangeRequest{RecoveryCode: joined.RecoveryCodes[0], NewVerifier: f.verifier("gail", "third")})
	if err != nil || changed.RecoveryCodesLeft != denproto.RecoveryCodes-1 || changed.SignedOut != 0 {
		t.Fatalf("change with a code: %+v %v", changed, err)
	}
	if _, err := f.signIn(s, "gail", "third", newDevice()); err != nil {
		t.Fatalf("the newest password: %v", err)
	}

	// New codes take the password, and replace every old one.
	_, err = f.d.NewRecoveryCodes(ctx, s, denproto.RecoveryCodesRequest{Verifier: f.verifier("gail", "second")})
	wantCode(t, err, denproto.CodeWrongPassword)
	codes, err := f.d.NewRecoveryCodes(ctx, s, denproto.RecoveryCodesRequest{Verifier: f.verifier("gail", "third")})
	if err != nil || len(codes.RecoveryCodes) != denproto.RecoveryCodes {
		t.Fatalf("new codes: %v", err)
	}
	_, err = f.recover("gail", joined.RecoveryCodes[1], "fourth", newDevice())
	wantCode(t, err, denproto.CodeUnauthorized)
	if _, err := f.recover("gail", codes.RecoveryCodes[0], "fourth", newDevice()); err != nil {
		t.Fatalf("a new code: %v", err)
	}
}

func TestDevicesAndRevoking(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	laptop := newDevice()
	resp, err := f.signIn(member, "bob", "password", laptop)
	if err != nil {
		t.Fatal(err)
	}
	onLaptop, _ := f.d.Authenticate(ctx, resp.Token)
	list, err := f.d.Devices(ctx, member)
	if err != nil || len(list.Devices) != 2 || list.RecoveryCodesLeft != denproto.RecoveryCodes {
		t.Fatalf("devices: %+v %v", list, err)
	}
	for _, d := range list.Devices {
		if d.Current != (d.Label == "test") {
			t.Fatalf("which is current: %+v", list.Devices)
		}
	}
	sub, _, _, _ := f.d.Hub.Subscribe(member.MemberID, false, "", 0)
	// A member can't revoke someone else's device.
	err = f.d.RevokeDevice(ctx, owner, laptop.id())
	wantCode(t, err, denproto.CodeNotFound)
	if err := f.d.RevokeDevice(ctx, member, laptop.id()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.Authenticate(ctx, resp.Token); err == nil {
		t.Fatal("the revoked device's session still works")
	}
	if _, err := f.d.Renew(ctx, onLaptop, nil, nil); !denproto.IsCode(err, denproto.CodeKeyRevoked) {
		t.Fatalf("renewing on the revoked device: %v", err)
	}
	e := <-sub.Events
	var removed denproto.DeviceRemoved
	json.Unmarshal(e.D, &removed)
	if e.T != denproto.EventDeviceRemoved || !denproto.Equal(removed.KeyID, laptop.id()) {
		t.Fatalf("event %s %+v", e.T, removed)
	}
	err = f.d.RevokeDevice(ctx, member, laptop.id())
	wantCode(t, err, denproto.CodeNotFound)
}

func (dev device) id() denproto.Bytes { return denproto.ID(dev.key.Public().(ed25519.PublicKey)) }
