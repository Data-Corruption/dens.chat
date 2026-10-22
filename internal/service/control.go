package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/backup"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/guard"
	"github.com/Data-Corruption/dens.chat/internal/vault"
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
			return backupReply(ctx, a, req.Password)
		},
		control.OpShortcuts: func(context.Context, control.Request) (control.Reply, error) {
			return control.Reply{Stream: a.Shortcuts.Serve}, nil
		},
		control.OpOwnerInvite: func(ctx context.Context, req control.Request) (control.Reply, error) {
			return ownerInviteReply(ctx, a, req.Password)
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
	err := a.VerifyPassword(ctx, password)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, vault.ErrWrongPassword):
		return control.Errorf("wrong password")
	case errors.Is(err, app.ErrNoPassword):
		return control.Errorf("set a local password in the browser before %s", doing)
	case errors.Is(err, app.ErrTooManyAttempts):
		return control.Errorf("%v", err)
	}
	return err
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
		DenEnabled:  hosting.On,
		DenProblem:  hosting.Problem,
	}
	if latest, ok := a.LatestUpdate(cfg); ok {
		status.UpdateAvailable = latest
	}
	return control.Reply{Result: status}, nil
}

// BackupResult names the archive a backup streams.
type BackupResult struct {
	Name string `json:"name"`
}

// backupReply streams an archive of a consistent database snapshot and the
// uploads. The password check is the re-authentication a backup needs: the
// archive carries the data key sealed under that password.
func backupReply(ctx context.Context, a *app.App, password string) (control.Reply, error) {
	if err := checkPassword(ctx, a, password, "making a backup"); err != nil {
		return control.Reply{}, err
	}
	// The den takes no file off the disk from before the snapshot until
	// the archive holds the uploads, so the archive has every file the
	// snapshot names (M8).
	release := a.Den.HoldFiles()
	temp, err := os.MkdirTemp(a.Layout.Temp, "backup-")
	if err != nil {
		release()
		return control.Reply{}, err
	}
	snapshot := filepath.Join(temp, "db.sqlite")
	if err := database.Snapshot(ctx, a.DB, snapshot); err != nil {
		release()
		_ = os.RemoveAll(temp)
		return control.Reply{}, err
	}
	now := time.Now().UTC()
	manifest := backup.Manifest{
		App:           a.BuildInfo().Name,
		Version:       a.BuildInfo().Version,
		SchemaVersion: database.SchemaVersion(a.BuildInfo()),
		Instance:      a.Layout.Instance,
		CreatedAt:     now,
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
	}
	reader, writer := io.Pipe()
	go func() {
		skipped, err := backup.Write(writer, manifest, snapshot, a.Layout.Uploads)
		release()
		_ = writer.CloseWithError(err)
		_ = os.RemoveAll(temp)
		if skipped > 0 {
			// Only a file whose upload failed as it was stored goes now,
			// and no row names it.
			a.Log.Infof("Backup left out %d files that went while it was written", skipped)
		}
		if err != nil {
			a.Log.Warnf("backup did not finish: %v", err)
		} else {
			a.Log.Info("Backup written")
		}
	}()
	name := fmt.Sprintf("%s-%s-%s.backup", a.BuildInfo().Name, a.Layout.Instance, now.Format("20060102-150405"))
	return control.Reply{Result: BackupResult{Name: name}, Body: reader}, nil
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
		return control.Reply{}, control.Errorf("hosting is off; turn it on in Dens's settings, then make the invite")
	}
	if err := checkPassword(ctx, a, password, "making an owner invite"); err != nil {
		return control.Reply{}, err
	}
	info, ok := a.Den.Info()
	if !ok {
		return control.Reply{}, control.Errorf("this instance's den hasn't been created yet; create it in the browser")
	}
	code, err := a.Den.OwnerInvite(ctx)
	if err != nil {
		return control.Reply{}, err
	}
	a.Log.Info("Made an owner invite")
	return control.Reply{Result: control.OwnerInvite{Invite: denproto.EncodeInvite(info.ID, code, info.URL), Den: info.Name}}, nil
}
