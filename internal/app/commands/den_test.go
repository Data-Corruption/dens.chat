package commands

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/layout"

	"github.com/urfave/cli/v3"
)

// fakeService answers the den commands' requests, as the service would,
// and keeps the requests that change something.
type fakeService struct {
	mu       sync.Mutex
	requests []control.Request
	exported time.Time
	asked    []string
	answer   bool
	// passwordSet is whether the status says a local password is set.
	passwordSet bool
	// noDen has the status hold no den yet, with hosting on.
	noDen bool
}

func (f *fakeService) keep(req control.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
}

func (f *fakeService) kept() []control.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]control.Request(nil), f.requests...)
}

// serveDen has the commands reach a fake service, and answers their
// questions as f.answer says.
func serveDen(t *testing.T) *fakeService {
	t.Helper()
	f := &fakeService{}
	client := serveHandlers(t, map[string]control.Handler{
		control.OpStatus: func(context.Context, control.Request) (control.Reply, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.noDen {
				return control.Reply{Result: control.Status{DenEnabled: true, PasswordSet: f.passwordSet}}, nil
			}
			return control.Reply{Result: control.Status{Den: "Test Den", DenExported: f.exported, PasswordSet: f.passwordSet}}, nil
		},
		control.OpPassword: func(_ context.Context, req control.Request) (control.Reply, error) {
			f.keep(req)
			f.mu.Lock()
			defer f.mu.Unlock()
			return control.Reply{Result: control.PasswordChanged{First: !f.passwordSet}}, nil
		},
		control.OpCreateDen: func(_ context.Context, req control.Request) (control.Reply, error) {
			f.keep(req)
			return control.Reply{Result: control.OwnerInvite{Invite: "dens1:test", Den: req.Name, URL: req.URL}}, nil
		},
		control.OpHosting: func(_ context.Context, req control.Request) (control.Reply, error) {
			f.keep(req)
			if req.On && req.Password == "held" {
				return control.Reply{}, control.Errorf("Hosting can't listen on port 8485 (TCP): another program holds it. Free it, or run the installer again with --den-port.")
			}
			return control.Reply{Result: control.Hosting{On: req.On, Serving: req.On, Den: "Test Den", URL: "https://den.test",
				DenPort: 8485, MediaUDPPort: 7881, MediaTCPPort: 7882}}, nil
		},
		control.OpRemoveDen: func(_ context.Context, req control.Request) (control.Reply, error) {
			f.keep(req)
			return control.Reply{Result: control.RemovedDen{Den: "Test Den", URL: "https://den.test"}}, nil
		},
		control.OpExport: func(_ context.Context, req control.Request) (control.Reply, error) {
			f.keep(req)
			return control.Reply{Result: control.Backup{Name: "dens-main-den-20261023-120000.backup", Size: 1 << 10},
				Body: io.NopCloser(strings.NewReader("the den's export"))}, nil
		},
	})
	dial, ask := controlClient, askYesNo
	controlClient = func(build.BuildInfo, string) (control.Client, layout.Layout, error) {
		return client, layout.Layout{Instance: layout.DefaultInstance, Form: layout.Development}, nil
	}
	askYesNo = func(question string) (bool, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.asked = append(f.asked, question)
		return f.answer, nil
	}
	t.Cleanup(func() { controlClient, askYesNo = dial, ask })
	return f
}

func (f *fakeService) questions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	asked := f.asked
	f.asked = nil
	return asked
}

// runDens runs dens with args, standard input reading stdin, and returns
// what it printed.
func runDens(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	in, inWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outReader, out, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = io.WriteString(inWriter, stdin)
		inWriter.Close()
	}()
	var printed bytes.Buffer
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(&printed, outReader)
		close(copied)
	}()
	stdinWas, stdoutWas := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, out
	bi := build.BuildInfo{Name: "dens", Version: "v1.3.0"}
	root := &cli.Command{Name: "dens", Flags: []cli.Flag{InstanceFlag}, Writer: io.Discard, ErrWriter: io.Discard,
		Commands: []*cli.Command{denCommand(bi), passwordCommand(bi)}}
	err = root.Run(context.Background(), append([]string{"dens"}, args...))
	os.Stdin, os.Stdout = stdinWas, stdoutWas
	out.Close()
	<-copied
	in.Close()
	outReader.Close()
	return printed.String(), err
}

