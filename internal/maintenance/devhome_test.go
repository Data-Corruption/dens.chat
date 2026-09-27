package maintenance

import "testing"

// setDevHome points development layouts at a temporary directory on every
// platform, so tests never touch the developer's real instances.
func setDevHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("LOCALAPPDATA", dir)
}
