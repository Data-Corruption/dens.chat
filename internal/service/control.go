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
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

func controlHandlers(a *app.App) map[string]control.Handler {
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
			url := fmt.Sprintf("http://127.0.0.1:%d/#token=%s", a.Instance.ClientPort, token)
			return control.Reply{Result: control.Pair{URL: url}}, nil
		},
		control.OpBackup: func(ctx context.Context, req control.Request) (control.Reply, error) {
			return backupReply(ctx, a, req.Password)
		},
	}
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
	status := control.Status{
		Version:     a.BuildInfo().Version,
		Instance:    a.Layout.Instance,
		ClientURL:   fmt.Sprintf("http://127.0.0.1:%d/", a.Instance.ClientPort),
		PasswordSet: passwordSet,
		DenEnabled:  a.Instance.Den.Enabled,
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
	if err := a.VerifyPassword(ctx, password); err != nil {
		switch {
		case errors.Is(err, vault.ErrWrongPassword):
			return control.Reply{}, control.Errorf("wrong password")
		case errors.Is(err, app.ErrNoPassword):
			return control.Reply{}, control.Errorf("set a local password in the browser before making a backup")
		case errors.Is(err, app.ErrTooManyAttempts):
			return control.Reply{}, control.Errorf("%v", err)
		}
		return control.Reply{}, err
	}
	temp, err := os.MkdirTemp(a.Layout.Temp, "backup-")
	if err != nil {
		return control.Reply{}, err
	}
	snapshot := filepath.Join(temp, "db.sqlite")
	if err := database.Snapshot(ctx, a.DB, snapshot); err != nil {
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
		err := backup.Write(writer, manifest, snapshot, a.Layout.Uploads)
		_ = writer.CloseWithError(err)
		_ = os.RemoveAll(temp)
		if err != nil {
			a.Log.Warnf("backup did not finish: %v", err)
		} else {
			a.Log.Info("Backup written")
		}
	}()
	name := fmt.Sprintf("%s-%s-%s.backup", a.BuildInfo().Name, a.Layout.Instance, now.Format("20060102-150405"))
	return control.Reply{Result: BackupResult{Name: name}, Body: reader}, nil
}
