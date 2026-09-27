package install

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/instance"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/server"
	"github.com/Data-Corruption/dens.chat/pkg/xsyscall"

	"golang.org/x/mod/semver"
)

// Options configure an install or update of one instance.
type Options struct {
	Instance string
	// Binary is the binary to install; the running executable by default.
	Binary string
	// Cosign is a verified cosign binary to keep for dens update.
	Cosign     string
	ReleaseURL string
	// User overrides the desktop user allowed to pair.
	User string
	// Den turns the den role on or off; nil keeps the current setting.
	Den          *bool
	ClientPort   int
	DenPort      int
	MediaUDPPort int
	MediaTCPPort int
	DryRun       bool
	Out          io.Writer
}

// member is one instance taking part in a transaction.
type member struct {
	l          layout.Layout
	prior      *maintenance.State
	account    host.Identity
	fresh      bool
	wasRunning bool
	opLock     *xsyscall.Lock
	lifeLock   *xsyscall.Lock
}

// Install installs instance opts.Instance, or updates or reconfigures it
// when it exists, and moves every other installed instance to this binary's
// version.
func Install(ctx context.Context, sys System, bi build.BuildInfo, opts Options) (err error) {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	p := newProgress(out)
	defer p.close()
	if bi.DevMode {
		return errors.New("a development build can't be installed; use a release build")
	}
	if !semver.IsValid(bi.Version) {
		return fmt.Errorf("this build's version %q is not a release version", bi.Version)
	}
	if err := sys.CheckAdmin(); err != nil {
		return err
	}
	if err := sys.Preflight(); err != nil {
		return err
	}
	target, err := sys.Layout(opts.Instance)
	if err != nil {
		return err
	}
	binary := opts.Binary
	if binary == "" {
		if binary, err = os.Executable(); err != nil {
			return fmt.Errorf("locate this binary: %w", err)
		}
	}

	members, err := transactionMembers(sys, target)
	if err != nil {
		return err
	}
	previous, err := readConfig(target)
	if err != nil {
		return err
	}
	cfg, err := desiredConfig(sys, target, bi, opts, previous)
	if err != nil {
		return err
	}
	running, _ := sys.ServiceRunning(target)
	if err := checkPorts(cfg, previous, running); err != nil {
		return err
	}
	printPlan(p, sys, bi, members, target, cfg, previous == nil)
	if opts.DryRun {
		return nil
	}

	for _, m := range members {
		if err := sys.PrepareRoot(m.l); err != nil {
			return err
		}
		m.opLock, err = maintenance.AcquireOperationLock(ctx, m.l)
		if err != nil {
			return err
		}
		defer m.opLock.Close()
	}
	p.openLog(target.MaintenanceLog)
	p.printf("Installing %s %s (instance %s)", bi.Name, bi.Version, target.Instance)

	for _, m := range members {
		state, err := maintenance.ReadState(m.l.State)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			m.fresh = true
		case err != nil:
			return fmt.Errorf("instance %s: %w", m.l.Instance, err)
		case state.Phase == maintenance.PhaseUninstalling:
			return fmt.Errorf("instance %s is being uninstalled; finish that first: %s", m.l.Instance, host.AdminCommand(bi.Name+" uninstall --instance "+m.l.Instance))
		default:
			m.prior = &state
		}
		if m.fresh && m.l.Instance != target.Instance {
			return fmt.Errorf("instance %s has no lifecycle state; uninstall it or install it first", m.l.Instance)
		}
		if acct, err := sys.ServiceAccount(m.l); err == nil {
			m.account = acct
		}
	}

	j := &journal{}
	defer func() {
		if err != nil {
			err = errors.Join(err, j.rollback(out))
		}
	}()

	// Restarting what was running is the last undo, once every other step,
	// including the state, is back as it was.
	j.add(func() error {
		var joined error
		for _, m := range members {
			if m.wasRunning {
				joined = errors.Join(joined, sys.StartService(m.l))
			}
		}
		return joined
	})

	for _, m := range members {
		if err := publish(sys, m, transition(m.prior, bi.Version), j); err != nil {
			return err
		}
	}

	for _, m := range members {
		running, err := sys.ServiceRunning(m.l)
		if err != nil {
			return fmt.Errorf("instance %s: %w", m.l.Instance, err)
		}
		if running {
			p.printf("Stopping %s ...", m.l.ServiceName)
			if err := sys.StopService(m.l); err != nil {
				return err
			}
			m.wasRunning = true
		}
	}

	for _, m := range members {
		if _, err := os.Stat(m.l.LifecycleLock); errors.Is(err, fs.ErrNotExist) {
			if err := sys.WriteControlFile(m.l, m.l.LifecycleLock, nil, m.account); err != nil {
				return err
			}
		}
		m.lifeLock, err = maintenance.AcquireLifecycleLock(ctx, m.l)
		if err != nil {
			return fmt.Errorf("instance %s: %w", m.l.Instance, err)
		}
		lock := m.lifeLock
		j.add(lock.Close)
	}

	targetMember := members[slices.IndexFunc(members, func(m *member) bool { return m.l.Instance == target.Instance })]
	created, err := sys.EnsureAccount(target)
	if err != nil {
		return err
	}
	if created {
		j.add(func() error { return sys.RemoveAccount(target) })
	}

	p.printf("Installing the binary at %s ...", target.Binary)
	undo, err := sys.InstallBinary(target, binary)
	if err != nil {
		return err
	}
	j.add(undo)
	if opts.Cosign != "" {
		if err := sys.InstallCosign(target, opts.Cosign); err != nil {
			return err
		}
	}

	for _, m := range members {
		p.printf("Registering %s ...", m.l.ServiceName)
		undo, err := sys.RegisterService(m.l)
		if err != nil {
			return err
		}
		j.add(undo)
		if m.account, err = sys.ServiceAccount(m.l); err != nil {
			return err
		}
		if err := sys.Secure(m.l, m.account); err != nil {
			return err
		}
	}

	if err := writeConfig(sys, target, targetMember.account, cfg, j); err != nil {
		return err
	}
	if _, err := os.Stat(target.HostKey); errors.Is(err, fs.ErrNotExist) {
		p.printf("Generating the data key ...")
		key := make([]byte, host.HostKeySize)
		if _, err := rand.Read(key); err != nil {
			return err
		}
		err := sys.WrapHostKey(target, key)
		clear(key)
		if err != nil {
			return err
		}
		j.add(func() error { return os.Remove(target.HostKey) })
	}
	if err := sys.ConfigureFirewall(target, cfg); err != nil {
		return err
	}
	j.add(func() error {
		restore := instance.Config{Den: instance.Den{Enabled: false}}
		if previous != nil {
			restore = *previous
		}
		return sys.ConfigureFirewall(target, restore)
	})
	if err := sys.RegisterExtras(target); err != nil {
		return err
	}
	// Rewrite the transitional states now that the service account can be
	// given read access to them.
	for _, m := range members {
		state, err := maintenance.ReadState(m.l.State)
		if err != nil {
			return err
		}
		if err := writeState(sys, m, state); err != nil {
			return err
		}
	}

	// The point of no return: each service migrates when it starts.
	for _, m := range members {
		_ = m.lifeLock.Close()
	}
	j.commit()
	for _, m := range members {
		p.printf("Starting %s (this migrates its data if needed) ...", m.l.ServiceName)
		if err := sys.StartService(m.l); err != nil {
			return fmt.Errorf("%s did not start: %w\nIts lifecycle state is left for recovery. Find out why in %s, fix the cause, and run the installer again", m.l.ServiceName, err, host.ServiceLogHint(m.l.ServiceName, m.l.Logs))
		}
		if err := writeState(sys, m, maintenance.NewState(maintenance.PhaseReady, bi.Version, "")); err != nil {
			return err
		}
	}
	for _, m := range members {
		if !m.fresh && !m.wasRunning {
			// It was stopped before; it only ran to migrate.
			if err := sys.StopService(m.l); err != nil {
				return err
			}
		}
	}

	if previous == nil {
		p.printf("Installed %s %s as instance %s.", bi.Name, bi.Version, target.Instance)
	} else {
		p.printf("%s %s is installed for instance %s.", bi.Name, bi.Version, target.Instance)
	}
	openCmd := bi.Name + " open"
	if target.Instance != layout.DefaultInstance {
		openCmd += " --instance " + target.Instance
	}
	p.printf("As %s, run: %s", cfg.DesktopUser.Name, openCmd)
	return nil
}

