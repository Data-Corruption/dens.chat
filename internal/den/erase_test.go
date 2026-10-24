package den

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
)

// Erasing the den, once another computer serves it (M10), empties every
// den table and the uploads, and forgets the den in memory: its identity,
// the sign-ins outstanding and the events a client would resume into. A
// den made here afterwards starts its IDs again.
func TestEraseLeavesNoDen(t *testing.T) {
	f, owner, member := chatFixture(t)
	ctx := context.Background()
	if owned, err := f.d.Owned(ctx); err != nil || !owned {
		t.Fatalf("a den its owner joined is owned: %v, %v", owned, err)
	}
	c := f.newChannel(owner, "general", denproto.ChannelRequest{})
	f.send(member, c.ID, "hello")
	file := f.mustUpload(member, "notes.txt", []byte("notes to keep"))
	f.mustSendFiles(member, c.ID, file.ID)
	f.mustUpload(owner, "pending.txt", []byte("an upload no message uses yet"))
	dm, err := f.d.OpenDM(ctx, owner, denproto.DMRequest{MemberID: denproto.FormatID(member.MemberID)})
	if err != nil {
		t.Fatal(err)
	}
	f.exchange(owner, member, mustID(dm.ID), false)
	if _, err := f.askSignIn("bob", "password", newDevice()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.Challenge(denproto.Random(denproto.NonceSize)); err != nil {
		t.Fatal(err)
	}
	epoch, seq := f.d.Hub.Epoch(), f.d.Hub.seq
	if stored, _ := f.stored(); len(stored) == 0 {
		t.Fatal("the den stored no files")
	}

	if err := f.d.Erase(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.d.Info(); ok {
		t.Fatal("the erased den is still there")
	}
	if owned, err := f.d.Owned(ctx); err != nil || owned {
		t.Fatalf("the erased den is owned: %v, %v", owned, err)
	}
	if err := f.d.Erase(ctx); err == nil {
		t.Fatal("a den erased twice")
	}
	tables, err := database.Tables(ctx, f.db)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if !database.IsDenTable(table) {
			continue
		}
		var rows int
		if err := f.db.QueryRow(`SELECT count(*) FROM "` + table + `"`).Scan(&rows); err != nil || rows != 0 {
			t.Errorf("%s keeps %d rows, %v", table, rows, err)
		}
	}
	var sequences int
	if err := f.db.QueryRow(`SELECT count(*) FROM sqlite_sequence WHERE name = 'den' OR name LIKE 'den\_%' ESCAPE '\'`).Scan(&sequences); err != nil || sequences != 0 {
		t.Errorf("sqlite_sequence keeps %d of the den's counts, %v", sequences, err)
	}
	if stored, _ := f.stored(); len(stored) != 0 {
		t.Errorf("the uploads keep %v", stored)
	}
	if len(f.d.nonces) != 0 || len(f.d.requests.byID) != 0 {
		t.Errorf("the den keeps %d nonces and %d sign-ins", len(f.d.nonces), len(f.d.requests.byID))
	}
	if _, missed, resumed, _ := f.d.Hub.Subscribe(member.MemberID, false, epoch, seq-1); resumed || len(missed) != 0 {
		t.Errorf("a client resumed into the erased den: %v, %d events", resumed, len(missed))
	}

	// Another den starts afresh, its first member the first again.
	joined, err := f.join(f.create(), "carol", newDevice())
	if err != nil {
		t.Fatal(err)
	}
	if joined.Member.ID != "1" || joined.Member.Role != denproto.RoleOwner {
		t.Fatalf("the next den's first member is %+v", joined.Member)
	}
}

// Files an erased den left behind, had the service stopped before they
// went, go as the install opens with no den; other files stay.
func TestOpenWithNoDenTidiesUploads(t *testing.T) {
	f := newFixture(t)
	left := filepath.Join(f.storage.Dir, "00112233445566778899aabbccddeeff")
	other := filepath.Join(f.storage.Dir, "notes")
	for _, path := range []string{left, left + thumbSuffix, other} {
		if err := os.WriteFile(path, []byte("sealed"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.open()
	for _, path := range []string{left, left + thumbSuffix} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s stayed: %v", filepath.Base(path), err)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("a file not named as an upload went: %v", err)
	}
}
