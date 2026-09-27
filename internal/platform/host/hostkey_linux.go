package host

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Data-Corruption/dens.chat/pkg/xsyscall"
)

// HostKeyCredential is the credential name of the data key, both inside the
// encrypted credential and in $CREDENTIALS_DIRECTORY.
const HostKeyCredential = "datakey"

// LoadHostKey returns the data key systemd decrypted for this unit from its
// LoadCredentialEncrypted= setting. The encrypted file itself is root-only;
// the service only ever sees the plaintext systemd hands it.
func LoadHostKey(string) ([]byte, error) {
	dir := os.Getenv("CREDENTIALS_DIRECTORY")
	if dir == "" {
		return nil, errors.New("CREDENTIALS_DIRECTORY is not set; the unit must load the data key with LoadCredentialEncrypted=")
	}
	file, err := xsyscall.OpenNoFollow(filepath.Join(dir, HostKeyCredential), os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open data key credential: %w", err)
	}
	defer file.Close()
	key, err := io.ReadAll(io.LimitReader(file, HostKeySize+1))
	if err != nil {
		return nil, fmt.Errorf("read data key credential: %w", err)
	}
	if err := checkKeySize(key); err != nil {
		return nil, err
	}
	return key, nil
}

// WrapHostKey encrypts key with systemd-creds for this host (bound to its TPM
// when there is one) and atomically writes the credential to path. It must
// run as root, which systemd-creds needs to read the host key.
func WrapHostKey(path string, key []byte) error {
	if err := checkKeySize(key); err != nil {
		return err
	}
	temp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	_ = os.Remove(temp)
	cmd := exec.Command("systemd-creds", "encrypt", "--name="+HostKeyCredential, "-", temp)
	cmd.Stdin = bytes.NewReader(key)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("systemd-creds encrypt: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err := os.Chmod(temp, 0o600); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("protect encrypted data key: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("publish encrypted data key: %w", err)
	}
	return nil
}
