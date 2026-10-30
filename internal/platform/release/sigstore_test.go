package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/platform/release/sigstoretest"
)

// The release identity v1.4.0 was signed as, which every release's is.
var releaseSigner = Signer{
	Identity: "https://github.com/Data-Corruption/dens.chat/.github/workflows/release.yml@refs/heads/main",
	Issuer:   "https://token.actions.githubusercontent.com",
}

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// v1.4.0's checksums, as the release host serves them with their cosign
// bundle, check out against Sigstore's public root as cosign verify-blob
// passed them, and fail once anything they're checked by changes.
func TestVerifyReleaseChecksums(t *testing.T) {
	root, err := PublicRoot()
	if err != nil {
		t.Fatal(err)
	}
	checksums := readFile(t, "testdata/v1.4.0/checksums.txt")
	bundle := readFile(t, "testdata/v1.4.0/checksums.txt.cosign.bundle")
	if err := root.Verify(checksums, bundle, releaseSigner); err != nil {
		t.Fatalf("v1.4.0's checksums: %v", err)
	}

	tampered := []byte(strings.Replace(string(checksums), "3069683d", "3069683e", 1))
	if err := root.Verify(tampered, bundle, releaseSigner); err == nil {
		t.Error("a changed checksums file checked out")
	}
	for _, c := range []struct {
		why    string
		signer Signer
	}{
		{"another workflow", Signer{Identity: strings.Replace(releaseSigner.Identity, "release.yml", "other.yml", 1), Issuer: releaseSigner.Issuer}},
		{"another branch", Signer{Identity: strings.Replace(releaseSigner.Identity, "refs/heads/main", "refs/heads/dev", 1), Issuer: releaseSigner.Issuer}},
		{"another issuer", Signer{Identity: releaseSigner.Identity, Issuer: "https://accounts.google.com"}},
		{"no signer", Signer{}},
	} {
		if err := root.Verify(checksums, bundle, c.signer); err == nil {
			t.Errorf("%s: checked out", c.why)
		}
	}
}

