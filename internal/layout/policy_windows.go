package layout

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileDeleteChild is FILE_DELETE_CHILD, which x/sys/windows doesn't define.
const fileDeleteChild = 0x40

// writeRights is every right that lets a principal change an object or its
// security: data, attributes, deletion, the DACL or the owner.
const writeRights = windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA | windows.FILE_WRITE_EA |
	windows.FILE_WRITE_ATTRIBUTES | fileDeleteChild | windows.DELETE | windows.WRITE_DAC |
	windows.WRITE_OWNER | windows.GENERIC_WRITE | windows.GENERIC_ALL

// CheckControl verifies the control directory and the named files in it
// before the service trusts them: owned by SYSTEM or Administrators, and
// writable by nobody else. Anything else is refused, never repaired.
func (l Layout) CheckControl(files ...string) error {
	if l.Dev {
		return checkOwnedByCurrentUser(l.Control)
	}
	for _, path := range append([]string{l.Control}, files...) {
		if err := checkAdminOnlyWrite(path); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}

// CheckData verifies that the data directory's DACL is protected from
// inheritance and grants access only to SYSTEM, Administrators and this
// process's account.
func (l Layout) CheckData() error {
	if l.Dev {
		return checkOwnedByCurrentUser(l.Data)
	}
	self, err := currentSID()
	if err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(l.Data, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("data directory %s: read security: %w", l.Data, err)
	}
	control, _, err := sd.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("data directory %s: DACL inherits from its parent", l.Data)
	}
	err = forEachAllowedACE(sd, func(sid *windows.SID, _ windows.ACCESS_MASK) error {
		if isSystemOrAdmins(sid) || sid.Equals(self) {
			return nil
		}
		return fmt.Errorf("grants access to %s", sid)
	})
	if err != nil {
		return fmt.Errorf("data directory %s: %w", l.Data, err)
	}
	return nil
}

// EnsureData creates the directories below Data, which inherit its DACL.
func (l Layout) EnsureData() error {
	for _, dir := range l.DataDirs()[1:] {
		if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := checkDirNoReparse(dir); err != nil {
			return err
		}
	}
	return nil
}

// EnsureDev creates a development instance's directories.
func (l Layout) EnsureDev() error {
	if !l.Dev {
		return errors.New("EnsureDev called on an installed layout")
	}
	for _, dir := range append([]string{l.Root, l.Control}, l.DataDirs()...) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := checkOwnedByCurrentUser(dir); err != nil {
			return err
		}
	}
	return nil
}

func checkAdminOnlyWrite(path string) error {
	if err := checkNoReparse(path); err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read security: %w", err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if owner == nil || !isSystemOrAdmins(owner) {
		return fmt.Errorf("owner is %v, want SYSTEM or Administrators", owner)
	}
	return forEachAllowedACE(sd, func(sid *windows.SID, mask windows.ACCESS_MASK) error {
		if mask&writeRights != 0 && !isSystemOrAdmins(sid) {
			return fmt.Errorf("%s may modify it", sid)
		}
		return nil
	})
}

// forEachAllowedACE calls fn for every access-allowed entry of sd's DACL. A
// missing DACL (which grants everyone everything) and any entry type other
// than plain allow or deny are errors.
func forEachAllowedACE(sd *windows.SECURITY_DESCRIPTOR, fn func(*windows.SID, windows.ACCESS_MASK) error) error {
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if dacl == nil {
		return errors.New("has no DACL")
	}
	for i := uint16(0); i < dacl.AceCount; i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(i), &ace); err != nil {
			return err
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
		default:
			return fmt.Errorf("has an unexpected ACE type %d", ace.Header.AceType)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if err := fn(sid, ace.Mask); err != nil {
			return err
		}
	}
	runtime.KeepAlive(sd)
	return nil
}

func isSystemOrAdmins(sid *windows.SID) bool {
	return sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid)
}

func currentSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read process token user: %w", err)
	}
	return user.User.Sid.Copy()
}

func checkNoReparse(path string) error {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attributes, err := windows.GetFileAttributes(pathPtr)
	if err != nil {
		return err
	}
	if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("is a reparse point")
	}
	return nil
}

func checkDirNoReparse(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	if err := checkNoReparse(path); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// checkOwnedByCurrentUser accepts a directory owned by this process's user
// or by its token's default owner (Administrators under elevation).
func checkOwnedByCurrentUser(path string) error {
	if err := checkDirNoReparse(path); err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("%s: read owner: %w", path, err)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return fmt.Errorf("%s: no owner", path)
	}
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return err
	}
	if owner.Equals(user.User.Sid) {
		return nil
	}
	defaultOwner, err := tokenDefaultOwner(token)
	if err == nil && owner.Equals(defaultOwner) {
		return nil
	}
	return fmt.Errorf("%s: owner %s is not the current user", path, owner)
}

func tokenDefaultOwner(token windows.Token) (*windows.SID, error) {
	var size uint32
	err := windows.GetTokenInformation(token, windows.TokenOwner, nil, 0, &size)
	if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
		return nil, fmt.Errorf("query token owner size: %v", err)
	}
	buffer := make([]byte, size)
	if err := windows.GetTokenInformation(token, windows.TokenOwner, &buffer[0], size, &size); err != nil {
		return nil, err
	}
	owner := *(**windows.SID)(unsafe.Pointer(&buffer[0]))
	if owner == nil || !owner.IsValid() {
		return nil, errors.New("token has no valid default owner")
	}
	copied, err := owner.Copy()
	runtime.KeepAlive(buffer)
	return copied, err
}

// ServiceControlFiles are the control files the service reads and checks,
// including the DPAPI blob it decrypts itself.
func (l Layout) ServiceControlFiles() []string {
	return []string{l.State, l.LifecycleLock, l.InstanceConfig, l.HostKey}
}
