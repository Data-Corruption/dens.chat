package host

import "testing"

func TestFreeSpace(t *testing.T) {
	free, err := FreeSpace(t.TempDir())
	if err != nil || free == 0 {
		t.Fatalf("FreeSpace = %d, %v", free, err)
	}
	if _, err := FreeSpace(t.TempDir() + "/missing"); err == nil {
		t.Error("a missing directory has free space")
	}
}
