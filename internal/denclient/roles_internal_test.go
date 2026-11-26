package denclient

import (
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func TestCleanRolesAndHostileRoleInputs(t *testing.T) {
	// Rejects contradictory staff_only + role_ids, role_ids on DM/Notes, and invalid IDs.
	if _, ok := cleanChannel(denproto.Channel{ID: "1", Kind: denproto.KindText, Name: "bad", StaffOnly: true, RoleIDs: []string{"10"}}); ok {
		t.Error("cleanChannel accepted staff_only with role_ids")
	}
	if _, ok := cleanChannel(denproto.Channel{ID: "2", Kind: denproto.KindDM, Members: []string{"1", "2"}, RoleIDs: []string{"10"}}); ok {
		t.Error("cleanChannel accepted role_ids on DM")
	}
	if _, ok := cleanChannel(denproto.Channel{ID: "3", Kind: denproto.KindNotes, Members: []string{"1"}, RoleIDs: []string{"10"}}); ok {
		t.Error("cleanChannel accepted role_ids on Notes")
	}
	if _, ok := cleanChannel(denproto.Channel{ID: "4", Kind: denproto.KindText, Name: "dup", RoleIDs: []string{"10", "10"}}); ok {
		t.Error("cleanChannel accepted duplicate role_ids")
	}
	if _, ok := cleanChannel(denproto.Channel{ID: "5", Kind: denproto.KindText, Name: "ok", RoleIDs: []string{"10"}}); !ok {
		t.Error("cleanChannel rejected valid role-gated channel")
	}

	// Rejects duplicate names (case-insensitively) and >MaxRoles.
	if _, ok := cleanRoles([]denproto.Role{{ID: "1", Name: "Raiders"}, {ID: "2", Name: "raiders"}}); ok {
		t.Error("cleanRoles accepted case-insensitive duplicate role names")
	}
	many := make([]denproto.Role, denproto.MaxRoles+1)
	for i := range many {
		many[i] = denproto.Role{ID: denproto.FormatID(int64(i + 1)), Name: "R" + denproto.FormatID(int64(i+1))}
	}
	if _, ok := cleanRoles(many); ok {
		t.Error("cleanRoles accepted more than MaxRoles")
	}

	// Ready rejects channels or members referencing unknown role IDs.
	denID := denproto.Random(denproto.IDSize)
	baseReady := func() denproto.Ready {
		return denproto.Ready{
			Epoch: "e1",
			Den: denproto.Den{
				ID:         denID,
				Name:       "Den",
				URL:        "https://den.example",
				Limits:     denproto.Limits{FileSize: denproto.MinFileSize, MemberStorage: denproto.MinFileSize, DenStorage: denproto.MinFileSize},
				CallLimits: denproto.DefaultCallLimits,
				Roles:      true,
			},
			Me:        denproto.Member{ID: "1", Username: "alice", DisplayName: "Alice", Role: denproto.RoleOwner, JoinedAt: 1},
			Members:   []denproto.Member{{ID: "1", Username: "alice", DisplayName: "Alice", Role: denproto.RoleOwner, JoinedAt: 1}},
			Channels:  []denproto.Channel{{ID: "10", Kind: denproto.KindText, Name: "raids", RoleIDs: []string{"5"}}},
			Roles:     []denproto.Role{{ID: "5", Name: "Raiders"}},
			Sync:      denproto.Sync{Epoch: denproto.Random(denproto.SyncEpochSize), Version: 1},
			SealCheck: denproto.Random(denproto.SealCheckSize),
		}
	}
	s := newState()
	if err := s.load(denID, baseReady(), nil, nil, nil, time.Now()); err != nil {
		t.Fatalf("valid ready failed: %v", err)
	}
	// Cache round-trip keeps HasRoles and Roles.
	kept, members := s.kept()
	restored, err := restore(denID, kept, members)
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if !restored.hasRoles || len(restored.roles) != 1 || restored.roles["5"].Name != "Raiders" {
		t.Fatalf("restored roles: hasRoles=%v roles=%+v", restored.hasRoles, restored.roles)
	}

	badChan := baseReady()
	badChan.Channels[0].RoleIDs = []string{"999"}
	if err := newState().load(denID, badChan, nil, nil, nil, time.Now()); err == nil {
		t.Error("load accepted channel referencing unknown role ID")
	}

	badMem := baseReady()
	badMem.Members[0].RoleIDs = []string{"999"}
	if err := newState().load(denID, badMem, nil, nil, nil, time.Now()); err == nil {
		t.Error("load accepted member referencing unknown role ID")
	}
}
