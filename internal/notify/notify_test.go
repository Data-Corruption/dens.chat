package notify

import (
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// place stands in for the helper or the shell.
type place struct {
	mu        sync.Mutex
	can       bool
	shown     []Notification
	withdrawn []string
	settings  []types.Notifications
}

func (p *place) Show(n Notification) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.can {
		p.shown = append(p.shown, n)
	}
	return p.can
}

func (p *place) Withdraw(keys []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.withdrawn = append(p.withdrawn, keys...)
}

func (p *place) Settings(s types.Notifications) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.settings = append(p.settings, s)
}

func (p *place) messages() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, n := range p.shown {
		out = append(out, n.Message)
	}
	return out
}

func testLog(t *testing.T) *xlog.Logger {
	t.Helper()
	log, err := xlog.New(filepath.Join(t.TempDir(), "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	return log
}

func newHub(t *testing.T, s types.Notifications, helper Place, android bool) (*Hub, *[]types.Notifications) {
	t.Helper()
	var kept []types.Notifications
	h := New(testLog(t), s, func(n types.Notifications) error {
		kept = append(kept, n)
		return nil
	}, helper, android)
	return h, &kept
}

// events drains what a page has been sent.
func events(ch <-chan Event) (shows []Notification, withdrawn []string, settings int) {
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			if e.Show != nil {
				shows = append(shows, *e.Show)
			}
			withdrawn = append(withdrawn, e.Withdraw...)
			if e.Status != nil {
				settings++
			}
		default:
			return
		}
	}
}

// drain is what a page has been sent, as a channel that ends.
func drain(ch <-chan Event) <-chan Event {
	out := make(chan Event, pageBuffer)
	for {
		select {
		case e := <-ch:
			out <- e
		default:
			close(out)
			return out
		}
	}
}

func dm(id string) Message {
	return Message{Den: "d1", DenName: "Den", Channel: "7", DM: true, ID: id, Author: "Alice", Text: "hello **there** ||secret||"}
}

func mention(id string) Message {
	return Message{Den: "d1", DenName: "Den", Channel: "5", ChannelName: "general", ID: id, Author: "Bob", Mention: true, Text: "@me look"}
}

func TestNothingNotifiesUntilAKindIsOn(t *testing.T) {
	helper := &place{can: true}
	h, _ := newHub(t, types.Notifications{}, helper, false)
	h.Message(dm("10"))
	h.Message(mention("11"))
	if got := helper.messages(); len(got) != 0 {
		t.Fatalf("notified %v with every kind off", got)
	}
	if err := h.SetKinds(nil, ptr(true), nil); err != nil {
		t.Fatal(err)
	}
	h.Message(dm("12"))
	h.Message(mention("13"))
	// A mention in a channel that isn't a DM is a mention; a DM mentioning
	// the member is a DM.
	h.Message(Message{Den: "d1", Channel: "7", DM: true, Mention: true, ID: "14", Author: "Alice", Text: "@me"})
	if got := helper.messages(); !slices.Equal(got, []string{"13"}) {
		t.Fatalf("with mentions on, notified %v, want 13", got)
	}
}