// dens den hosting off asks first, as the page's switch does, and --yes
// answers for scripts; on takes the local password, says what it serves
// and where Caddy and the router send it, or names a held port, and, for
// a den exported since, asks first, though not on the input the password
// comes from (M10).
func TestHostingCommands(t *testing.T) {
	f := serveDen(t)
	if _, err := runDens(t, "", "den", "hosting", "off"); err == nil || len(f.questions()) != 1 || len(f.kept()) != 0 {
		t.Fatalf("turning hosting off, answering no: %v, %v", err, f.kept())
	}
	f.answer = true
	out, err := runDens(t, "", "den", "hosting", "off")
	if err != nil || len(f.kept()) != 1 || f.kept()[0].On || !strings.Contains(out, "Hosting is off.") {
		t.Fatalf("turning hosting off, answering yes: %v, %v, %q", err, f.kept(), out)
	}
	f.questions()
	if _, err := runDens(t, "", "den", "hosting", "off", "--yes"); err != nil || len(f.questions()) != 0 {
		t.Fatalf("turning hosting off with --yes: %v", err)
	}

	out, err = runDens(t, "local password\n", "den", "hosting", "on", "--password-stdin")
	if err != nil || len(f.questions()) != 0 {
		t.Fatalf("turning hosting on: %v", err)
	}
	if last := f.kept()[len(f.kept())-1]; !last.On || last.Password != "local password" {
		t.Fatalf("turning hosting on asked %+v", last)
	}
	for _, want := range []string{"Test Den serves at https://den.test", "127.0.0.1:8485", "port 7881 (UDP)", "port 7882 (TCP)"} {
		if !strings.Contains(out, want) {
			t.Errorf("turning hosting on said %q, without %q", out, want)
		}
	}
	if _, err := runDens(t, "held\n", "den", "hosting", "on", "--password-stdin"); err == nil ||
		!strings.Contains(err.Error(), "port 8485") || !strings.Contains(err.Error(), "--den-port") {
		t.Fatalf("turning hosting on with a port held: %v", err)
	}

	f.mu.Lock()
	f.exported = time.Now()
	f.mu.Unlock()
	sent := len(f.kept())
	if _, err := runDens(t, "local password\n", "den", "hosting", "on", "--password-stdin"); err == nil ||
		!strings.Contains(err.Error(), "--yes") || len(f.kept()) != sent {
		t.Fatalf("turning hosting on after an export, with the password on standard input: %v", err)
	}
	if _, err := runDens(t, "local password\n", "den", "hosting", "on", "--password-stdin", "--yes"); err != nil || len(f.kept()) != sent+1 {
		t.Fatalf("turning hosting on after an export, with --yes: %v", err)
	}
	f.answer = false
	if _, err := runDens(t, "", "den", "hosting", "on"); err == nil || len(f.kept()) != sent+1 {
		t.Fatalf("turning hosting on after an export, answering no: %v", err)
	}
	if asked := f.questions(); len(asked) != 1 || !strings.Contains(asked[0], "anyway") {
		t.Fatalf("turning hosting on after an export asked %q", asked)
	}
}

