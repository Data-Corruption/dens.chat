package main

import "context"

// Android is never a source in the spike.
func capture(ctx context.Context, o captureOptions, emit func(Chunk)) {
	logf("cap", "no capture on Android")
	<-ctx.Done()
}