func TestWhereANotificationShows(t *testing.T) {
	helper := &place{can: true}
	h, _ := newHub(t, types.Notifications{DMs: true}, helper, false)
	a, stopA := h.Watch("a")
	defer stopA()
	b, stopB := h.Watch("b")
	defer stopB()
	events(a)
	events(b)

	// A page showing the DM on screen: nothing notifies.
	h.SetPage("a", true, true)
	h.Focus("a", "d1", "7")
	h.Message(dm("10"))
	if s, _, _ := events(a); len(s) != 0 || len(helper.messages()) != 0 {
		t.Fatalf("notified though a page shows the DM: page %v, helper %v", s, helper.messages())
	}

	// The page shows another channel: it shows it, through the browser.
	h.Focus("a", "d1", "5")
	h.Message(dm("11"))
	if s, _, _ := events(a); len(s) != 1 || s[0].Message != "11" || s[0].Text != "hello there (spoiler)" || s[0].Title != "Alice" {
		t.Fatalf("page a was sent %+v, want message 11 with its start", s)
	}

	// Hidden, with b on screen most lately and the browser letting it
	// notify, b shows it.
	h.SetPage("a", false, true)
	h.Focus("a", "d1", "")
	h.SetPage("b", true, true)
	h.SetPage("b", false, true)
	h.Message(dm("12"))
	if s, _, _ := events(b); len(s) != 1 || s[0].Message != "12" {
		t.Fatalf("page b was sent %+v, want message 12", s)
	}
	// Where the channel's notification moved from, it's taken back.
	if _, w, _ := events(a); !slices.Equal(w, []string{"d1/7"}) {
		t.Fatalf("page a was told to take back %v, want d1/7", w)
	}

	// No page may notify: the helper shows it, and b takes its back.
	h.SetPage("a", false, false)
	h.SetPage("b", false, false)
	h.Message(dm("13"))
	if got := helper.messages(); !slices.Equal(got, []string{"13"}) {
		t.Fatalf("the helper showed %v, want 13", got)
	}
	if _, w, _ := events(b); !slices.Equal(w, []string{"d1/7"}) {
		t.Fatalf("page b was told to take back %v, want d1/7", w)
	}

	// A helper that can't show it, and no shell: it shows nowhere.
	helper.mu.Lock()
	helper.can = false
	helper.mu.Unlock()
	h.Message(dm("14"))
	if got := helper.messages(); len(got) != 1 {
		t.Fatalf("the helper showed %v", got)
	}
}

func TestTheShellShowsWhatNothingElseCan(t *testing.T) {
	shell := &place{can: true}
	h, _ := newHub(t, types.Notifications{DMs: true}, &place{}, true)
	h.SetShell(shell)
	p, stop := h.Watch("webview")
	defer stop()
	// The shell holds its page as shown through a call: the page says it's
	// on screen, showing the DM, but the app isn't in front.
	h.SetPage("webview", true, false)
	h.Focus("webview", "d1", "7")
	h.Message(dm("10"))
	if got := shell.messages(); !slices.Equal(got, []string{"10"}) {
		t.Fatalf("the shell showed %v with the app behind, want 10", got)
	}
	h.SetFront(true)
	h.Message(dm("11"))
	if got := shell.messages(); len(got) != 1 {
		t.Fatalf("the shell showed %v with the app in front, showing the DM", got)
	}
	if s, _, _ := events(p); len(s) != 0 {
		t.Fatalf("the page was sent %v", s)
	}
	if len(shell.settings) != 1 || !shell.settings[0].DMs {
		t.Fatalf("the shell heard the settings %v", shell.settings)
	}
	// The page hears whether Android lets the app post notifications.
	if st := h.Status(); st.Shell == nil || st.Shell.Allowed {
		t.Fatalf("the status before the shell said %+v", st)
	}
	h.SetAllowed(true)
	var last *Status
	for e := range drain(p) {
		if e.Status != nil {
			last = e.Status
		}
	}
	if last == nil || !last.Shell.Allowed {
		t.Fatalf("the page heard %+v", last)
	}
}

func TestOnceAndReplaced(t *testing.T) {
	helper := &place{can: true}
	h, _ := newHub(t, types.Notifications{DMs: true}, helper, false)
	h.Message(dm("10"))
	h.Message(dm("10"))
	h.Message(dm("9"))
	h.Message(dm("11"))
	if got := helper.messages(); !slices.Equal(got, []string{"10", "11"}) {
		t.Fatalf("the helper showed %v, want 10 then 11", got)
	}
	if helper.shown[1].Key != helper.shown[0].Key {
		t.Fatalf("a newer message's key %q differs from its channel's %q", helper.shown[1].Key, helper.shown[0].Key)
	}
}

