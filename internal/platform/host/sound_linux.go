//go:build !android

package host

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
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
	c, err := pulse.NewClient(pulse.ClientApplicationName("Dens"), pulse.ClientApplicationIconName("chat.dens.Dens"))
	if err != nil {
		return errNoServer
	}
	// Closing the connection ends the recording. The stream's own Close,
	// and Closed, read what the client's reader writes as the server goes,
	// unguarded; the requests each second find a server gone instead.
	defer c.Close()
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	key := hex.EncodeToString(id)
	writer := pulse.Int16Writer(func(pcm []int16) (int, error) {
		take(pcm, soundRate)
		return len(pcm), nil
	})
	stream, err := c.NewRecord(writer, pulse.RecordStereo, pulse.RecordSampleRate(soundRate), pulse.RecordMediaName("Relay"),
		pulse.RecordRawOption(func(r *proto.CreateRecordStream) {
			r.SourceName = defaultMonitor
			r.SourceIndex = proto.Undefined
			r.BufferFragSize = soundPiece
			r.AdjustLatency = true
			r.Properties[streamKey] = proto.PropListString(key)
		}))
	if err != nil {
		return errors.New("the default output's monitor can't be recorded")
	}
	if stream.SampleRate() != soundRate || stream.Channels() != 2 {
		return errors.New("the server records its monitor only in another format")
	}
	stream.Start()
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

// monitors names the source the capture's stream, by its key, records,
// and the default output's monitor.
func monitors(c *pulse.Client, key string) (on, def string, err error) {
	var server proto.GetServerInfoReply
	if err := c.RawRequest(&proto.GetServerInfo{}, &server); err != nil {
		return "", "", err
	}
	var sink proto.GetSinkInfoReply
	if err := c.RawRequest(&proto.GetSinkInfo{SinkIndex: proto.Undefined, SinkName: server.DefaultSinkName}, &sink); err != nil {
		// No default output, as with every device gone: nothing to follow.
		return "", "", nil
	}
	var outputs proto.GetSourceOutputInfoListReply
	if err := c.RawRequest(&proto.GetSourceOutputInfoList{}, &outputs); err != nil {
		return "", "", err
	}
	for _, o := range outputs {
		if v, ok := o.Properties[streamKey]; !ok || v.String() != key {
			continue
		}
		var src proto.GetSourceInfoReply
		if err := c.RawRequest(&proto.GetSourceInfo{SourceIndex: o.SourceIndex}, &src); err != nil {
			return "", "", err
		}
		return src.SourceName, sink.MonitorSourceName, nil
	}
	return "", sink.MonitorSourceName, nil
}
