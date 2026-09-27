package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"
)

// journal records how to undo each step of a transaction, newest first.
type journal struct {
	undo      []func() error
	committed bool
}

func (j *journal) add(f func() error) {
	if f != nil {
		j.undo = append(j.undo, f)
	}
}

// commit marks the point of no return: once the services start, a failure
// keeps the transitional state for the next installer run to recover.
func (j *journal) commit() { j.committed = true }

// rollback undoes every recorded step in reverse unless committed.
func (j *journal) rollback(out io.Writer) error {
	if j.committed || len(j.undo) == 0 {
		return nil
	}
	fmt.Fprintln(out, "Rolling back ...")
	var joined error
	for _, f := range slices.Backward(j.undo) {
		joined = errors.Join(joined, f())
	}
	if joined != nil {
		fmt.Fprintf(out, "Rollback was incomplete: %v\n", joined)
	} else {
		fmt.Fprintln(out, "Rolled back.")
	}
	return joined
}

// progress writes to the terminal and appends to the maintenance log.
type progress struct {
	out io.Writer
	log *os.File
}

func newProgress(out io.Writer) *progress { return &progress{out: out} }

func (p *progress) openLog(path string) {
	if p.log != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		p.log = f
	}
}

func (p *progress) printf(format string, args ...any) {
	fmt.Fprintf(p.out, format+"\n", args...)
	if p.log != nil {
		fmt.Fprintf(p.log, "%s "+format+"\n", append([]any{time.Now().UTC().Format(time.RFC3339)}, args...)...)
	}
}

func (p *progress) close() {
	if p.log != nil {
		_ = p.log.Close()
		p.log = nil
	}
}
