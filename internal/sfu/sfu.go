// Package sfu is the den's selective forwarding unit: one Pion peer
// connection per member in a call, on the den's two media ports, which
// forwards each member's audio to the others in their call without
// decoding it. The den (internal/den) decides who may be in which call;
// this package carries the media and makes the offers.
package sfu

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
	"golang.org/x/net/netutil"
)

// Defaults from protocol.md.
const (
	answerTimeout  = 15 * time.Second
	connectTimeout = 30 * time.Second
	// A TCP connection must name a call in its first message within this
	// long. The port holds at most maxTCPConns at once, so a flood of idle
	// connections costs bounded memory.
	firstMessageTimeout = 5 * time.Second
	maxTCPConns         = 128
	// A slow TCP reader drops packets past this much queued, rather than
	// holding up everyone else's audio.
	tcpWriteBuffer = 256 << 10
)

// ErrClosed is returned once calls have stopped.
var ErrClosed = errors.New("calls have stopped")

// opus is the one codec calls carry.
var opus = webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2,
	SDPFmtpLine: "minptime=10;useinbandfec=1"}

// Config holds what development instances and tests change.
type Config struct {
	// Log takes Pion's own messages. They name addresses, so only
	// development instances set it; otherwise they're dropped.
	Log func(string)
	// Loopback has the den answer checks that reach it on loopback, which
	// browsers don't send but tests do.
	Loopback bool
	// AnswerTimeout and ConnectTimeout replace the protocol's, for tests.
	AnswerTimeout, ConnectTimeout time.Duration
}

// SFU runs a den's calls.
type SFU struct {
	api     *webrtc.API
	udp     *ice.UDPMuxDefault
	tcp     *ice.TCPMuxDefault
	answer  time.Duration
	connect time.Duration

	// mu guards the rooms, and is taken before any peer's lock.
	mu     sync.Mutex
	closed bool
	rooms  map[string]map[string]*Peer // room, then member
}

// New runs calls on the den's media ports, which it closes when it stops.
func New(udp *net.UDPConn, tcp net.Listener, cfg Config) (*SFU, error) {
	logs := pionLogs{sink: cfg.Log}
	media := &webrtc.MediaEngine{}
	if err := media.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: opus, PayloadType: 111}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptorsWithOptions(media, registry, webrtc.WithInterceptorLoggerFactory(logs)); err != nil {
		return nil, err
	}
	s := &SFU{
		udp: ice.NewUDPMuxDefault(ice.UDPMuxParams{UDPConn: steadyConn{udp}, Logger: logs.NewLogger("udpmux")}),
		tcp: ice.NewTCPMuxDefault(ice.TCPMuxParams{
			Listener: netutil.LimitListener(tcp, maxTCPConns), Logger: logs.NewLogger("tcpmux"),
			ReadBufferSize: 8, WriteBufferSize: tcpWriteBuffer,
			FirstStunBindTimeout: firstMessageTimeout, AliveDurationForConnFromStun: firstMessageTimeout,
		}),
		answer:  cfg.AnswerTimeout,
		connect: cfg.ConnectTimeout,
		rooms:   map[string]map[string]*Peer{},
	}
	if s.answer == 0 {
		s.answer = answerTimeout
	}
	if s.connect == 0 {
		s.connect = connectTimeout
	}
	var settings webrtc.SettingEngine
	settings.LoggerFactory = logs
	// ICE-lite: the den only answers the browser's checks, from wherever
	// they come, so it needs no candidates of its own.
	settings.SetLite(true)
	settings.SetICEUDPMux(s.udp)
	settings.SetICETCPMux(s.tcp)
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6,
		webrtc.NetworkTypeTCP4, webrtc.NetworkTypeTCP6})
	// The den neither looks up members' .local names nor sends queries on
	// its own network.
	settings.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	settings.SetIncludeLoopbackCandidate(cfg.Loopback)
	s.api = webrtc.NewAPI(webrtc.WithMediaEngine(media), webrtc.WithInterceptorRegistry(registry), webrtc.WithSettingEngine(settings))
	return s, nil
}

// Close ends every call and closes the media ports.
func (s *SFU) Close() error {
	s.mu.Lock()
	s.closed = true
	var peers []*Peer
	for _, room := range s.rooms {
		for _, p := range room {
			peers = append(peers, p)
		}
	}
	s.mu.Unlock()
	for _, p := range peers {
		p.Close()
	}
	return errors.Join(s.udp.Close(), s.tcp.Close())
}

// steadyConn keeps the UDP mux reading past a failed read. Pion's mux
// closes itself on any read error but a timeout, which would end every
// call at once. Go already keeps Windows from reporting an earlier send's
// ICMP errors as read errors, so this guards against the rest: only the
// socket closing, or reads failing for about a second straight, stops it.
type steadyConn struct{ *net.UDPConn }

const maxReadFailures = 100

func (c steadyConn) ReadFromAddrPort(b []byte) (n int, addr netip.AddrPort, err error) {
	for failures := 0; ; failures++ {
		n, addr, err = c.UDPConn.ReadFromUDPAddrPort(b)
		if err == nil || errors.Is(err, net.ErrClosed) || os.IsTimeout(err) || failures >= maxReadFailures {
			return n, addr, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (c steadyConn) WriteToAddrPort(b []byte, addr netip.AddrPort) (int, error) {
	return c.UDPConn.WriteToUDPAddrPort(b, addr)
}

func (c steadyConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := c.ReadFromAddrPort(b)
	if err != nil {
		return n, nil, err
	}
	return n, net.UDPAddrFromAddrPort(addr), nil
}

// pionLogs passes Pion's messages from info up to a sink, or drops them.
type pionLogs struct{ sink func(string) }

func (l pionLogs) NewLogger(scope string) logging.LeveledLogger {
	return pionLogger{scope: scope, sink: l.sink}
}

type pionLogger struct {
	scope string
	sink  func(string)
}

func (l pionLogger) log(level, msg string) {
	if l.sink != nil {
		l.sink("pion " + l.scope + " " + level + ": " + msg)
	}
}

func (pionLogger) Trace(string)          {}
func (pionLogger) Tracef(string, ...any) {}
func (pionLogger) Debug(string)          {}
func (pionLogger) Debugf(string, ...any) {}
func (l pionLogger) Info(msg string)     { l.log("info", msg) }
func (l pionLogger) Infof(format string, args ...any) {
	l.log("info", fmt.Sprintf(format, args...))
}
func (l pionLogger) Warn(msg string) { l.log("warning", msg) }
func (l pionLogger) Warnf(format string, args ...any) {
	l.log("warning", fmt.Sprintf(format, args...))
}
func (l pionLogger) Error(msg string) { l.log("error", msg) }
func (l pionLogger) Errorf(format string, args ...any) {
	l.log("error", fmt.Sprintf(format, args...))
}
