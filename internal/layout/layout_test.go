package layout

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestValidateInstance(t *testing.T) {
	for _, name := range []string{"main", "a", "den-2", "abcdefghijklmnop"} {
		if err := ValidateInstance(name); err != nil {
			t.Errorf("ValidateInstance(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"", "Main", "2den", "-den", "den-", "de--n", "den_2", "abcdefghijklmnopq", "den/x", "den.x"} {
		if err := ValidateInstance(name); err == nil {
			t.Errorf("ValidateInstance(%q) accepted", name)
		}
	}
}

func TestInstalledLayoutNamesEverything(t *testing.T) {
	l, err := New("dens", "main", Installed)
	if err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"Root": l.Root, "Control": l.Control, "State": l.State, "HostKey": l.HostKey,
		"Data": l.Data, "DB": l.DB, "Logs": l.Logs, "ControlEndpoint": l.ControlEndpoint,
		"Binary": l.Binary, "ServiceName": l.ServiceName, "Account": l.Account, "Cosign": l.Cosign,
	} {
		if path == "" {
			t.Errorf("%s is empty", name)
		}
	}
	if !strings.HasPrefix(l.State, l.Control) || !strings.HasPrefix(l.DB, l.Data) {
		t.Fatalf("state or database outside their directories: %+v", l)
	}
	if filepath.Dir(l.Control) != l.Root || filepath.Dir(l.Data) != l.Root {
		t.Fatalf("control and data must sit directly in the root: %+v", l)
	}
	if !strings.Contains(l.ServiceName, "main") || !strings.Contains(l.Account, "main") {
		t.Fatalf("service and account must name the instance: %q %q", l.ServiceName, l.Account)
	}
}

func TestDevelopmentLayoutIsSeparate(t *testing.T) {
	installed, err := New("dens", "main", Installed)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := New("dens", "main", Development)
	if err != nil {
		t.Fatal(err)
	}
	if dev.Root == installed.Root || dev.ControlEndpoint == installed.ControlEndpoint {
		t.Fatal("development instance shares paths with the installed one")
	}
	if dev.Binary != "" || dev.ServiceName != "" || dev.Account != "" {
		t.Fatal("development layout has system integration")
	}
}

func TestDevelopmentDirectoriesPassTheirChecks(t *testing.T) {
	setDevHome(t)
	l, err := New("dens", "main", Development)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(l.Root, filepath.Dir(filepath.Dir(l.Root))) {
		t.Fatal("unexpected root")
	}
	if err := l.EnsureSession(); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if err := l.CheckControl(); err != nil {
		t.Fatalf("CheckControl: %v", err)
	}
	if err := l.CheckData(); err != nil {
		t.Fatalf("CheckData: %v", err)
	}
	if err := l.EnsureData(); err != nil {
		t.Fatalf("EnsureData: %v", err)
	}
}

func TestInstancesListsTheRootsByName(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("LOCALAPPDATA", data)
	if names, err := Instances("dens", Development); err != nil || len(names) != 0 {
		t.Fatalf("Instances before any = %v, %v", names, err)
	}
	l, err := New("dens", "main", Development)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Dir(l.Root)
	for _, dir := range []string{"main", "second", "Not-An-Instance"} {
		if err := os.MkdirAll(filepath.Join(base, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "third"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	names, err := Instances("dens", Development)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names, []string{"main", "second"}) {
		t.Fatalf("Instances = %v, want main and second", names)
	}
}

// Only a session form runs as its user, and only a known form has a
// layout, so a form a newer binary knows never runs as another.
func TestFormsAreKnownAndSay(t *testing.T) {
	for _, c := range []struct {
		form    Form
		name    string
		session bool
	}{
		{Installed, "installed", false},
		{Development, "development", true},
		{Flatpak, "Flatpak", true},
	} {
		if c.form.String() != c.name || c.form.Session() != c.session {
			t.Errorf("%d: %q, session %v; want %q, session %v", int(c.form), c.form, c.form.Session(), c.name, c.session)
		}
	}
	setDevHome(t)
	if _, err := New("dens", "main", Form(-1)); err == nil {
		t.Fatal("an unknown form has a layout")
	}
	installed, err := New("dens", "main", Installed)
	if err != nil {
		t.Fatal(err)
	}
	if err := installed.EnsureSession(); err == nil {
		t.Fatal("EnsureSession made an installed layout's directories")
	}
}
