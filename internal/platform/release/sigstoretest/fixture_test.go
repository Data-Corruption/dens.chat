package sigstoretest

import (
	"os"
	"testing"
)

// TestFixture makes and uses a Sigstore of the Android e2e's own, for its
// fixture releases (M12.4). With DENS_SIGSTORE_DIR alone it makes one
// there: its keys, and the trusted root a fixture build embeds. With
// DENS_SIGSTORE_SIGN too, it signs that file as DENS_SIGSTORE_IDENTITY,
// vouched for by DENS_SIGSTORE_ISSUER, into FILE.cosign.bundle, as cosign
// sign-blob does.
func TestFixture(t *testing.T) {
	dir := os.Getenv("DENS_SIGSTORE_DIR")
	if dir == "" {
		t.Skip("the Android e2e's fixture releases run this")
	}
	file := os.Getenv("DENS_SIGSTORE_SIGN")
	if file == "" {
		s, err := New()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Save(dir); err != nil {
			t.Fatal(err)
		}
		return
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := s.Sign(data, os.Getenv("DENS_SIGSTORE_IDENTITY"), os.Getenv("DENS_SIGSTORE_ISSUER"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file+".cosign.bundle", bundle, 0o644); err != nil {
		t.Fatal(err)
	}
}
