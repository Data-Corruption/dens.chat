// Package ffmpeg runs Dens's media module: FFmpeg's libraries and Dens's own
// driver (driver/driver.c), compiled to WebAssembly and translated to Go
// (module, which scripts/ffmpeg.sh generates). The module is its own sandbox:
// it sees only its memory and the functions this package gives it, so a
// hostile file that takes over a decoder stays inside it.
//
// Each job runs in a worker process, a hidden command of the same binary,
// with a memory cap and a deadline, so a decoder that loops, balloons or
// recurses ends its job and not the service. The worker holds no keys and
// opens no files: the module's reads and writes come back to the Runner over
// the worker's standard input and output, and the Runner answers from the
// files the job names.
package ffmpeg

import (
	"errors"
	"fmt"
	"io"
	"sync"
)

// Op is what a job does.
type Op string

const (
	// OpProbe describes a file: its container, its streams and the names
	// of its metadata.
	OpProbe Op = "probe"
	// OpStrip copies a file's video and audio into a new container,
	// without anything else it carried.
	OpStrip Op = "strip"
	// OpStill turns an image a browser can't show into a JPEG or PNG, or
	// makes a photo's smaller copy.
	OpStill Op = "still"
	// OpPoster makes a video's preview from its first frame.
	OpPoster Op = "poster"
	// OpScan reads a video's packets without decoding them, to plan its
	// smaller copy.
	OpScan Op = "scan"
	// OpEncode makes a chunk of a video's smaller copy, in AV1.
	OpEncode Op = "encode"
	// OpMux puts a video's copy together from its chunks and the video's
	// sound.
	OpMux Op = "mux"
	// OpDemux hands a video's packets out, for the page to make its copy
	// with WebCodecs (M5.5).
	OpDemux Op = "demux"
)

// Input is a file a job reads.
type Input interface {
	io.ReaderAt
	Size() int64
}

// Output is a file a job writes, and may read back, as a muxer moving its
// index to the front does.
type Output interface {
	Input
	io.WriterAt
}

// Probed describes a file, naming its metadata without its values.
type Probed struct {
	Format       string   `json:"format"`
	DurationMS   int64    `json:"duration_ms"`
	Chapters     int      `json:"chapters"`
	Metadata     []string `json:"metadata"`
	StreamGroups int      `json:"stream_groups"`
	Streams      []Stream `json:"streams"`
}

// Stream is one of a file's streams.
type Stream struct {
	Type  string `json:"type"`
	Codec string `json:"codec"`
	// Width and Height are a video's size as it shows, after its turn.
	Width       int      `json:"width"`
	Height      int      `json:"height"`
	Turn        string   `json:"turn"`
	AttachedPic bool     `json:"attached_pic"`
	Decoder     bool     `json:"decoder"`
	SideData    []string `json:"side_data"`
	Metadata    []string `json:"metadata"`
}

// Video returns the first video stream that isn't cover art, if any.
func (p Probed) Video() (Stream, bool) {
	for _, s := range p.Streams {
		if s.Type == "video" && !s.AttachedPic {
			return s, true
		}
	}
	return Stream{}, false
}

// Stripped describes a stripped copy.
type Stripped struct {
	// Muxer is the container it was written in, and MIME what it plays as.
	Muxer           string `json:"muxer"`
	MIME            string `json:"mime"`
	Kept            int    `json:"kept"`
	Dropped         int    `json:"dropped"`
	SideDataDropped int    `json:"side_data_dropped"`
	SEIDropped      int    `json:"sei_dropped"`
	Bytes           int64  `json:"bytes"`
}

