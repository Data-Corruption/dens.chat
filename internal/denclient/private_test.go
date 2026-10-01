package denclient_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/media"
)

// checkState waits for a DM's check to match, as one member's Dens sees
// it.
func checkState(t *testing.T, m *denclient.Manager, denID, dm string, match func(denclient.DMCheck) bool) denclient.DMCheck {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		st, err := m.CheckState(context.Background(), denID, dm)
		if err == nil && match(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting on the DM's check: %+v %v", st, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// checkDM starts a DM's key from a, lets both Dens move the exchange on,
// and has each member type the digits the other's screen shows.
func checkDM(t *testing.T, a, b *denclient.Manager, denID, dm string) {
	t.Helper()
	ctx := context.Background()
	for _, m := range []*denclient.Manager{a, b} {
		viewOf(t, m, denID, "the DM", func(v denclient.View) bool {
			return slices.ContainsFunc(v.Channels, func(c denproto.Channel) bool { return c.ID == dm })
		})
	}
	if err := a.StartDMKey(ctx, denID, dm, false); err != nil {
		t.Fatalf("start: %v", err)
	}
	ready := func(st denclient.DMCheck) bool { return st.Half != "" }
	x, y := checkState(t, a, denID, dm, ready), checkState(t, b, denID, dm, ready)
	if !x.Starter || y.Starter || x.KeyID != y.KeyID {
		t.Fatalf("the two sides: %+v %+v", x, y)
	}
	// Each screen shows its own half, which the other doesn't.
	if x.Half == y.Half {
		t.Fatal("both screens show the same digits")
	}
	if err := a.CheckDM(ctx, denID, dm, x.Half); err == nil {
		t.Fatal("a member passed the check with their own digits")
	}
	if err := a.CheckDM(ctx, denID, dm, y.Half); err != nil {
		t.Fatalf("a's check: %v", err)
	}
	// Whoever checks first still has their digits to read out.
	if st := checkState(t, a, denID, dm, func(st denclient.DMCheck) bool { return st.Checked }); st.Partner || st.Half != x.Half {
		t.Fatalf("a's check once a typed b's digits: %+v", st)
	}
	if err := b.CheckDM(ctx, denID, dm, x.Half); err != nil {
		t.Fatalf("b's check: %v", err)
	}
	both := func(st denclient.DMCheck) bool { return st.Checked && st.Partner }
	if st := checkState(t, a, denID, dm, both); st.Half != "" {
		t.Fatalf("digits once both checked: %+v", st)
	}
	checkState(t, b, denID, dm, both)
}

// openDM opens a DM from a to b and waits until both Dens have it.
func openDM(t *testing.T, a, b *denclient.Manager, denID string) string {
	t.Helper()
	dm, err := a.OpenDM(context.Background(), denID, me(t, b, denID).ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []*denclient.Manager{a, b} {
		viewOf(t, m, denID, "the DM", func(v denclient.View) bool {
			return slices.ContainsFunc(v.Channels, func(c denproto.Channel) bool { return c.ID == dm.ID })
		})
	}
	return dm.ID
}

func sendDM(t *testing.T, m *denclient.Manager, denID, dm, text string) denclient.PageMessage {
	t.Helper()
	msg, err := m.Send(context.Background(), denID, dm, denproto.SendRequest{Nonce: denproto.Random(16), Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func history(t *testing.T, m *denclient.Manager, denID, channel string) []denclient.PageMessage {
	t.Helper()
	page, err := m.History(context.Background(), denID, channel, denclient.HistoryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	return page.Messages
}

// A DM's text and files leave each member's Dens sealed: the den stores
// what it can't open, and the other member reads it all, with replies,
// edits, ticks and photos, which the sender's Dens strips and previews.
func TestDMsStaySealed(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	dm := openDM(t, owner, member, denID)
	if _, err := owner.Send(ctx, denID, dm, denproto.SendRequest{Nonce: denproto.Random(16), Text: "too soon"}); !isInput(err) {
		t.Fatalf("sending before the check: %v", err)
	}
	checkDM(t, owner, member, denID, dm)

	first := sendDM(t, owner, denID, dm, "the secret list:\n[ ] milk\n[ ] eggs")
	if first.Text != "the secret list:\n[ ] milk\n[ ] eggs" || first.Locked != "" || first.KeyID == "" || first.Sealed != nil {
		t.Fatalf("sent: %+v", first)
	}
	var stored []byte
	if err := h.s.db.QueryRow(`SELECT text FROM den_messages WHERE id = ?`, first.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("secret")) || len(stored) < 40 {
		t.Fatalf("the den stores the DM's text as %q", stored)
	}
	// Not even the den's own data key opens it, as it does a channel's.
	if _, err := h.s.v.Open(stored, []byte("den_messages.text:"+first.ID)); err == nil {
		t.Fatal("the den's data key opens the DM's text")
	}
	got := history(t, member, denID, dm)
	if len(got) != 1 || got[0].Text != first.Text || got[0].Locked != "" || got[0].Nonce != nil {
		t.Fatalf("bob's history: %+v", got)
	}
	reply, err := member.Send(ctx, denID, dm, denproto.SendRequest{Nonce: denproto.Random(16), Text: "on it", ReplyTo: first.ID})
	if err != nil || reply.Reply == nil || reply.Reply.Text != first.Text || reply.Reply.Locked || reply.Message.Reply != nil {
		t.Fatalf("a reply: %+v %v", reply, err)
	}

	edited, err := owner.Edit(ctx, denID, dm, first.ID, denproto.EditRequest{Revision: 1, Text: "the list:\n[ ] milk\n[ ] eggs"})
	if err != nil || edited.Revision != 2 || edited.EditedAt == 0 || edited.Text != "the list:\n[ ] milk\n[ ] eggs" {
		t.Fatalf("an edit: %+v %v", edited, err)
	}
	ticked, err := owner.SetTask(ctx, denID, dm, first.ID, 1, denproto.TaskRequest{Checked: true, Text: "eggs"})
	if err != nil || ticked.Revision != 3 || ticked.Text != "the list:\n[ ] milk\n[x] eggs" || ticked.EditedAt != edited.EditedAt {
		t.Fatalf("a tick: %+v %v", ticked, err)
	}
	_, err = owner.SetTask(ctx, denID, dm, first.ID, 1, denproto.TaskRequest{Checked: true, Text: "bread"})
	var conflict *denclient.ErrEditConflict
	if !errors.As(err, &conflict) || conflict.Current.Text != ticked.Text {
		t.Fatalf("a tick on a line that moved: %v", err)
	}
	if _, err := owner.Edit(ctx, denID, dm, first.ID, denproto.EditRequest{Revision: 1, Text: "stale"}); !errors.As(err, &conflict) {
		t.Fatalf("a stale edit: %v", err)
	}

	photo := gpsJPEG(t)
	up, err := owner.Upload(ctx, denID, dm, "IMG_2001.jpg", int64(len(photo)), bytes.NewReader(photo))
	if err != nil || !up.Stripped || up.Thumb == nil || up.Width == 0 || up.Type != "image/jpeg" || up.Sealed {
		t.Fatalf("a DM photo: %+v %v", up, err)
	}
	msg, err := owner.Send(ctx, denID, dm, denproto.SendRequest{Nonce: denproto.Random(16), Attachments: []string{up.ID}})
	if err != nil || len(msg.Attachments) != 1 || msg.Attachments[0].ID != up.ID {
		t.Fatalf("sending it: %+v %v", msg, err)
	}
	var seen denclient.PageMessage
	for _, m := range history(t, member, denID, dm) {
		if m.ID == msg.ID {
			seen = m
		}
	}
	if len(seen.Attachments) != 1 || seen.Attachments[0].Name != "IMG_2001.jpg" || seen.Attachments[0].Thumb == nil {
		t.Fatalf("bob sees %+v", seen)
	}
	data, kind, err := fetch(t, member, denID, up.ID, false)
	if err != nil || kind != media.JPEG || bytes.Contains(data, []byte("GPSLatitude")) || int64(len(data)) != up.Size {
		t.Fatalf("bob's copy of the photo: %d bytes, %v, %v", len(data), kind, err)
	}
	if _, kind, err := fetch(t, member, denID, up.ID, true); err != nil || !kind.Image() {
		t.Fatalf("its preview: %v %v", kind, err)
	}
	// All the den has of it is noise.
	bob, _ := denproto.ParseID(me(t, member, denID).ID)
	r, _, err := h.d.OpenFile(ctx, &den.Session{MemberID: bob}, up.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := io.ReadAll(r)
	r.Close()
	if media.Sniff(blob[:media.SniffLen]) != media.Other || bytes.Contains(blob, data[:64]) {
		t.Fatal("the den can read the photo")
	}
}

// Someone at the den who plays each member's side of the exchange to the
// other, as a tampered den could, ends up sharing a key with each; but the
// two members' digits don't match, so neither check passes and nothing is
// sent.
func TestDenInTheMiddle(t *testing.T) {
	h, owner, _, denID, _ := chatDen(t)
	ctx := context.Background()
	mitm := h.middle(t)
	carol := h.clientThrough(t, mitm.Listener.Addr().String())
	invite, _, err := owner.CreateInvite(ctx, denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	join(t, carol, invite, "carol")
	dm := openDM(t, owner, carol, denID)
	if err := owner.StartDMKey(ctx, denID, dm, false); err != nil {
		t.Fatal(err)
	}
	ready := func(st denclient.DMCheck) bool { return st.Half != "" }
	x, y := checkState(t, owner, denID, dm, ready), checkState(t, carol, denID, dm, ready)
	if !mitm.played() {
		t.Fatal("the middle never played both sides")
	}
	if err := owner.CheckDM(ctx, denID, dm, y.Half); !isInput(err) {
		t.Fatalf("alice typing carol's digits: %v", err)
	}
	if err := carol.CheckDM(ctx, denID, dm, x.Half); !isInput(err) {
		t.Fatalf("carol typing alice's digits: %v", err)
	}
	if _, err := owner.Send(ctx, denID, dm, denproto.SendRequest{Nonce: denproto.Random(16), Text: "hi"}); !isInput(err) {
		t.Fatalf("sending without a check: %v", err)
	}
}

// middle sits between one member's Dens and the den, and runs one DM
// exchange with each side: it shows the member an offer of its own,
// answers the other side's offer itself, and shows the member the reveal
// that fits its offer.
type middle struct {
	*httptest.Server
	mu sync.Mutex
	// The sides it plays, by the real offer's commitment.
	fake map[string]*played
}

// played is the middle's two sides of one exchange: its offer to the
// member, its answer to the real offer, and the member's answer to its
// offer.
type played struct {
	starter   denproto.Starter
	offer     denproto.ExchangeOffer
	answer    denproto.ExchangeAnswer
	responder denproto.Responder
	theirs    *denproto.ExchangeAnswer
}

func (mi *middle) played() bool {
	mi.mu.Lock()
	defer mi.mu.Unlock()
	for _, p := range mi.fake {
		if p.theirs != nil {
			return true
		}
	}
	return false
}

func (h *denHost) middle(t *testing.T) *middle {
	t.Helper()
	target, _ := url.Parse("https://example.com")
	proxy := httputil.NewSingleHostReverseProxy(target)
	transport := h.public.Client().Transport.(*http.Transport).Clone()
	public := h.public.Listener.Addr().String()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, public)
	}
	proxy.Transport = transport
	mi := &middle{fake: map[string]*played{}}
	keys := regexp.MustCompile(`^/api/dms/(\d+)/keys$`)
	answers := regexp.MustCompile(`^/api/dms/(\d+)/keys/\d+/answer$`)
	info, _ := h.d.Info()
	// play returns the fake sides of an exchange, made when the member
	// first sees its real offer: an offer of its own for the member, and
	// its own answer to the real offer for the other side.
	play := func(channel string, k denproto.DMKey) *played {
		mi.mu.Lock()
		defer mi.mu.Unlock()
		o := k.Exchange.Offer
		if p, ok := mi.fake[o.Commit.String()]; ok {
			return p
		}
		ch, _ := denproto.ParseID(channel)
		starter, _ := denproto.ParseID(k.StartedBy)
		var other int64
		for _, m := range dmMembers(t, h, ch) {
			if m != starter {
				other = m
			}
		}
		xctx := denproto.DMExchangeContext(info.ID, ch, starter, other)
		s, offer, _ := denproto.StartExchange(xctx)
		r, answer, _ := denproto.AnswerExchange(xctx, o)
		p := &played{starter: s, offer: offer, answer: answer, responder: r}
		mi.fake[o.Commit.String()] = p
		return p
	}
	proxy.ModifyResponse = func(res *http.Response) error {
		m := keys.FindStringSubmatch(res.Request.URL.Path)
		if res.Request.Method != http.MethodGet || m == nil || res.StatusCode != http.StatusOK {
			return nil
		}
		var list denproto.DMKeyList
		if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
			return err
		}
		res.Body.Close()
		for i, k := range list.Keys {
			if k.Exchange == nil {
				continue
			}
			p := play(m[1], k)
			x := k.Exchange
			x.Offer = p.offer
			mi.mu.Lock()
			if x.Answer != nil && p.theirs != nil {
				x.Answer = p.theirs
			}
			if x.Reveal != nil && p.theirs != nil {
				rev, _, _ := p.starter.Finish(*p.theirs)
				x.Reveal = &rev
			}
			mi.mu.Unlock()
			list.Keys[i].Exchange = x
		}
		data, _ := json.Marshal(list)
		res.Body = io.NopCloser(bytes.NewReader(data))
		res.ContentLength = int64(len(data))
		res.Header.Set("Content-Length", strconv.Itoa(len(data)))
		return nil
	}
	mi.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost && answers.MatchString(req.URL.Path) {
			var body denproto.KeyAnswerRequest
			if err := json.NewDecoder(req.Body).Decode(&body); err == nil {
				mi.mu.Lock()
				for _, p := range mi.fake {
					if p.theirs == nil {
						theirs := body.Answer
						p.theirs = &theirs
						body.Answer = p.answer
					}
				}
				mi.mu.Unlock()
			}
			data, _ := json.Marshal(body)
			req.Body = io.NopCloser(bytes.NewReader(data))
			req.ContentLength = int64(len(data))
		}
		req.Host = "example.com"
		proxy.ServeHTTP(w, req)
	}))
	mi.TLS = h.public.TLS
	mi.StartTLS()
	t.Cleanup(mi.Close)
	return mi
}

// dmMembers are a DM's two members, as the den has them.
func dmMembers(t *testing.T, h *denHost, channel int64) []int64 {
	t.Helper()
	var low, high int64
	if err := h.s.db.QueryRow(`SELECT dm_low, dm_high FROM den_channels WHERE id = ?`, channel).Scan(&low, &high); err != nil {
		t.Fatal(err)
	}
	return []int64{low, high}
}

// clientThrough starts a Manager that reaches the den's public address,
// example.com, at addr instead.
func (h *denHost) clientThrough(t *testing.T, addr string) *denclient.Manager {
	t.Helper()
	m, _ := h.clientVia(t, newStore(t), noOwnDen, addr)
	return m
}

// A new device the member approves gets their seal, and with it their DM
// history. Starting over there signs their other devices out, leaves what
// the old keys sealed unreadable to them, though not to the other member,
// and each DM needs a new check.
func TestNewDevicesAndStartingOver(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	dm := openDM(t, owner, member, denID)
	checkDM(t, owner, member, denID, dm)
	before := sendDM(t, member, denID, dm, "from before")

	laptop := h.client(t, noOwnDen)
	signInApproved(t, laptop, member, denID, denclient.SignInRequest{Den: "https://example.com", Username: "bob", Password: "correct horse"}, false)
	waitFor(t, laptop, "the laptop connected", connected)
	got := history(t, laptop, denID, dm)
	if len(got) != 1 || got[0].Text != "from before" || got[0].Locked != "" {
		t.Fatalf("the laptop's history: %+v", got)
	}

	if _, err := laptop.StartOver(ctx, denID, "wrong password"); !denproto.IsCode(err, denproto.CodeWrongPassword) {
		t.Fatalf("starting over with the wrong password: %v", err)
	}
	seal, err := laptop.StartOver(ctx, denID, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := denproto.ParseSeal(seal); err != nil {
		t.Fatalf("the new seal %q: %v", seal, err)
	}
	waitFor(t, member, "the first device signed out", revoked("You started over"))
	if shown, err := laptop.ShowSeal(denID); err != nil || shown != seal {
		t.Fatalf("the seal kept: %q %v", shown, err)
	}
	got = history(t, laptop, denID, dm)
	if len(got) != 1 || got[0].Locked != "lost" || got[0].Text != "" {
		t.Fatalf("the old message after starting over: %+v", got)
	}
	if got := history(t, owner, denID, dm); len(got) != 1 || got[0].Text != "from before" {
		t.Fatalf("the other member lost their history: %+v", got)
	}
	if _, err := laptop.Send(ctx, denID, dm, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x"}); !isInput(err) {
		t.Fatalf("sending under the old key: %v", err)
	}
	if _, err := owner.Send(ctx, denID, dm, denproto.SendRequest{Nonce: denproto.Random(16), Text: "x"}); !isInput(err) {
		t.Fatalf("the other member sending under the old key: %v", err)
	}
	checkDM(t, laptop, owner, denID, dm)
	after := sendDM(t, owner, denID, dm, "after")
	got = history(t, laptop, denID, dm)
	if len(got) != 2 || got[1].Text != "after" || got[0].KeyID == got[1].KeyID || got[0].ID != before.ID || after.KeyID != got[1].KeyID {
		t.Fatalf("history across the new check: %+v", got)
	}
	v, _ := laptop.View(denID)
	var retired denproto.DMKey
	for _, k := range v.DMKeys {
		if k.ID == before.KeyID {
			retired = k
		}
	}
	if retired.Retired != denproto.RetiredStartedOver || retired.RetiredBy != me(t, laptop, denID).ID || retired.Sealed != nil {
		t.Fatalf("the old key as the page sees it: %+v", retired)
	}
}

// A device signed in with a recovery code has no seal, so DMs say so until
// the member types the one they saved.
func TestRecoveredDeviceTypesTheSeal(t *testing.T) {
	h, owner, _, denID, _ := chatDen(t)
	ctx := context.Background()
	desk := h.client(t, noOwnDen)
	joined := joinWithCodes(t, owner, desk, denID, "dora")
	if !joined.SealNew || joined.Seal == "" {
		t.Fatalf("a first join makes a seal: %+v", joined)
	}
	dm := openDM(t, owner, desk, denID)
	checkDM(t, owner, desk, denID, dm)
	sendDM(t, owner, denID, dm, "for dora")

	fresh := h.client(t, noOwnDen)
	if _, err := signIn(t, fresh, denclient.SignInRequest{Den: "https://example.com", Username: "dora", Password: "next password", RecoveryCode: joined.RecoveryCodes[0]}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, fresh, "the fresh machine connected", connected)
	if got := history(t, fresh, denID, dm); len(got) != 1 || got[0].Locked != "no_seal" {
		t.Fatalf("before the seal: %+v", got)
	}
	if st, err := fresh.CheckState(ctx, denID, dm); err != nil || st.Seal {
		t.Fatalf("the check without a seal: %+v %v", st, err)
	}
	if err := fresh.TypeSeal(ctx, denID, "not a seal"); !isInput(err) {
		t.Fatalf("a mistyped seal: %v", err)
	}
	if err := fresh.TypeSeal(ctx, denID, joined.Seal); err != nil {
		t.Fatal(err)
	}
	if got := history(t, fresh, denID, dm); len(got) != 1 || got[0].Text != "for dora" {
		t.Fatalf("after the seal: %+v", got)
	}
}
