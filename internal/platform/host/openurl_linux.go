package host

import (
	"os/exec"
)

// OpenURL asks the desktop to open url in the user's default browser.
func OpenURL(url string) error {
	cmd := exec.Command("xdg-open", url)
	if err := cmd.Start(); err != nil {
		return err
	}
	// xdg-open hands off to the browser and exits; reap it in the background.
	go func() { _ = cmd.Wait() }()
	return nil
}