func TestReadingTakesItBack(t *testing.T) {
	helper := &place{can: true}
	h, _ := newHub(t, types.Notifications{DMs: true, Mentions: true}, helper, false)
	h.Message(dm("100"))
	h.Message(mention("101"))
	h.Cleared("d1", "7", "99")
	if len(helper.withdrawn) != 0 {
		t.Fatalf("reading up to an older message took back %v", helper.withdrawn)
	}
	h.Cleared("d1", "7", "100")
	if !slices.Equal(helper.withdrawn, []string{"d1/7"}) {
		t.Fatalf("reading the DM took back %v, want d1/7", helper.withdrawn)
	}
	// Once taken back, the same message doesn't come back, but a newer
	// one shows.
	h.Deleted("d1", "5", "100")
	h.Deleted("d1", "5", "101")
	if !slices.Equal(helper.withdrawn, []string{"d1/7", "d1/5"}) {
		t.Fatalf("deleting the mention took back %v", helper.withdrawn)
	}
	h.Message(mention("102"))
	h.Cleared("d1", "", "1000")
	if !slices.Equal(helper.withdrawn, []string{"d1/7", "d1/5", "d1/5"}) {
		t.Fatalf("retention through 1000 took back %v", helper.withdrawn)
	}
}

func TestMutes(t *testing.T) {
	helper := &place{can: true}
	h, kept := newHub(t, types.Notifications{DMs: true, Mentions: true}, helper, false)
	h.Message(dm("10"))
	if err := h.Mute("d1", "7", true); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(helper.withdrawn, []string{"d1/7"}) {
		t.Fatalf("muting the DM took back %v", helper.withdrawn)
	}
	h.Message(dm("11"))
	h.Message(mention("12"))
	if got := helper.messages(); !slices.Equal(got, []string{"10", "12"}) {
		t.Fatalf("with the DM muted, the helper showed %v, want 10 and 12", got)
	}
	if err := h.Mute("d1", "", true); err != nil {
		t.Fatal(err)
	}
	h.Message(mention("13"))
	if got := helper.messages(); len(got) != 2 {
		t.Fatalf("with the den muted, the helper showed %v", got)
	}
	if m := h.Muted("d1"); !m.Den || !slices.Equal(m.Channels, []string{"7"}) {
		t.Fatalf("the den's mutes are %+v", m)
	}
	if err := h.Mute("d1", "", false); err != nil {
		t.Fatal(err)
	}
	if err := h.Mute("d1", "7", false); err != nil {
		t.Fatal(err)
	}
	if s := h.Settings(); s.Muted != nil {
		t.Fatalf("unmuting everything left %v", s.Muted)
	}
	if len(*kept) != 4 {
		t.Fatalf("kept the settings %d times, want 4", len(*kept))
	}

	// Leaving a den forgets its mutes and takes back what it shows.
	if err := h.Mute("d1", "5", true); err != nil {
		t.Fatal(err)
	}
	h.Message(dm("14"))
	h.Forgot("d1")
	if s := h.Settings(); s.Muted != nil || !slices.Contains(helper.withdrawn, "d1/7") {
		t.Fatalf("after forgetting the den, mutes %v and took back %v", s.Muted, helper.withdrawn)
	}
}

