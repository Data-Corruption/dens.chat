package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"golang.org/x/sys/unix"
)

const heartbeatLog = "heartbeat.log"

// clocks reads the two clocks a gap is told by: CLOCK_MONOTONIC stops
// while the phone sleeps, and CLOCK_BOOTTIME doesn't. A gap in both is the
// probe not running while the phone was awake: frozen, starved or dead. A
// gap in boot time alone is the phone asleep.
func clocks() (mono, boot float64) {
	var ts unix.Timespec
	_ = unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts)
	mono = float64(ts.Sec) + float64(ts.Nsec)/1e9
	_ = unix.ClockGettime(unix.CLOCK_BOOTTIME, &ts)
	boot = float64(ts.Sec) + float64(ts.Nsec)/1e9
	return mono, boot
}

// heartbeat writes a line a second until a signal ends it; SIGKILL, which
// is how Android ends an app's processes, leaves only the gap before the
// next START.
func heartbeat() error {
	f, err := os.OpenFile(heartbeatLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	write := func(kind, rest string) {
		mono, boot := clocks()
		fmt.Fprintf(f, "%s %s mono=%.3f boot=%.3f %s\n", kind, time.Now().Format("2006-01-02T15:04:05.000Z07:00"), mono, boot, rest)
	}
	write("START", fmt.Sprintf("pid=%d ppid=%d version=%s cgroup=%s", os.Getpid(), os.Getppid(), version,
		strings.ReplaceAll(readTrim("/proc/self/cgroup"), "\n", "|")))
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for seq := 1; ; seq++ {
		select {
		case s := <-sigs:
			write("END", "signal="+s.String())
			return nil
		case <-tick.C:
			write("HB", fmt.Sprintf("seq=%d oom=%s parent_oom=%s", seq, readTrim("/proc/self/oom_score_adj"),
				readTrim(fmt.Sprintf("/proc/%d/oom_score_adj", os.Getppid()))))
		}
	}
}

// gaps reads the heartbeat and lists every stretch of more than three
// seconds without a line, with the oom_score_adj values it saw.
func gaps(w io.Writer) error {
	f, err := os.Open(heartbeatLog)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(w, "no heartbeat yet")
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	var lastMono, lastBoot float64
	var lastTime string
	var beats, starts int
	oom := map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		kind, at := fields[0], fields[1]
		mono, _ := strconv.ParseFloat(strings.TrimPrefix(fields[2], "mono="), 64)
		boot, _ := strconv.ParseFloat(strings.TrimPrefix(fields[3], "boot="), 64)
		switch kind {
		case "START":
			starts++
			if lastTime != "" {
				fmt.Fprintf(w, "%s  started again, %s after the last line\n", at, span(boot-lastBoot))
			} else {
				fmt.Fprintf(w, "%s  started\n", at)
			}
		case "END":
			fmt.Fprintf(w, "%s  ended: %s\n", at, strings.Join(fields[4:], " "))
		case "HB":
			beats++
			for _, kv := range fields[5:] {
				oom[kv]++
			}
			if lastTime != "" && boot-lastBoot > 3 {
				awake := mono - lastMono
				fmt.Fprintf(w, "%s  gap of %s: %s awake, %s asleep\n", at, span(boot-lastBoot), span(awake), span(boot-lastBoot-awake))
			}
		}
		lastMono, lastBoot, lastTime = mono, boot, at
	}
	fmt.Fprintf(w, "%d beats, %d starts, last at %s\n", beats, starts, lastTime)
	for kv, n := range oom {
		fmt.Fprintf(w, "  %s: %d beats\n", kv, n)
	}
	return sc.Err()
}

func span(s float64) string {
	return (time.Duration(s * float64(time.Second))).Round(100 * time.Millisecond).String()
}

// burn starts N children through host.StartLowPriority, each spinning for
// SECONDS, as the media runner's workers would, and writes to burn.txt how
// each ended: Android caps an app's child processes (phantom processes) and
// may kill busy ones in the background.
func burn(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: aprobe burn N SECONDS")
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 || n > 64 {
		return errors.New("N is 1 to 64")
	}
	out, err := os.OpenFile("burn.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	var mu sync.Mutex
	logf := func(format string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(out, "%s %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, a...))
	}
	logf("burn %d children for %s s", n, args[1])
	exe, _ := os.Executable()
	var wg sync.WaitGroup
	for i := range n {
		cmd := exec.Command(exe, "spin", args[1])
		var stdout strings.Builder
		cmd.Stdout = &stdout
		start := time.Now()
		if err := host.StartLowPriority(cmd); err != nil {
			logf("child %d: start: %v", i, err)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := cmd.Wait()
			logf("child %d (pid %d): %s after %v: %s", i, cmd.Process.Pid, orOK(err), time.Since(start).Round(time.Millisecond), strings.TrimSpace(stdout.String()))
		}()
	}
	wg.Wait()
	logf("burn done")
	return nil
}

// spin keeps one thread busy for SECONDS and says how much of the CPU it
// got.
func spin(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: aprobe spin SECONDS")
	}
	secs, err := strconv.Atoi(args[0])
	if err != nil {
		return err
	}
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	x := uint64(1)
	for time.Now().Before(deadline) {
		for range 1 << 20 {
			x = x*6364136223846793005 + 1442695040888963407
		}
	}
	var ru unix.Rusage
	_ = unix.Getrusage(unix.RUSAGE_SELF, &ru)
	fmt.Printf("cpu %.1f s of %d s (%x)", float64(ru.Utime.Sec)+float64(ru.Utime.Usec)/1e6, secs, x&0xf)
	return nil
}
