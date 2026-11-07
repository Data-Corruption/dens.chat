package install

import (
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A private directory lets in Administrators and SYSTEM alone, from the
// moment it exists, without inheriting the parent's permissions: dens
// update checks the installer there and runs it, and the helper's task
// definition passes through one, where the member's programs can't swap
// them: nothing the member's account can change runs elevated.
func TestPrivateDirAdmitsAdministratorsAlone(t *testing.T) {
	parent := t.TempDir()
	dir, err := privateDir(parent, "dens-update-")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dir, parent+`\dens-update-`) {
		t.Fatalf("made %s, outside %s or without its prefix", dir, parent)
	}
	// An unelevated test is the directory's owner, which may always read
	// and change its DACL: it gives itself the directory back at the end,
	// so the test's own cleanup can remove it.
	t.Cleanup(func() {
		if err := setDACL(dir, rootSDDL+"(A;OICI;FA;;;"+currentUser(t)+")"); err != nil {
			t.Error(err)
		}
	})
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Error("the directory inherits its parent's permissions")
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	admitted := map[string]bool{}
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			t.Errorf("entry %d isn't an allowing one: type %d", i, ace.Header.AceType)
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		admitted[sid] = true
	}
	if len(admitted) != 2 || !admitted["S-1-5-18"] || !admitted["S-1-5-32-544"] {
		t.Errorf("the directory admits %v, want SYSTEM and Administrators alone", admitted)
	}

	// Each call makes a directory of its own.
	other, err := privateDir(parent, "dens-update-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = setDACL(other, rootSDDL+"(A;OICI;FA;;;"+currentUser(t)+")") })
	if other == dir {
		t.Errorf("two calls made the same directory, %s", dir)
	}
}

func currentUser(t *testing.T) string {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return user.User.Sid.String()
}
