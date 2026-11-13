//go:build linux && !android

package main

import (
	"context"
	"math"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

// exercise puts the capture through question 4's cases by itself, over the
// same protocol: it plays a quiet tone with a click every two seconds to
// the default output, then mutes that output, sets its volume to nothing,
// and makes a second, empty output the default, undoing each in turn. The
// capture's line each second shows what the monitor gave meanwhile.
func exercise(ctx context.Context) {
	c, err := pulse.NewClient(pulse.ClientApplicationName("Relay probe tone"))
	if err != nil {
		logf("exer", "no connection: %v", err)
		return
	}
	defer c.Close()
	const rate = 48000
	var frame int64
	tone := pulse.Int16Reader(func(buf []int16) (int, error) {
		for i := 0; i+1 < len(buf); i += 2 {
			v := 0.05 * math.Sin(2*math.Pi*440*float64(frame)/rate)
			if in := frame % (2 * rate); in < rate/200 {
				v = 0.8 * math.Sin(2*math.Pi*2000*float64(in)/rate)
			}
			buf[i] = int16(v * 32767)
			buf[i+1] = buf[i]
			frame++
		}
		return len(buf), nil
	})
	stream, err := c.NewPlayback(tone, pulse.PlaybackStereo, pulse.PlaybackSampleRate(rate), pulse.PlaybackLatency(0.04),
		pulse.PlaybackMediaName("Relay probe tone"))
	if err != nil {
		logf("exer", "no tone: %v", err)
		return
	}
	defer stream.Close()
	stream.Start()
	logf("exer", "playing a tone to the default output")

	var server proto.GetServerInfoReply
	var out proto.GetSinkInfoReply
	if c.RawRequest(&proto.GetServerInfo{}, &server) != nil ||
		c.RawRequest(&proto.GetSinkInfo{SinkIndex: proto.Undefined, SinkName: server.DefaultSinkName}, &out) != nil {
		logf("exer", "no default output to exercise")
		<-ctx.Done()
		return
	}
	first := server.DefaultSinkName
	volumes := out.ChannelVolumes
	silent := make(proto.ChannelVolumes, len(volumes))
	do := func(what string, req proto.RequestArgs, rpl proto.Reply) bool {
		err := c.RawRequest(req, rpl)
		if err != nil {
			logf("exer", "%s: refused: %v", what, err)
			return false
		}
		logf("exer", "%s", what)
		return true
	}
	// Whatever ends the probe, the output is left as it was found.
	var module *proto.LoadModuleReply
	restore := func() {
		_ = c.RawRequest(&proto.SetSinkMute{SinkIndex: proto.Undefined, SinkName: first, Mute: out.Mute}, nil)
		_ = c.RawRequest(&proto.SetSinkVolume{SinkIndex: proto.Undefined, SinkName: first, ChannelVolumes: volumes}, nil)
		_ = c.RawRequest(&proto.SetDefaultSink{SinkName: first}, nil)
		if module != nil {
			_ = c.RawRequest(&proto.UnloadModule{ModuleIndex: module.ModuleIndex}, nil)
		}
	}
	defer restore()
	wait := func(d time.Duration) bool {
		sleep(ctx, d)
		return ctx.Err() == nil
	}

	if !wait(20 * time.Second) {
		return
	}
	do("muted "+first, &proto.SetSinkMute{SinkIndex: proto.Undefined, SinkName: first, Mute: true}, nil)
	if !wait(15 * time.Second) {
		return
	}
	do("unmuted "+first, &proto.SetSinkMute{SinkIndex: proto.Undefined, SinkName: first, Mute: false}, nil)
	if !wait(15 * time.Second) {
		return
	}
	do("volume 0 on "+first, &proto.SetSinkVolume{SinkIndex: proto.Undefined, SinkName: first, ChannelVolumes: silent}, nil)
	if !wait(15 * time.Second) {
		return
	}
	do("volume back on "+first, &proto.SetSinkVolume{SinkIndex: proto.Undefined, SinkName: first, ChannelVolumes: volumes}, nil)
	if !wait(15 * time.Second) {
		return
	}
	var loaded proto.LoadModuleReply
	if do("made a second output, relay_probe_second", &proto.LoadModule{Name: "module-null-sink", Args: "sink_name=relay_probe_second"}, &loaded) {
		module = &loaded
		if !wait(2 * time.Second) {
			return
		}
		do("default output is relay_probe_second", &proto.SetDefaultSink{SinkName: "relay_probe_second"}, nil)
		if !wait(20 * time.Second) {
			return
		}
		do("default output is "+first+" again", &proto.SetDefaultSink{SinkName: first}, nil)
		if !wait(10 * time.Second) {
			return
		}
		do("removed the second output", &proto.UnloadModule{ModuleIndex: loaded.ModuleIndex}, nil)
		module = nil
	}
	logf("exer", "done: the tone plays on until the probe ends")
	<-ctx.Done()
}
