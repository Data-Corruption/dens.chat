package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/guard"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

func controlHandlers(a *app.App, quit func(why string)) map[string]control.Handler {
	return map[string]control.Handler{
		control.OpStatus: func(ctx context.Context, _ control.Request) (control.Reply, error) {
			return statusReply(ctx, a)
		},
		control.OpPair: func(context.Context, control.Request) (control.Reply, error) {
			token, err := a.Pairing.Issue()
			if err != nil {
				return control.Reply{}, err
			}
			a.Log.Info("Issued a pairing link")
			url := guard.PageOrigin(a.Instance.ClientPort) + "/#token=" + token
			return control.Reply{Result: control.Pair{URL: url}}, nil
		},
		control.OpBackup: func(ctx context.Context, req control.Request) (control.Reply, error) {
			if a.Locked() {
				return control.Reply{}, control.Errorf("Dens is locked; unlock it in the browser, then make the backup")
			}
			return backupReply(ctx, a, req.Password)
		},
		control.OpShortcuts: func(context.Context, control.Request) (control.Reply, error) {
			if a.Locked() {
				return control.Reply{}, control.Errorf("Dens is locked")
			}
			return control.Reply{Stream: a.Shortcuts.Serve}, nil
		},
		control.OpOwnerInvite: func(ctx context.Context, req control.Request) (control.Reply, error) {
			return ownerInviteReply(ctx, a, req.Password)
		},
		control.OpHosting: func(ctx context.Context, req control.Request) (control.Reply, error) {
			return hostingReply(ctx, a, req)
		},
		control.OpExport: func(ctx context.Context, req control.Request) (control.Reply, error) {
			if a.Locked() {
				return control.Reply{}, control.Errorf("Dens is locked; unlock it in the browser, then export the den")
			}
			return exportReply(ctx, a, req.Password)
		},
		control.OpRemoveDen: func(ctx context.Context, req control.Request) (control.Reply, error) {
			if a.Locked() {
				return control.Reply{}, control.Errorf("Dens is locked; unlock it in the browser, then remove the den")
			}
			return removeDenReply(ctx, a, req.Password)
		},
		control.OpPassword: func(ctx context.Context, req control.Request) (control.Reply, error) {
			return passwordReply(ctx, a, req)
		},
		control.OpCreateDen: func(ctx context.Context, req control.Request) (control.Reply, error) {
			return createDenReply(ctx, a, req)
		},
		control.OpLogs: func(_ context.Context, req control.Request) (control.Reply, error) {
			return logsReply(a, req.Level)
		},
		control.OpQuit: func(context.Context, control.Request) (control.Reply, error) {
			if !a.Layout.Form.Session() {
				return control.Reply{}, control.Errorf("the service manager runs this instance; stop it with: %s",
					host.AdminCommand(a.BuildInfo().Name+" service stop"))
			}
			return control.Reply{After: func() { quit("asked to over the control endpoint") }}, nil
		},
	}
}

// checkPassword re-authenticates the desktop user with the local password,
// before what the sensitive operation named by doing does.
func checkPassword(ctx context.Context, a *app.App, password, doing string) error {
	return passwordProblem(a, a.VerifyPassword(ctx, password), doing)
}

// passwordProblem says what a check of the local password found, before
// the operation named by doing.
func passwordProblem(a *app.App, err error, doing string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, vault.ErrWrongPassword):
		return control.Errorf("wrong password")
	case errors.Is(err, app.ErrNoPassword):
		return control.Errorf("set a local password with %s, or in the browser, before %s", commandFor(a, "password"), doing)
	case errors.Is(err, app.ErrTooManyAttempts), errors.Is(err, app.ErrLocked):
		return control.Errorf("%v", err)
	}
	return err
}

// logsReply streams the newest logs, or with level sets how much they say
// from now on (M11).
func logsReply(a *app.App, level string) (control.Reply, error) {
	if level != "" {
		normalized, err := xlog.NormalizeLevel(level)
		if err != nil {
			return control.Reply{}, control.Errorf("the log level is one of %s", strings.ReplaceAll(xlog.ValidLevels, "|", ", "))
		}
		kept, err := a.SetLogLevel(normalized)
		if err != nil {
			return control.Reply{}, err
		}
		return control.Reply{Result: control.LogLevel{Level: kept}}, nil
	}
	name, logs, err := a.Logs()
	if err != nil {
		return control.Reply{}, err
	}
	return control.Reply{Result: control.Backup{Name: name, Size: int64(len(logs))}, Body: io.NopCloser(bytes.NewReader(logs))}, nil
}

