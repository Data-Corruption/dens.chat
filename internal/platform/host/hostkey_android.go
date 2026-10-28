package host

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// LoadHostKey returns the data key the app's shell hands the service on its
// standard input (M12), as systemd hands an installed service its key on
// Linux: the shell unwraps it with its Keystore key, writes it, and closes
// the pipe. The wrapped key at path is the shell's to read.
func LoadHostKey(string) ([]byte, error) {
	return readShellKey(os.Stdin)
}

// readShellKey reads exactly a data key from r, to its end.
func readShellKey(r io.Reader) ([]byte, error) {
	key, err := io.ReadAll(io.LimitReader(r, HostKeySize+1))
	if err != nil {
		clear(key)
		return nil, fmt.Errorf("read the data key from the app's shell: %w", err)
	}
	if err := checkKeySize(key); err != nil {
		clear(key)
		return nil, fmt.Errorf("the app's shell: %w", err)
	}
	return key, nil
}

// WrapHostKey refuses: on Android the app's shell wraps the data key, with
// a Keystore key no other process may use.
func WrapHostKey(string, []byte) error {
	return errors.New("the Android app's shell wraps its data key itself")
}
