package commands

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"syscall"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

// controlClient returns a client for instance's control endpoint, and the
// identity the service must have before the client trusts it.
func controlClient(bi build.BuildInfo, instanceName string) (control.Client, layout.Layout, error) {
	l, err := layout.New(bi.Name, instanceName, bi.DevMode)
	if err != nil {
		return control.Client{}, layout.Layout{}, err
	}
	server := host.ControlServer{ServiceName: l.ServiceName}
	if l.Dev {
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
	var opErr *net.OpError
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) || errors.As(err, &opErr) {
		if l.Dev {
			return fmt.Errorf("the development service for instance %s isn't running; start it with: %s service run: %w", l.Instance, bi.Name, err)
		}
		start := bi.Name + " service start"
		if l.Instance != layout.DefaultInstance {
			start += " --instance " + l.Instance
		}
		return fmt.Errorf("the Dens service for instance %s isn't running; start it with: %s: %w", l.Instance, host.AdminCommand(start), err)
	}
	return err
}
