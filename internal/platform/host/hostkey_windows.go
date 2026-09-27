package host

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/Data-Corruption/dens.chat/pkg/xsyscall"

	"golang.org/x/sys/windows"
)

// LoadHostKey reads the machine-scope DPAPI blob at path and returns the data
// key it protects. The blob's DACL, set on the directory it inherits from,
// is what keeps other accounts from decrypting it.
func LoadHostKey(path string) ([]byte, error) {
	file, err := xsyscall.OpenNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open data key blob: %w", err)
	}
	defer file.Close()
	blob, err := io.ReadAll(io.LimitReader(file, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("read data key blob: %w", err)
	}
	key, err := dpapiUnprotect(blob)
	if err != nil {
		return nil, fmt.Errorf("unprotect data key: %w", err)
	}
	if err := checkKeySize(key); err != nil {
		return nil, err
	}
	return key, nil
}

// WrapHostKey protects key with machine-scope DPAPI and atomically writes the
// blob to path, where it inherits its directory's DACL.
func WrapHostKey(path string, key []byte) error {
	if err := checkKeySize(key); err != nil {
		return err
	}
	blob, err := dpapiProtect(key)
	if err != nil {
		return fmt.Errorf("protect data key: %w", err)
	}
	temp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(temp, blob, 0o600); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("write data key blob: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("publish data key blob: %w", err)
	}
	return nil
}

func dpapiProtect(data []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	flags := uint32(windows.CRYPTPROTECT_UI_FORBIDDEN | windows.CRYPTPROTECT_LOCAL_MACHINE)
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, flags, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return bytes.Clone(unsafe.Slice(out.Data, out.Size)), nil
}

func dpapiUnprotect(blob []byte) ([]byte, error) {
	if len(blob) == 0 {
		return nil, errors.New("empty blob")
	}
	in := windows.DataBlob{Size: uint32(len(blob)), Data: &blob[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	plain := unsafe.Slice(out.Data, out.Size)
	defer func() {
		clear(plain)
		windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	}()
	return bytes.Clone(plain), nil
}