func statusReply(ctx context.Context, a *app.App) (control.Reply, error) {
	passwordSet, err := a.PasswordSet(ctx)
	if err != nil {
		return control.Reply{}, err
	}
	cfg, err := config.View(a.DB)
	if err != nil {
		return control.Reply{}, err
	}
	hosting := a.Hosting.State()
	status := control.Status{
		Version:     a.BuildInfo().Version,
		Instance:    a.Layout.Instance,
		ClientURL:   guard.PageOrigin(a.Instance.ClientPort) + "/",
		PasswordSet: passwordSet,
		Locked:      a.Locked(),
		DenEnabled:  hosting.On,
		DenProblem:  hosting.Problem,
		DenExported: cfg.DenExported,
	}
	if a.Den != nil {
		if info, ok := a.Den.Info(); ok {
			status.Den = info.Name
		}
	}
	if latest, ok := a.LatestUpdate(cfg); ok {
		status.UpdateAvailable = latest
	}
	return control.Reply{Result: status}, nil
}

// backupReply streams the archive of a backup. The password check is the
// re-authentication a backup needs: the archive carries the data key
// sealed under that password.
func backupReply(ctx context.Context, a *app.App, password string) (control.Reply, error) {
	if err := checkPassword(ctx, a, password, "making a backup"); err != nil {
		return control.Reply{}, err
	}
	archive, err := a.Backup(ctx)
	if err != nil {
		return control.Reply{}, err
	}
	return control.Reply{Result: control.Backup{Name: archive.Name, Size: archive.Size}, Body: archive}, nil
}

// hostingReply turns hosting on or off, as the page's switch does (M10):
// on takes the local password, since it opens ports to the internet, and
// fails, naming the port and the flag that moves it, while another program
// holds one.
func hostingReply(ctx context.Context, a *app.App, req control.Request) (control.Reply, error) {
	if req.On {
		if !a.CanHost() {
			return control.Reply{}, control.Errorf("%v", app.ErrCantHost)
		}
		if err := checkPassword(ctx, a, req.Password, "turning hosting on"); err != nil {
			return control.Reply{}, err
		}
	}
	var port *app.PortError
	switch err := a.Hosting.Set(ctx, req.On); {
	case errors.As(err, &port):
		return control.Reply{}, control.Errorf("%s", port.Error())
	case errors.Is(err, app.ErrCantHost):
		return control.Reply{}, control.Errorf("%v", err)
	case err != nil:
		return control.Reply{}, err
	}
	state := a.Hosting.State()
	den := a.Instance.Den
	result := control.Hosting{On: state.On, Serving: state.Serving, Problem: state.Problem,
		DenPort: den.Port, MediaUDPPort: den.MediaUDPPort, MediaTCPPort: den.MediaTCPPort}
	if a.Den != nil {
		if info, ok := a.Den.Info(); ok {
			result.Den, result.URL = info.Name, info.URL
		}
	}
	return control.Reply{Result: result}, nil
}

// exportReply streams the den's export (M10), as backupReply streams a
// backup, which the local password protects the same way. Hosting must be
// off first, so nothing reaches the den after the export's snapshot.
func exportReply(ctx context.Context, a *app.App, password string) (control.Reply, error) {
	if _, ok := a.Den.Info(); !ok {
		return control.Reply{}, control.Errorf("this instance holds no den to export")
	}
	hostingOn := control.Errorf("hosting is on, so members could still reach the den once it's exported; "+
		"turn it off with: %s, then export the den", commandFor(a, "den hosting off"))
	if a.Hosting.State().On {
		return control.Reply{}, hostingOn
	}
	if err := checkPassword(ctx, a, password, "exporting the den"); err != nil {
		return control.Reply{}, err
	}
	archive, err := a.ExportDen(ctx)
	switch {
	case errors.Is(err, app.ErrHostingOn):
		return control.Reply{}, hostingOn
	case errors.Is(err, app.ErrNoDen):
		return control.Reply{}, control.Errorf("this instance holds no den to export")
	case err != nil:
		return control.Reply{}, err
	}
	return control.Reply{Result: control.Backup{Name: archive.Name, Size: archive.Size}, Body: archive}, nil
}

// removeDenReply deletes this install's copy of its den once another
// computer serves it (M10), after the local password. A refusal says which
// of the checks didn't hold, and what to do.
func removeDenReply(ctx context.Context, a *app.App, password string) (control.Reply, error) {
	info, ok := a.Den.Info()
	if !ok {
		return control.Reply{}, control.Errorf("this instance holds no den")
	}
	err := a.RemoveDen(ctx, func() error {
		return passwordProblem(a, a.VerifyPassword(ctx, password), "removing the den")
	})
	var address *app.AddressError
	switch {
	case err == nil:
	case errors.Is(err, app.ErrHostingOn):
		return control.Reply{}, control.Errorf("hosting is on here, so this computer still serves the den; "+
			"once another computer hosts it, turn hosting off here with: %s, then remove the den", commandFor(a, "den hosting off"))
	case errors.Is(err, app.ErrNotExported):
		return control.Reply{}, control.Errorf("the den hasn't been exported since hosting was last on here, so no other computer holds all of it; "+
			"move it first, starting with: %s", commandFor(a, "den export"))
	case errors.Is(err, app.ErrNoDen):
		return control.Reply{}, control.Errorf("this instance holds no den")
	case errors.As(err, &address):
		return control.Reply{}, control.Errorf("%s", address.Error())
	default:
		return control.Reply{}, err
	}
	return control.Reply{Result: control.RemovedDen{Den: info.Name, URL: info.URL}}, nil
}