// dens den export writes the den's export where it's told, and says how to
// finish the move; dens den remove asks first, takes the local password,
// and says where Dens reaches the den now (M10).
func TestExportAndRemoveCommands(t *testing.T) {
	f := serveDen(t)
	path := filepath.Join(t.TempDir(), "den.backup")
	out, err := runDens(t, "local password\n", "den", "export", "--password-stdin", "--output", path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "the den's export" {
		t.Fatalf("the export wrote %q, %v", got, err)
	}
	for _, want := range []string{path, "dens restore", "dens den hosting on", "dens den remove"} {
		if !strings.Contains(out, want) {
			t.Errorf("the export said %q, without %q", out, want)
		}
	}
	if req := f.kept()[0]; req.Op != control.OpExport || req.Password != "local password" {
		t.Fatalf("the export asked %+v", req)
	}

	if _, err := runDens(t, "local password\n", "den", "remove", "--password-stdin"); err == nil || !strings.Contains(err.Error(), "--yes") || len(f.kept()) != 1 {
		t.Fatalf("removing with the password on standard input, without --yes: %v", err)
	}
	if _, err := runDens(t, "", "den", "remove"); !errors.Is(err, errCancelled) || len(f.kept()) != 1 {
		t.Fatalf("removing, answering no: %v", err)
	}
	out, err = runDens(t, "local password\n", "den", "remove", "--password-stdin", "--yes")
	if err != nil || !strings.Contains(out, "Removed Test Den from this computer") || !strings.Contains(out, "https://den.test") {
		t.Fatalf("removing: %v, %q", err, out)
	}
	if req := f.kept()[1]; req.Op != control.OpRemoveDen || req.Password != "local password" {
		t.Fatalf("removing asked %+v", req)
	}
}

// dens password sets the local password where none is set, which standard
// input gives once where the terminal asks twice, and changes it given the
// current one, which comes first (M10).
func TestPasswordCommand(t *testing.T) {
	f := serveDen(t)
	out, err := runDens(t, "a new password\n", "password", "--password-stdin")
	if err != nil || !strings.Contains(out, "The local password is set.") {
		t.Fatalf("setting the password: %v, %q", err, out)
	}
	if req := f.kept()[0]; req.Op != control.OpPassword || req.Password != "" || req.NewPassword != "a new password" {
		t.Fatalf("setting the password asked %+v", req)
	}
	f.mu.Lock()
	f.passwordSet = true
	f.mu.Unlock()
	out, err = runDens(t, "the current one\r\nthe next one\r\n", "password", "--password-stdin")
	if err != nil || !strings.Contains(out, "The local password is changed. A backup made before still opens with the one it was made under.") {
		t.Fatalf("changing the password: %v, %q", err, out)
	}
	if req := f.kept()[1]; req.Password != "the current one" || req.NewPassword != "the next one" {
		t.Fatalf("changing the password asked %+v", req)
	}
}

// dens den create creates the den with its name and address, after the
// local password, and prints its owner invite, and how to make another
// (M10). Where the instance hosts a den already, or hosting is off, it
// says so before asking for the password.
func TestCreateCommand(t *testing.T) {
	f := serveDen(t)
	if _, err := runDens(t, "", "den", "create", "Our Den"); err == nil || !strings.Contains(err.Error(), "usage: den create NAME ADDRESS") {
		t.Fatalf("creating with no address: %v", err)
	}
	if _, err := runDens(t, "local password\n", "den", "create", "--password-stdin", "Our Den", "https://den.example.com"); err == nil ||
		!strings.Contains(err.Error(), "this instance hosts Test Den already") || len(f.kept()) != 0 {
		t.Fatalf("creating a second den: %v", err)
	}
	f.mu.Lock()
	f.noDen = true
	f.mu.Unlock()
	out, err := runDens(t, "local password\n", "den", "create", "--password-stdin", "Our Den", "https://den.example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Created Our Den, which members reach at https://den.example.com", "dens1:test", "Add a den", "dens den owner-invite"} {
		if !strings.Contains(out, want) {
			t.Errorf("creating the den said %q, without %q", out, want)
		}
	}
	if req := f.kept()[0]; req.Op != control.OpCreateDen || req.Name != "Our Den" || req.URL != "https://den.example.com" || req.Password != "local password" {
		t.Fatalf("creating the den asked %+v", req)
	}
}
