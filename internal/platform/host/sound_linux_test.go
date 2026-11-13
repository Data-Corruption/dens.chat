//go:build !android

package host

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jfreymuth/pulse/proto"
)

// pulseStandIn is a PulseAudio server for the capture's tests, speaking
// as much of the native protocol as the capture asks of one, at its
// version 13: two outputs, each with its monitor, the default between
// them, and a recording that stays on the monitor it opened on, as
// PulseAudio's own does as the default changes. A recording, once it
// runs, gets a ramp of 16-bit stereo in 5 ms pieces.
type pulseStandIn struct {
	t  *testing.T
	ln net.Listener

	mu      sync.Mutex
	def     string
	streams []standInStream
	conns   []net.Conn
	frames  int
}

// standInStream is a recording: the monitor it's on and its properties.
type standInStream struct {
	source uint32
	props  proto.PropList
}

// The outputs: their names, and their monitors' indexes.
var standInSinks = map[string]uint32{"speakers": 10, "headphones": 11}

func newPulseStandIn(t *testing.T) *pulseStandIn {
	path := filepath.Join(t.TempDir(), "native")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PULSE_SERVER", "unix:"+path)
	t.Setenv("PULSE_COOKIE", filepath.Join(t.TempDir(), "none"))
	s := &pulseStandIn{t: t, ln: ln, def: "speakers"}
	go s.accept()
	t.Cleanup(func() {
		_ = ln.Close()
		s.mu.Lock()
		for _, c := range s.conns {
			_ = c.Close()
		}
		s.mu.Unlock()
	})
	return s
}

func (s *pulseStandIn) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, c)
		s.mu.Unlock()
		go s.serve(c)
	}
}

func (s *pulseStandIn) setDefault(name string) {
	s.mu.Lock()
	s.def = name
	s.mu.Unlock()
}

// dropAll closes every connection, as a server that stops.
func (s *pulseStandIn) dropAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		_ = c.Close()
	}
	s.conns = nil
}

func (s *pulseStandIn) recordings() []standInStream {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.streams)
}

