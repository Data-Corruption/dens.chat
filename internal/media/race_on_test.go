//go:build race

package media

// raceOn says the test binary, and each media worker it starts, runs under
// the race detector, which slows the module some fifty times over (see the
// ffmpeg package's).
const raceOn = true
