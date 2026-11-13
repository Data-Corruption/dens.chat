// relayprobe is the Relay spike's probe (README.md): it links to another
// probe across the LAN, reads keys, captures the system's sound, sends and
// receives both, serves the page that plays the sound, and logs what it
// measured.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	started = time.Now()
	logMu   sync.Mutex
	logFile *os.File
	quiet   bool
)

// now is this probe's clock: the time since it started, which never jumps.
func now() time.Duration { return time.Since(started) }

func since(t time.Duration) string { return ms(now() - t) }

func ms(d time.Duration) string {
	return strconv.FormatFloat(float64(d)/float64(time.Millisecond), 'f', 1, 64) + "ms"
}

// logf writes one line: the wall clock, this probe's clock, what it's
// about, and what was measured.
func logf(kind, format string, args ...any) {
	line := fmt.Sprintf("%s %10.3f %-5s %s\n", time.Now().Format("15:04:05.000"), now().Seconds(), kind, fmt.Sprintf(format, args...))
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		_, _ = logFile.WriteString(line)
	}
	if !quiet {
		_, _ = os.Stdout.WriteString(line)
	}
}

type options struct {
	link     LinkConfig
	peer     string
	send     string
	keys     string
	page     string
	monitor  bool
	run      time.Duration
	logPath  string
	attempt  time.Duration
	buffer   int
	source   string
	keepWarm bool
	convert  bool
	service  string
	exercise bool
	nullOut  bool
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "clicks" {
		os.Exit(clicksCommand(os.Args[2:]))
	}
	var o options
	var ice string
	flag.StringVar(&o.peer, "peer", "", "the other probe's address, or address:port; without it nothing links")
	flag.StringVar(&o.link.Role, "role", "", "a or b: one probe of each")
	flag.IntVar(&o.link.Port, "port", 47800, "the UDP port this probe binds")
	flag.StringVar(&o.link.Secret, "secret", "relay", "what both probes make their certificates and ICE credentials from")
	flag.StringVar(&o.link.Local, "local", "", "gather on this address alone")
	flag.StringVar(&ice, "ice", "5s,25s,2s", "ICE's timeouts: disconnected, failed, keepalive")
	flag.DurationVar(&o.link.RTOMax, "rto", 100*time.Millisecond, "the longest SCTP waits before it sends again after a loss; 0 for Pion's own, which starts at a second")
	how := flag.String("sound", "sctp", "how the sound crosses: sctp, on a channel of its own, or rtp, beside the channels; the same on both probes")
	flag.IntVar(&packetFrames, "frames", 240, "the frames a packet of sound holds: 240 is 5 ms at 48 kHz")
	flag.DurationVar(&o.attempt, "attempt", time.Minute, "how long one attempt asks before the next")
	flag.StringVar(&o.send, "send", "off", "the sound to send: system, tone, or off")
	flag.StringVar(&o.keys, "keys", "off", "the keys to read and send: codes such as F8,KeyV, synth for made-up presses, or off")
	flag.StringVar(&o.page, "page", "127.0.0.1:47880", "where the page that plays what arrives is served; off for none")
	flag.BoolVar(&o.monitor, "monitor", false, "capture this system's sound too, to time the clicks that come out of it")
	flag.DurationVar(&o.run, "for", 0, "stop after this long")
	flag.StringVar(&o.logPath, "log", "", "the log's file; beside the probe unless given")
	flag.IntVar(&o.buffer, "buffer", 40, "the milliseconds of sound the page keeps before it plays")
	flag.StringVar(&o.source, "source", "", "Linux: the PulseAudio source to record, the default output's monitor unless given")
	flag.BoolVar(&o.keepWarm, "keep-warm", false, "Windows: play silence so the loopback never runs dry")
	flag.BoolVar(&o.convert, "convert", false, "Windows: ask WASAPI for 48 kHz, 16 bits, stereo in place of the mix format")
	flag.StringVar(&o.service, "service", "", "Windows: run as the service of this name")
	flag.BoolVar(&o.nullOut, "null-output", false, "Linux: make an output that plays to nowhere the default while the probe runs")
	flag.BoolVar(&o.exercise, "exercise", false, "put the capture through its cases: mute and volume 0, and on Linux a tone and a second default output")
	flag.BoolVar(&quiet, "quiet", false, "log to the file alone")
	profile := flag.String("profile", "", "write a CPU profile here")
	flag.Parse()

	switch *how {
	case "sctp":
	case "rtp":
		o.link.RTP = true
		if packetFrames > 288 {
			fatal("-sound rtp: a packet holds 288 frames at most, to fit one datagram")
		}
	default:
		fatal("-sound takes sctp or rtp")
	}
	parts := strings.Split(ice, ",")
	if len(parts) != 3 {
		fatal("-ice takes three durations")
	}
	var err error
	if o.link.Disconnected, err = time.ParseDuration(parts[0]); err != nil {
		fatal("-ice: %v", err)
	}
	if o.link.Failed, err = time.ParseDuration(parts[1]); err != nil {
		fatal("-ice: %v", err)
	}
	if o.link.KeepAlive, err = time.ParseDuration(parts[2]); err != nil {
		fatal("-ice: %v", err)
	}
	o.link.Remote = o.link.Port
	if o.peer != "" {
		host, port, err := net.SplitHostPort(o.peer)
		if err != nil {
			host = o.peer
		} else if o.link.Remote, err = strconv.Atoi(port); err != nil {
			fatal("-peer: %v", err)
		}
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			fatal("-peer: no address for %s", host)
		}
		o.link.Peer = ips[0].String()
		for _, ip := range ips {
			if ip.To4() != nil {
				o.link.Peer = ip.String()
				break
			}
		}
		if o.link.Role != "a" && o.link.Role != "b" {
			fatal("-role must be a or b")
		}
	}

	if *profile != "" {
		if f, err := os.Create(*profile); err == nil {
			_ = pprof.StartCPUProfile(f)
			defer pprof.StopCPUProfile()
		}
	}
	host, _ := os.Hostname()
	if o.logPath == "" {
		exe, _ := os.Executable()
		o.logPath = filepath.Join(filepath.Dir(exe), fmt.Sprintf("relayprobe-%s-%s.log", host, started.Format("0102-150405")))
	}
	if logFile, err = os.OpenFile(o.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "no log at %s: %v\n", o.logPath, err)
	}
	logf("probe", "host=%s os=%s arch=%s pid=%d args=%q log=%s", host, runtime.GOOS, runtime.GOARCH, os.Getpid(), os.Args[1:], o.logPath)
	logf("probe", "where=%s", where())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if o.run > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.run)
		defer cancel()
	}
	if o.service != "" {
		runService(ctx, o)
	} else {
		run(ctx, o)
	}
	logf("probe", "done after=%s cpu=%s", time.Since(started).Round(time.Second), cpuTime().Round(time.Millisecond))
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}