func (s *pulseStandIn) serve(c net.Conn) {
	r := bufio.NewReader(c)
	var wmu sync.Mutex
	send := func(channel uint32, payload []byte) error {
		head := make([]byte, 20)
		binary.BigEndian.PutUint32(head, uint32(len(payload)))
		binary.BigEndian.PutUint32(head[4:], channel)
		wmu.Lock()
		defer wmu.Unlock()
		_, err := c.Write(append(head, payload...))
		return err
	}
	reply := func(tag uint32, fields ...any) {
		var b bytes.Buffer
		tagged(&b, uint32(proto.OpReply))
		tagged(&b, tag)
		for _, f := range fields {
			encodeValue(&b, f)
		}
		_ = send(0xFFFFFFFF, b.Bytes())
	}
	stream := -1
	for {
		head := make([]byte, 20)
		if _, err := io.ReadFull(r, head); err != nil {
			return
		}
		body := make([]byte, binary.BigEndian.Uint32(head))
		if _, err := io.ReadFull(r, body); err != nil {
			return
		}
		vals, err := decodeValues(body)
		if err != nil || len(vals) < 2 {
			s.t.Errorf("the stand-in couldn't read a request: %v", err)
			return
		}
		op, tag := vals[0].(uint32), vals[1].(uint32)
		args := vals[2:]
		s.mu.Lock()
		def := s.def
		s.mu.Unlock()
		switch op {
		case proto.OpAuth:
			reply(tag, &proto.AuthReply{Version: 13})
		case proto.OpSetClientName:
			reply(tag, &proto.SetClientNameReply{ClientIndex: 1})
		case proto.OpSubscribe, proto.OpFlushRecordStream, proto.OpDeleteRecordStream:
			reply(tag)
		case proto.OpCreateRecordStream:
			var props proto.PropList
			for _, a := range args {
				if p, ok := a.(proto.PropList); ok {
					props = p
					break
				}
			}
			s.mu.Lock()
			s.streams = append(s.streams, standInStream{source: standInSinks[def], props: props})
			stream = len(s.streams) - 1
			s.mu.Unlock()
			reply(tag, &proto.CreateRecordStreamReply{StreamIndex: uint32(stream), SourceOutputIndex: uint32(100 + stream),
				BufferMaxLength: 1 << 16, BufferFragSize: 960, SampleSpec: proto.SampleSpec{Format: 3, Channels: 2, Rate: 48000},
				ChannelMap: proto.ChannelMap{1, 2}, SourceIndex: standInSinks[def], SourceName: def + ".monitor"})
		case proto.OpCorkRecordStream:
			reply(tag)
			if corked, _ := args[1].(bool); !corked && stream >= 0 {
				go s.play(uint32(stream), send)
			}
		case proto.OpGetServerInfo:
			reply(tag, &proto.GetServerInfoReply{PackageName: "stand-in", DefaultSampleSpec: proto.SampleSpec{Format: 3, Channels: 2, Rate: 48000},
				DefaultSinkName: def})
		case proto.OpGetSinkInfo:
			reply(tag, &proto.GetSinkInfoReply{SinkName: def, SampleSpec: proto.SampleSpec{Format: 3, Channels: 2, Rate: 48000}, ChannelMap: proto.ChannelMap{1, 2},
				ChannelVolumes: proto.ChannelVolumes{0x10000, 0x10000}, MonitorSourceIndex: standInSinks[def], MonitorSourceName: def + ".monitor",
				Properties: proto.PropList{}})
		case proto.OpGetSourceOutputInfoList:
			var list []any
			for i, st := range s.recordings() {
				list = append(list, &proto.GetSourceOutputInfoReply{SourceOutpuIndex: uint32(100 + i), SourceIndex: st.source,
					SampleSpec: proto.SampleSpec{Format: 3, Channels: 2, Rate: 48000}, ChannelMap: proto.ChannelMap{1, 2}, Properties: st.props})
			}
			reply(tag, list...)
		case proto.OpGetSourceInfo:
			index, _ := args[0].(uint32)
			name := ""
			for n, i := range standInSinks {
				if i == index {
					name = n + ".monitor"
				}
			}
			reply(tag, &proto.GetSourceInfoReply{SourceIndex: index, SourceName: name, SampleSpec: proto.SampleSpec{Format: 3, Channels: 2, Rate: 48000},
				ChannelMap: proto.ChannelMap{1, 2}, ChannelVolumes: proto.ChannelVolumes{0x10000, 0x10000}, Properties: proto.PropList{}})
		default:
			s.t.Errorf("the stand-in has no answer to request %d", op)
			return
		}
	}
}

// play sends a recording a ramp, frame by frame, in 5 ms pieces, until
// its connection closes.
func (s *pulseStandIn) play(stream uint32, send func(uint32, []byte) error) {
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for range tick.C {
		piece := make([]byte, 240*4)
		s.mu.Lock()
		for i := 0; i < 240; i++ {
			v := uint16(int16(s.frames % 30000))
			binary.LittleEndian.PutUint16(piece[4*i:], v)
			binary.LittleEndian.PutUint16(piece[4*i+2:], v)
			s.frames++
		}
		s.mu.Unlock()
		if send(stream, piece) != nil {
			return
		}
	}
}

// tagged writes a uint32 as a tagstruct does.
func tagged(b *bytes.Buffer, v uint32) {
	b.WriteByte('L')
	_ = binary.Write(b, binary.BigEndian, v)
}

