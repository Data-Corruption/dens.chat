package host

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// CurrentUser returns the account this process runs as.
func CurrentUser() (Identity, error) {
	sid, err := processSID()
	if err != nil {
		return Identity{}, err
	}
	return identityForSID(sid), nil
}

// LookupUser resolves an account by name (for example "PC\alice").
func LookupUser(name string) (Identity, error) {
	sid, _, _, err := windows.LookupSID("", name)
	if err != nil {
		return Identity{}, fmt.Errorf("look up account %q: %w", name, err)
	}
	return identityForSID(sid), nil
}

// LookupUserID resolves an account by SID string.
func LookupUserID(id string) (Identity, error) {
	sid, err := windows.StringToSid(id)
	if err != nil {
		return Identity{}, fmt.Errorf("parse SID %q: %w", id, err)
	}
	return identityForSID(sid), nil
}

func identityForSID(sid *windows.SID) Identity {
	identity := Identity{ID: sid.String()}
	if account, domain, _, err := sid.LookupAccount(""); err == nil {
		identity.Name = account
		if domain != "" {
			identity.Name = domain + `\` + account
		}
	}
	return identity
}

func processSID() (*windows.SID, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return nil, fmt.Errorf("open process token: %w", err)
	}
	defer token.Close()
	return tokenSID(token)
}

func tokenSID(token windows.Token) (*windows.SID, error) {
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read token user: %w", err)
	}
	return user.User.Sid.Copy()
}

// processOwner returns the account a process runs as. It needs only
// PROCESS_QUERY_LIMITED_INFORMATION, which a user holds on its own processes.
func processOwner(pid uint32) (Identity, error) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return Identity{}, fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return Identity{}, fmt.Errorf("open token of process %d: %w", pid, err)
	}
	defer token.Close()
	sid, err := tokenSID(token)
	if err != nil {
		return Identity{}, err
	}
	return identityForSID(sid), nil
}
