package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

// Windows: WASAPI's loopback of the default output, in shared mode, read
// by polling, from whatever session the probe runs in. Each second the
// probe logs the device, its mute and volume, and asks which output is the
// default now; when that changes, or the device goes, it opens the new
// one.

const (
	waveFormatPCM        = 1
	waveFormatFloat      = 3
	waveFormatExtensible = 0xFFFE
	hrDeviceInvalidated  = 0x88890004
	hrNotFound           = 0x80070490
)

func capture(ctx context.Context, o captureOptions, emit func(Chunk)) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		// S_FALSE, already initialized on this thread, comes back as an error.
		var oe *ole.OleError
		if !errors.As(err, &oe) || oe.Code() != 1 {
			logf("cap", "event COM: %v", err)
			return
		}
	}
	defer ole.CoUninitialize()
	sink := newCapSink(emit)
	go sink.run(ctx)

	var devices *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &devices); err != nil {
		sink.event("no device enumerator: %v", err)
		return
	}
	defer devices.Release()

	none := false
	for ctx.Err() == nil {
		began := now()
		err := captureOnce(ctx, devices, o, sink)
		if ctx.Err() != nil {
			return
		}
		if code := hresult(err); code == hrNotFound {
			if !none {
				sink.event("no default output: silence until one shows")
				none = true
			}
			sink.describe("dev=none")
			sleep(ctx, 500*time.Millisecond)
			continue
		}
		none = false
		sink.event("capture ended after=%s: %v", since(began), err)
		sleep(ctx, 200*time.Millisecond)
	}
}

func hresult(err error) uint32 {
	var oe *ole.OleError
	if errors.As(err, &oe) {
		return uint32(oe.Code())
	}
	return 0
}

func defaultDevice(devices *wca.IMMDeviceEnumerator, mic bool) (*wca.IMMDevice, string, error) {
	flow := uint32(wca.ERender)
	if mic {
		flow = wca.ECapture
	}
	var dev *wca.IMMDevice
	if err := devices.GetDefaultAudioEndpoint(flow, wca.EConsole, &dev); err != nil {
		return nil, "", err
	}
	var id string
	if err := dev.GetId(&id); err != nil {
		dev.Release()
		return nil, "", err
	}
	return dev, id, nil
}

func deviceName(dev *wca.IMMDevice) string {
	var props *wca.IPropertyStore
	if err := dev.OpenPropertyStore(wca.STGM_READ, &props); err != nil {
		return "?"
	}
	defer props.Release()
	var v wca.PROPVARIANT
	if err := props.GetValue(&wca.PKEY_Device_FriendlyName, &v); err != nil {
		return "?"
	}
	return v.String()
}

// format reads a WAVEFORMATEX as WASAPI lays it out, packed, with the
// extensible form's subformat behind it.
type format struct {
	tag      uint16
	channels int
	rate     int
	bits     int
	float    bool
}

func readFormat(wfx *wca.WAVEFORMATEX) format {
	f := format{tag: wfx.WFormatTag, channels: int(wfx.NChannels), rate: int(wfx.NSamplesPerSec), bits: int(wfx.WBitsPerSample)}
	f.float = f.tag == waveFormatFloat
	if f.tag == waveFormatExtensible && wfx.CbSize >= 22 {
		// The subformat's first four bytes, 24 bytes in: 1 is PCM, 3 float.
		sub := *(*uint32)(unsafe.Add(unsafe.Pointer(wfx), 24))
		f.float = sub == waveFormatFloat
	}
	return f
}

func (f format) String() string {
	kind := "int"
	if f.float {
		kind = "float"
	}
	return fmt.Sprintf("%dHz/%dch/%dbit-%s", f.rate, f.channels, f.bits, kind)
}

