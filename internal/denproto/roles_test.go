package denproto

import (
	"strconv"
	"strings"
	"testing"
)

func TestCleanRoleName(t *testing.T) {
	got, err := CleanRoleName("  Raid   Team\u200b ")
	if err != nil || got != "Raid Team" {
		t.Fatalf("CleanRoleName = %q, %v; want %q", got, err, "Raid Team")
	}
	for _, bad := range []string{"", "   ", "\u200b", strings.Repeat("a", MaxNameRunes+1), string([]byte{0xff})} {
		if _, err := CleanRoleName(bad); err == nil {
			t.Errorf("CleanRoleName(%q) passed", bad)
		}
	}
}

func TestCheckRole(t *testing.T) {
	if err := CheckRole(Role{ID: "1", Name: "Raiders"}); err != nil {
		t.Fatalf("valid role failed: %v", err)
	}
	for _, bad := range []Role{
		{ID: "", Name: "Raiders"},
		{ID: "01", Name: "Raiders"},
		{ID: "1", Name: ""},
		{ID: "1", Name: " Raiders"},
		{ID: "1", Name: "Raid\u202eers"},
		{ID: "1", Name: strings.Repeat("x", MaxNameRunes+1)},
	} {
		if CheckRole(bad) == nil {
			t.Errorf("CheckRole(%+v) passed", bad)
		}
	}
}

func TestCheckRoleIDs(t *testing.T) {
	if err := CheckRoleIDs([]string{"1", "2", "10"}); err != nil {
		t.Fatalf("valid role IDs failed: %v", err)
	}
	if err := CheckRoleIDs(nil); err != nil {
		t.Fatalf("nil role IDs failed: %v", err)
	}
	for _, bad := range [][]string{
		{""},
		{"01"},
		{"abc"},
		{"1", "2", "1"},
	} {
		if CheckRoleIDs(bad) == nil {
			t.Errorf("CheckRoleIDs(%v) passed", bad)
		}
	}
	tooMany := make([]string, MaxRoles+1)
	for i := range tooMany {
		tooMany[i] = strconv.Itoa(i + 1)
	}
	if CheckRoleIDs(tooMany) == nil {
		t.Error("CheckRoleIDs accepted more than MaxRoles")
	}
	if !KnownCode(CodeRoleNameTaken) {
		t.Error("CodeRoleNameTaken is not in KnownCode")
	}
}
