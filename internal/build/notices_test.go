package build

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The notices name every module the binary links, on every platform it's
// released for, so a new dependency can't arrive without its license.
// scripts/notices.sh --check, in CI, also compares the licenses' text.
func TestNoticesNameEveryModule(t *testing.T) {
	notices := Notices()
	for _, want := range []string{"Go: the standard library and runtime", "FFmpeg ", "zlib ", "wasi-libc", "Preact "} {
		if !strings.Contains(notices, "\n"+want) {
			t.Errorf("the notices have no %q", want)
		}
	}
	for _, target := range []string{"linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"} {
		goos, goarch, _ := strings.Cut(target, "/")
		cmd := exec.Command("go", "list", "-deps",
			"-f", "{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}",
			"github.com/Data-Corruption/dens.chat/cmd")
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list for %s: %v", target, err)
		}
		for _, module := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if module != "" && !strings.Contains(notices, "\n"+module+"\n") {
				t.Errorf("%s links %s, which isn't in the notices; run ./scripts/notices.sh", target, module)
			}
		}
	}
}
