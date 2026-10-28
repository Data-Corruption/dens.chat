package host

import (
	"bytes"
	"strings"
	"testing"
)

// The shell writes the data key and closes the pipe (M12): exactly a key is
// one, and anything else is refused.
func TestShellKeyIsExactlyAKey(t *testing.T) {
	want := bytes.Repeat([]byte{7}, HostKeySize)
	key, err := readShellKey(bytes.NewReader(want))
	if err != nil || !bytes.Equal(key, want) {
		t.Fatalf("readShellKey = %x, %v", key, err)
	}
	for name, sent := range map[string][]byte{
		"nothing":   nil,
		"too short": want[:HostKeySize-1],
		"too long":  append(bytes.Clone(want), 0),
	} {
		if _, err := readShellKey(bytes.NewReader(sent)); err == nil || !strings.Contains(err.Error(), "shell") {
			t.Errorf("%s: readShellKey = %v, want refused", name, err)
		}
	}
	if err := WrapHostKey("datakey.keystore", want); err == nil {
		t.Fatal("WrapHostKey wrapped a key on Android")
	}
}