// stereo16 turns a packet in the device's format into 16-bit stereo: the
// first two channels of more, one channel doubled.
func stereo16(data *byte, frames int, f format) []int16 {
	out := make([]int16, frames*2)
	width := f.bits / 8
	raw := unsafe.Slice(data, frames*f.channels*width)
	sample := func(frame, ch int) int16 {
		at := (frame*f.channels + ch) * width
		switch {
		case f.float && f.bits == 32:
			v := *(*float32)(unsafe.Pointer(&raw[at]))
			return int16(math.Max(-32768, math.Min(32767, float64(v)*32767)))
		case !f.float && f.bits == 16:
			return *(*int16)(unsafe.Pointer(&raw[at]))
		case !f.float && f.bits == 24:
			return int16(uint16(raw[at+1]) | uint16(raw[at+2])<<8)
		case !f.float && f.bits == 32:
			return int16(*(*int32)(unsafe.Pointer(&raw[at])) >> 16)
		}
		return 0
	}
	for i := 0; i < frames; i++ {
		out[2*i] = sample(i, 0)
		if f.channels > 1 {
			out[2*i+1] = sample(i, 1)
		} else {
			out[2*i+1] = out[2*i]
		}
	}
	return out
}

func captureOnce(ctx context.Context, devices *wca.IMMDeviceEnumerator, o captureOptions, sink *capSink) error {
	dev, id, err := defaultDevice(devices, o.mic)
	if err != nil {
		return err
	}
	defer dev.Release()
	name := deviceName(dev)

	var client *wca.IAudioClient
	if err := dev.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &client); err != nil {
		return fmt.Errorf("activate %q: %w", name, err)
	}
	defer client.Release()
	var mix *wca.WAVEFORMATEX
	if err := client.GetMixFormat(&mix); err != nil {
		return fmt.Errorf("mix format of %q: %w", name, err)
	}
	defer ole.CoTaskMemFree(uintptr(unsafe.Pointer(mix)))
	mixed := readFormat(mix)
	use, wfx := mixed, mix
	flags := uint32(0)
	if !o.mic {
		flags = wca.AUDCLNT_STREAMFLAGS_LOOPBACK
	}
	if o.convert {
		// WASAPI's own conversion, in place of the probe's.
		wfx = &wca.WAVEFORMATEX{WFormatTag: waveFormatPCM, NChannels: 2, NSamplesPerSec: 48000, NAvgBytesPerSec: 48000 * 4, NBlockAlign: 4, WBitsPerSample: 16}
		use = readFormat(wfx)
		flags |= wca.AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM | wca.AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY
	}
	// A buffer of 200 ms, in 100 ns units.
	if err := client.Initialize(wca.AUDCLNT_SHAREMODE_SHARED, flags, 2_000_000, 0, wfx, nil); err != nil {
		return fmt.Errorf("initialize %q as %s: %w", name, use, err)
	}
	var bufferFrames uint32
	_ = client.GetBufferSize(&bufferFrames)
	var period, minPeriod wca.REFERENCE_TIME
	_ = client.GetDevicePeriod(&period, &minPeriod)
	var reader *wca.IAudioCaptureClient
	if err := client.GetService(wca.IID_IAudioCaptureClient, &reader); err != nil {
		return fmt.Errorf("capture client of %q: %w", name, err)
	}
	defer reader.Release()

	var volume *wca.IAudioEndpointVolume
	if err := dev.Activate(wca.IID_IAudioEndpointVolume, wca.CLSCTX_ALL, nil, &volume); err == nil {
		defer volume.Release()
	} else {
		volume = nil
	}

	// Loopback gives nothing while nothing plays. Silence of the probe's
	// own, played to the same output, keeps it coming, if asked for.
	var warm *warmth
	if o.keepWarm && !o.mic {
		if warm, err = keepWarm(dev); err != nil {
			sink.event("keep-warm failed: %v", err)
		} else {
			defer warm.close()
		}
	}

	if err := client.Start(); err != nil {
		return fmt.Errorf("start %q: %w", name, err)
	}
	defer client.Stop()
	sink.event("opened dev=%q id=%s mix=%s use=%s buffer=%dframes period=%s min-period=%s keep-warm=%t", name, id, mixed, use,
		bufferFrames, ms(time.Duration(period)*100), ms(time.Duration(minPeriod)*100), warm != nil)

	var discontinuities, silentPackets, packets int
	nextCheck := now()
	for ctx.Err() == nil {
		for {
			var next uint32
			if err := reader.GetNextPacketSize(&next); err != nil {
				return fmt.Errorf("next packet of %q: %w", name, err)
			}
			if next == 0 {
				break
			}
			var data *byte
			var frames, bufFlags uint32
			if err := reader.GetBuffer(&data, &frames, &bufFlags, nil, nil); err != nil {
				return fmt.Errorf("buffer of %q: %w", name, err)
			}
			packets++
			if bufFlags&wca.AUDCLNT_BUFFERFLAGS_DATA_DISCONTINUITY != 0 {
				discontinuities++
			}
			var pcm []int16
			if bufFlags&wca.AUDCLNT_BUFFERFLAGS_SILENT != 0 {
				silentPackets++
				pcm = make([]int16, frames*2)
			} else {
				pcm = stereo16(data, int(frames), use)
			}
			if err := reader.ReleaseBuffer(frames); err != nil {
				return fmt.Errorf("release of %q: %w", name, err)
			}
			sink.data(pcm, use.rate)
		}
		if warm != nil {
			warm.feed()
		}
		if at := now(); at >= nextCheck {
			nextCheck = at + time.Second
			// Is this still the default output, and is it still there?
			cur, curID, err := defaultDevice(devices, o.mic)
			if err != nil {
				return fmt.Errorf("the default output went: %w", err)
			}
			cur.Release()
			if curID != id {
				sink.event("default output changed from=%q to id=%s", name, curID)
				return nil
			}
			var state uint32
			_ = dev.GetState(&state)
			mute, level := false, float32(-1)
			if volume != nil {
				_ = volume.GetMute(&mute)
				_ = volume.GetMasterVolumeLevelScalar(&level)
			}
			sink.describe("dev=%q state=%d mute=%t vol=%.2f packets=%d discontinuities=%d silent-packets=%d", name, state, mute, level,
				packets, discontinuities, silentPackets)
			packets, discontinuities, silentPackets = 0, 0, 0
		}
		time.Sleep(3 * time.Millisecond)
	}
	return nil
}

