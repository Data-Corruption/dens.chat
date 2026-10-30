package release

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// publicRoot is Sigstore's public-good trusted root, which cosign checks
// releases against: the target trusted_root.json of Sigstore's TUF
// repository (tuf-repo-cdn.sigstore.dev), as the pinned cosign fetched and
// verified it on 2026-10-28, SHA-256
// 6494e21ea73fa7ee769f85f57d5a3e6a08725eae1e38c755fc3517c9e6bc0b66. A
// release's own pipeline checks its signature as this file does, before
// the release is offered (release.md), so a Sigstore change that this root
// or Verify doesn't follow stops a release rather than its updates.
//
//go:embed trusted_root.json
var publicRoot []byte

// The trusted root and bundle formats Verify reads: cosign 3's.
const (
	trustedRootType = "application/vnd.dev.sigstore.trustedroot+json;version=0.1"
	bundleType      = "application/vnd.dev.sigstore.bundle.v0.3+json"
)

// Fulcio's extensions naming the OIDC issuer that vouched for the signer,
// as a DER string, and as raw bytes in the older form.
var (
	oidIssuer       = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 8}
	oidIssuerLegacy = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}
)

// Signer is who must have signed a release: the workflow its signing
// certificate names, and the OIDC issuer that vouched for it, as the
// installers pass them to cosign.
type Signer struct {
	Identity string
	Issuer   string
}

// TrustedRoot is what a release's signature is checked against where no
// cosign runs, as in the Android app (M12): the certificate authorities
// that issue signing certificates, Fulcio's, and the transparency logs
// whose record of a signature says when it was made, Rekor's.
type TrustedRoot struct {
	logs []transparencyLog
	cas  []certificateAuthority
}

type transparencyLog struct {
	id       []byte
	key      *ecdsa.PublicKey
	from, to time.Time
}

type certificateAuthority struct {
	root          *x509.CertPool
	intermediates *x509.CertPool
	from, to      time.Time
}

// PublicRoot is Sigstore's public-good trusted root, which signs Dens's
// releases.
func PublicRoot() (*TrustedRoot, error) { return ParseTrustedRoot(publicRoot) }

type validity struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// times parses a validity period; a missing end leaves it open.
func (v validity) times() (from, to time.Time, err error) {
	if from, err = time.Parse(time.RFC3339Nano, v.Start); err != nil {
		return from, to, fmt.Errorf("its start: %w", err)
	}
	if v.End != "" {
		if to, err = time.Parse(time.RFC3339Nano, v.End); err != nil {
			return from, to, fmt.Errorf("its end: %w", err)
		}
	}
	return from, to, nil
}

func within(t, from, to time.Time) bool {
	return !t.Before(from) && (to.IsZero() || !t.After(to))
}

