package host

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows reads its default output's loopback through WASAPI, in shared
// mode, from the desktop session the helper runs in. Windows converts it
// to 16-bit stereo at the output's own rate, mixing more channels down as
// it would for a stereo output, and the capture asks for what it holds
// every few milliseconds. Each second it asks which output is the
// default, and opens the new one as that changes, or as its device goes.

var (
	clsidDeviceEnumerator = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidDeviceEnumerator   = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidAudioClient        = windows.GUID{Data1: 0x1CB9AD4C, Data2: 0xDBFA, Data3: 0x4C32, Data4: [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidAudioCaptureClient = windows.GUID{Data1: 0xC8ADBD64, Data2: 0xE71E, Data3: 0x48A0, Data4: [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
)

// The methods called, by their place in each interface's table, after
// IUnknown's three.
const (
	getDefaultAudioEndpoint = 4 // IMMDeviceEnumerator
	activate                = 3 // IMMDevice
	getID                   = 5
	initialize              = 3 // IAudioClient
	getMixFormat            = 8
	startClient             = 10
	stopClient              = 11
	getService              = 14
	getBuffer               = 3 // IAudioCaptureClient
	releaseBuffer           = 4
	getNextPacketSize       = 5
)

const (
	clsctxAll = 0x17
	// eRender and eConsole: the output most programs play to.
	eRender  = 0
	eConsole = 0
	// The stream reads the output's loopback, which Windows converts to
	// the format asked for, without changing its rate.
	streamLoopback       = 0x00020000
	streamAutoConvertPCM = 0x80000000
	streamSRCDefault     = 0x08000000
	bufferSilent         = 0x2
	// A buffer of 200 ms, in WASAPI's units of 100 ns.
	captureBuffer = 2_000_000
	// What the capture's device going, and no output at all, answer.
	deviceInvalidated hresult = 0x88890004
	notFound          hresult = 0x80070490
	// readEvery is how often the capture asks for what WASAPI holds,
	// which hands over 10 ms at a time.
	readEvery = 5 * time.Millisecond
)

// waveFormat is WAVEFORMATEX, as Windows reads it: 18 bytes, packed.
type waveFormat struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	Size           uint16
}

// CaptureSound reads the default output's loopback until ctx ends, on a
// thread of its own in COM's multithreaded apartment.
func CaptureSound(ctx context.Context, take func(pcm []int16, rate int), problem func(why string)) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// S_FALSE says the thread was in the apartment already, which still
	// takes its CoUninitialize.
	if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil && !errors.Is(err, windows.Errno(1)) {
		return fmt.Errorf("COM: %w", err)
	}
	defer windows.CoUninitialize()
	var devices *comObject
	if r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidDeviceEnumerator)), uintptr(unsafe.Pointer(&devices))); int32(r) < 0 {
		return fmt.Errorf("the audio devices: %w", hresult(r))
	}
	defer devices.release()
	for ctx.Err() == nil {
		err := captureLoopback(ctx, devices, take, problem)
		if ctx.Err() != nil {
			return nil
		}
		var hr hresult
		switch {
		case err == nil, errors.As(err, &hr) && hr == deviceInvalidated:
			// The default changed, or its device went: Windows has
			// another default a moment later.
			soundPause(ctx, 200*time.Millisecond)
		case errors.As(err, &hr) && hr == notFound:
			problem("this computer has no sound output")
			soundPause(ctx, 500*time.Millisecond)
		default:
			problem(err.Error())
			soundPause(ctx, time.Second)
		}
	}
	return nil
}

// defaultOutput is the default output and its ID.
func defaultOutput(devices *comObject) (*comObject, string, error) {
	var dev *comObject
	if err := devices.call(getDefaultAudioEndpoint, eRender, eConsole, uintptr(unsafe.Pointer(&dev))); err != nil {
		return nil, "", err
	}
	var id *uint16
	if err := dev.call(getID, uintptr(unsafe.Pointer(&id))); err != nil {
		dev.release()
		return nil, "", err
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(id))
	return dev, windows.UTF16PtrToString(id), nil
}

// captureLoopback reads the default output's loopback until ctx ends, it
// fails, or another output becomes the default, when it returns nil.
func captureLoopback(ctx context.Context, devices *comObject, take func([]int16, int), problem func(string)) error {
	dev, id, err := defaultOutput(devices)
	if err != nil {
		return err
	}
	defer dev.release()
	var client *comObject
	if err := dev.call(activate, uintptr(unsafe.Pointer(&iidAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&client))); err != nil {
		return fmt.Errorf("the output's audio client: %w", err)
	}
	defer client.release()
	var mix *waveFormat
	if err := client.call(getMixFormat, uintptr(unsafe.Pointer(&mix))); err != nil {
		return fmt.Errorf("the output's format: %w", err)
	}
	rate := int(mix.SamplesPerSec)
	windows.CoTaskMemFree(unsafe.Pointer(mix))
	want := waveFormat{FormatTag: 1, Channels: 2, SamplesPerSec: uint32(rate), AvgBytesPerSec: uint32(rate) * 4, BlockAlign: 4, BitsPerSample: 16}
	if err := client.call(initialize, 0, streamLoopback|streamAutoConvertPCM|streamSRCDefault, captureBuffer, 0,
		uintptr(unsafe.Pointer(&want)), 0); err != nil {
		return fmt.Errorf("the output's loopback: %w", err)
	}
	var reader *comObject
	if err := client.call(getService, uintptr(unsafe.Pointer(&iidAudioCaptureClient)), uintptr(unsafe.Pointer(&reader))); err != nil {
		return fmt.Errorf("the loopback's reader: %w", err)
	}
	defer reader.release()
	if err := client.call(startClient); err != nil {
		return fmt.Errorf("start the loopback: %w", err)
	}
	defer client.call(stopClient)
	problem("")

	var silence []int16
	tick := time.NewTicker(readEvery)
	defer tick.Stop()
	check := time.Now().Add(time.Second)
	for {
		for {
			var next uint32
			if err := reader.call(getNextPacketSize, uintptr(unsafe.Pointer(&next))); err != nil {
				return err
			}
			if next == 0 {
				break
			}
			var data *byte
			var frames, flags uint32
			if err := reader.call(getBuffer, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&frames)),
				uintptr(unsafe.Pointer(&flags)), 0, 0); err != nil {
				return err
			}
			if flags&bufferSilent != 0 || data == nil {
				if len(silence) < int(frames)*2 {
					silence = make([]int16, frames*2)
				}
				take(silence[:frames*2], rate)
			} else {
				take(unsafe.Slice((*int16)(unsafe.Pointer(data)), frames*2), rate)
			}
			if err := reader.call(releaseBuffer, uintptr(frames)); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		if now := time.Now(); now.After(check) {
			check = now.Add(time.Second)
			cur, curID, err := defaultOutput(devices)
			if err != nil {
				return err
			}
			cur.release()
			if curID != id {
				return nil
			}
		}
	}
}