// encodeValue writes a reply's fields as PulseAudio's tagstruct does, as
// the protocol's own writer would at version 13.
func encodeValue(b *bytes.Buffer, v any) {
	rv := reflect.ValueOf(v).Elem()
	rt := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		if tag := string(rt.Field(i).Tag); tag != "" {
			if ver, err := strconv.Atoi(tag); err == nil && ver > 13 {
				continue
			}
		}
		switch f := rv.Field(i).Interface().(type) {
		case string:
			if f == "" {
				b.WriteByte('N')
			} else {
				b.WriteByte('t')
				b.WriteString(f)
				b.WriteByte(0)
			}
		case uint32:
			tagged(b, f)
		case proto.Version:
			tagged(b, uint32(f))
		case bool:
			if f {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		case proto.SampleSpec:
			b.Write([]byte{'a', f.Format, f.Channels})
			_ = binary.Write(b, binary.BigEndian, f.Rate)
		case proto.ChannelMap:
			b.Write([]byte{'m', byte(len(f))})
			b.Write(f)
		case proto.ChannelVolumes:
			b.Write([]byte{'v', byte(len(f))})
			for _, x := range f {
				_ = binary.Write(b, binary.BigEndian, uint32(x))
			}
		case proto.Microseconds:
			b.WriteByte('U')
			_ = binary.Write(b, binary.BigEndian, uint64(f))
		case proto.Volume:
			b.WriteByte('V')
			_ = binary.Write(b, binary.BigEndian, uint32(f))
		case proto.PropList:
			b.WriteByte('P')
			for k, x := range f {
				b.WriteByte('t')
				b.WriteString(k)
				b.WriteByte(0)
				tagged(b, uint32(len(x)))
				b.WriteByte('x')
				_ = binary.Write(b, binary.BigEndian, uint32(len(x)))
				b.Write(x)
			}
			b.WriteByte('N')
		default:
			panic("the stand-in can't write a " + rt.Field(i).Type.String())
		}
	}
}

// decodeValues reads a request's tagstruct, each value as the Go value
// the protocol's own types hold it in.
func decodeValues(body []byte) ([]any, error) {
	r := bytes.NewReader(body)
	u32 := func() uint32 {
		var v uint32
		_ = binary.Read(r, binary.BigEndian, &v)
		return v
	}
	str := func() string {
		var s []byte
		for {
			c, err := r.ReadByte()
			if err != nil || c == 0 {
				return string(s)
			}
			s = append(s, c)
		}
	}
	var out []any
	for r.Len() > 0 {
		t, _ := r.ReadByte()
		switch t {
		case 'L', 'V':
			out = append(out, u32())
		case 't':
			out = append(out, str())
		case 'N':
			out = append(out, "")
		case '1', '0':
			out = append(out, t == '1')
		case 'B':
			c, _ := r.ReadByte()
			out = append(out, c)
		case 'R', 'r', 'U':
			var v uint64
			_ = binary.Read(r, binary.BigEndian, &v)
			out = append(out, v)
		case 'a':
			f, _ := r.ReadByte()
			ch, _ := r.ReadByte()
			out = append(out, proto.SampleSpec{Format: f, Channels: ch, Rate: u32()})
		case 'x':
			x := make([]byte, u32())
			_, _ = io.ReadFull(r, x)
			out = append(out, x)
		case 'm':
			n, _ := r.ReadByte()
			m := make([]byte, n)
			_, _ = io.ReadFull(r, m)
			out = append(out, proto.ChannelMap(m))
		case 'v':
			n, _ := r.ReadByte()
			v := make(proto.ChannelVolumes, n)
			for i := range v {
				v[i] = proto.Volume(u32())
			}
			out = append(out, v)
		case 'P':
			p := proto.PropList{}
			for {
				k, _ := r.ReadByte()
				if k == 'N' {
					break
				}
				key := str()
				_, _ = r.ReadByte()
				u32()
				_, _ = r.ReadByte()
				x := make([]byte, u32())
				_, _ = io.ReadFull(r, x)
				p[key] = x
			}
			out = append(out, p)
		default:
			return out, errors.New("a tagstruct value of type " + string(t))
		}
	}
	return out, nil
}

// heard keeps what a capture hands over.
type heard struct {
	mu       sync.Mutex
	frames   []int16
	rates    []int
	problems []string
}

func (h *heard) take(pcm []int16, rate int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.frames = append(h.frames, pcm...)
	if len(h.rates) == 0 || h.rates[len(h.rates)-1] != rate {
		h.rates = append(h.rates, rate)
	}
}

func (h *heard) problem(why string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.problems = append(h.problems, why)
}

