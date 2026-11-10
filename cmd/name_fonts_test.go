package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// The fonts a name may show in are the ones the build pins, in
// the den's order, and the notices carry each one's license, so a font
// can't reach the binary without it.
func TestNameFontsArePinnedAndNoticed(t *testing.T) {
	root := repoRoot(t)
	vendor, err := os.ReadFile(filepath.Join(root, "scripts", "vendor.sh"))
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)\nNAME_FONTS=\(\n(.*?)\n\)\n`).FindSubmatch(vendor)
	if block == nil {
		t.Fatal("scripts/vendor.sh has no NAME_FONTS")
	}
	var pinned []string
	notices := build.Notices()
	for _, line := range strings.Split(string(block[1]), "\n") {
		fields := strings.Fields(strings.Trim(strings.TrimSpace(line), `"`))
		if len(fields) < 5 {
			t.Fatalf("NAME_FONTS has the line %q", line)
		}
		name, family := fields[0], strings.Join(fields[4:], " ")
		pinned = append(pinned, name)
		license, err := os.ReadFile(filepath.Join(root, "scripts", "notices", "font-"+name+".txt"))
		if err != nil || !strings.Contains(string(license), "SIL OPEN FONT LICENSE Version 1.1") {
			t.Errorf("scripts/notices has no Open Font License for %s: %v", name, err)
		}
		if !strings.Contains(notices, "\n"+family+", one of the names' fonts in the page\n") {
			t.Errorf("the notices don't name the font %s; run ./scripts/notices.sh", family)
		}
	}
	if !slices.Equal(pinned, denproto.NameFonts) {
		t.Errorf("scripts/vendor.sh pins the fonts %q, and the den takes %q", pinned, denproto.NameFonts)
	}
}
