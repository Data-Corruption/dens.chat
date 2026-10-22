package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// fakeSpawn stands in for Flatpak's portal: the service it "starts" says
// answer on the pipe the launch hands it, or closes it saying nothing.
type fakeSpawn struct {
	mu     sync.Mutex
	argv   [][]string
	answer string
	after  time.Duration
}

func (f *fakeSpawn) spawn(argv []string, files ...*os.File) error {
	f.mu.Lock()
	f.argv = append(f.argv, argv)
	answer, after := f.answer, f.after
	f.mu.Unlock()
	// The launch closes its copy as Spawn returns, as the portal keeps
	// its own.
	fd, err := syscall.Dup(int(files[0].Fd()))
	if err != nil {
		return err
	}
	pipe := os.NewFile(uintptr(fd), "ready")
	go func() {
		defer pipe.Close()
		time.Sleep(50*time.Millisecond + after)
		if answer != "" {
			_, _ = pipe.WriteString(answer + "\n")
		}
	}()
	return nil
}

// A launch starts the Flatpak's service through Flatpak's Spawn when none
// runs, and hears from it that it's ready, or why it stopped; one that
// finds an older build's service has it quit first, and one that finds
// its own build's or a newer one starts nothing (M9.3).
func TestLaunchStartsTheFlatpaksService(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "run"))
	l, err := layout.New("dens", "main", layout.Flatpak)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.EnsureSession(); err != nil {
		t.Fatal(err)
	}
	me, err := host.CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	client := control.Client{Endpoint: l.ControlEndpoint, Server: host.ControlServer{Account: me}, Timeout: 5 * time.Second}
	f := &fakeSpawn{}
	saved := spawnLatest
	spawnLatest = f.spawn
	t.Cleanup(func() { spawnLatest = saved })
	bi := build.BuildInfo{Name: "dens", Version: "v1.1.0"}
	ctx := context.Background()

	for _, c := range []struct{ answer, want string }{
		{readyWord, ""},
		{"service start refused: Dens was rolled back to v1.1.0", "rolled back"},
		{"", "stopped as it started"},
	} {
		f.mu.Lock()
		f.answer = c.answer
		f.mu.Unlock()
		err := ensureFlatpakService(ctx, bi, client, l)
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("a service that said %q: %v", c.answer, err)
		}
	}
	f.mu.Lock()
	argv := strings.Join(f.argv[0], " ")
	f.mu.Unlock()
	if argv != "dens service run --ready-fd 3" {
		t.Fatalf("spawned %q", argv)
	}

	// A running service answers the launch.
	ln, err := host.ListenControl(l.ControlEndpoint, me)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := xlog.New(filepath.Join(dir, "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	var mu sync.Mutex
	version, quits := "v1.1.0", 0
	server := &control.Server{Allowed: me, Log: logs, Handlers: map[string]control.Handler{
		control.OpStatus: func(context.Context, control.Request) (control.Reply, error) {
			mu.Lock()
			defer mu.Unlock()
			return control.Reply{Result: control.Status{Version: version}}, nil
		},
		control.OpQuit: func(context.Context, control.Request) (control.Reply, error) {
			mu.Lock()
			defer mu.Unlock()
			quits++
			return control.Reply{}, nil
		},
	}}
	serving, stop := context.WithCancel(ctx)
	defer stop()
	go func() { _ = server.Serve(serving, ln) }()

	f.mu.Lock()
	f.argv, f.answer = nil, readyWord
	f.mu.Unlock()
	for _, running := range []string{"v1.1.0", "v1.2.0"} {
		mu.Lock()
		version = running
		mu.Unlock()
		if err := ensureFlatpakService(ctx, bi, client, l); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	version = "v1.0.0"
	mu.Unlock()
	if err := ensureFlatpakService(ctx, bi, client, l); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	spawned := len(f.argv)
	f.mu.Unlock()
	mu.Lock()
	if quits != 1 || spawned != 1 {
		t.Fatalf("an older build's service quit %d times, and %d started", quits, spawned)
	}
	// Another launch's service, of this build, answering first, as when two
	// start at once: this one's waits for its lock, then refuses.
	version = "v1.1.0"
	mu.Unlock()
	f.mu.Lock()
	f.answer, f.after = "service start refused: another Dens is running", 5*time.Second
	f.mu.Unlock()
	if err := spawnService(ctx, bi, client, l); err != nil {
		t.Fatalf("a launch whose service lost to another's: %v", err)
	}
}

// Two launches at once start one service between them: the second waits
// for the first, then finds the service it started (M9.3).
func TestTwoLaunchesStartOneService(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "run"))
	l, err := layout.New("dens", "main", layout.Flatpak)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.EnsureSession(); err != nil {
		t.Fatal(err)
	}
	me, err := host.CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	client := control.Client{Endpoint: l.ControlEndpoint, Server: host.ControlServer{Account: me}, Timeout: 5 * time.Second}
	logs, err := xlog.New(filepath.Join(dir, "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	serving, stop := context.WithCancel(context.Background())
	defer stop()
	var mu sync.Mutex
	spawned := 0
	saved := spawnLatest
	// The "service" each spawn starts answers on the control endpoint, and
	// says it's ready.
	spawnLatest = func(argv []string, files ...*os.File) error {
		mu.Lock()
		spawned++
		mu.Unlock()
		ln, err := host.ListenControl(l.ControlEndpoint, me)
		if err != nil {
			return err
		}
		server := &control.Server{Allowed: me, Log: logs, Handlers: map[string]control.Handler{
			control.OpStatus: func(context.Context, control.Request) (control.Reply, error) {
				return control.Reply{Result: control.Status{Version: "v1.1.0"}}, nil
			},
		}}
		go func() { _ = server.Serve(serving, ln) }()
		_, err = files[0].WriteString(readyWord + "\n")
		return err
	}
	t.Cleanup(func() { spawnLatest = saved })
	bi := build.BuildInfo{Name: "dens", Version: "v1.1.0"}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- ensureFlatpakService(context.Background(), bi, client, l)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if spawned != 1 {
		t.Fatalf("two launches started %d services", spawned)
	}
}
