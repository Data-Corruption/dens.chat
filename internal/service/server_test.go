package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// A server sets Dens up from the command line, with no page (M10): the
// local password, set once and then changed only with the current one;
// the den, created only while hosting serves, with no account on the
// server; and its owner invite, which makes another computer's Dens the
// den's owner.
func TestADenFromTheCommandLine(t *testing.T) {
	ctx := context.Background()
	address := startForward(t)
	server := newComputer(t, hosting(t))
	address.to(server.opts.DevDenPort)

	server.refused("hosting with no password", control.Request{Op: control.OpHosting, On: true, Password: localPassword},
		"set a local password with dens password")
	server.refused("a short password", control.Request{Op: control.OpPassword, NewPassword: "short"}, "password must be at least")
	var changed control.PasswordChanged
	if err := server.call(control.Request{Op: control.OpPassword, NewPassword: "the first password"}, &changed); err != nil || !changed.First {
		t.Fatalf("the first password: %+v %v", changed, err)
	}
	if !server.status().PasswordSet {
		t.Fatal("the status has no password")
	}
	server.refused("changing it without the current one", control.Request{Op: control.OpPassword, NewPassword: localPassword}, "wrong password")
	server.refused("changing it with a wrong one", control.Request{Op: control.OpPassword, Password: "wrong wrong wrong", NewPassword: localPassword},
		"wrong password")
	if err := server.call(control.Request{Op: control.OpPassword, Password: "the first password", NewPassword: localPassword}, &changed); err != nil ||
		changed.First {
		t.Fatalf("changing the password: %+v %v", changed, err)
	}
	server.refused("the password it changed", control.Request{Op: control.OpHosting, On: true, Password: "the first password"}, "wrong password")

	// A development instance with a den port hosts from its start.
	if err := server.call(control.Request{Op: control.OpHosting, On: false}, nil); err != nil {
		t.Fatal(err)
	}
	create := control.Request{Op: control.OpCreateDen, Password: localPassword, Name: "Server Den", URL: address.url}
	server.refused("creating the den with hosting off", create, "hosting is off; turn it on with dens den hosting on")
	if err := server.call(control.Request{Op: control.OpHosting, On: true, Password: localPassword}, nil); err != nil {
		t.Fatal(err)
	}
	bad := create
	bad.Name = " "
	server.refused("a den with no name", bad, "the den's name")
	bad = create
	bad.URL = "den.example.com"
	server.refused("a den with no scheme in its address", bad, "the den's address")
	bad = create
	bad.Password = "wrong wrong wrong"
	server.refused("creating the den with a wrong password", bad, "wrong password")
	var invite control.OwnerInvite
	if err := server.call(create, &invite); err != nil {
		t.Fatal(err)
	}
	if invite.Den != "Server Den" || invite.URL != address.url || !strings.HasPrefix(invite.Invite, "dens1:") {
		t.Fatalf("the owner invite: %+v", invite)
	}
	if s := server.status(); s.Den != "Server Den" {
		t.Fatalf("the status after creating the den: %+v", s)
	}
	if list := server.a.Dens.Statuses(); len(list) != 0 {
		t.Fatalf("the server joined its den: %+v", list)
	}
	server.refused("a second den", create, "this instance hosts Server Den already")

	// The owner's own computer redeems the invite, from Add a den.
	pc := newComputer(t, nil)
	var err error
	for range 100 { // the den connections start just after the service does
		_, err = pc.a.Dens.Join(ctx, invite.Invite, denclient.JoinRequest{Username: "alice", DisplayName: "Alice", Password: "den password"})
		if !errors.Is(err, denclient.ErrNotStarted) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if st := waitDen(t, pc.a.Dens, "the owner connected", connected); st.Role != denproto.RoleOwner {
		t.Fatalf("the invite made %+v", st)
	}
	if owned, err := server.a.Den.Owned(ctx); err != nil || !owned {
		t.Fatalf("the den's owner: %v %v", owned, err)
	}
}
