package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The mark's copies outside the binary, the site's icon and the Flatpak's,
// are the mark (M9.3).
func TestMarkCopiesAreTheMark(t *testing.T) {
	for _, path := range []string{"docs/static/favicon.svg", "scripts/flatpak/chat.dens.Dens.svg"} {
		data, err := os.ReadFile(filepath.Join("..", "..", path))
		if err != nil {
			t.Fatal(err)
		}
		// A checkout may end the file's line as its system does.
		if strings.TrimRight(string(data), "\r\n") != Mark {
			t.Errorf("%s isn't the mark", path)
		}
	}
}
