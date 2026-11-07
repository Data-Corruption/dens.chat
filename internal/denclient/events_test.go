package denclient_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/notify"
)

// eventsIn says what a member's view shows of the den's events: each by
// name, with how many are interested, the member's mark and Live.
func eventsIn(t *testing.T, m *denclient.Manager, denID string) string {
	t.Helper()
	v, err := m.View(denID)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range v.Events {
		s := fmt.Sprintf("%s:%d", e.Name, e.Interested)
		if e.Mine == nil {
			t.Fatalf("the view's %q carries no mark", e.Name)
		}
		if *e.Mine {
			s += "+mine"
		}
		if e.Live {
			s += "+live"
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// The den's events reach each member's view with their own marks, and a
// cached member's view keeps them with the den away (M17.4). The member
// marks one Interested, which the others' views count; as it starts the
// member's Dens notifies them, naming it and its voice channel, and
// carol's, not interested, doesn't; End now takes the notice back.
func TestEvents(t *testing.T) {
	heard := &notices{}
	carolHeard := &notices{}
	h, owner, member, denID, _ := chatDen(t, func(m *denclient.Manager) { m.Notices = heard })
	h.d.StartTimer()
	ctx := context.Background()
	s := newCachedStore(t)
	invite, _, err := owner.CreateInvite(ctx, denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	carol, stop := h.clientVia(t, s, noOwnDen, h.public.Listener.Addr().String(), func(m *denclient.Manager) { m.Notices = carolHeard })
	join(t, carol, invite, "carol")
	name, kind := "lounge", denproto.KindVoice
	if err := owner.Manage(ctx, denID, "channels", http.MethodPost, "", denproto.ChannelRequest{Name: &name, Kind: kind}); err != nil {
		t.Fatal(err)
	}
	var lounge string
	eventually(t, "the lounge on every view", func() bool {
		for _, m := range []*denclient.Manager{owner, member, carol} {
			v, err := m.View(denID)
			if err != nil {
				t.Fatal(err)
			}
			i := slices.IndexFunc(v.Channels, func(c denproto.Channel) bool { return c.Kind == denproto.KindVoice })
			if i < 0 {
				return false
			}
			lounge = v.Channels[i].ID
		}
		return true
	})
	v, err := member.View(denID)
	if err != nil || v.EventLimits == nil || v.EventLimits.MemberEvents != 0 || v.Events == nil {
		t.Fatalf("bob's view has event limits %+v and events %v, %v", v.EventLimits, v.Events, err)
	}
	// A member making one, while events are staff's, is refused by the den.
	starts, ends := time.Now().Add(time.Hour).UnixMilli(), time.Now().Add(2*time.Hour).UnixMilli()
	night := "Game night"
	req := denproto.EventRequest{Name: &night, StartsAt: &starts, EndsAt: &ends, ChannelID: &lounge}
	if _, err := member.CreateEvent(ctx, denID, req); !denproto.IsCode(err, denproto.CodeForbidden) {
		t.Fatalf("bob made an event: %v", err)
	}
	e, err := owner.CreateEvent(ctx, denID, req)
	if err != nil || e.Mine == nil || *e.Mine || e.ChannelID != lounge {
		t.Fatalf("alice made %+v, %v", e, err)
	}
	eventually(t, "the event on every view", func() bool {
		return eventsIn(t, member, denID) == "Game night:0" && eventsIn(t, carol, denID) == "Game night:0"
	})

	in, err := member.SetInterest(ctx, denID, e.ID, true)
	if err != nil || in.Interested != 1 || !*in.Mine {
		t.Fatalf("bob's mark: %+v, %v", in, err)
	}
	eventually(t, "bob's mark counted", func() bool {
		return eventsIn(t, member, denID) == "Game night:1+mine" && eventsIn(t, owner, denID) == "Game night:1" &&
			eventsIn(t, carol, denID) == "Game night:1"
	})

	// Moved to start now, it starts at once.
	now, later := time.Now().UnixMilli(), time.Now().Add(time.Hour).UnixMilli()
	if _, err := owner.UpdateEvent(ctx, denID, e.ID, denproto.EventRequest{StartsAt: &now, EndsAt: &later}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "bob's Dens to hear it start", func() bool {
		heard.mu.Lock()
		defer heard.mu.Unlock()
		return len(heard.starts) > 0
	})
	heard.mu.Lock()
	got := heard.starts[0]
	heard.mu.Unlock()
	want := notify.Start{Den: denID, DenName: "Chat Den", Event: e.ID, Name: "Game night", Channel: "lounge"}
	if got != want {
		t.Errorf("bob's Dens was told %+v, want %+v", got, want)
	}
	eventually(t, "the event live on every view", func() bool {
		return eventsIn(t, member, denID) == "Game night:1+mine+live" && eventsIn(t, carol, denID) == "Game night:1+live"
	})
	carolHeard.mu.Lock()
	if len(carolHeard.starts) != 0 {
		t.Errorf("carol's Dens, not interested, was told of the start: %+v", carolHeard.starts)
	}
	carolHeard.mu.Unlock()

	// A second event, to come, which carol's cache keeps.
	soon, after := time.Now().Add(time.Hour).UnixMilli(), time.Now().Add(3*time.Hour).UnixMilli()
	quiz, place := "Quiz", "The back room"
	if _, err := owner.CreateEvent(ctx, denID, denproto.EventRequest{Name: &quiz, StartsAt: &soon, EndsAt: &after, Place: &place}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the quiz on carol's view", func() bool { return eventsIn(t, carol, denID) == "Game night:1+live Quiz:0" })

	if err := owner.EndEvent(ctx, denID, e.ID); err != nil {
		t.Fatal(err)
	}
	heard.waitSaid(t, "over "+e.ID)
	eventually(t, "the event gone from every view", func() bool {
		return eventsIn(t, member, denID) == "Quiz:0" && eventsIn(t, carol, denID) == "Quiz:0"
	})

	// With the den out of reach, carol's Dens, started again, shows what
	// it kept.
	stop()
	carol, stop = h.clientVia(t, s, noOwnDen, "127.0.0.1:1")
	defer stop()
	eventually(t, "carol's kept view", func() bool {
		v, err := carol.View(denID)
		return err == nil && len(v.Events) == 1
	})
	if got := eventsIn(t, carol, denID); got != "Quiz:0" {
		t.Errorf("with the den away, carol's view shows %s", got)
	}
	v, err = carol.View(denID)
	if err != nil || v.EventLimits == nil || v.Events[0].Place != place {
		t.Errorf("with the den away, carol's view keeps the limits %+v and %+v, %v", v.EventLimits, v.Events, err)
	}
}

// A link's card reads an event by its ID (M17.5): one to come from the
// view, with the member's mark and its badge, so the den hears nothing of
// it; an ended one from the den, saying so with its badge; and one the
// member can't see not at all.
func TestEventCards(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t)
	h.d.StartTimer()
	ctx := context.Background()
	staffOnly := true
	for _, c := range []denproto.ChannelRequest{{Name: ptrTo("lounge"), Kind: denproto.KindVoice}, {Name: ptrTo("office"), Kind: denproto.KindVoice, StaffOnly: &staffOnly}} {
		if err := owner.Manage(ctx, denID, "channels", http.MethodPost, "", c); err != nil {
			t.Fatal(err)
		}
	}
	var lounge, office string
	eventually(t, "the channels on alice's view", func() bool {
		v, err := owner.View(denID)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range v.Channels {
			switch c.Name {
			case "lounge":
				lounge = c.ID
			case "office":
				office = c.ID
			}
		}
		return lounge != "" && office != ""
	})
	pic := upload(t, owner, denID, "badge.png", emojiPNG(t, 128, 128))
	starts, ends := time.Now().Add(time.Hour).UnixMilli(), time.Now().Add(2*time.Hour).UnixMilli()
	req := denproto.EventRequest{Name: ptrTo("Game night"), StartsAt: &starts, EndsAt: &ends, ChannelID: &lounge, Badge: &pic.ID, BadgeTitle: ptrTo("Was there")}
	e, err := owner.CreateEvent(ctx, denID, req)
	if err != nil || e.Badge == nil || e.Badge.Title != "Was there" {
		t.Fatalf("alice made %+v, %v", e, err)
	}
	staff, err := owner.CreateEvent(ctx, denID, denproto.EventRequest{Name: ptrTo("Meeting"), StartsAt: &starts, EndsAt: &ends, ChannelID: &office})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the event on bob's view", func() bool { return eventsIn(t, member, denID) == "Game night:0" })

	asked := h.requests.Load()
	got, err := member.Event(ctx, denID, e.ID)
	if err != nil || got.Badge == nil || got.Badge.Image.ID != pic.ID || got.Mine == nil || *got.Mine || got.Ended {
		t.Fatalf("bob's card reads %+v, %v", got, err)
	}
	if n := h.requests.Load(); n != asked {
		t.Errorf("reading an event to come asked the den %d times", n-asked)
	}
	if _, err := member.Event(ctx, denID, staff.ID); !denproto.IsCode(err, denproto.CodeNotFound) {
		t.Errorf("bob's card read the staff meeting: %v", err)
	}

	now := time.Now().UnixMilli()
	if _, err := owner.UpdateEvent(ctx, denID, e.ID, denproto.EventRequest{StartsAt: &now, EndsAt: &ends}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the event live on bob's view", func() bool { return eventsIn(t, member, denID) == "Game night:0+live" })
	if err := owner.EndEvent(ctx, denID, e.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the event gone from bob's view", func() bool { return eventsIn(t, member, denID) == "" })
	got, err = member.Event(ctx, denID, e.ID)
	if err != nil || !got.Ended || got.Live || got.Badge == nil || got.Badge.Image.ID != pic.ID {
		t.Errorf("bob's card reads the ended event as %+v, %v", got, err)
	}
	var input *denclient.InputError
	if _, err := member.Event(ctx, denID, "../x"); !errors.As(err, &input) {
		t.Errorf("an ID no den writes: %v", err)
	}
}

func ptrTo[T any](v T) *T { return &v }
