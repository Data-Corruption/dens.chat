package host

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/Data-Corruption/dens.chat/pkg/xsyscall"
)

// LoadOrCreateDevKey keeps the data key of a development instance as a
// plaintext file owned by the developer. Development instances have no
// service manager to hold a host-bound key; they never hold real data.
func LoadOrCreateDevKey(path string) ([]byte, error) {
	file, err := xsyscall.OpenNoFollow(path, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		key := make([]byte, HostKeySize)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate development data key: %w", err)
		}
		if err := os.WriteFile(path, key, 0o600); err != nil {
			return nil, fmt.Errorf("write development data key: %w", err)
		}
		return key, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open development data key: %w", err)
	}
	defer file.Close()
	key, err := io.ReadAll(io.LimitReader(file, HostKeySize+1))
	if err != nil {
		return nil, fmt.Errorf("read development data key: %w", err)
	}
	if err := checkKeySize(key); err != nil {
		return nil, err
	}
	return key, nil
}