func (h *heard) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.frames) / 2
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	end := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The capture records the default output's monitor over PulseAudio's
// protocol, 16-bit stereo at 48 kHz, as the server sends it; and opens
// again on the new default's monitor once its recording has sat on the
// old one's for two seconds, which PulseAudio leaves it on.
func TestCaptureSoundFollowsTheDefaultOutput(t *testing.T) {
	s := newPulseStandIn(t)
	h := &heard{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- CaptureSound(ctx, h.take, h.problem) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("the capture ended with %v", err)
		}
	}()

	waitUntil(t, "a second of sound", func() bool { return h.count() >= 48000 })
	h.mu.Lock()
	ramp := slices.Clone(h.frames[:2000])
	rates, problems := slices.Clone(h.rates), slices.Clone(h.problems)
	h.mu.Unlock()
	for i := 0; i+1 < len(ramp); i += 2 {
		if ramp[i] != ramp[i+1] || (i >= 2 && ramp[i] != ramp[i-2]+1) {
			t.Fatalf("the capture's frames aren't the server's ramp: %v", ramp[max(0, i-4):i+4])
		}
	}
	if !slices.Equal(rates, []int{48000}) || !slices.Equal(problems, []string{""}) {
		t.Errorf("the capture reported rates %v and problems %q", rates, problems)
	}
	streams := s.recordings()
	if len(streams) != 1 || streams[0].source != standInSinks["speakers"] || streams[0].props[streamKey] == nil {
		t.Fatalf("the server's recordings: %+v", streams)
	}

	s.setDefault("headphones")
	moved := time.Now()
	waitUntil(t, "a recording on the new default's monitor", func() bool {
		r := s.recordings()
		return len(r) == 2 && r[1].source == standInSinks["headphones"]
	})
	if d := time.Since(moved); d < 900*time.Millisecond || d > 4*time.Second {
		t.Errorf("the capture followed the default after %s, not two seconds of its stream astray", d)
	}
	before := h.count()
	waitUntil(t, "sound from the new recording", func() bool { return h.count() > before+4800 })
}

// A server that goes away is tried again each second, and a capture with
// none to connect to says so.
func TestCaptureSoundWithNoServer(t *testing.T) {
	s := newPulseStandIn(t)
	h := &heard{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- CaptureSound(ctx, h.take, h.problem) }()
	waitUntil(t, "sound", func() bool { return h.count() > 4800 })

	_ = s.ln.Close()
	s.dropAll()
	waitUntil(t, "the capture saying there's no server", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return slices.Contains(h.problems, errNoServer.Error())
	})
	cancel()
	if err := <-done; err != nil {
		t.Errorf("the capture ended with %v", err)
	}
}

// TestCaptureProbe reads this computer's sound where DENS_CAPTURE_PROBE
// is set, as the Flatpak e2e runs it inside the Flatpak's sandbox, through
// the socket its permission gives it: from the first sound, which may take
// the server a while, for two seconds, failing unless a tone comes, at
// least -20 dB, and nearly all of it.
func TestCaptureProbe(t *testing.T) {
	if os.Getenv("DENS_CAPTURE_PROBE") == "" {
		t.Skip("set DENS_CAPTURE_PROBE to read this computer's sound")
	}
	h := &heard{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- CaptureSound(ctx, h.take, h.problem) }()
	waitUntil(t, "the first sound", func() bool { return h.count() > 0 })
	first := time.Since(start)
	h.mu.Lock()
	from := len(h.frames)
	h.mu.Unlock()
	time.Sleep(2 * time.Second)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	frames := h.frames[from:]
	var sum float64
	for _, v := range frames {
		sum += float64(v) * float64(v)
	}
	level := math.Inf(-1)
	if len(frames) > 0 {
		level = 20 * math.Log10(math.Sqrt(sum/float64(len(frames)))/32768)
	}
	t.Logf("read the first sound %s after opening, then %d frames in 2 s at %v, at %.1f dB, its problems %q", first.Round(time.Millisecond),
		len(frames)/2, h.rates, level, h.problems)
	if len(frames)/2 < 2*48000*9/10 || level < -20 {
		t.Fatalf("no tone came: %d frames at %.1f dB", len(frames)/2, level)
	}
}
