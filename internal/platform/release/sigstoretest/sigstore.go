// Package sigstoretest stands in for Sigstore's public-good instance, for
// tests and the Android e2e's fixture releases (M12): a certificate
// authority that issues signing certificates, as Fulcio does, and a log
// that promises each signature's entry, as Rekor does, whose trusted root a
// test build checks releases against in place of Sigstore's. It signs a
// file as cosign sign-blob does, into the same bundle.
package sigstoretest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

var oidIssuer = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 8}

// Sigstore is a certificate authority, under a root of its own, and a log.
type Sigstore struct {
	rootKey, caKey, logKey *ecdsa.PrivateKey
	root, ca               *x509.Certificate
	index                  int64
}

// New makes a Sigstore with new keys, whose root and log hold from a day
// ago.
func New() (*Sigstore, error) {
	s := &Sigstore{}
	var err error
	for _, k := range []**ecdsa.PrivateKey{&s.rootKey, &s.caKey, &s.logKey} {
		if *k, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
			return nil, err
		}
	}
	from := time.Now().Add(-24 * time.Hour)
	root := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"sigstoretest"}, CommonName: "root"},
		NotBefore:             from,
		NotAfter:              from.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	if s.root, err = sign(root, root, &s.rootKey.PublicKey, s.rootKey); err != nil {
		return nil, err
	}
	ca := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{Organization: []string{"sigstoretest"}, CommonName: "intermediate"},
		NotBefore:             from,
		NotAfter:              root.NotAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	if s.ca, err = sign(ca, s.root, &s.caKey.PublicKey, s.rootKey); err != nil {
		return nil, err
	}
	return s, nil
}

func sign(template, parent *x509.Certificate, pub *ecdsa.PublicKey, key *ecdsa.PrivateKey) (*x509.Certificate, error) {
	der, err := x509.CreateCertificate(rand.Reader, template, parent, pub, key)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(der)
}

func (s *Sigstore) logID() []byte {
	der, _ := x509.MarshalPKIXPublicKey(&s.logKey.PublicKey)
	id := sha256.Sum256(der)
	return id[:]
}

// Root is the trusted root that checks this Sigstore's signatures, in
// Sigstore's JSON form.
func (s *Sigstore) Root() []byte {
	logKey, _ := x509.MarshalPKIXPublicKey(&s.logKey.PublicKey)
	from := s.root.NotBefore.UTC().Format(time.RFC3339)
	type raw struct {
		RawBytes []byte `json:"rawBytes"`
	}
	root := map[string]any{
		"mediaType": "application/vnd.dev.sigstore.trustedroot+json;version=0.1",
		"tlogs": []any{map[string]any{
			"baseUrl":       "https://rekor.sigstoretest.invalid",
			"hashAlgorithm": "SHA2_256",
			"publicKey": map[string]any{
				"rawBytes":   logKey,
				"keyDetails": "PKIX_ECDSA_P256_SHA_256",
				"validFor":   map[string]string{"start": from},
			},
			"logId": map[string]any{"keyId": s.logID()},
		}},
		"certificateAuthorities": []any{map[string]any{
			"uri":       "https://fulcio.sigstoretest.invalid",
			"certChain": map[string]any{"certificates": []raw{{s.ca.Raw}, {s.root.Raw}}},
			"validFor":  map[string]string{"start": from},
		}},
	}
	data, _ := json.Marshal(root)
	return data
}

// Signature varies what Sign makes from cosign's, so a test can see each
// of a verifier's checks refuse it.
type Signature struct {
	// Identity and Issuer are who the certificate names.
	Identity, Issuer string
	// LogDelay is how long after the certificate is issued the log takes
	// the signature: past its ten minutes, the certificate no longer held.
	LogDelay time.Duration
	// Corrupt spoils the signature before the log takes it, which a log
	// that checked what it took would refuse.
	Corrupt bool
}

// Sign signs artifact as identity, vouched for by issuer, and returns the
// bundle cosign sign-blob would.
func (s *Sigstore) Sign(artifact []byte, identity, issuer string) ([]byte, error) {
	return s.SignAs(artifact, Signature{Identity: identity, Issuer: issuer})
}

