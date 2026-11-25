package denclient_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// waitDevice waits for m's devices on a den to hold one that matches.
func waitDevice(t *testing.T, m *denclient.Manager, denID, what string, match func(denproto.Device) bool) denproto.Device {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		list, err := m.Devices(context.Background(), denID)
		if err == nil {
			if i := slices.IndexFunc(list.Devices, match); i >= 0 {
				return list.Devices[i]
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s: %+v %v", what, list, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A device named on itself tells the den as it connects and as it's
// renamed, and the den lists it so, to its other devices too, which hear of
// it; one named before it signs in asks for approval by that name.
func TestADeviceNamesItself(t *testing.T) {
	h, _, desk, denID, _ := chatDen(t)
	ctx := context.Background()
	if v, err := desk.View(denID); err != nil || !v.DeviceNames || !v.Statuses {
		t.Fatalf("the den's view doesn't say it keeps device names and statuses: %+v %v", v, err)
	}
	desk.SetLabel("Bob's desk (Linux)")
	if desk.Label() != "Bob's desk (Linux)" {
		t.Fatalf("the desk's label is %q", desk.Label())
	}
	waitDevice(t, desk, denID, "the desk's new label", func(d denproto.Device) bool { return d.Current && d.Label == "Bob's desk (Linux)" })

	// The laptop's name reaches the desk's approval.
	laptop := h.client(t, noOwnDen)
	laptop.SetLabel("Bob's laptop (Linux)")
	asked := make(chan string, 1)
	go func() {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			for _, s := range desk.Statuses() {
				for _, r := range s.Requests {
					if s.DenID == denID {
						asked <- r.Label
						return
					}
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
		close(asked)
	}()
	signInApproved(t, laptop, desk, denID, denclient.SignInRequest{Den: "https://example.com", Username: "bob", Password: "correct horse"}, true)
	if label := <-asked; label != "Bob's laptop (Linux)" {
		t.Fatalf("the desk was asked to approve %q", label)
	}

	// Renamed, the laptop's label reaches the desk's list and its page.
	stream, stop := desk.Stream()
	defer stop()
	laptop.SetLabel("Laptop (Windows)")
	waitDevice(t, desk, denID, "the laptop's new label on the desk", func(d denproto.Device) bool { return !d.Current && d.Label == "Laptop (Windows)" })
	deadline := time.After(10 * time.Second)
	for heard := false; !heard; {
		select {
		case e := <-stream:
			for _, ev := range e.Events {
				var d denproto.DeviceUpdated
				if ev.T == denproto.EventDeviceUpdated && json.Unmarshal(ev.D, &d) == nil && d.Label == "Laptop (Windows)" {
					heard = true
				}
			}
		case <-deadline:
			t.Fatal("the desk's page never heard of the laptop's new label")
		}
	}

	// Renamed while the den is away, the desk tells it as it connects
	// again.
	tlsConfig, publicAddr, loopbackAddr := h.public.TLS, h.public.Listener.Addr().String(), h.loopback.Listener.Addr().String()
	h.stop()
	desk.SetLabel("Desk (Linux)")
	h.open(time.Hour, tlsConfig, publicAddr, loopbackAddr)
	waitFor(t, desk, "the desk connected again", connected)
	waitDevice(t, laptop, denID, "the desk's label told as it connected", func(d denproto.Device) bool { return !d.Current && d.Label == "Desk (Linux)" })
	if list, err := desk.Devices(ctx, denID); err != nil || len(list.Devices) != 2 {
		t.Fatalf("the desk's devices: %+v %v", list, err)
	}

	// Renamed while its socket is down, it tells the den as it resumes on
	// the same den, which sends no snapshot.
	bob, err := desk.View(denID)
	if err != nil {
		t.Fatal(err)
	}
	bobID, _ := denproto.ParseID(bob.Me.ID)
	h.d.CloseMemberSockets(bobID, websocket.StatusServiceRestart, "den restarting")
	waitFor(t, desk, "the desk away", func(s denclient.Status) bool { return s.State != denclient.StateConnected })
	desk.SetLabel("Desk, resumed (Linux)")
	waitFor(t, desk, "the desk back", connected)
	waitDevice(t, laptop, denID, "the desk's label told as it resumed", func(d denproto.Device) bool { return !d.Current && d.Label == "Desk, resumed (Linux)" })
}

// A status reaches every member with its end, which the record carries
// past it to whoever holds it, and the den leaves out of a snapshot after.
func TestAStatusReachesMembers(t *testing.T) {
	h, owner, member, denID, _ := chatDen(t)
	ctx := context.Background()
	if _, err := owner.SetStatus(ctx, denID, denproto.StatusRequest{Status: string(make([]byte, 2000))}); err == nil {
		t.Fatal("a status of 2,000 zero bytes was taken")
	}
	invite, _, err := owner.CreateInvite(ctx, denID, denproto.InviteCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(1500 * time.Millisecond).UnixMilli()
	me, err := owner.SetStatus(ctx, denID, denproto.StatusRequest{Status: "🍕 at lunch", Until: until})
	if err != nil || me.Status != "🍕 at lunch" || me.StatusUntil != until {
		t.Fatalf("setting a status: %+v %v", me, err)
	}
	statusOf := func(m *denclient.Manager, want string) denproto.Member {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			v, err := m.View(denID)
			if err == nil {
				if i := slices.IndexFunc(v.Members, func(x denproto.Member) bool { return x.ID == me.ID }); i >= 0 && v.Members[i].Status == want {
					return v.Members[i]
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("the status never read %q: %+v %v", want, v.Members, err)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if got := statusOf(member, "🍕 at lunch"); got.StatusUntil != until {
		t.Fatalf("the member reads the status until %d", got.StatusUntil)
	}
	if v, _ := owner.View(denID); v.Me.Status != "🍕 at lunch" {
		t.Fatalf("the owner's own record: %+v", v.Me)
	}

	time.Sleep(time.Until(time.UnixMilli(until)))
	carol := h.client(t, noOwnDen)
	join(t, carol, invite, "carol")
	if got := statusOf(carol, ""); got.StatusUntil != 0 {
		t.Fatalf("after its end a new member reads a status until %d", got.StatusUntil)
	}
}
