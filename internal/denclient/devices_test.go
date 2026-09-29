package denclient_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// joinWithCodes joins a new member and returns their recovery codes.
func joinWithCodes(t *testing.T, owner, member *denclient.Manager, denID, username string) []string {
	t.Helper()
	invite, _, err := owner.CreateInvite(context.Background(), denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for range 100 { // Run may not have started yet
		_, codes, err = member.Join(context.Background(), invite, denclient.JoinRequest{Username: username, DisplayName: username, Password: "old password"})
		if err != denclient.ErrNotStarted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, member, username+" connected", connected)
	return codes
}

func signIn(t *testing.T, m *denclient.Manager, req denclient.SignInRequest) (denclient.SignedIn, error) {
	t.Helper()
	for range 100 {
		in, err := m.SignIn(context.Background(), req)
		if err != denclient.ErrNotStarted {
			return in, err
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the manager never started")
	return denclient.SignedIn{}, nil
}

// revoked matches a den that signed this device out with a message that
// starts with want.
func revoked(want string) func(denclient.Status) bool {
	return func(s denclient.Status) bool {
		return s.State == denclient.StateRevoked && strings.HasPrefix(s.Error, want)
	}
}

// A member who lost their machine recovers on a fresh one, whose new
// password signs the old one out at once. Signed in again, the old machine
// can be signed out from the device list, and a password change signs out
// every device but the one that made it.
func TestRecoverOnAFreshMachineAndRevokeTheOldKey(t *testing.T) {
	h, owner, _, denID, _ := chatDen(t)
	ctx := context.Background()
	old := h.client(t, noOwnDen)
	codes := joinWithCodes(t, owner, old, denID, "erin")

	fresh := h.client(t, noOwnDen)
	start := time.Now()
	in, err := signIn(t, fresh, denclient.SignInRequest{Den: "https://example.com", Username: "Erin", Password: "new password", RecoveryCode: strings.ToLower(codes[2])})
	if err != nil || in.RecoveryCodesLeft != denproto.RecoveryCodes-1 || in.SignedOut != 1 || in.Den.Username != "erin" || in.Den.Name != "Chat Den" {
		t.Fatalf("recover: %+v %v", in, err)
	}
	waitFor(t, old, "the old machine signed out by the new password", revoked("Your den password was changed"))
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("signing the old machine out took %v", took)
	}
	waitFor(t, fresh, "the fresh machine connected", connected)

	// The old password is gone, and the new one signs the old machine in.
	if _, err := signIn(t, old, denclient.SignInRequest{Den: "https://example.com", Username: "erin", Password: "old password"}); !denproto.IsCode(err, denproto.CodeUnauthorized) {
		t.Fatalf("the old password: %v", err)
	}
	if _, err := signIn(t, old, denclient.SignInRequest{Den: "https://example.com", Username: "erin", Password: "new password"}); err != nil {
		t.Fatalf("signing the old machine in again: %v", err)
	}
	waitFor(t, old, "the old machine back", connected)

	list, err := fresh.Devices(ctx, denID)
	if err != nil || len(list.Devices) != 2 || list.RecoveryCodesLeft != in.RecoveryCodesLeft {
		t.Fatalf("devices: %+v %v", list, err)
	}
	var oldKey denproto.Device
	for _, d := range list.Devices {
		if !d.Current {
			oldKey = d
		}
	}
	start = time.Now()
	if err := fresh.RevokeDevice(ctx, denID, oldKey.KeyID.String()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, old, "the old machine revoked", revoked("This device was signed out"))
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("revoking took %v", took)
	}

	// Signed in once more, the old machine changes the password.
	if _, err := signIn(t, old, denclient.SignInRequest{Den: "https://example.com", Username: "erin", Password: "new password"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, old, "the old machine back again", connected)
	changed, err := old.ChangePassword(ctx, denID, "new password", "", "newest password")
	if err != nil || changed.SignedOut != 1 || changed.RecoveryCodesLeft != in.RecoveryCodesLeft {
		t.Fatalf("change: %+v %v", changed, err)
	}
	waitFor(t, fresh, "the fresh machine signed out by the change", revoked("Your den password was changed"))
	if list, err := old.Devices(ctx, denID); err != nil || len(list.Devices) != 1 || !list.Devices[0].Current {
		t.Fatalf("the old machine's devices after its change: %+v %v", list, err)
	}
}

func TestSignInPinnedByAnOldInvite(t *testing.T) {
	h, owner, _, denID, _ := chatDen(t)
	ctx := context.Background()
	joinWithCodes(t, owner, h.client(t, noOwnDen), denID, "frank")
	// The invite frank used is spent, but still names the den.
	invite, _, err := owner.CreateInvite(ctx, denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	laptop := h.client(t, noOwnDen)
	in, err := signIn(t, laptop, denclient.SignInRequest{Den: invite, Username: "frank", Password: "old password"})
	if err != nil || in.Den.DenID != denID || in.RecoveryCodesLeft != denproto.RecoveryCodes || in.SignedOut != 0 {
		t.Fatalf("%+v %v", in, err)
	}
	waitFor(t, laptop, "the laptop connected", connected)
	if _, err := signIn(t, laptop, denclient.SignInRequest{Den: invite, Username: "frank", Password: "old password"}); !errors.Is(err, denclient.ErrAlreadyJoined) {
		t.Fatalf("signing in twice: %v", err)
	}
	if in.Den.Fingerprint != denproto.Fingerprint(mustID(t, denID)) {
		t.Fatalf("fingerprint %q", in.Den.Fingerprint)
	}
}

// An owner who signs out the machine that hosts their den signs in again
// there by the den's address, which needn't loop back from that machine.
func TestOwnerSignsInAgainOnTheHostingMachine(t *testing.T) {
	h, owner, _, denID, _ := chatDen(t)
	ctx := context.Background()
	list, err := owner.Devices(ctx, denID)
	if err != nil || len(list.Devices) != 1 || !list.Devices[0].Current {
		t.Fatalf("devices: %+v %v", list, err)
	}
	if err := owner.RevokeDevice(ctx, denID, list.Devices[0].KeyID.String()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, owner, "the owner signed out", func(s denclient.Status) bool { return s.State == denclient.StateRevoked })
	h.public.Listener.Close()
	in, err := signIn(t, owner, denclient.SignInRequest{Den: "https://example.com", Username: "alice", Password: "correct horse"})
	if err != nil || in.Den.DenID != denID || in.Den.URL != "https://example.com" || !in.Den.Own {
		t.Fatalf("%+v %v", in, err)
	}
	waitFor(t, owner, "the owner back", connected)
}

func mustID(t *testing.T, s string) []byte {
	t.Helper()
	b, err := denproto.ParseBytes(s, denproto.IDSize)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// relay passes requests on to the den's public server from another
// address, as whoever holds a domain the den left could, and records what
// went through it.
type relay struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
}

func (h *denHost) relay(t *testing.T) *relay {
	t.Helper()
	target, _ := url.Parse("https://example.com")
	proxy := httputil.NewSingleHostReverseProxy(target)
	transport := h.public.Client().Transport.(*http.Transport).Clone()
	public := h.public.Listener.Addr().String()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, public)
	}
	proxy.Transport = transport
	r := &relay{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.paths = append(r.paths, req.URL.Path)
		r.mu.Unlock()
		req.Host = "example.com"
		proxy.ServeHTTP(w, req)
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *relay) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.paths...)
}

