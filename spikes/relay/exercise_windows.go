package main

import (
	"context"
	"runtime"
	"time"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

// exercise puts the capture through two of question 3's cases by itself:
// it mutes the default output, then sets its volume to nothing, undoing
// each. Something must be playing, and the capture's line each second
// shows what the loopback gave meanwhile. Changing the default output and
// taking it away are left to hands.
func exercise(ctx context.Context) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	_ = ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
	defer ole.CoUninitialize()
	var devices *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &devices); err != nil {
		logf("exer", "no device enumerator: %v", err)
		return
	}
	defer devices.Release()
	dev, _, err := defaultDevice(devices, false)
	if err != nil {
		logf("exer", "no default output to exercise: %v", err)
		return
	}
	defer dev.Release()
	name := deviceName(dev)
	var volume *wca.IAudioEndpointVolume
	if err := dev.Activate(wca.IID_IAudioEndpointVolume, wca.CLSCTX_ALL, nil, &volume); err != nil {
		logf("exer", "no volume control on %q: %v", name, err)
		return
	}
	defer volume.Release()
	var muted bool
	var level float32
	_ = volume.GetMute(&muted)
	_ = volume.GetMasterVolumeLevelScalar(&level)
	// Whatever ends the probe, the output is left as it was found.
	defer func() {
		_ = volume.SetMute(muted, nil)
		_ = volume.SetMasterVolumeLevelScalar(level, nil)
	}()
	step := func(wait time.Duration, what string, do func() error) bool {
		sleep(ctx, wait)
		if ctx.Err() != nil {
			return false
		}
		if err := do(); err != nil {
			logf("exer", "%s: refused: %v", what, err)
		} else {
			logf("exer", "%s", what)
		}
		return true
	}
	_ = step(20*time.Second, "muted "+name, func() error { return volume.SetMute(true, nil) }) &&
		step(15*time.Second, "unmuted "+name, func() error { return volume.SetMute(muted, nil) }) &&
		step(15*time.Second, "volume 0 on "+name, func() error { return volume.SetMasterVolumeLevelScalar(0, nil) }) &&
		step(15*time.Second, "volume back on "+name, func() error { return volume.SetMasterVolumeLevelScalar(level, nil) })
	logf("exer", "done")
	<-ctx.Done()
}