// ParseTrustedRoot reads a trusted root in Sigstore's JSON form. It keeps
// the logs whose keys are ECDSA P-256, Rekor's first log's kind, which
// signs a promise of each entry; a log of any other kind can't vouch for a
// signature here.
func ParseTrustedRoot(data []byte) (*TrustedRoot, error) {
	var raw struct {
		MediaType string `json:"mediaType"`
		Tlogs     []struct {
			HashAlgorithm string `json:"hashAlgorithm"`
			PublicKey     struct {
				RawBytes   []byte   `json:"rawBytes"`
				KeyDetails string   `json:"keyDetails"`
				ValidFor   validity `json:"validFor"`
			} `json:"publicKey"`
			LogID struct {
				KeyID []byte `json:"keyId"`
			} `json:"logId"`
		} `json:"tlogs"`
		CertificateAuthorities []struct {
			CertChain struct {
				Certificates []struct {
					RawBytes []byte `json:"rawBytes"`
				} `json:"certificates"`
			} `json:"certChain"`
			ValidFor validity `json:"validFor"`
		} `json:"certificateAuthorities"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("read the trusted root: %w", err)
	}
	if raw.MediaType != trustedRootType {
		return nil, fmt.Errorf("the trusted root is %q, not %q", raw.MediaType, trustedRootType)
	}
	root := &TrustedRoot{}
	for i, l := range raw.Tlogs {
		if l.PublicKey.KeyDetails != "PKIX_ECDSA_P256_SHA_256" || l.HashAlgorithm != "SHA2_256" {
			continue
		}
		key, err := x509.ParsePKIXPublicKey(l.PublicKey.RawBytes)
		if err != nil {
			return nil, fmt.Errorf("the trusted root's log %d: %w", i, err)
		}
		ec, ok := key.(*ecdsa.PublicKey)
		if !ok || len(l.LogID.KeyID) == 0 {
			return nil, fmt.Errorf("the trusted root's log %d has no ECDSA key and ID", i)
		}
		from, to, err := l.PublicKey.ValidFor.times()
		if err != nil {
			return nil, fmt.Errorf("the trusted root's log %d: %w", i, err)
		}
		root.logs = append(root.logs, transparencyLog{id: l.LogID.KeyID, key: ec, from: from, to: to})
	}
	for i, ca := range raw.CertificateAuthorities {
		chain := ca.CertChain.Certificates
		if len(chain) == 0 {
			return nil, fmt.Errorf("the trusted root's certificate authority %d has no certificates", i)
		}
		// The chain runs from the authority that issues signing
		// certificates to its root.
		c := certificateAuthority{root: x509.NewCertPool(), intermediates: x509.NewCertPool()}
		for j, raw := range chain {
			cert, err := x509.ParseCertificate(raw.RawBytes)
			if err != nil {
				return nil, fmt.Errorf("the trusted root's certificate authority %d, certificate %d: %w", i, j, err)
			}
			if j == len(chain)-1 {
				c.root.AddCert(cert)
			} else {
				c.intermediates.AddCert(cert)
			}
		}
		var err error
		if c.from, c.to, err = ca.ValidFor.times(); err != nil {
			return nil, fmt.Errorf("the trusted root's certificate authority %d: %w", i, err)
		}
		root.cas = append(root.cas, c)
	}
	if len(root.logs) == 0 || len(root.cas) == 0 {
		return nil, errors.New("the trusted root has no log or certificate authority to check against")
	}
	return root, nil
}

// The parts of cosign's bundle Verify reads. Protobuf's JSON puts 64-bit
// integers in strings, and bytes in standard base64.
type bundleJSON struct {
	MediaType            string `json:"mediaType"`
	VerificationMaterial struct {
		Certificate *struct {
			RawBytes []byte `json:"rawBytes"`
		} `json:"certificate"`
		TlogEntries []struct {
			LogIndex string `json:"logIndex"`
			LogID    struct {
				KeyID []byte `json:"keyId"`
			} `json:"logId"`
			KindVersion struct {
				Kind    string `json:"kind"`
				Version string `json:"version"`
			} `json:"kindVersion"`
			IntegratedTime   string `json:"integratedTime"`
			InclusionPromise *struct {
				SignedEntryTimestamp []byte `json:"signedEntryTimestamp"`
			} `json:"inclusionPromise"`
			CanonicalizedBody []byte `json:"canonicalizedBody"`
		} `json:"tlogEntries"`
	} `json:"verificationMaterial"`
	MessageSignature *struct {
		MessageDigest struct {
			Algorithm string `json:"algorithm"`
			Digest    []byte `json:"digest"`
		} `json:"messageDigest"`
		Signature []byte `json:"signature"`
	} `json:"messageSignature"`
}

// hashedRekord is the log's entry for a signed digest.
type hashedRekord struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Spec       struct {
		Data struct {
			Hash struct {
				Algorithm string `json:"algorithm"`
				Value     string `json:"value"`
			} `json:"hash"`
		} `json:"data"`
		Signature struct {
			Content   []byte `json:"content"`
			PublicKey struct {
				Content []byte `json:"content"`
			} `json:"publicKey"`
		} `json:"signature"`
	} `json:"spec"`
}

// Verify checks that bundle, the cosign bundle cosign sign-blob writes for
// artifact, signs it as signer, as cosign verify-blob does: a signing
// certificate one of the root's authorities issued, naming signer's
// identity and issuer; the artifact's digest signed with that certificate's
// key; and an entry for that signature in one of the root's logs, whose
// signed promise gives the time the signature was made, within the
// certificate's ten minutes. It doesn't check the certificate's
// transparency log or a timestamp authority's stamp, which the log's
// promise stands in for.
func (r *TrustedRoot) Verify(artifact, bundle []byte, signer Signer) error {
	if signer.Identity == "" || signer.Issuer == "" {
		return errors.New("no signer to check the signature against")
	}
	var b bundleJSON
	if err := json.Unmarshal(bundle, &b); err != nil {
		return fmt.Errorf("read the signature's bundle: %w", err)
	}
	if b.MediaType != bundleType {
		return fmt.Errorf("the signature's bundle is %q, not %q", b.MediaType, bundleType)
	}
	sig := b.MessageSignature
	material := b.VerificationMaterial
	if sig == nil || material.Certificate == nil {
		return errors.New("the bundle holds no signature of a file and its certificate")
	}
	digest := sha256.Sum256(artifact)
	if sig.MessageDigest.Algorithm != "SHA2_256" || !bytes.Equal(sig.MessageDigest.Digest, digest[:]) {
		return errors.New("the bundle signs another file")
	}
	leaf, err := x509.ParseCertificate(material.Certificate.RawBytes)
	if err != nil {
		return fmt.Errorf("read the signing certificate: %w", err)
	}
	if len(material.TlogEntries) != 1 {
		return fmt.Errorf("the bundle has %d log entries, not one", len(material.TlogEntries))
	}
	entry := material.TlogEntries[0]
	if entry.KindVersion.Kind != "hashedrekord" || entry.KindVersion.Version != "0.0.1" {
		return fmt.Errorf("the log's entry is a %s %s, not a hashedrekord 0.0.1", entry.KindVersion.Kind, entry.KindVersion.Version)
	}
	integrated, err := strconv.ParseInt(entry.IntegratedTime, 10, 64)
	if err != nil {
		return fmt.Errorf("the log entry's time: %w", err)
	}
	index, err := strconv.ParseInt(entry.LogIndex, 10, 64)
	if err != nil || index < 0 {
		return fmt.Errorf("the log entry's index %q", entry.LogIndex)
	}
	at := time.Unix(integrated, 0)

	// The log's promise: its signature over the entry and when it took it,
	// as RFC 8785's canonical JSON of the four, whose values need no
	// escaping, being base64, digits and hex.
	var log *transparencyLog
	for i := range r.logs {
		if bytes.Equal(r.logs[i].id, entry.LogID.KeyID) {
			log = &r.logs[i]
			break
		}
	}
	if log == nil {
		return errors.New("no log the root trusts recorded the signature")
	}
	if !within(at, log.from, log.to) {
		return fmt.Errorf("the log recorded the signature at %s, outside its key's time", at.UTC().Format(time.RFC3339))
	}
	if entry.InclusionPromise == nil {
		return errors.New("the log's entry has no promise to check")
	}
	payload := `{"body":"` + base64.StdEncoding.EncodeToString(entry.CanonicalizedBody) +
		`","integratedTime":` + strconv.FormatInt(integrated, 10) +
		`,"logID":"` + hex.EncodeToString(entry.LogID.KeyID) +
		`","logIndex":` + strconv.FormatInt(index, 10) + `}`
	promised := sha256.Sum256([]byte(payload))
	if !ecdsa.VerifyASN1(log.key, promised[:], entry.InclusionPromise.SignedEntryTimestamp) {
		return errors.New("the log's promise of its entry doesn't check out")
	}

	// The entry the log promised is of this signature, of this digest, by
	// this certificate.
	var body hashedRekord
	if err := json.Unmarshal(entry.CanonicalizedBody, &body); err != nil {
		return fmt.Errorf("read the log's entry: %w", err)
	}
	certPEM, _ := pem.Decode(body.Spec.Signature.PublicKey.Content)
	switch {
	case body.APIVersion != "0.0.1" || body.Kind != "hashedrekord":
		return errors.New("the log's entry isn't a hashedrekord 0.0.1")
	case body.Spec.Data.Hash.Algorithm != "sha256" || body.Spec.Data.Hash.Value != hex.EncodeToString(digest[:]):
		return errors.New("the log's entry is of another file")
	case !bytes.Equal(body.Spec.Signature.Content, sig.Signature):
		return errors.New("the log's entry is of another signature")
	case certPEM == nil || certPEM.Type != "CERTIFICATE" || !bytes.Equal(certPEM.Bytes, leaf.Raw):
		return errors.New("the log's entry names another certificate")
	}

	// The certificate, issued by one of the root's authorities, held when
	// the log took the signature, and names the signer.
	issued := false
	for _, ca := range r.cas {
		if !within(at, ca.from, ca.to) {
			continue
		}
		if _, err := leaf.Verify(x509.VerifyOptions{
			Roots:         ca.root,
			Intermediates: ca.intermediates,
			CurrentTime:   at,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		}); err == nil {
			issued = true
			break
		}
	}
	if !issued {
		return errors.New("no certificate authority the root trusts issued the signing certificate for when the log took it")
	}
	named := false
	for _, u := range leaf.URIs {
		if u.String() == signer.Identity {
			named = true
		}
	}
	if !named {
		return fmt.Errorf("the signing certificate doesn't name %s", signer.Identity)
	}
	if issuer, err := certIssuer(leaf); err != nil {
		return err
	} else if issuer != signer.Issuer {
		return fmt.Errorf("the signing certificate's issuer is %s, not %s", issuer, signer.Issuer)
	}

	key, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || !ecdsa.VerifyASN1(key, digest[:], sig.Signature) {
		return errors.New("the signature doesn't check out")
	}
	return nil
}

// certIssuer is the OIDC issuer a Fulcio certificate names.
func certIssuer(c *x509.Certificate) (string, error) {
	for _, e := range c.Extensions {
		if e.Id.Equal(oidIssuer) {
			var issuer string
			if rest, err := asn1.Unmarshal(e.Value, &issuer); err != nil || len(rest) != 0 {
				return "", errors.New("the signing certificate's issuer can't be read")
			}
			return issuer, nil
		}
	}
	for _, e := range c.Extensions {
		if e.Id.Equal(oidIssuerLegacy) {
			return string(e.Value), nil
		}
	}
	return "", errors.New("the signing certificate names no issuer")
}