// commandFor is a dens command for this instance, as its desktop user
// types it.
func commandFor(a *app.App, args string) string {
	command := a.BuildInfo().Name + " " + args
	if a.Layout.Instance != layout.DefaultInstance {
		command += " --instance " + a.Layout.Instance
	}
	return command
}

// ownerInviteReply makes an invite that hands the den this instance hosts
// to whoever redeems it, and makes its owner a moderator (M8). The den's
// machine has the last word on who owns it, so the desktop user's local
// password is all it takes: that's also the way back for an owner locked
// out of their account.
func ownerInviteReply(ctx context.Context, a *app.App, password string) (control.Reply, error) {
	if !a.CanHost() {
		return control.Reply{}, control.Errorf("the %s hosts no den", a.Layout.Form)
	}
	if !a.Hosting.State().On {
		return control.Reply{}, control.Errorf("hosting is off; turn it on with %s, then make the invite", commandFor(a, "den hosting on"))
	}
	if err := checkPassword(ctx, a, password, "making an owner invite"); err != nil {
		return control.Reply{}, err
	}
	info, ok := a.Den.Info()
	if !ok {
		return control.Reply{}, control.Errorf("this instance's den hasn't been created yet; create it with %s, or in the browser",
			commandFor(a, "den create NAME ADDRESS"))
	}
	code, err := a.Den.OwnerInvite(ctx)
	if err != nil {
		return control.Reply{}, err
	}
	a.Log.Info("Made an owner invite")
	return control.Reply{Result: control.OwnerInvite{Invite: denproto.EncodeInvite(info.ID, code, info.URL), Den: info.Name, URL: info.URL}}, nil
}

// passwordReply sets the local password, or changes it given the current
// one, as the page does (M10), so a server never needs the page. The
// Flatpak's first password asks whether to remember its key, which only
// its page does, so the Flatpak keeps its password in its settings.
func passwordReply(ctx context.Context, a *app.App, req control.Request) (control.Reply, error) {
	if a.Layout.Form == layout.Flatpak {
		return control.Reply{}, control.Errorf("the Flatpak's local password is set and changed in Dens's settings")
	}
	if err := vault.ValidatePassword(req.NewPassword); err != nil {
		return control.Reply{}, control.Errorf("%v", err)
	}
	first, err := a.ChangePassword(ctx, req.Password, req.NewPassword)
	if err := passwordProblem(a, err, "changing it"); err != nil {
		return control.Reply{}, err
	}
	if first {
		a.Log.Info("Local password set")
	} else {
		a.Log.Info("Local password changed")
	}
	return control.Reply{Result: control.PasswordChanged{First: first}}, nil
}

// createDenReply creates the den this instance hosts from the command line
// (M10), with no account on this computer, and makes the owner invite that
// whoever will own it redeems from their own Dens. Create your den, on the
// page, joins as the owner here instead. Hosting must be serving, so the
// invite reaches the den at its address.
func createDenReply(ctx context.Context, a *app.App, req control.Request) (control.Reply, error) {
	if !a.CanHost() {
		return control.Reply{}, control.Errorf("%v", app.ErrCantHost)
	}
	if info, ok := a.Den.Info(); ok {
		return control.Reply{}, control.Errorf("this instance hosts %s already, at %s, and an instance hosts one den", info.Name, info.URL)
	}
	switch state := a.Hosting.State(); {
	case !state.On:
		return control.Reply{}, control.Errorf("hosting is off; turn it on with %s, then create the den", commandFor(a, "den hosting on"))
	case !state.Serving && state.Problem != "":
		return control.Reply{}, control.Errorf("hosting is on, but the den doesn't serve: %s", state.Problem)
	case !state.Serving:
		return control.Reply{}, control.Errorf("hosting is on, but the den hasn't started yet; try again in a moment")
	}
	name, err := denproto.CleanName(req.Name, denproto.MaxNameRunes)
	if err != nil {
		return control.Reply{}, control.Errorf("the den's name: %v", err)
	}
	url, err := denproto.NormalizeDenURL(req.URL)
	if err != nil {
		return control.Reply{}, control.Errorf("the den's address: %v", err)
	}
	if err := checkPassword(ctx, a, req.Password, "creating the den"); err != nil {
		return control.Reply{}, err
	}
	code, err := a.Den.Create(ctx, name, url)
	if err != nil {
		return control.Reply{}, err
	}
	info, _ := a.Den.Info()
	a.Dens.Changed()
	return control.Reply{Result: control.OwnerInvite{Invite: denproto.EncodeInvite(info.ID, code, info.URL), Den: info.Name, URL: info.URL}}, nil
}
