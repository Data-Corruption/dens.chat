package commands

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/build"
)

// An uninstall goes ahead only on its phrase typed out, more than the y a
// reflex gives (M10). Its question says what the instance holds where that
// could be read, points to a backup first and to stopping the service for
// someone who only wants Dens off; any other answer cancels, and input
// that ends says to use --yes in scripts.
func TestUninstallAsksForThePhrase(t *testing.T) {
	ask := askLine
	t.Cleanup(func() { askLine = ask })
	bi := build.BuildInfo{Name: "dens", Version: "v1.3.0"}
	var asked string
	for _, c := range []struct {
		answer string
		err    error
		want   string // "" goes ahead
	}{
		{answer: "perm delete data"},
		{answer: "y", want: "cancelled"},
		{answer: "perm delete", want: "cancelled"},
		{answer: "PERM DELETE DATA", want: "cancelled"},
		{err: io.EOF, want: "use --yes in scripts"},
	} {
		askLine = func(question string) (string, error) {
			asked = question
			return c.answer, c.err
		}
		said, err := printed(t, func() error { return confirmUninstall(bi, "second", "hosts the den Foo and has accounts in 3 dens") })
		switch {
		case c.want == "" && err != nil:
			t.Fatalf("%q: %v", c.answer, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Fatalf("%q: %v, want %q", c.answer, err, c.want)
		}
		for _, line := range []string{
			"This deletes instance second and all of its data, including its keys: it hosts the den Foo and has accounts in 3 dens.",
			"Make a backup first if you may want it back: dens backup --instance second",
			"dens service stop --instance second",
		} {
			if !strings.Contains(said, line) {
				t.Fatalf("the question said\n%s\nwithout %q", said, line)
			}
		}
		if asked != `Type "perm delete data" to uninstall instance second: ` {
			t.Fatalf("asked %q", asked)
		}
	}

	// Where what the instance holds couldn't be read, it says no more.
	askLine = func(string) (string, error) { return "perm delete data", nil }
	said, err := printed(t, func() error { return confirmUninstall(bi, "main", "") })
	if err != nil || !strings.Contains(said, "This deletes instance main and all of its data, including its keys.\n") ||
		!strings.Contains(said, ": dens backup\n") {
		t.Fatalf("said %q, %v", said, err)
	}
}

// printed runs f and returns what it printed on standard output.
func printed(t *testing.T, f func() error) (string, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(&out, reader)
		close(copied)
	}()
	was := os.Stdout
	os.Stdout = writer
	err = f()
	os.Stdout = was
	writer.Close()
	<-copied
	reader.Close()
	return out.String(), err
}
