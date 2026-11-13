//go:build !android

package host

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jfreymuth/pulse/proto"
	"golang.org/x/sys/unix"
)

// Linux reads the default output's monitor over PulseAudio's protocol, at
// the server's socket in the session's runtime directory, or where
// PULSE_SERVER says, as the Flatpak's sandbox sets it.
const (
	soundRate = 48000
	// defaultMonitor names the default output's monitor to the server.
	defaultMonitor = "@DEFAULT_MONITOR@"
	// soundPiece is how much the server hands over at a time, which it
	// keeps its own latency to: 5 ms of 16-bit stereo.
	soundPiece = soundRate / 200 * 4
	// streamKey is the property that tells this capture's stream from
	// others when the capture asks the server where it sits.
	streamKey = "dens.capture"
)

// errNoServer is no PulseAudio or PipeWire server answering, as where
// neither runs, or the Flatpak lacks the socket's permission.
var errNoServer = errors.New("no PulseAudio or PipeWire server answers")

// CaptureSound reads the default output's monitor until ctx ends.
func CaptureSound(ctx context.Context, take func(pcm []int16, rate int), problem func(why string)) error {
	for ctx.Err() == nil {
		err := captureMonitor(ctx, take, problem)
		if ctx.Err() != nil {
			return nil
		}
		// nil is a stream opened again at once, to follow the default.
		if err != nil {
			problem(err.Error())
			soundPause(ctx, time.Second)
		}
	}
	return nil
}

// captureMonitor records the default output's monitor until ctx ends,
// the server stops answering, or the recording has sat for two seconds on
// another output's monitor, where PulseAudio leaves it as the default
// changes and PipeWire doesn't: then it returns nil, to open again.
func captureMonitor(ctx context.Context, take func([]int16, int), problem func(string)) error {
	var pcm []int16
	c, conn, err := pulseConnect(func(data []byte) {
		pcm = pcm[:0]
		for i := 0; i+2 <= len(data); i += 2 {
			pcm = append(pcm, int16(binary.LittleEndian.Uint16(data[i:])))
		}
		take(pcm, soundRate)
	})
	if err != nil {
		return errNoServer
	}
	// Closing the connection ends the recording, and the requests each
	// second find a server gone.
	defer conn.Close()
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	key := hex.EncodeToString(id)
	var stream proto.CreateRecordStreamReply
	err = c.Request(&proto.CreateRecordStream{
		SampleSpec:         proto.SampleSpec{Format: proto.FormatInt16LE, Channels: 2, Rate: soundRate},
		ChannelMap:         proto.ChannelMap{proto.ChannelLeft, proto.ChannelRight},
		ChannelVolumes:     proto.ChannelVolumes{0x100, 0x100},
		SourceIndex:        proto.Undefined,
		SourceName:         defaultMonitor,
		BufferMaxLength:    proto.Undefined,
		BufferFragSize:     soundPiece,
		AdjustLatency:      true,
		Corked:             true,
		DirectOnInputIndex: proto.Undefined,
		Properties:         proto.PropList{"media.name": proto.PropListString("Relay"), streamKey: proto.PropListString(key)},
	}, &stream)
	if err != nil {
		return errors.New("the default output's monitor can't be recorded")
	}
	if stream.Rate != soundRate || stream.Channels != 2 || stream.Format != proto.FormatInt16LE {
		return errors.New("the server records its monitor only in another format")
	}
	if c.Request(&proto.FlushRecordStream{StreamIndex: stream.StreamIndex}, nil) != nil ||
		c.Request(&proto.CorkRecordStream{StreamIndex: stream.StreamIndex, Corked: false}, nil) != nil {
		return errors.New("the server stopped answering")
	}
	problem("")

	astray := 0
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		on, def, err := monitors(c, key)
		switch {
		case err != nil:
			return errors.New("the server stopped answering")
		case on == "" || on == def:
			astray = 0
		default:
			if astray++; astray >= 2 {
				return nil
			}
		}
	}
}