// Image describes a still or a poster.
type Image struct {
	// Format is "jpeg", or "png" for an image with transparency or made
	// from a PNG.
	Format       string `json:"format"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	SourceWidth  int    `json:"source_width"`
	SourceHeight int    `json:"source_height"`
	Turn         string `json:"turn"`
	ICCBytes     int    `json:"icc_bytes"`
	Bytes        int    `json:"bytes"`
	Tiles        int    `json:"tiles"`
	TilesPlaced  int    `json:"tiles_placed"`
}

// MIME is the image's media type.
func (i Image) MIME() string {
	if i.Format == "png" {
		return "image/png"
	}
	return "image/jpeg"
}

// Scanned describes a video's packets, to plan its smaller copy by. Times
// are in microseconds, from the video's own start.
type Scanned struct {
	// StartUS and EndUS are when the video's frames start and end.
	StartUS int64   `json:"start_us"`
	EndUS   int64   `json:"end_us"`
	Frames  int64   `json:"frames"`
	FPS     float64 `json:"fps"`
	// VideoBytes are the video's, and AudioBytes those of the sound a copy
	// carries, of AudioStreams streams.
	VideoBytes   int64 `json:"video_bytes"`
	AudioBytes   int64 `json:"audio_bytes"`
	AudioStreams int   `json:"audio_streams"`
	// Keyframes are the times of the video's keyframes, each at least half
	// a second after the last listed, where a chunk starts without
	// decoding before it.
	Keyframes []int64 `json:"keyframes"`
}

// Chunk is a part of a video's smaller copy: the frames the video shows from
// StartUS up to EndUS, at most FPS a second, fitting MaxSide, at KBPS.
type Chunk struct {
	StartUS, EndUS     int64
	MaxSide, FPS, KBPS int
}

// Encoded describes a chunk of a video's copy.
type Encoded struct {
	Frames  int   `json:"frames"`
	Decoded int   `json:"decoded"`
	Packets int   `json:"packets"`
	Bytes   int64 `json:"bytes"`
	Width   int   `json:"width"`
	Height  int   `json:"height"`
	// Skip says the frames nothing refers to went undecoded.
	Skip bool `json:"skip"`
}

// Muxed describes a video's copy, put together.
type Muxed struct {
	Bytes        int64 `json:"bytes"`
	VideoPackets int   `json:"video_packets"`
	Keyframes    int   `json:"keyframes"`
	AudioPackets int   `json:"audio_packets"`
	AudioStreams int   `json:"audio_streams"`
	DurationUS   int64 `json:"duration_us"`
}

// Demuxed describes a video's packets, handed out as records, and what
// decoding them takes (M5.5).
type Demuxed struct {
	// Codec is FFmpeg's name for it, such as "h264", and Extradata, in
	// hex, its configuration record, an avcC or hvcC for H.264 and HEVC.
	Codec     string `json:"codec"`
	Extradata string `json:"extradata"`
	// Width and Height are the frames' as the stream decodes them, and
	// Crop the container's crop of them: top, bottom, left and right.
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Crop   [4]int `json:"crop"`
	SAR    [2]int `json:"sar"`
	// Profile, Level and BitDepth name the stream, as its codec string
	// does; -99 is FFmpeg's unknown.
	Profile  int   `json:"profile"`
	Level    int   `json:"level"`
	BitDepth int   `json:"bit_depth"`
	Packets  int   `json:"packets"`
	Bytes    int64 `json:"bytes"`
}

var (
	// ErrUnreadable is a file FFmpeg can't read as what it seems to be:
	// damaged, or in a codec or form the module doesn't take.
	ErrUnreadable = errors.New("the media module can't read the file")
	// ErrFailed is a job that ended without an answer: the module trapped,
	// the worker ran out of memory or time, or it died.
	ErrFailed = errors.New("the media module failed on the file")
)

// JobError says how a job ended without an answer.
type JobError struct {
	// Err is ErrUnreadable or ErrFailed.
	Err error
	// Reason is FFmpeg's, or what ended the worker.
	Reason string
}

func (e *JobError) Error() string { return fmt.Sprintf("%v: %s", e.Err, e.Reason) }
func (e *JobError) Unwrap() error { return e.Err }

// Buffer is an Output in memory, for what's small: a still or a poster.
type Buffer struct {
	mu   sync.Mutex
	data []byte
}

// Bytes returns what was written.
func (b *Buffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data
}

func (b *Buffer) Size() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return int64(len(b.data))
}

func (b *Buffer) ReadAt(p []byte, off int64) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	if off >= int64(len(b.data)) {
		return 0, io.EOF
	}
	n := copy(p, b.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (b *Buffer) WriteAt(p []byte, off int64) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	if end := off + int64(len(p)); end > int64(len(b.data)) {
		b.data = append(b.data, make([]byte, end-int64(len(b.data)))...)
	}
	return copy(b.data[off:], p), nil
}

// Limited is an Output that refuses to grow past Max, so a file can't strip
// or convert into more than its caller takes. Over says it refused.
type Limited struct {
	Output
	Max  int64
	over bool
}

// ErrLimit is a write past a Limited's Max.
var ErrLimit = errors.New("the output is larger than allowed")

func (l *Limited) WriteAt(p []byte, off int64) (int, error) {
	if off+int64(len(p)) > l.Max {
		l.over = true
		return 0, ErrLimit
	}
	return l.Output.WriteAt(p, off)
}

// Over reports whether a write went past Max.
func (l *Limited) Over() bool { return l.over }
