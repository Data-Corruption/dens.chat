//go:build linked

package main

import "github.com/Data-Corruption/dens.chat/spikes/ffmpeg/ffwasm"

// link keeps the module and every export Dens would call, without running it.
func link() func() {
	return func() {
		m := ffwasm.New(nil, nil, nil)
		m.X_initialize()
		m.Xdm_init(0)
		m.Xdm_probe()
		m.Xdm_strip(0)
		m.Xdm_still(0, 0)
		m.Xdm_poster(0, 0)
		m.Xdm_error(0, 0, 0)
		m.Xfree(m.Xmalloc(0))
	}
}
