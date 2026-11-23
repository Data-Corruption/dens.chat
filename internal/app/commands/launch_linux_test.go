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
	// unstarted is how many spawns' sandboxes end before the service runs
	// in them, which says nothing, before one starts.
	unstarted int
}

func (f *fakeSpawn) spawn(_ context.Context, argv []string, files ...*os.File) (<-chan struct{}, error) {
	f.mu.Lock()
	f.argv = append(f.argv, argv)
	answer, after := f.answer, f.after
	failed := f.unstarted > 0
	if failed {
		f.unstarted--
	}
	f.mu.Unlock()
	ended := make(chan struct{})
	if failed {
		// Nothing holds the pipe once the launch closes its own copy.
		close(ended)
		return ended, nil
	}
	// The launch closes its copy as Spawn returns, as the portal keeps
	// its own.
	fd, err := syscall.Dup(int(files[0].Fd()))
	if err != nil {
		return nil, err
	}
	pipe := os.NewFile(uintptr(fd), "ready")
	go func() {
		defer pipe.Close()
		time.Sleep(50*time.Millisecond + after)
		if answer != "" {
			_, _ = pipe.WriteString(answer + "\n")
		}
	}()
	return ended, nil
}

// A launch starts the Flatpak's service through Flatpak's Spawn when none
// runs, and hears from it that it's ready, or why it stopped; one that
// ended saying nothing is started again; one that finds an older build's
// service has it quit first, and one that finds its own build's or a
// newer one starts nothing.
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
		{"", "didn't start"},
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
	// A service that says nothing, like the last, never ran, or stopped
	// with no word: its sandbox ended before Flatpak had set it up, which
	// the portal may still have called started. It's started again,
	// spawnTries times in all.
	f.mu.Lock()
	silent := len(f.argv) - 2
	f.mu.Unlock()
	if silent != spawnTries {
		t.Fatalf("a service that said nothing was started %d times", silent)
	}
	for _, c := range []struct {
		unstarted, spawns int
		want              string
	}{{2, 3, ""}, {spawnTries, spawnTries, "didn't start"}} {
		f.mu.Lock()
		f.argv, f.answer, f.unstarted = nil, readyWord, c.unstarted
		f.mu.Unlock()
		err := ensureFlatpakService(ctx, bi, client, l)
		f.mu.Lock()
		spawns := len(f.argv)
		f.unstarted = 0
		f.mu.Unlock()
		if spawns != c.spawns || c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("with %d sandboxes unstarted: %d spawns, %v", c.unstarted, spawns, err)
		}
	}

	// A start that finds its instance held and nothing answering, as a
	// service from before v1.15.0 that has stopped answering, goes on
	// to wait.
	tellRunning(client)

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
	version, quits, waits := "v1.1.0", 0, 0
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
		control.OpTakeOver: func(context.Context, control.Request) (control.Reply, error) {
			mu.Lock()
			defer mu.Unlock()
			waits++
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
	// A start that finds its instance held tells the service holding it.
	tellRunning(client)
	mu.Lock()
	defer mu.Unlock()
	if waits != 1 {
		t.Fatalf("the running service was told %d times that a start waits", waits)
	}
}

// Two launches at once start one service between them: the second waits
// for the first, then finds the service it started.
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
	spawnLatest = func(_ context.Context, argv []string, files ...*os.File) (<-chan struct{}, error) {
		mu.Lock()
		spawned++
		mu.Unlock()
		ln, err := host.ListenControl(l.ControlEndpoint, me)
		if err != nil {
			return nil, err
		}
		server := &control.Server{Allowed: me, Log: logs, Handlers: map[string]control.Handler{
			control.OpStatus: func(context.Context, control.Request) (control.Reply, error) {
				return control.Reply{Result: control.Status{Version: "v1.1.0"}}, nil
			},
		}}
		go func() { _ = server.Serve(serving, ln) }()
		_, err = files[0].WriteString(readyWord + "\n")
		return make(chan struct{}), err
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