// warmth is a render stream of silence on the device being captured.
type warmth struct {
	client *wca.IAudioClient
	render *wca.IAudioRenderClient
	frames uint32
}

func keepWarm(dev *wca.IMMDevice) (*warmth, error) {
	w := &warmth{}
	if err := dev.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &w.client); err != nil {
		return nil, err
	}
	var mix *wca.WAVEFORMATEX
	if err := w.client.GetMixFormat(&mix); err != nil {
		w.client.Release()
		return nil, err
	}
	defer ole.CoTaskMemFree(uintptr(unsafe.Pointer(mix)))
	if err := w.client.Initialize(wca.AUDCLNT_SHAREMODE_SHARED, 0, 2_000_000, 0, mix, nil); err != nil {
		w.client.Release()
		return nil, err
	}
	_ = w.client.GetBufferSize(&w.frames)
	if err := w.client.GetService(wca.IID_IAudioRenderClient, &w.render); err != nil {
		w.client.Release()
		return nil, err
	}
	w.feed()
	if err := w.client.Start(); err != nil {
		w.close()
		return nil, err
	}
	return w, nil
}

func (w *warmth) feed() {
	var padding uint32
	if w.client.GetCurrentPadding(&padding) != nil || padding >= w.frames {
		return
	}
	var data *byte
	n := w.frames - padding
	if w.render.GetBuffer(n, &data) == nil {
		_ = w.render.ReleaseBuffer(n, wca.AUDCLNT_BUFFERFLAGS_SILENT)
	}
}

func (w *warmth) close() {
	_ = w.client.Stop()
	w.render.Release()
	w.client.Release()
}
