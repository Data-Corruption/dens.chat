//go:build race

package denclient_test

// raceOn says the test binary, and each media worker it starts, runs under
// the race detector, which slows the media module some fifty times over.
const raceOn = true
