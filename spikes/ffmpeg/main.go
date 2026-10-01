// Command ffspike runs the ffmpeg spike's driver, translated from WebAssembly
// to Go, on media files. Each job runs in a child copy of the command, as
// Dens would run a hidden command, with a cap on the module's memory and a
// deadline; the parent reports the result, the time and the peak memory.
//
//	ffspike [-mem MB] [-timeout D] probe IN
//	ffspike [-mem MB] [-timeout D] strip IN OUT [MUXER]
//	ffspike check EXIFTOOL ORIGINAL STRIPPED
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/spikes/ffmpeg/ffwasm"
)

const avLogWarning = 24

func main() {
	memMB := flag.Int64("mem", 1024, "cap on the module's memory, in MB")
	timeout := flag.Duration("timeout", 10*time.Minute, "deadline for the job")
	flag.Parse()
	args := flag.Args()
	if len(args) > 0 && args[0] == "work" {
		os.Exit(work(*memMB, args[1:]))
	}
	if len(args) == 4 && args[0] == "check" {
		os.Exit(check(args[1], args[2], args[3]))
	}
	if len(args) < 2 || (args[0] != "probe" && args[0] != "strip") || (args[0] == "strip" && len(args) < 3) {
		fmt.Fprintln(os.Stderr, "usage: ffspike [-mem MB] [-timeout D] probe IN | strip IN OUT [MUXER]")
		os.Exit(2)
	}
	os.Exit(run(*memMB, *timeout, args))
}

// report is what the parent learns about a job.
type report struct {
	Op        string          `json:"op"`
	File      string          `json:"file"`
	OK        bool            `json:"ok"`
	Error     string          `json:"error,omitempty"`
	Ms        int64           `json:"ms"`
	CPUMs     int64           `json:"cpu_ms"`
	PeakRSSMB float64         `json:"peak_rss_mb"`
	ModuleMB  float64         `json:"module_mb"`
	Result    json.RawMessage `json:"result,omitempty"`
}

func run(memMB int64, timeout time.Duration, args []string) int {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, self, append([]string{"-mem", fmt.Sprint(memMB), "work"}, args...)...)
	var stdout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, os.Stderr
	start := time.Now()
	err = cmd.Run()
	rep := report{Op: args[0], File: filepath.Base(args[1]), Ms: time.Since(start).Milliseconds()}
	if ps := cmd.ProcessState; ps != nil {
		rep.PeakRSSMB = peakRSSMB(ps)
		rep.CPUMs = (ps.UserTime() + ps.SystemTime()).Milliseconds()
	}
	// The child's last line on stdout is its own account of the job.
	var child struct {
		Error    string          `json:"error"`
		ModuleMB float64         `json:"module_mb"`
		PeakMB   float64         `json:"peak_mb"`
		Result   json.RawMessage `json:"result"`
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if jerr := json.Unmarshal([]byte(lines[len(lines)-1]), &child); jerr == nil {
		rep.Error, rep.ModuleMB, rep.Result = child.Error, child.ModuleMB, child.Result
		if rep.PeakRSSMB == 0 {
			rep.PeakRSSMB = child.PeakMB
		}
	}
	switch {
	case ctx.Err() != nil:
		rep.Error = "deadline passed: " + timeout.String()
	case err != nil && rep.Error == "":
		rep.Error = err.Error()
	}
	rep.OK = err == nil && rep.Error == ""
	out, _ := json.Marshal(rep)
	fmt.Println(string(out))
	if !rep.OK {
		return 1
	}
	return 0
}

// work runs one job in this process, and prints its account as JSON.
func work(memMB int64, args []string) int {
	// A decoder taken over by a hostile file recursing without end hits this,
	// not Go's 1 GB default.
	debug.SetMaxStack(64 << 20)
	h := &host{mem: newMemory(memMB << 20), log: os.Stderr, start: time.Now()}
	var res struct {
		Error    string          `json:"error,omitempty"`
		ModuleMB float64         `json:"module_mb"`
		PeakMB   float64         `json:"peak_mb,omitempty"`
		Result   json.RawMessage `json:"result,omitempty"`
	}
	err := job(h, args)
	res.ModuleMB = float64(h.mem.peak<<16) / (1 << 20)
	res.PeakMB = selfPeakMB()
	res.Result = h.result
	if err != nil {
		res.Error = err.Error()
	}
	out, _ := json.Marshal(res)
	fmt.Println(string(out))
	if err != nil {
		return 1
	}
	return 0
}

func job(h *host, args []string) (err error) {
	// A trap in the module is a Go panic; it ends the job, never quietly.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("trap: %v", r)
		}
	}()
	in, err := os.Open(args[1])
	if err != nil {
		return err
	}
	defer in.Close()
	h.files[0] = in
	if h.mem.Grow(32, h.mem.max) < 0 {
		return errors.New("the memory cap is below the module's minimum")
	}
	m := ffwasm.New(h, h, h)
	m.X_initialize()
	m.Xdm_init(avLogWarning)

	var ret int32
	switch args[0] {
	case "probe":
		ret = m.Xdm_probe()
	case "strip":
		muxer := ""
		if len(args) > 3 {
			muxer = args[3]
		} else if muxer, err = muxerFor(args[2]); err != nil {
			return err
		}
		out, err := os.OpenFile(args[2], os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		defer out.Close()
		h.files[1] = out
		ret = m.Xdm_strip(cstring(m, h, muxer))
		if ret >= 0 {
			if err := out.Close(); err != nil {
				return err
			}
		}
	}
	if ret < 0 {
		buf := m.Xmalloc(256)
		m.Xdm_error(ret, buf, 256)
		msg, _, _ := bytes.Cut(h.bytes(buf, 256), []byte{0})
		return fmt.Errorf("ffmpeg: %s", msg)
	}
	return nil
}

func cstring(m *ffwasm.Module, h *host, s string) int32 {
	p := m.Xmalloc(int32(len(s) + 1))
	copy(h.bytes(p, int32(len(s))+1), s+"\x00")
	return p
}

// muxerFor names the format a stripped copy keeps, from the output's name.
func muxerFor(name string) (string, error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mov":
		return "mov", nil
	case ".mp4", ".m4v":
		return "mp4", nil
	case ".m4a":
		return "ipod", nil
	case ".mkv":
		return "matroska", nil
	case ".webm":
		return "webm", nil
	case ".mp3":
		return "mp3", nil
	case ".flac":
		return "flac", nil
	case ".ogg", ".oga":
		return "ogg", nil
	case ".opus":
		return "opus", nil
	case ".wav":
		return "wav", nil
	}
	return "", fmt.Errorf("no format for %s", name)
}