// transactionMembers is the target plus every installed instance, sorted
// by name so concurrent transactions take locks in the same order.
func transactionMembers(sys System, target layout.Layout) ([]*member, error) {
	names, err := installedInstances(filepath.Dir(target.Root))
	if err != nil {
		return nil, err
	}
	if !slices.Contains(names, target.Instance) {
		names = append(names, target.Instance)
	}
	slices.Sort(names)
	members := make([]*member, 0, len(names))
	for _, name := range names {
		l := target
		if name != target.Instance {
			if l, err = sys.Layout(name); err != nil {
				return nil, err
			}
		}
		members = append(members, &member{l: l})
	}
	return members, nil
}

// installedInstances lists the instances under base that have lifecycle state.
func installedInstances(base string) ([]string, error) {
	entries, err := os.ReadDir(base)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || layout.ValidateInstance(entry.Name()) != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(base, entry.Name(), "control", layout.StateFileName)); err == nil {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

// transition is the state an instance moves through to reach version.
func transition(prior *maintenance.State, version string) maintenance.State {
	switch {
	case prior == nil:
		return maintenance.NewState(maintenance.PhaseInstalling, "", version)
	case prior.Version == "":
		// A first install that never finished.
		return maintenance.NewState(maintenance.PhaseInstalling, "", version)
	default:
		return maintenance.NewState(maintenance.PhaseUpdating, prior.Version, version)
	}
}

func publish(sys System, m *member, next maintenance.State, j *journal) error {
	prior := m.prior
	if err := writeState(sys, m, next); err != nil {
		return err
	}
	j.add(func() error {
		if prior == nil {
			return os.Remove(m.l.State)
		}
		return writeState(sys, m, *prior)
	})
	return nil
}

func writeState(sys System, m *member, state maintenance.State) error {
	data, err := maintenance.EncodeState(state)
	if err != nil {
		return err
	}
	return sys.WriteControlFile(m.l, m.l.State, data, m.account)
}

func readConfig(l layout.Layout) (*instance.Config, error) {
	cfg, err := instance.Read(l.InstanceConfig)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// desiredConfig is the target instance's configuration after this run: the
// current one (or the defaults) with the requested changes.
func desiredConfig(sys System, l layout.Layout, bi build.BuildInfo, opts Options, previous *instance.Config) (instance.Config, error) {
	cfg := instance.Default(l.Instance)
	if previous != nil {
		cfg = *previous
	}
	switch {
	case opts.ReleaseURL != "":
		cfg.ReleaseURL = opts.ReleaseURL
	case previous == nil:
		cfg.ReleaseURL = bi.ReleaseURL
	}
	if opts.User != "" || previous == nil {
		user, err := sys.DesktopUser(opts.User)
		if err != nil {
			return cfg, err
		}
		cfg.DesktopUser = user
	}
	if opts.Den != nil {
		cfg.Den.Enabled = *opts.Den
	}
	for _, set := range []struct {
		value int
		field *int
	}{
		{opts.ClientPort, &cfg.ClientPort},
		{opts.DenPort, &cfg.Den.Port},
		{opts.MediaUDPPort, &cfg.Den.MediaUDPPort},
		{opts.MediaTCPPort, &cfg.Den.MediaTCPPort},
	} {
		if set.value != 0 {
			*set.field = set.value
		}
	}
	return cfg, cfg.Validate()
}

func writeConfig(sys System, l layout.Layout, account host.Identity, cfg instance.Config, j *journal) error {
	data, err := instance.Encode(cfg)
	if err != nil {
		return err
	}
	previous, readErr := os.ReadFile(l.InstanceConfig)
	if err := sys.WriteControlFile(l, l.InstanceConfig, data, account); err != nil {
		return err
	}
	j.add(func() error {
		if errors.Is(readErr, fs.ErrNotExist) {
			return os.Remove(l.InstanceConfig)
		}
		return sys.WriteControlFile(l, l.InstanceConfig, previous, account)
	})
	return nil
}

// checkPorts refuses ports another program holds. Ports the instance
// already uses are skipped while its service is running, since the service
// holds them itself.
func checkPorts(cfg instance.Config, previous *instance.Config, running bool) error {
	type port struct {
		network, addr string
		value, old    int
	}
	var ports []port
	old := instance.Config{}
	if previous != nil {
		old = *previous
	}
	ports = append(ports, port{"loopback", "", cfg.ClientPort, old.ClientPort})
	if cfg.Den.Enabled {
		oldDen := old.Den
		if !oldDen.Enabled {
			oldDen = instance.Den{}
		}
		ports = append(ports,
			port{"tcp4", "127.0.0.1", cfg.Den.Port, oldDen.Port},
			port{"tcp", "", cfg.Den.MediaTCPPort, oldDen.MediaTCPPort},
			port{"udp", "", cfg.Den.MediaUDPPort, oldDen.MediaUDPPort},
		)
	}
	for _, p := range ports {
		if running && p.value == p.old {
			continue
		}
		address := net.JoinHostPort(p.addr, strconv.Itoa(p.value))
		var err error
		switch p.network {
		case "loopback":
			// The client listener binds both loopbacks; check the same way.
			var lns []net.Listener
			if lns, err = server.ListenLoopback(p.value); err == nil {
				for _, ln := range lns {
					_ = ln.Close()
				}
			}
		case "tcp", "udp":
			// Media ports listen on every interface; checking them by
			// binding would make Windows Firewall ask about the installer.
			err = host.PortFree(p.network, p.value)
		default:
			var ln net.Listener
			if ln, err = net.Listen(p.network, address); err == nil {
				_ = ln.Close()
			}
		}
		if err != nil {
			return fmt.Errorf("port %d is in use by another program; choose another with the matching port flag: %w", p.value, err)
		}
	}
	return nil
}

func printPlan(p *progress, sys System, bi build.BuildInfo, members []*member, target layout.Layout, cfg instance.Config, fresh bool) {
	action := "Update"
	if fresh {
		action = "Install"
	}
	p.printf("%s %s %s:", action, bi.Name, bi.Version)
	p.printf("  instance      %s", target.Instance)
	p.printf("  desktop user  %s", cfg.DesktopUser)
	p.printf("  client        http://127.0.0.1:%d/ (loopback only)", cfg.ClientPort)
	if cfg.Den.Enabled {
		p.printf("  den           127.0.0.1:%d behind Caddy; media %d/udp and %d/tcp from the internet",
			cfg.Den.Port, cfg.Den.MediaUDPPort, cfg.Den.MediaTCPPort)
	} else {
		p.printf("  den           not hosted")
	}
	if cfg.ReleaseURL != "" {
		p.printf("  updates from  %s", cfg.ReleaseURL)
	}
	if plan, ok := sys.(interface {
		Plan(l layout.Layout) []string
	}); ok {
		for _, line := range plan.Plan(target) {
			p.printf("  %s", line)
		}
	}
	for _, m := range members {
		if m.l.Instance != target.Instance {
			p.printf("  also moves instance %s to %s", m.l.Instance, bi.Version)
		}
	}
}