// SignAs signs artifact as sig says.
func (s *Sigstore) SignAs(artifact []byte, sig Signature) ([]byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	san, err := url.Parse(sig.Identity)
	if err != nil {
		return nil, err
	}
	issuer, err := asn1.Marshal(sig.Issuer)
	if err != nil {
		return nil, err
	}
	issued := time.Now().Add(-time.Minute).Truncate(time.Second)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	leaf, err := sign(&x509.Certificate{
		SerialNumber:    serial,
		NotBefore:       issued,
		NotAfter:        issued.Add(10 * time.Minute),
		KeyUsage:        x509.KeyUsageDigitalSignature,
		ExtKeyUsage:     []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		URIs:            []*url.URL{san},
		ExtraExtensions: []pkix.Extension{{Id: oidIssuer, Value: issuer}},
	}, s.ca, &key.PublicKey, s.caKey)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(artifact)
	signature, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		return nil, err
	}
	if sig.Corrupt {
		signature[len(signature)-1] ^= 1
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw})
	body, err := json.Marshal(map[string]any{
		"apiVersion": "0.0.1",
		"kind":       "hashedrekord",
		"spec": map[string]any{
			"data": map[string]any{"hash": map[string]string{"algorithm": "sha256", "value": hex.EncodeToString(digest[:])}},
			"signature": map[string]any{
				"content":   signature,
				"publicKey": map[string]any{"content": certPEM},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	s.index++
	integrated := issued.Add(30 * time.Second).Add(sig.LogDelay).Unix()
	payload := `{"body":"` + base64.StdEncoding.EncodeToString(body) +
		`","integratedTime":` + strconv.FormatInt(integrated, 10) +
		`,"logID":"` + hex.EncodeToString(s.logID()) +
		`","logIndex":` + strconv.FormatInt(s.index, 10) + `}`
	promised := sha256.Sum256([]byte(payload))
	promise, err := ecdsa.SignASN1(rand.Reader, s.logKey, promised[:])
	if err != nil {
		return nil, err
	}

	return json.Marshal(map[string]any{
		"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
		"verificationMaterial": map[string]any{
			"certificate": map[string]any{"rawBytes": leaf.Raw},
			"tlogEntries": []any{map[string]any{
				"logIndex":          strconv.FormatInt(s.index, 10),
				"logId":             map[string]any{"keyId": s.logID()},
				"kindVersion":       map[string]string{"kind": "hashedrekord", "version": "0.0.1"},
				"integratedTime":    strconv.FormatInt(integrated, 10),
				"inclusionPromise":  map[string]any{"signedEntryTimestamp": promise},
				"canonicalizedBody": body,
			}},
		},
		"messageSignature": map[string]any{
			"messageDigest": map[string]any{"algorithm": "SHA2_256", "digest": digest[:]},
			"signature":     signature,
		},
	})
}

// The files Save keeps a Sigstore in.
const (
	keysFile = "sigstore.keys"
	rootFile = "trusted_root.json"
)

// Save keeps the Sigstore in dir, its keys in a file only its owner reads,
// beside its trusted root, so a later step signs with it.
func (s *Sigstore) Save(dir string) error {
	var out []byte
	for _, k := range []*ecdsa.PrivateKey{s.rootKey, s.caKey, s.logKey} {
		der, err := x509.MarshalPKCS8PrivateKey(k)
		if err != nil {
			return err
		}
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})...)
	}
	for _, c := range []*x509.Certificate{s.root, s.ca} {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})...)
	}
	if err := os.WriteFile(filepath.Join(dir, keysFile), out, 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, rootFile), s.Root(), 0o644)
}

// Load reads a Sigstore Save kept in dir.
func Load(dir string) (*Sigstore, error) {
	data, err := os.ReadFile(filepath.Join(dir, keysFile))
	if err != nil {
		return nil, err
	}
	var keys []*ecdsa.PrivateKey
	var certs []*x509.Certificate
	for {
		var block *pem.Block
		if block, data = pem.Decode(data); block == nil {
			break
		}
		switch block.Type {
		case "PRIVATE KEY":
			k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, err
			}
			ec, ok := k.(*ecdsa.PrivateKey)
			if !ok {
				return nil, errors.New("a key that isn't ECDSA")
			}
			keys = append(keys, ec)
		case "CERTIFICATE":
			c, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, err
			}
			certs = append(certs, c)
		}
	}
	if len(keys) != 3 || len(certs) != 2 {
		return nil, fmt.Errorf("%s holds %d keys and %d certificates, not 3 and 2", keysFile, len(keys), len(certs))
	}
	return &Sigstore{rootKey: keys[0], caKey: keys[1], logKey: keys[2], root: certs[0], ca: certs[1], index: time.Now().Unix()}, nil
}
