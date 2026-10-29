package host

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The installer's Start menu shortcut (M12.3) opens the page, and carries
// Dens's app ID, which Windows shows a desktop app's toasts under only once
// a shortcut carries it. Go has no shortcuts, so the shell's own objects
// make it, through COM.

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")

	clsidShellLink   = windows.GUID{Data1: 0x00021401, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidShellLinkW    = windows.GUID{Data1: 0x000214F9, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidPersistFile   = windows.GUID{Data1: 0x0000010B, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidPropertyStore = windows.GUID{Data1: 0x886D8EEB, Data2: 0x8CF2, Data3: 0x4446, Data4: [8]byte{0x8D, 0x02, 0xCD, 0xBA, 0x1D, 0xBD, 0xCF, 0x99}}
	// PKEY_AppUserModel_ID.
	appUserModelID = propertyKey{fmtid: windows.GUID{Data1: 0x9F4C2855, Data2: 0x9F79, Data3: 0x4B39, Data4: [8]byte{0xA8, 0xD0, 0xE1, 0xD4, 0x2D, 0xE1, 0xD5, 0xF3}}, pid: 5}
)

// The methods called, by their place in each interface's table.
const (
	shellLinkSetDescription      = 7
	shellLinkSetWorkingDirectory = 9
	shellLinkSetArguments        = 11
	shellLinkSetPath             = 20
	propertyStoreSetValue        = 6
	propertyStoreCommit          = 7
	persistFileSave              = 6

	clsctxInprocServer = 1
	vtLPWSTR           = 31
)

type propertyKey struct {
	fmtid windows.GUID
	pid   uint32
}

// propVariant is a PROPVARIANT holding a string.
type propVariant struct {
	vt       uint16
	reserved [3]uint16
	val      uintptr
	_        uintptr
}

// MakeShortcut writes a shortcut at path that runs target with args, under
// appID, the app ID its app's toasts show under.
func MakeShortcut(path, target, args, description, appID string) error {
	// The shell's objects live in a single-threaded apartment, on the
	// thread that made them.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// S_FALSE is a thread that had COM already, which still needs its
	// CoUninitialize.
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err != nil && err != syscall.Errno(1) {
		return fmt.Errorf("start COM: %w", err)
	}
	defer windows.CoUninitialize()
	var link *comObject
	if r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidShellLinkW)), uintptr(unsafe.Pointer(&link))); int32(r) < 0 {
		return fmt.Errorf("make a shortcut: HRESULT 0x%08X", uint32(r))
	}
	defer link.release()
	for _, set := range []struct {
		method int
		value  string
	}{
		{shellLinkSetPath, target},
		{shellLinkSetArguments, args},
		{shellLinkSetDescription, description},
		{shellLinkSetWorkingDirectory, ""},
	} {
		value, err := windows.UTF16PtrFromString(set.value)
		if err != nil {
			return err
		}
		if err := link.call(set.method, uintptr(unsafe.Pointer(value))); err != nil {
			return fmt.Errorf("set the shortcut's %d: %w", set.method, err)
		}
	}
	store, err := link.query(&iidPropertyStore)
	if err != nil {
		return fmt.Errorf("the shortcut's properties: %w", err)
	}
	defer store.release()
	id, err := windows.UTF16PtrFromString(appID)
	if err != nil {
		return err
	}
	value := propVariant{vt: vtLPWSTR, val: uintptr(unsafe.Pointer(id))}
	if err := store.call(propertyStoreSetValue, uintptr(unsafe.Pointer(&appUserModelID)), uintptr(unsafe.Pointer(&value))); err != nil {
		return fmt.Errorf("set the shortcut's app ID: %w", err)
	}
	runtime.KeepAlive(id)
	if err := store.call(propertyStoreCommit); err != nil {
		return fmt.Errorf("keep the shortcut's app ID: %w", err)
	}
	file, err := link.query(&iidPersistFile)
	if err != nil {
		return fmt.Errorf("save the shortcut: %w", err)
	}
	defer file.release()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err := file.call(persistFileSave, uintptr(unsafe.Pointer(name)), 1); err != nil {
		return fmt.Errorf("save the shortcut at %s: %w", path, err)
	}
	return nil
}