// mutate changes a bundle's JSON as change does.
func mutate(t *testing.T, bundle []byte, change func(b map[string]any)) []byte {
	t.Helper()
	var b map[string]any
	if err := json.Unmarshal(bundle, &b); err != nil {
		t.Fatal(err)
	}
	change(b)
	out, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func material(b map[string]any) map[string]any { return b["verificationMaterial"].(map[string]any) }

func entry(b map[string]any) map[string]any {
	return material(b)["tlogEntries"].([]any)[0].(map[string]any)
}

// A signature from a Sigstore of a test's own checks out against its root
// alone, and each part a verifier checks, changed, fails it.
func TestVerifyRefuses(t *testing.T) {
	s, err := sigstoretest.New()
	if err != nil {
		t.Fatal(err)
	}
	root, err := ParseTrustedRoot(s.Root())
	if err != nil {
		t.Fatal(err)
	}
	checksums := []byte("0123  android-arm64.apk\n")
	bundle, err := s.Sign(checksums, releaseSigner.Identity, releaseSigner.Issuer)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Verify(checksums, bundle, releaseSigner); err != nil {
		t.Fatalf("a test signature: %v", err)
	}

	public, err := PublicRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := public.Verify(checksums, bundle, releaseSigner); err == nil {
		t.Error("a test signature checked out against Sigstore's root")
	}
	other, err := sigstoretest.New()
	if err != nil {
		t.Fatal(err)
	}
	otherRoot, err := ParseTrustedRoot(other.Root())
	if err != nil {
		t.Fatal(err)
	}
	if err := otherRoot.Verify(checksums, bundle, releaseSigner); err == nil {
		t.Error("a signature checked out against another Sigstore's root")
	}

	late, err := s.SignAs(checksums, sigstoretest.Signature{Identity: releaseSigner.Identity, Issuer: releaseSigner.Issuer, LogDelay: 20 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	corrupt, err := s.SignAs(checksums, sigstoretest.Signature{Identity: releaseSigner.Identity, Issuer: releaseSigner.Issuer, Corrupt: true})
	if err != nil {
		t.Fatal(err)
	}
	otherSig, err := s.Sign([]byte("other\n"), releaseSigner.Identity, releaseSigner.Issuer)
	if err != nil {
		t.Fatal(err)
	}
	var otherBundle map[string]any
	if err := json.Unmarshal(otherSig, &otherBundle); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		why    string
		bundle []byte
	}{
		{"a log that took the signature after its certificate ran out", late},
		{"a signature the log took that doesn't check out", corrupt},
		{"an older bundle format", mutate(t, bundle, func(b map[string]any) {
			b["mediaType"] = "application/vnd.dev.sigstore.bundle.v0.2+json"
		})},
		{"the log's time moved", mutate(t, bundle, func(b map[string]any) {
			at, _ := strconv.ParseInt(entry(b)["integratedTime"].(string), 10, 64)
			entry(b)["integratedTime"] = strconv.FormatInt(at+1, 10)
		})},
		{"the log's index moved", mutate(t, bundle, func(b map[string]any) {
			entry(b)["logIndex"] = "999999"
		})},
		{"no promise", mutate(t, bundle, func(b map[string]any) { delete(entry(b), "inclusionPromise") })},
		{"another log's ID", mutate(t, bundle, func(b map[string]any) {
			entry(b)["logId"] = map[string]any{"keyId": "wNI9atQGlz+VWfO6LRygH4QUfY/8W4RFwiT5i5WRgB0="}
		})},
		{"two entries", mutate(t, bundle, func(b map[string]any) {
			material(b)["tlogEntries"] = append(material(b)["tlogEntries"].([]any), entry(b))
		})},
		{"another file's certificate", mutate(t, bundle, func(b map[string]any) {
			material(b)["certificate"] = material(otherBundle)["certificate"]
		})},
		{"another file's signature", mutate(t, bundle, func(b map[string]any) {
			b["messageSignature"].(map[string]any)["signature"] = otherBundle["messageSignature"].(map[string]any)["signature"]
		})},
		{"another file's log entry", mutate(t, bundle, func(b map[string]any) {
			material(b)["tlogEntries"] = material(otherBundle)["tlogEntries"]
		})},
		{"another digest", mutate(t, bundle, func(b map[string]any) {
			b["messageSignature"].(map[string]any)["messageDigest"] = otherBundle["messageSignature"].(map[string]any)["messageDigest"]
		})},
		{"a signed envelope in place of the file's signature", mutate(t, bundle, func(b map[string]any) {
			delete(b, "messageSignature")
			b["dsseEnvelope"] = map[string]any{"payload": "e30="}
		})},
	} {
		err := root.Verify(checksums, c.bundle, releaseSigner)
		if err == nil {
			t.Errorf("%s: checked out", c.why)
		}
		t.Logf("%s: %v", c.why, err)
	}
}

// TestReleaseChecksOutForTheApp checks a new release's signed checksums as
// the Android app's updater will, for the release pipeline, once cosign has
// signed them and before anything is published (M12.4). It runs only when
// DENS_CHECK_RELEASE names the release's directory; DENS_CHECK_IDENTITY
// and DENS_CHECK_ISSUER are who signed it.
func TestReleaseChecksOutForTheApp(t *testing.T) {
	dir := os.Getenv("DENS_CHECK_RELEASE")
	if dir == "" {
		t.Skip("the release pipeline runs this, on a release it signed")
	}
	root, err := PublicRoot()
	if err != nil {
		t.Fatal(err)
	}
	signer := Signer{Identity: os.Getenv("DENS_CHECK_IDENTITY"), Issuer: os.Getenv("DENS_CHECK_ISSUER")}
	checksums := readFile(t, filepath.Join(dir, "checksums.txt"))
	if err := root.Verify(checksums, readFile(t, filepath.Join(dir, "checksums.txt.cosign.bundle")), signer); err != nil {
		t.Fatalf("the app can't check this release's signature: %v", err)
	}
	t.Logf("checksums.txt checks out as signed by %s", signer.Identity)
}