func TestTurningAKindOffTakesItBack(t *testing.T) {
	helper := &place{can: true}
	h, _ := newHub(t, types.Notifications{DMs: true, Mentions: true}, helper, false)
	p, stop := h.Watch("a")
	defer stop()
	h.Message(dm("10"))
	h.Message(mention("11"))
	if err := h.SetKinds(ptr(false), nil, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(helper.withdrawn, []string{"d1/7"}) {
		t.Fatalf("turning DMs off took back %v, want d1/7", helper.withdrawn)
	}
	// Each page and place hears the settings.
	if _, _, settings := events(p); settings != 2 {
		t.Fatalf("the page heard the settings %d times, want 2", settings)
	}
	if len(helper.settings) != 1 || helper.settings[0].DMs {
		t.Fatalf("the helper heard %+v", helper.settings)
	}
}

func TestHiddenText(t *testing.T) {
	helper := &place{can: true}
	h, _ := newHub(t, types.Notifications{DMs: true, Mentions: true, HideText: true}, helper, false)
	h.Message(dm("10"))
	h.Message(mention("11"))
	h.Message(Message{Den: "d1", Channel: "8", DM: true, ID: "12", Author: "Carol", Files: 2})
	h.SetKinds(nil, nil, ptr(false))
	h.Message(Message{Den: "d1", Channel: "9", DM: true, ID: "13", Author: "Dan", Files: 2})
	h.Message(Message{Den: "d1", Channel: "10", DM: true, ID: "14", Author: "Eve"})
	var got [][2]string
	for _, n := range helper.shown {
		got = append(got, [2]string{n.Title, n.Text})
	}
	want := [][2]string{{"Alice", "Sent you a message"}, {"Bob in #general", "Mentioned you"}, {"Carol", "Sent you a message"},
		{"Dan", "Sent 2 files"}, {"Eve", "Sent you a message"}}
	if !slices.Equal(got, want) {
		t.Fatalf("shown %q, want %q", got, want)
	}
	if n := helper.shown[0]; n.Path != "/den/d1/7#message=10" || n.URL("http://localhost:8484", "") != "http://localhost:8484/den/d1/7#message=10" ||
		n.URL("http://localhost:8484", "tok") != "http://localhost:8484/den/d1/7#token=tok&message=10" {
		t.Fatalf("the notification's path %q, and its URLs %q, %q", n.Path, n.URL("http://localhost:8484", ""), n.URL("http://localhost:8484", "tok"))
	}
}

func TestSettingsThatWontKeepDontChange(t *testing.T) {
	h := New(testLog(t), types.Notifications{}, func(types.Notifications) error { return errors.New("disk full") }, nil, false)
	if err := h.SetKinds(ptr(true), nil, nil); err == nil {
		t.Fatal("settings that couldn't be kept changed nothing, and said nothing")
	}
	if h.Settings().DMs {
		t.Fatal("settings that couldn't be kept changed")
	}
}

// A page that goes, as when its stream reconnects, takes nothing back
// with it, and its message doesn't notify again.
func TestAPageThatGoes(t *testing.T) {
	helper := &place{can: true}
	h, _ := newHub(t, types.Notifications{DMs: true}, helper, false)
	p, stop := h.Watch("a")
	h.SetPage("a", false, true)
	h.Message(dm("10"))
	if s, _, _ := events(p); len(s) != 1 {
		t.Fatalf("the page was sent %v", s)
	}
	stop()
	h.Message(dm("10"))
	h.Cleared("d1", "7", "10")
	if len(helper.shown) != 0 || len(helper.withdrawn) != 0 {
		t.Fatalf("after the page went, the helper showed %v and took back %v", helper.messages(), helper.withdrawn)
	}
	h.Message(dm("11"))
	if got := helper.messages(); !slices.Equal(got, []string{"11"}) {
		t.Fatalf("a newer message showed %v through the helper, want 11", got)
	}
}

func TestAPageThatFallsBehindIsDropped(t *testing.T) {
	h, _ := newHub(t, types.Notifications{DMs: true}, &place{}, false)
	p, stop := h.Watch("a")
	defer stop()
	h.SetPage("a", false, true)
	// A channel each, so none replaces another.
	for i := range pageBuffer + 1 {
		h.Message(Message{Den: "d1", Channel: strconv.Itoa(100 + i), DM: true, ID: "10", Author: "Alice"})
	}
	n := 0
	for range p {
		n++
	}
	if n > pageBuffer {
		t.Fatalf("the page was sent %d events before its stream closed", n)
	}
}
