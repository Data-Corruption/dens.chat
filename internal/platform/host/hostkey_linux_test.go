//go:build !android

package host

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHostKeyFromCredentialsDirectory(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, HostKeySize)
	for i := range key {
		key[i] = byte(i)
	}
	if err := os.WriteFile(filepath.Join(dir, HostKeyCredential), key, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", dir)
	got, err := LoadHostKey("")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(key) {
		t.Fatal("loaded key differs")
	}

	if err := os.WriteFile(filepath.Join(dir, HostKeyCredential), key[:10], 0o400); err == nil {
		if _, err := LoadHostKey(""); err == nil {
			t.Fatal("short key accepted")
		}
	}
	t.Setenv("CREDENTIALS_DIRECTORY", "")
	if _, err := LoadHostKey(""); err == nil {
		t.Fatal("missing CREDENTIALS_DIRECTORY accepted")
	}
}
