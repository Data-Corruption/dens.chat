package den

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

type fixture struct {
	t       *testing.T
	db      *sql.DB
	v       *vault.Vault
	log     *xlog.Logger
	d       *Den
	clock   time.Time
	storage Storage
	// seals are the members' DM seals, which their clients hold, and keys
	// the DMs' keys, made as tests need them.
	seals map[int64]denproto.Bytes
	keys  map[string]testKey
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	log, err := xlog.New(filepath.Join(dir, "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.New(filepath.Join(dir, "db"), log, build.BuildInfo{DefaultLogLevel: "warn"}, database.ApplyPendingMigrations)
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.New(denproto.Random(vault.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, db: db, v: v, log: log, clock: time.UnixMilli(1_759_000_000_000), storage: testStorage(t),
		seals: map[int64]denproto.Bytes{}, keys: map[string]testKey{}}
	t.Cleanup(func() {
		if f.d != nil {
			f.d.Close()
		}
		db.Close()
		v.Close()
		log.Close()
	})
	f.open()
	return f
}

func (f *fixture) open() {
	f.t.Helper()
	if f.d != nil {
		f.d.Close()
	}
	d, err := Open(context.Background(), f.db, f.v, f.log, f.storage)
	if err != nil {
		f.t.Fatal(err)
	}
	d.now = func() time.Time { return f.clock }
	d.Hub.now = d.now
	f.d = d
}

// The test binary is its own media worker, as ffmpeg.TestWorkerEnv says.
func TestMain(m *testing.M) {
	if os.Getenv(ffmpeg.TestWorkerEnv) == "1" {
		os.Exit(ffmpeg.Work(os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

// testStorage makes the directories a den keeps uploads in.
func testStorage(t *testing.T) Storage {
	t.Helper()
	s := Storage{Dir: filepath.Join(t.TempDir(), "uploads"), Temp: filepath.Join(t.TempDir(), "tmp"),
		Media: ffmpeg.TestRunner(nil)}
	for _, dir := range []string{s.Dir, s.Temp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func (f *fixture) create() []byte {
	f.t.Helper()
	code, err := f.d.Create(context.Background(), "Test Den", "https://den.test")
	if err != nil {
		f.t.Fatal(err)
	}
	return code
}

// device is a client's key for this den.
type device struct {
	key ed25519.PrivateKey
}

func newDevice() device {
	_, key, _ := ed25519.GenerateKey(nil)
	return device{key: key}
}

func (dev device) pub() denproto.Bytes { return denproto.Bytes(dev.key.Public().(ed25519.PublicKey)) }

// proof runs a challenge, checks the den's answer, and signs the nonce.
func (f *fixture) proof(dev device) (nonce, proof denproto.Bytes) {
	f.t.Helper()
	clientNonce := denproto.Random(denproto.NonceSize)
	resp, err := f.d.Challenge(clientNonce)
	if err != nil {
		f.t.Fatal(err)
	}
	info, _ := f.d.Info()
	if _, err := denproto.VerifyDen(info.ID, clientNonce, resp); err != nil {
		f.t.Fatal(err)
	}
	if err := denproto.CheckDenURL(resp.URL, info.URL); err != nil {
		f.t.Fatal(err)
	}
	return resp.Nonce, denproto.Prove(dev.key, info.ID, resp.Nonce)
}

// join joins with the password "password" and a new DM seal, as a fresh
// install does.
func (f *fixture) join(code []byte, username string, dev device) (denproto.JoinResponse, error) {
	return f.joinAs(code, username, "password", dev, denproto.NewSeal())
}

func (f *fixture) joinWith(code []byte, username, password string, dev device) (denproto.JoinResponse, error) {
	return f.joinAs(code, username, password, dev, denproto.NewSeal())
}

func (f *fixture) joinAs(code []byte, username, password string, dev device, seal denproto.Bytes) (denproto.JoinResponse, error) {
	nonce, proof := f.proof(dev)
	info, _ := f.d.Info()
	check, err := denproto.SealCheck(seal, info.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	joined, err := f.d.Join(context.Background(), denproto.JoinRequest{
		Invite: code, Username: username, DisplayName: "Name of " + username,
		Verifier: denproto.Verifier(password, info.ID, strings.ToLower(username)), PublicKey: dev.pub(),
		DeviceLabel: "test", Nonce: nonce, Proof: proof, SealCheck: check,
	})
	if err == nil {
		id, _ := denproto.ParseID(joined.Member.ID)
		f.seals[id] = seal
	}
	return joined, err
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if !denproto.IsCode(err, code) {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func TestCreateAndReopen(t *testing.T) {
	f := newFixture(t)
	if _, ok := f.d.Info(); ok {
		t.Fatal("a fresh den claims to exist")
	}
	f.create()
	if _, err := f.d.Create(context.Background(), "Again", "https://den.test"); !errors.Is(err, ErrAlreadyCreated) {
		t.Fatalf("second create: %v", err)
	}
	before, _ := f.d.Info()
	f.open()
	after, ok := f.d.Info()
	if !ok || !denproto.Equal(before.ID, after.ID) || after.Name != "Test Den" {
		t.Fatalf("reopened den %+v, want %+v", after, before)
	}
	f.proof(newDevice()) // the reloaded identity key still signs challenges
}

func TestJoinFlow(t *testing.T) {
	f := newFixture(t)
	ownerCode := f.create()
	owner := newDevice()
	joined, err := f.join(ownerCode, "Alice", owner)
	if err != nil {
		t.Fatal(err)
	}
	if joined.Member.Role != denproto.RoleOwner || joined.Member.Username != "alice" || len(joined.RecoveryCodes) != denproto.RecoveryCodes {
		t.Fatalf("owner join: %+v", joined)
	}
	if _, err := f.join(ownerCode, "mallory", newDevice()); err == nil {
		t.Fatal("the owner invite worked twice")
	} else {
		wantCode(t, err, denproto.CodeInviteInvalid)
	}
	s, err := f.d.Authenticate(context.Background(), joined.Token)
	if err != nil || s.Role != denproto.RoleOwner {
		t.Fatalf("authenticate owner: %+v, %v", s, err)
	}

	inv, err := f.d.CreateInvite(context.Background(), s, denproto.InviteCreateRequest{MaxUses: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.Preview(context.Background(), inv.Code); err != nil {
		t.Fatalf("preview: %v", err)
	}
	bob, err := f.join(inv.Code, "bob", newDevice())
	if err != nil || bob.Member.Role != denproto.RoleMember {
		t.Fatalf("member join: %+v, %v", bob, err)
	}
	_, err = f.join(inv.Code, "BOB", newDevice())
	wantCode(t, err, denproto.CodeUsernameTaken)
	if _, err := f.join(inv.Code, "carol", newDevice()); err != nil {
		t.Fatalf("second use: %v", err)
	}
	_, err = f.join(inv.Code, "dave", newDevice())
	wantCode(t, err, denproto.CodeInviteInvalid)

	bs, _ := f.d.Authenticate(context.Background(), bob.Token)
	_, err = f.d.CreateInvite(context.Background(), bs, denproto.InviteCreateRequest{})
	wantCode(t, err, denproto.CodeForbidden)
	_, err = f.d.Update(context.Background(), bs, denproto.DenUpdateRequest{})
	wantCode(t, err, denproto.CodeForbidden)
}

func TestProofsAndNonces(t *testing.T) {
	f := newFixture(t)
	code := f.create()
	dev := newDevice()
	nonce, proof := f.proof(dev)
	info, _ := f.d.Info()
	req := denproto.JoinRequest{
		Invite: code, Username: "alice", DisplayName: "Alice", Verifier: denproto.Random(32),
		PublicKey: dev.pub(), DeviceLabel: "test", Nonce: nonce, Proof: denproto.Prove(newDevice().key, info.ID, nonce),
		SealCheck: denproto.Random(denproto.SealCheckSize),
	}
	_, err := f.d.Join(context.Background(), req)
	wantCode(t, err, denproto.CodeBadSignature)
	// The failed attempt spent the nonce.
	req.Proof = proof
	_, err = f.d.Join(context.Background(), req)
	wantCode(t, err, denproto.CodeBadNonce)

	nonce, proof = f.proof(dev)
	f.clock = f.clock.Add(2 * nonceLifetime)
	req.Nonce, req.Proof = nonce, proof
	_, err = f.d.Join(context.Background(), req)
	wantCode(t, err, denproto.CodeBadNonce)
}

func TestLoginRenewExpire(t *testing.T) {
	f := newFixture(t)
	dev := newDevice()
	if _, err := f.join(f.create(), "alice", dev); err != nil {
		t.Fatal(err)
	}
	nonce, proof := f.proof(dev)
	sess, err := f.d.Login(context.Background(), denproto.LoginRequest{KeyID: denproto.ID(ed25519.PublicKey(dev.pub())), Nonce: nonce, Proof: proof})
	if err != nil {
		t.Fatal(err)
	}
	stranger := newDevice()
	nonce, proof = f.proof(stranger)
	_, err = f.d.Login(context.Background(), denproto.LoginRequest{KeyID: denproto.ID(ed25519.PublicKey(stranger.pub())), Nonce: nonce, Proof: proof})
	wantCode(t, err, denproto.CodeUnauthorized)

	s, err := f.d.Authenticate(context.Background(), sess.Token)
	if err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(50 * time.Minute)
	nonce, proof = f.proof(dev)
	until, err := f.d.Renew(context.Background(), s, nonce, proof)
	if err != nil || !until.Equal(f.clock.Add(DefaultTokenLifetime)) {
		t.Fatalf("renew: %v, %v", until, err)
	}
	f.clock = f.clock.Add(59 * time.Minute)
	if _, err := f.d.Authenticate(context.Background(), sess.Token); err != nil {
		t.Fatalf("renewed session expired early: %v", err)
	}
	f.clock = f.clock.Add(2 * time.Minute)
	_, err = f.d.Authenticate(context.Background(), sess.Token)
	wantCode(t, err, denproto.CodeUnauthorized)
}

func TestInviteRules(t *testing.T) {
	f := newFixture(t)
	owner, err := f.join(f.create(), "alice", newDevice())
	if err != nil {
		t.Fatal(err)
	}
	s, _ := f.d.Authenticate(context.Background(), owner.Token)
	for _, bad := range []denproto.InviteCreateRequest{{ExpiresIn: 10}, {ExpiresIn: 31 * 86400}, {MaxUses: 101}, {MaxUses: -1}} {
		_, err := f.d.CreateInvite(context.Background(), s, bad)
		wantCode(t, err, denproto.CodeInvalidField)
	}
	inv, err := f.d.CreateInvite(context.Background(), s, denproto.InviteCreateRequest{ExpiresIn: 3600})
	if err != nil {
		t.Fatal(err)
	}
	list, err := f.d.Invites(context.Background(), s)
	if err != nil || len(list) != 1 || list[0].Code != nil {
		t.Fatalf("invites: %+v, %v", list, err)
	}
	f.clock = f.clock.Add(2 * time.Hour)
	_, err = f.d.Preview(context.Background(), inv.Code)
	wantCode(t, err, denproto.CodeInviteInvalid)

	inv, _ = f.d.CreateInvite(context.Background(), s, denproto.InviteCreateRequest{})
	id := mustInt(t, inv.ID)
	if err := f.d.RevokeInvite(context.Background(), s, id); err != nil {
		t.Fatal(err)
	}
	_, err = f.join(inv.Code, "bob", newDevice())
	wantCode(t, err, denproto.CodeInviteInvalid)
}

func mustInt(t *testing.T, s string) int64 {
	t.Helper()
	var n int64
	for _, r := range s {
		n = n*10 + int64(r-'0')
	}
	return n
}

func TestHubResume(t *testing.T) {
	h := NewHub()
	h.Publish("a", nil, Everyone)      // seq 1
	h.Publish("b", nil, OnlyMember(7)) // seq 2, only member 7
	h.Publish("c", nil, Everyone)      // seq 3

	_, missed, resumed, _ := h.Subscribe(8, false, h.Epoch(), 1)
	if !resumed || len(missed) != 1 || missed[0].Seq != 3 {
		t.Fatalf("member 8 resumed=%v missed=%+v", resumed, missed)
	}
	_, missed, resumed, _ = h.Subscribe(7, false, h.Epoch(), 1)
	if !resumed || len(missed) != 2 {
		t.Fatalf("member 7 resumed=%v missed=%+v", resumed, missed)
	}
	if _, _, resumed, seq := h.Subscribe(7, false, "other-epoch", 1); resumed || seq != 3 {
		t.Fatalf("another epoch resumed=%v seq=%d", resumed, seq)
	}
	if _, _, resumed, _ := h.Subscribe(7, false, h.Epoch(), 9); resumed {
		t.Fatal("resumed from a seq the den never issued")
	}

	h.maxLen = 2
	h.Publish("d", nil, Everyone) // ring now holds seq 3 and 4
	if _, _, resumed, _ := h.Subscribe(7, false, h.Epoch(), 1); resumed {
		t.Fatal("resumed past the end of the ring")
	}
	if _, _, resumed, _ := h.Subscribe(7, false, h.Epoch(), 2); !resumed {
		t.Fatal("couldn't resume from the ring's edge")
	}
}

func TestHubAudiencesAndRefresh(t *testing.T) {
	h := NewHub()
	member, _, _, _ := h.Subscribe(1, false, "", 0)
	staff, _, _, _ := h.Subscribe(2, true, "", 0)
	h.Publish("staff", nil, Staff)
	h.Publish("public", nil, NonStaff)
	h.Publish("all", nil, Everyone)
	if e := <-member.Events; e.T != "public" {
		t.Fatalf("member got %s first", e.T)
	}
	if e := <-staff.Events; e.T != "staff" {
		t.Fatalf("staff got %s first", e.T)
	}
	h.RefreshNonStaff()
	<-member.Events // "all"
	// The refresh takes a seq of its own, after the last event.
	if e := <-member.Events; e.T != eventRefresh || e.Seq != 4 {
		t.Fatalf("member got %+v, want a refresh at seq 4", e)
	}
	<-staff.Events // "all"
	select {
	case e := <-staff.Events:
		t.Fatalf("staff got %+v", e)
	default:
	}
	// A non-staff resume point from before the refresh can't be replayed,
	// even one that saw every event before it.
	if _, _, resumed, _ := h.Subscribe(1, false, h.Epoch(), 3); resumed {
		t.Fatal("resumed across a visibility change")
	}
	if _, missed, resumed, _ := h.Subscribe(1, false, h.Epoch(), 4); !resumed || len(missed) != 0 {
		t.Fatalf("couldn't resume after the refresh's snapshot: %v %v", resumed, missed)
	}
	if _, missed, resumed, _ := h.Subscribe(2, true, h.Epoch(), 2); !resumed || len(missed) != 1 {
		t.Fatalf("staff couldn't resume, or got the refresh replayed: %v %+v", resumed, missed)
	}
}

// A role change starts the member's sockets over with what they can see
// now, and their older resume points resync.
func TestHubRoleChange(t *testing.T) {
	h := NewHub()
	sub, _, _, _ := h.Subscribe(1, false, "", 0)
	other, _, _, _ := h.Subscribe(2, false, "", 0)
	h.Publish("before", nil, Everyone)
	<-sub.Events
	<-other.Events
	h.SetStaff(1, true)
	if e := <-sub.Events; e.T != eventRefresh || e.Seq != 2 || !h.Staff(sub) {
		t.Fatalf("member got %+v, staff %v", e, h.Staff(sub))
	}
	select {
	case e := <-other.Events:
		t.Fatalf("another member got %+v", e)
	default:
	}
	h.Publish("staff", nil, Staff)
	if e := <-sub.Events; e.T != "staff" {
		t.Fatalf("the new moderator got %+v", e)
	}
	if _, _, resumed, _ := h.Subscribe(1, true, h.Epoch(), 1); resumed {
		t.Fatal("resumed across a role change")
	}
	if _, _, resumed, _ := h.Subscribe(2, false, h.Epoch(), 1); !resumed {
		t.Fatal("another member couldn't resume")
	}
}

func TestHubDropsSlowSubscriber(t *testing.T) {
	h := NewHub()
	h.subSize = 2
	sub, _, _, _ := h.Subscribe(1, false, "", 0)
	for range 3 {
		h.Publish("e", nil, Everyone)
	}
	n := 0
	for range sub.Events {
		n++
	}
	if n != 2 || !sub.Slow() {
		t.Fatalf("got %d events, slow=%v", n, sub.Slow())
	}
}

func TestLimiterEvictsOldest(t *testing.T) {
	l := newLimiter(1, time.Hour, 2)
	if !l.allow("a") || l.allow("a") {
		t.Fatal("burst of one not enforced")
	}
	l.allow("b")
	l.allow("c") // evicts a
	if !l.allow("a") {
		t.Fatal("evicted key kept its empty bucket")
	}
	if len(l.index) != 2 {
		t.Fatalf("limiter holds %d keys, capacity 2", len(l.index))
	}
}

// TestHubDropsBySize checks that the ring holds its events to maxBytes as
// well as to maxLen (M8), and that a resume point the drop took gets a
// snapshot rather than a replay with a hole in it.
func TestHubDropsBySize(t *testing.T) {
	h := NewHub()
	big := strings.Repeat("x", 1000)
	h.Publish("a", big, Everyone) // seq 1
	size := h.bytes
	h.maxBytes = 3 * size
	h.Publish("b", big, Everyone) // seq 2
	h.Publish("c", big, Everyone) // seq 3
	if _, missed, resumed, _ := h.Subscribe(1, false, h.Epoch(), 0); !resumed || len(missed) != 3 {
		t.Fatalf("within the size: resumed=%v, %d missed", resumed, len(missed))
	}
	h.Publish("d", big, Everyone) // seq 4, which takes seq 1 out
	if h.bytes > h.maxBytes || h.first != 2 {
		t.Fatalf("the ring holds %d bytes of at most %d, from seq %d", h.bytes, h.maxBytes, h.first)
	}
	if _, _, resumed, _ := h.Subscribe(1, false, h.Epoch(), 0); resumed {
		t.Fatal("resumed from before the events the size took out")
	}
	if _, missed, resumed, _ := h.Subscribe(1, false, h.Epoch(), 1); !resumed || len(missed) != 3 || missed[0].Seq != 2 {
		t.Fatalf("resuming at the ring's edge: resumed=%v, missed %+v", resumed, missed)
	}
}

// TestHubTrimsInPlace checks that the ring replays what it holds as its
// oldest events go and the rest move down within its slice.
func TestHubTrimsInPlace(t *testing.T) {
	h := NewHub()
	h.maxLen = 5
	for i := 1; i <= 100; i++ {
		h.Publish("e", i, Everyone)
		_, missed, resumed, _ := h.Subscribe(1, false, h.Epoch(), uint64(max(i-5, 0)))
		if !resumed || len(missed) != min(i, 5) {
			t.Fatalf("after %d events: resumed=%v, %d missed", i, resumed, len(missed))
		}
		for j, e := range missed {
			if want := uint64(i - len(missed) + j + 1); e.Seq != want || string(e.D) != strconv.FormatUint(want, 10) {
				t.Fatalf("after %d events, replayed %+v where seq %d belongs", i, e, want)
			}
		}
		if len(h.ring) > 2*h.maxLen {
			t.Fatalf("after %d events the ring's slice holds %d", i, len(h.ring))
		}
	}
}

// TestOldestNonceGoes checks that with maxNonces outstanding, a challenge
// takes the place of the oldest nonce rather than being refused (M8).
func TestOldestNonceGoes(t *testing.T) {
	f := newFixture(t)
	f.create()
	nonce := func(i int) string { return fmt.Sprintf("%032d", i) }
	for i := range maxNonces {
		f.d.issueNonce(nonce(i))
	}
	f.d.consumeNonce([]byte(nonce(5))) // spent, so the order holds it until it reaches the front
	if _, err := f.d.Challenge(denproto.Random(denproto.NonceSize)); err != nil {
		t.Fatalf("a challenge with every nonce outstanding: %v", err)
	}
	if f.d.consumeNonce([]byte(nonce(0))) {
		t.Fatal("the oldest nonce is still outstanding")
	}
	if !f.d.consumeNonce([]byte(nonce(1))) || !f.d.consumeNonce([]byte(nonce(maxNonces-1))) {
		t.Fatal("a nonce newer than the oldest went")
	}
	if len(f.d.nonces) > maxNonces || len(f.d.nonceOrder)-f.d.nonceHead > maxNonces {
		t.Fatalf("%d nonces outstanding, %d in order", len(f.d.nonces), len(f.d.nonceOrder)-f.d.nonceHead)
	}
	// Those that expired go first.
	f.clock = f.clock.Add(2 * nonceLifetime)
	f.d.issueNonce(nonce(-1))
	if len(f.d.nonces) != 1 || len(f.d.nonceOrder)-f.d.nonceHead != 1 {
		t.Fatalf("%d nonces outstanding after the rest expired", len(f.d.nonces))
	}
}

// TestMessageBudget checks each member's budget across every channel and
// DM (M8): ten messages at once, then one every half second; and 256 KiB
// of text at once, then 16 KiB a second.
func TestMessageBudget(t *testing.T) {
	f := newFixture(t)
	for i := range 10 {
		if err := f.d.AllowMessage(1, 100); err != nil {
			t.Fatalf("message %d: %v", i+1, err)
		}
	}
	wantCode(t, f.d.AllowMessage(1, 100), denproto.CodeRateLimited)
	if err := f.d.AllowMessage(2, 100); err != nil {
		t.Fatalf("another member's budget: %v", err)
	}
	// Large texts spend the bytes first, and a refused one spends none.
	for range 4 {
		if err := f.d.AllowMessage(3, 64<<10); err != nil {
			t.Fatal(err)
		}
	}
	wantCode(t, f.d.AllowMessage(3, 64<<10), denproto.CodeRateLimited)
	time.Sleep(time.Second)
	if err := f.d.AllowMessage(3, 15<<10); err != nil {
		t.Fatalf("after a second's refill: %v", err)
	}
}

// TestOwnerInvite checks that an owner invite from the den's machine hands
// the den on (M8): whoever redeems it is the owner, the owner before them a
// moderator, and every member hears of both.
func TestOwnerInvite(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alice, err := f.join(f.create(), "alice", newDevice())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.SetupInvite(ctx); err == nil {
		t.Fatal("the page's setup made an owner invite for a den with an owner")
	}
	code, err := f.d.OwnerInvite(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := f.d.Authenticate(ctx, alice.Token)
	if list, err := f.d.Invites(ctx, s); err != nil || len(list) != 0 {
		t.Fatalf("staff see the owner invite: %+v %v", list, err)
	}
	sub, _, _, _ := f.d.Hub.Subscribe(s.MemberID, true, "", 0)
	_, err = f.join(code, "alice", newDevice())
	wantCode(t, err, denproto.CodeUsernameTaken)
	bob, err := f.join(code, "bob", newDevice())
	if err != nil || bob.Member.Role != denproto.RoleOwner {
		t.Fatalf("redeeming the owner invite: %+v %v", bob.Member, err)
	}
	if e := <-sub.Events; e.T != denproto.EventMemberJoined {
		t.Fatalf("event %+v", e)
	}
	e := <-sub.Events
	var m denproto.Member
	if e.T != denproto.EventMemberUpdated || json.Unmarshal(e.D, &m) != nil || m.ID != alice.Member.ID || m.Role != denproto.RoleModerator {
		t.Fatalf("event %+v", e)
	}
	if s, err = f.d.Authenticate(ctx, alice.Token); err != nil || s.Role != denproto.RoleModerator {
		t.Fatalf("the owner before: %+v %v", s, err)
	}
	_, err = f.join(code, "carol", newDevice())
	wantCode(t, err, denproto.CodeInviteInvalid)
	// The new owner runs the den, and the one before can't remove them.
	b, _ := f.d.Authenticate(ctx, bob.Token)
	name := "Bob's den"
	if _, err := f.d.Update(ctx, b, denproto.DenUpdateRequest{Name: &name}); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.d.Remove(ctx, s, bob.Member.ID, denproto.RemoveRequest{}), denproto.CodeForbidden)
	// An owner invite lasts an hour.
	code, err = f.d.OwnerInvite(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(ownerInviteLifetime + time.Second)
	_, err = f.join(code, "dave", newDevice())
	wantCode(t, err, denproto.CodeInviteInvalid)
}
