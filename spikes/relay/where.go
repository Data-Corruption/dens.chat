package main

import (
	"fmt"
	"os"
	"os/user"
	"strings"
)

// where says what the probe runs as and inside, for the log: the same
// link from a desktop session, a service's sandbox, the Flatpak's or the
// Android app's is four different answers to the firewall's question.
func where() string {
	var parts []string
	if u, err := user.Current(); err == nil {
		parts = append(parts, fmt.Sprintf("user=%q uid=%s", u.Username, u.Uid))
	} else {
		parts = append(parts, fmt.Sprintf("uid=%d", os.Getuid()))
	}
	if id := os.Getenv("FLATPAK_ID"); id != "" {
		parts = append(parts, "flatpak="+id)
	} else if _, err := os.Stat("/.flatpak-info"); err == nil {
		parts = append(parts, "flatpak=yes")
	}
	if os.Getenv("INVOCATION_ID") != "" {
		parts = append(parts, "systemd-unit=yes")
	}
	for _, name := range []string{"XDG_SESSION_TYPE", "XDG_CURRENT_DESKTOP", "SESSIONNAME"} {
		if v := os.Getenv(name); v != "" {
			parts = append(parts, name+"="+v)
		}
	}
	return strings.Join(append(parts, winSession()), " ")
}
