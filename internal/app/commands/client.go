package commands

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"syscall"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

// controlClient returns a client for instance's control endpoint, and the
// identity the service must have before the client trusts it; tests
// replace it.
var controlClient = func(bi build.BuildInfo, instanceName string) (control.Client, layout.Layout, error) {
	l, err := layout.New(bi.Name, instanceName, app.FormOf(bi))
	if err != nil {
		return control.Client{}, layout.Layout{}, err
	}
	// A session form's service runs as its user, and the installed one as
	// its own account.
	server := host.ControlServer{ServiceName: l.ServiceName}
	if l.Form.Session() {
		server.Account, err = host.CurrentUser()
	} else {
		server.Account, err = host.LookupUser(l.Account)
	}
	if err != nil {
		return control.Client{}, l, fmt.Errorf("instance %s is not installed on this computer: %w", instanceName, err)
	}
	return control.Client{Endpoint: l.ControlEndpoint, Server: server, Timeout: 30 * time.Second}, l, nil
}

// explainCallError turns a failure to reach the service into advice.
func explainCallError(bi build.BuildInfo, l layout.Layout, err error) error {
	var userErr *control.UserError
	if errors.As(err, &userErr) {
		return err
	}
	// The Windows pipe's DACL admits only the desktop user, so other
	// accounts fail to open it rather than getting the service's refusal.
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("instance %s only answers the desktop user it was installed for: %w", l.Instance, err)
	}
	if notRunning(err) {
		flag := ""
		if l.Instance != layout.DefaultInstance {
			flag = " --instance " + l.Instance
		}
		switch l.Form {
		case layout.Development:
			return fmt.Errorf("the development service for instance %s isn't running; start it with: %s service run%s: %w", l.Instance, bi.Name, flag, err)
		case layout.Flatpak:
			return fmt.Errorf("Dens isn't running; open it from your apps menu, or start it with: flatpak run %s: %w", layout.DesktopID, err)
		case layout.Android:
			return fmt.Errorf("Dens isn't running; open it from your apps: %w", err)
		}
		start := bi.Name + " service start" + flag
		return fmt.Errorf("the Dens service for instance %s isn't running; start it with: %s: %w", l.Instance, host.AdminCommand(start), err)
	}
	return err
}

// notRunning reports whether err is the service's endpoint not answering:
// not there, or nobody listening.
func notRunning(err error) bool {
	var opErr *net.OpError
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) || errors.As(err, &opErr)
}

// quitService stops a session form's running service over its control
// endpoint (M9.3), as a restore needs; one that isn't running is already
// stopped.
func quitService(bi build.BuildInfo, instanceName string) error {
	client, l, err := controlClient(bi, instanceName)
	if err != nil {
		return err
	}
	switch err := client.Call(control.Request{Op: control.OpQuit}, nil); {
	case err == nil:
		fmt.Println("Stopped Dens, which was running.")
		return nil
	case notRunning(err):
		return nil
	default:
		return explainCallError(bi, l, err)
	}
}
