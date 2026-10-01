//go:build race

package ffmpeg

// raceOn says the test binary, and so each worker it starts, runs under the
// race detector, which slows the single-threaded module some fifty times
// over and finds nothing in it. Such runs cover the Runner and the host, and
// leave the module's long cases to the run without it, which
// scripts/test.sh makes too.
const raceOn = true
