package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeUnder writes a file at path below root, making its directories.
func writeUnder(t *testing.T, root, path, data string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPreflightRefusesDistributions checks that NixOS and SteamOS are
// refused before anything changes, saying why, and NixOS with the guide to
// running Dens in an Incus container (M8).
func TestPreflightRefusesDistributions(t *testing.T) {
	for _, tc := range []struct {
		name, file, osRelease, want string
	}{
		{"nixos", "etc/os-release", "BUILD_ID=\"26.05\"\nID=nixos\nNAME=NixOS\n", incusGuide},
		{"steamos", "etc/os-release", "NAME=\"SteamOS\"\nID=steamos\nID_LIKE=arch\n", "SteamOS"},
		{"nixos in /usr/lib", "usr/lib/os-release", "ID='nixos'\n", "NixOS"},
		{"debian", "etc/os-release", "ID=debian\nVERSION_ID=\"13\"\n", ""},
		{"bazzite", "etc/os-release", "ID=bazzite\nID_LIKE=\"fedora\"\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeUnder(t, root, tc.file, tc.osRelease)
			err := checkDistribution(root)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("got %v, want a refusal naming %q", err, tc.want)
			}
		})
	}
	if err := checkDistribution(t.TempDir()); err == nil {
		t.Fatal("a system without os-release passed")
	}
}

// TestPreflightRefusesLXCDropIn checks that an install in a container
// refuses to go on while LXC's drop-in for every service is in effect,
// saying how to mask it, and goes on once it's masked (M8).
func TestPreflightRefusesLXCDropIn(t *testing.T) {
	root := t.TempDir()
	if err := checkLXCDropIn(root); err != nil {
		t.Fatalf("no container: %v", err)
	}
	writeUnder(t, root, "run/systemd/system/"+lxcDropIn, "[Service]\nNoNewPrivileges=no\n")
	err := checkLXCDropIn(root)
	if err == nil || !strings.Contains(err.Error(), "ln -s /dev/null /etc/systemd/system/"+lxcDropIn) {
		t.Fatalf("the drop-in in effect: %v", err)
	}
	mask := filepath.Join(root, "etc/systemd/system", lxcDropIn)
	if err := os.MkdirAll(filepath.Dir(mask), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", mask); err != nil {
		t.Fatal(err)
	}
	if err := checkLXCDropIn(root); err != nil {
		t.Fatalf("masked: %v", err)
	}
}
