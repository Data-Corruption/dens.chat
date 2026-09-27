package install

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/pkg/xsyscall"
)

// Uninstall removes an instance: its service, account, firewall rules and
// everything under its installation root, including its data. After the
// last instance it also removes what instances share. Once started it only
// moves forward; running it again finishes an interrupted uninstall.
func Uninstall(ctx context.Context, sys System, bi build.BuildInfo, opts Options) error {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	p := newProgress(out)
	defer p.close()
	if err := sys.CheckAdmin(); err != nil {
		return err
	}
	target, err := sys.Layout(opts.Instance)
	if err != nil {
		return err
	}
	if _, err := os.Stat(target.Control); errors.Is(err, fs.ErrNotExist) {
		p.printf("Instance %s is not installed.", target.Instance)
		return nil
	}
	others, err := installedInstances(filepath.Dir(target.Root))
	if err != nil {
		return err
	}
	last := len(others) == 0 || (len(others) == 1 && others[0] == target.Instance)
	p.printf("Uninstall instance %s:", target.Instance)
	p.printf("  removes %s (all of its data)", target.Root)
	if plan, ok := sys.(interface {
		UninstallPlan(l layout.Layout, last bool) []string
	}); ok {
		for _, line := range plan.UninstallPlan(target, last) {
			p.printf("  %s", line)
		}
	}
	if opts.DryRun {
		return nil
	}

	opLock, err := maintenance.AcquireOperationLock(ctx, target)
	if err != nil {
		return err
	}
	releaseOp := true
	defer func() {
		if releaseOp {
			_ = opLock.Close()
		}
	}()
	p.openLog(target.MaintenanceLog)

	version := bi.Version
	if state, err := maintenance.ReadState(target.State); err == nil {
		switch {
		case state.Version != "":
			version = state.Version
		case state.TargetVersion != "":
			version = state.TargetVersion
		}
	}
	data, err := maintenance.EncodeState(maintenance.NewState(maintenance.PhaseUninstalling, version, ""))
	if err != nil {
		return err
	}
	account, _ := sys.ServiceAccount(target)
	if err := sys.WriteControlFile(target, target.State, data, account); err != nil {
		return err
	}

	if running, err := sys.ServiceRunning(target); err == nil && running {
		p.printf("Stopping %s ...", target.ServiceName)
		if err := sys.StopService(target); err != nil {
			return err
		}
	}
	var lifeLock *xsyscall.Lock
	if _, err := os.Stat(target.LifecycleLock); err == nil {
		if lifeLock, err = maintenance.AcquireLifecycleLock(ctx, target); err != nil {
			return err
		}
	}
	p.printf("Removing %s ...", target.ServiceName)
	if err := sys.UnregisterService(target); err != nil {
		return err
	}

	// The locks live inside what is about to be deleted.
	p.close()
	if lifeLock != nil {
		_ = lifeLock.Close()
	}
	_ = opLock.Close()
	releaseOp = false
	if err := os.RemoveAll(target.Root); err != nil {
		return fmt.Errorf("remove %s: %w", target.Root, err)
	}
	if err := sys.RemoveAccount(target); err != nil {
		return err
	}
	if last {
		if err := sys.RemoveShared(target); err != nil {
			return err
		}
		_ = os.Remove(filepath.Dir(target.Root))
	}
	fmt.Fprintf(out, "Uninstalled instance %s.\n", target.Instance)
	return nil
}
