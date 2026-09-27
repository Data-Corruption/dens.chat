package host

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
)

// CurrentUser returns the account this process runs as.
func CurrentUser() (Identity, error) {
	uid := os.Getuid()
	identity := Identity{ID: strconv.Itoa(uid)}
	if u, err := user.LookupId(identity.ID); err == nil {
		identity.Name = u.Username
	}
	return identity, nil
}

// LookupUser resolves an account by name.
func LookupUser(name string) (Identity, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return Identity{}, fmt.Errorf("look up account %q: %w", name, err)
	}
	return Identity{ID: u.Uid, Name: u.Username}, nil
}

// LookupUserID resolves an account by UID.
func LookupUserID(id string) (Identity, error) {
	u, err := user.LookupId(id)
	if err != nil {
		return Identity{}, fmt.Errorf("look up UID %s: %w", id, err)
	}
	return Identity{ID: u.Uid, Name: u.Username}, nil
}
