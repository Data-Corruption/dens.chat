package service_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// A download whose archive fails partway ends without the response's last
// chunk, so the browser marks it failed rather than keeping what came as a
// whole backup (M10). An upload the service can't read fails the archive;
// root reads any file, so the test needs another account.
func TestPageDownloadOfAFailedBackupFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode")
	}
	a, client := startDevService(t)
	b := pair(t, a, client)
	if err := os.MkdirAll(a.Layout.Uploads, 0o700); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(a.Layout.Uploads, "0123456789abcdef")
	if err := os.WriteFile(unreadable, []byte("sealed bytes"), 0); err != nil {
		t.Fatal(err)
	}
	var link struct {
		URL string `json:"url"`
	}
	b.expect(http.StatusOK, "a backup", "POST", "/api/backup", map[string]string{"password": localPassword}, &link)
	resp, err := b.http.Get(b.base + link.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Fatal("a backup that failed partway downloaded as if it were whole")
	}
}
