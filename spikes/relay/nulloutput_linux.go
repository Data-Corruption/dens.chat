//go:build linux && !android

package main

import (
	"context"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

// nullOutput makes an output that plays to nowhere the default until the
// context ends, and then puts the default back. A VMware guest's real
// output is its host's speakers: a relay of the host's sound played there
// would be captured by the host again, round and round.
func nullOutput(ctx context.Context) {
	c, err := pulse.NewClient(pulse.ClientApplicationName("Relay probe output"))
	if err != nil {
		logf("out", "no connection: %v", err)
		return
	}
	defer c.Close()
	var server proto.GetServerInfoReply
	if err := c.RawRequest(&proto.GetServerInfo{}, &server); err != nil {
		logf("out", "no server info: %v", err)
		return
	}
	var loaded proto.LoadModuleReply
	if err := c.RawRequest(&proto.LoadModule{Name: "module-null-sink", Args: "sink_name=relay_probe_null"}, &loaded); err != nil {
		logf("out", "no null output: %v", err)
		return
	}
	defer func() {
		_ = c.RawRequest(&proto.SetDefaultSink{SinkName: server.DefaultSinkName}, nil)
		_ = c.RawRequest(&proto.UnloadModule{ModuleIndex: loaded.ModuleIndex}, nil)
	}()
	if err := c.RawRequest(&proto.SetDefaultSink{SinkName: "relay_probe_null"}, nil); err != nil {
		logf("out", "the null output isn't the default: %v", err)
		return
	}
	logf("out", "the default output is relay_probe_null, in place of %s, until the probe ends", server.DefaultSinkName)
	<-ctx.Done()
}