// pulseConnect connects to the server and signs in, handing data each
// piece a recording gets. It speaks the protocol on a connection of its
// own making, since the server knows a client by its socket's
// credentials, and takes the cookie in the member's configuration only
// from one that sends none. The Flatpak's sandbox can't read the cookie,
// and the kernel adds the credentials by itself only to what's sent
// before the server takes the connection: the socket is told to send
// them always, without which a sandboxed capture is refused as often as
// not.
func pulseConnect(data func([]byte)) (*proto.Client, net.Conn, error) {
	var last error = errNoServer
	for _, at := range pulseServers() {
		conn, err := net.DialTimeout(at[0], at[1], pulseTimeout)
		if err != nil {
			last = err
			continue
		}
		if u, ok := conn.(*net.UnixConn); ok {
			if raw, err := u.SyscallConn(); err == nil {
				_ = raw.Control(func(fd uintptr) { _ = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_PASSCRED, 1) })
			}
		}
		c := &proto.Client{Callback: func(msg any) {
			if p, ok := msg.(*proto.DataPacket); ok {
				data(p.Data)
			}
		}}
		c.SetTimeout(pulseTimeout)
		c.Open(conn)
		// A server that knows the socket's credentials takes any cookie.
		cookie, err := os.ReadFile(pulseCookie())
		if err != nil {
			cookie = make([]byte, 256)
		}
		var auth proto.AuthReply
		if err = c.Request(&proto.Auth{Version: c.Version(), Cookie: cookie}, &auth); err == nil {
			c.SetVersion(auth.Version)
			err = c.Request(&proto.SetClientName{Props: proto.PropList{
				"application.name":      proto.PropListString("Dens"),
				"application.icon_name": proto.PropListString("chat.dens.Dens"),
			}}, &proto.SetClientNameReply{})
		}
		if err != nil {
			_ = conn.Close()
			last = err
			continue
		}
		return c, conn, nil
	}
	return nil, nil, last
}

// pulseTimeout is how long the server has to take a connection and to
// answer a request.
const pulseTimeout = time.Second

// pulseCookie is where the member's cookie is kept.
func pulseCookie() string {
	if path, ok := os.LookupEnv("PULSE_COOKIE"); ok {
		return path
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "pulse", "cookie")
}

// pulseServers are the servers to try, each its network and address:
// those PULSE_SERVER lists, in PulseAudio's server strings, or the socket
// in the session's runtime directory.
func pulseServers() [][2]string {
	list, ok := os.LookupEnv("PULSE_SERVER")
	if !ok {
		return [][2]string{{"unix", filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "pulse", "native")}}
	}
	host, _ := os.Hostname()
	var out [][2]string
	for _, s := range strings.Fields(list) {
		// {name} keeps a server to the computer of that name.
		if strings.HasPrefix(s, "{") {
			end := strings.IndexByte(s, '}')
			if end < 0 || s[1:end] != host {
				continue
			}
			s = s[end+1:]
		}
		network := "tcp"
		for _, n := range []string{"unix", "tcp4", "tcp6", "tcp"} {
			if rest, ok := strings.CutPrefix(s, n+":"); ok {
				network, s = n, rest
				break
			}
		}
		switch {
		case s == "":
			continue
		case strings.HasPrefix(s, "/"):
			network = "unix"
		case network != "unix":
			if _, _, err := net.SplitHostPort(s); err != nil {
				s = net.JoinHostPort(s, "4713")
			}
		}
		out = append(out, [2]string{network, s})
	}
	return out
}

// monitors names the source the capture's stream, by its key, records,
// and the default output's monitor.
func monitors(c *proto.Client, key string) (on, def string, err error) {
	var server proto.GetServerInfoReply
	if err := c.Request(&proto.GetServerInfo{}, &server); err != nil {
		return "", "", err
	}
	var sink proto.GetSinkInfoReply
	if err := c.Request(&proto.GetSinkInfo{SinkIndex: proto.Undefined, SinkName: server.DefaultSinkName}, &sink); err != nil {
		// No default output, as with every device gone: nothing to follow.
		return "", "", nil
	}
	var outputs proto.GetSourceOutputInfoListReply
	if err := c.Request(&proto.GetSourceOutputInfoList{}, &outputs); err != nil {
		return "", "", err
	}
	for _, o := range outputs {
		if v, ok := o.Properties[streamKey]; !ok || v.String() != key {
			continue
		}
		var src proto.GetSourceInfoReply
		if err := c.Request(&proto.GetSourceInfo{SourceIndex: o.SourceIndex}, &src); err != nil {
			return "", "", err
		}
		return src.SourceName, sink.MonitorSourceName, nil
	}
	return "", sink.MonitorSourceName, nil
}
