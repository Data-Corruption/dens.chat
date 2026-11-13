//go:build linux && !android

package main

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

// Linux: the monitor of the default output, over PulseAudio's protocol,
// which PipeWire speaks too. The stream asks for the default's monitor by
// its special name and is left free to move, and each second the probe
// logs the default output, its mute and volume, and the source the stream
// sits on, so the log shows whether the server moved it as the default
// changed.

const pulseMonitor = "@DEFAULT_MONITOR@"

func capture(ctx context.Context, o captureOptions, emit func(Chunk)) {
	sink := newCapSink(emit)
	go sink.run(ctx)
	source := o.source
	if source == "" {
		source = pulseMonitor
	}
	if o.mic {
		source = "@DEFAULT_SOURCE@"
	}
	logf("cap", "pulse server=%q runtime=%q source=%s", os.Getenv("PULSE_SERVER"), os.Getenv("XDG_RUNTIME_DIR"), source)
	for ctx.Err() == nil {
		began := now()
		err := captureOnce(ctx, source, sink)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			// The probe closed it to follow the default: open it at once.
			continue
		}
		sink.event("stream ended after=%s error=%q", since(began), fmt.Sprint(err))
		sink.describe("no stream")
		sleep(ctx, time.Second)
	}
}

func captureOnce(ctx context.Context, source string, sink *capSink) error {
	c, err := pulse.NewClient(pulse.ClientApplicationName("Relay probe"))
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer c.Close()
	var server proto.GetServerInfoReply
	if err := c.RawRequest(&proto.GetServerInfo{}, &server); err != nil {
		return fmt.Errorf("server info: %w", err)
	}
	sink.event("server=%q version=%q default-sink=%s default-spec=%d/%dch", server.PackageName, server.PackageVersion,
		server.DefaultSinkName, server.DefaultSampleSpec.Rate, server.DefaultSampleSpec.Channels)

	const rate = 48000
	// The stream's name tells it from another probe's, when the probe asks
	// the server where it sits.
	media := fmt.Sprintf("Relay probe capture %d", os.Getpid())
	var got atomic.Int64
	writer := pulse.Int16Writer(func(buf []int16) (int, error) {
		frames := make([]int16, len(buf))
		copy(frames, buf)
		got.Add(int64(len(buf)))
		sink.data(frames, rate)
		return len(buf), nil
	})
	stream, err := c.NewRecord(writer, pulse.RecordStereo, pulse.RecordSampleRate(rate),
		pulse.RecordMediaName(media),
		pulse.RecordRawOption(func(r *proto.CreateRecordStream) {
			r.SourceName = source
			r.SourceIndex = proto.Undefined
			// 5 ms at a time, and the server's own latency kept down to match.
			r.BufferFragSize = rate / 200 * 4
			r.AdjustLatency = true
		}))
	if err != nil {
		return fmt.Errorf("record %s: %w", source, err)
	}
	defer stream.Close()
	stream.Start()
	if stream.SampleRate() != rate || stream.Channels() != 2 {
		return fmt.Errorf("the server gave %d Hz, %d channels", stream.SampleRate(), stream.Channels())
	}

	var lastSource, lastDefault string
	astray := 0
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		if stream.Closed() {
			return fmt.Errorf("the stream closed: %v", stream.Error())
		}
		if err := c.RawRequest(&proto.GetServerInfo{}, &server); err != nil {
			return fmt.Errorf("server info: %w", err)
		}
		var out proto.GetSinkInfoReply
		outErr := c.RawRequest(&proto.GetSinkInfo{SinkIndex: proto.Undefined, SinkName: server.DefaultSinkName}, &out)
		var outputs proto.GetSourceOutputInfoListReply
		srcName := "?"
		if err := c.RawRequest(&proto.GetSourceOutputInfoList{}, &outputs); err != nil {
			srcName = "unknown: " + err.Error()
		}
		for _, on := range outputs {
			var src proto.GetSourceInfoReply
			if on.MediaName == media && c.RawRequest(&proto.GetSourceInfo{SourceIndex: on.SourceIndex}, &src) == nil {
				srcName = src.SourceName
			}
		}
		if server.DefaultSinkName != lastDefault {
			if lastDefault != "" {
				sink.event("default output changed from=%s to=%s", lastDefault, server.DefaultSinkName)
			}
			lastDefault = server.DefaultSinkName
		}
		if srcName != lastSource {
			if lastSource != "" {
				sink.event("the stream moved from=%s to=%s", lastSource, srcName)
			}
			lastSource = srcName
		}
		if outErr != nil {
			sink.describe("out=%s (%v) on=%s", server.DefaultSinkName, outErr, srcName)
			continue
		}
		vol := 0.0
		for _, v := range out.ChannelVolumes {
			vol += float64(v) / 0x10000
		}
		if len(out.ChannelVolumes) > 0 {
			vol /= float64(len(out.ChannelVolumes))
		}
		follows := out.MonitorSourceName == srcName
		sink.describe("out=%s mute=%t vol=%.2f state=%d on=%s follows=%t", server.DefaultSinkName, out.Mute, vol, out.State, srcName, follows)
		// A server that leaves the stream on the old output's monitor gets
		// two seconds to move it; then the probe opens it again, on the
		// default's.
		if follows || source != pulseMonitor || srcName == "?" {
			astray = 0
		} else if astray++; astray >= 2 {
			sink.event("the stream stayed on %s as the default became %s: opening it again", srcName, server.DefaultSinkName)
			return nil
		}
	}
}