// run is the probe: the page, the capture, and the link with what rides
// it, until the context ends.
func run(ctx context.Context, o options) {
	var wg sync.WaitGroup
	go watchCPU(ctx)

	var player *Player
	if o.page != "" && o.page != "off" && o.page != `""` {
		player = newPlayer(o.buffer)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := player.serve(ctx, o.page); err != nil {
				logf("page", "error=%q", err)
			}
		}()
	}

	if o.nullOut {
		// Before the capture opens, so a monitor follows the null output.
		ready := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			go func() {
				sleep(ctx, 700*time.Millisecond)
				close(ready)
			}()
			nullOutput(ctx)
		}()
		<-ready
	}
	// A probe's capture is either its source, sent with the clicks in it
	// timed, or a check on its own output, where the other side's clicks
	// come out.
	sound := newSound(player)
	// With no peer, what this probe would send plays on its own page.
	sound.local = o.peer == ""
	source := sourceClicks(sound)
	send := func(c Chunk) {
		source.feed(c)
		sound.feed(c)
	}
	copts := captureOptions{source: o.source, keepWarm: o.keepWarm, convert: o.convert}
	switch {
	case o.send == "system":
		wg.Add(1)
		go func() {
			defer wg.Done()
			capture(ctx, copts, send)
		}()
	case o.monitor:
		wg.Add(1)
		go func() {
			defer wg.Done()
			capture(ctx, copts, outputClicks(sound).feed)
		}()
	}
	if o.exercise {
		wg.Add(1)
		go func() {
			defer wg.Done()
			exercise(ctx)
		}()
	}
	if o.send == "tone" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tone(ctx, send)
		}()
	}

	keys := newKeys()
	if o.keys != "off" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			keys.read(ctx, o.keys)
		}()
	}

	if o.peer != "" {
		keepLinked(ctx, o.link, o.attempt, func(lctx context.Context, l *Link) {
			session(lctx, l, o, sound, keys)
		})
	}
	<-ctx.Done()
	wg.Wait()
}

// watchCPU logs the processor time the probe took, every ten seconds, as
// a share of one core.
func watchCPU(ctx context.Context) {
	last, at := cpuTime(), time.Now()
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cpu, n := cpuTime(), time.Now()
			logf("cpu", "share=%.2f%% of a core", 100*float64(cpu-last)/float64(n.Sub(at)))
			last, at = cpu, n
		}
	}
}