// A server at another address that passes the den's answers along gets the
// den's own address in them, signed, so nothing more goes through it: the
// client follows the den to where it is.
func TestRelayIsFollowedPastNotThrough(t *testing.T) {
	h, owner, _, denID, _ := chatDen(t)
	joinWithCodes(t, owner, h.client(t, noOwnDen), denID, "gina")
	r := h.relay(t)
	laptop := h.client(t, noOwnDen)
	in, err := signIn(t, laptop, denclient.SignInRequest{Den: r.URL, Username: "gina", Password: "old password"})
	if err != nil || in.Den.URL != "https://example.com" {
		t.Fatalf("%+v %v", in, err)
	}
	if seen := r.seen(); len(seen) != 1 || seen[0] != "/api/auth/challenge" {
		t.Fatalf("through the relay: %v", seen)
	}
}

// A member whose install was off while the den moved finds it at its new
// address, which the den signs at the old one.
func TestOfflineMemberFollowsAMovedDen(t *testing.T) {
	h, owner, _, denID, _ := chatDen(t)
	s := newStore(t)
	member, stop := h.clientFrom(t, s, noOwnDen)
	joinWithCodes(t, owner, member, denID, "hugo")
	stop()
	moved := h.loopback.URL
	if err := owner.UpdateDen(context.Background(), denID, denproto.DenUpdateRequest{URL: &moved}); err != nil {
		t.Fatal(err)
	}
	member, _ = h.clientFrom(t, s, noOwnDen)
	st := waitFor(t, member, "hugo connected at the new address", func(s denclient.Status) bool {
		return s.State == denclient.StateConnected && s.URL == moved
	})
	if st.Error != "" {
		t.Fatalf("%+v", st)
	}
}

// A connected member moves with the den as soon as it announces its new
// address, rather than at its next sign-in, so its session's token stops
// going to the old one.
func TestConnectedMemberMovesWithTheDen(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	moved := h.loopback.URL
	if err := owner.UpdateDen(ctx, denID, denproto.DenUpdateRequest{URL: &moved}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, member, "the member hears the new address", func(s denclient.Status) bool { return s.URL == moved })
	h.public.Listener.Close()
	h.public.CloseClientConnections()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := member.Devices(ctx, denID)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the member still calls the old address: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A member away for longer than the den's old address lasted enters its
// new one, where it must prove it's the same den.
func TestStrandedMemberGivesTheNewAddress(t *testing.T) {
	h, owner, _, denID, _ := chatDen(t)
	ctx := context.Background()
	s := newStore(t)
	member, stop := h.clientFrom(t, s, noOwnDen)
	joinWithCodes(t, owner, member, denID, "ivy")
	stop()
	moved := h.loopback.URL
	if err := owner.UpdateDen(ctx, denID, denproto.DenUpdateRequest{URL: &moved}); err != nil {
		t.Fatal(err)
	}
	h.public.Listener.Close()
	member, _ = h.clientFrom(t, s, noOwnDen)
	waitFor(t, member, "ivy offline", func(s denclient.Status) bool { return s.State == denclient.StateOffline })

	other := startDen(t, newStore(t), time.Hour)
	if _, err := other.d.Create(ctx, "Other Den", other.loopback.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := member.Relocate(ctx, denID, other.loopback.URL); !errors.Is(err, denproto.ErrWrongIdentity) {
		t.Fatalf("another den's address: %v", err)
	}
	if _, err := member.Relocate(ctx, denID, "not an address"); err == nil {
		t.Fatal("a malformed address was taken")
	}
	// Long enough offline that the next retry is seconds away.
	time.Sleep(2 * time.Second)
	start := time.Now()
	st, err := member.Relocate(ctx, denID, moved)
	if err != nil || st.URL != moved {
		t.Fatalf("%+v %v", st, err)
	}
	waitFor(t, member, "ivy connected at the new address", connected)
	if took := time.Since(start); took > time.Second {
		t.Fatalf("reconnecting took %v", took)
	}
}
