// Command probe serves the video spike's WebCodecs probe: a page that asks a
// browser what it decodes and encodes, how fast, and how well, on the phone
// videos the spike measures. The server demuxes each video with the pinned
// native FFmpeg in tools/, hands the page its packets, and scores the copies
// the page sends back with VMAF and SSIM, as native.sh scores its own. Each
// browser's report lands in out/spikes/video/probe/.
//
//	go run ./video/probe [-addr 127.0.0.1:8790] (from spikes/)
package main

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

//go:embed probe.html
var page embed.FS

var (
	root    = flag.String("root", "../", "the repository's root")
	ffDir   = flag.String("ffmpeg", "tools/ffmpeg-n9.0.2-17-g2a571b6068-linux64-lgpl-9.0/bin", "the native FFmpeg's bin, under root")
	addr    = flag.String("addr", "127.0.0.1:8790", "where to listen")
	maxPkts = flag.Int("packets", 300, "the most packets of a video the page gets")
)

// video is a phone video as the page gets it: its decoder's configuration
// and its first packets, in decode order.
type video struct {
	Name        string  `json:"name"`
	Codec       string  `json:"codec"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	FPS         float64 `json:"fps"`
	Transfer    string  `json:"transfer"`
	PixFmt      string  `json:"pix_fmt"`
	Description string  `json:"description"`
	Packets     int     `json:"packets"`
	path        string
	data        []byte // the packets, each a header and its bytes
}

func main() {
	flag.Parse()
	if err := os.Chdir(*root); err != nil {
		log.Fatal(err)
	}
	out := filepath.Join("out", "spikes", "video", "probe")
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}
	var videos []*video
	for _, name := range []string{"iphone-12-pro-hevc.mov", "iphone-14-pro-live-photo-hevc.mov", "iphone-x-h264.mov", "android-nokia-6-1-h264.mp4"} {
		v, err := load(filepath.Join("test-files", "mobile", "videos", name))
		if err != nil {
			log.Fatalf("%s: %v", name, err)
		}
		log.Printf("%s: %s %dx%d %.1f fps, %d packets, %d bytes", v.Name, v.Codec, v.Width, v.Height, v.FPS, v.Packets, len(v.data))
		videos = append(videos, v)
	}
	byName := map[string]*video{}
	for _, v := range videos {
		byName[v.Name] = v
	}

	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(page))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, page, "probe.html")
	})
	mux.HandleFunc("GET /api/videos", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(videos)
	})
	mux.HandleFunc("GET /api/videos/{name}/packets", func(w http.ResponseWriter, r *http.Request) {
		v := byName[r.PathValue("name")]
		if v == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(v.data)
	})
	// A copy the page made: an IVF of AV1 or VP9 frames, of the video's
	// first frames scaled to the copy's size, scored against the same
	// frames scaled without loss.
	mux.HandleFunc("POST /api/score", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		v := byName[q.Get("video")]
		width, _ := strconv.Atoi(q.Get("width"))
		height, _ := strconv.Atoi(q.Get("height"))
		frames, _ := strconv.Atoi(q.Get("frames"))
		if v == nil || width < 16 || height < 16 || frames < 1 {
			http.Error(w, "bad score request", http.StatusBadRequest)
			return
		}
		name := fmt.Sprintf("%d-%s-%s-%dx%d.ivf", time.Now().UnixMilli(), v.Name, q.Get("label"), width, height)
		ivf := filepath.Join(out, name)
		f, err := os.Create(ivf)
		if err == nil {
			_, err = io.Copy(f, io.LimitReader(r.Body, 512<<20))
			f.Close()
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		res, err := score(v, ivf, width, height, frames)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(res)
	})
	mux.HandleFunc("POST /api/results", func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil || !json.Valid(data) {
			http.Error(w, "bad results", http.StatusBadRequest)
			return
		}
		var head struct {
			Browser string `json:"browser"`
		}
		_ = json.Unmarshal(data, &head)
		label := regexp.MustCompile(`[^a-zA-Z0-9.-]+`).ReplaceAllString(head.Browser, "-")
		file := filepath.Join(out, fmt.Sprintf("%s-%s.json", time.Now().Format("20060102-150405"), label))
		var pretty bytes.Buffer
		_ = json.Indent(&pretty, data, "", "  ")
		if err := os.WriteFile(file, pretty.Bytes(), 0o644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("results from %s in %s", head.Browser, file)
		_, _ = w.Write([]byte(`{"saved":true}`))
	})
	log.Printf("open http://%s/", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

// load demuxes a video's first packets with the native FFmpeg, and reads
// its decoder's configuration from the container.
func load(path string) (*video, error) {
	ff := filepath.Join(*ffDir, "ffprobe")
	probe, err := exec.Command(ff, "-v", "error", "-select_streams", "v:0", "-show_streams", "-show_packets",
		"-show_entries", "stream=width,height,avg_frame_rate,color_transfer,pix_fmt:packet=pts_time,duration_time,size,flags",
		"-read_intervals", "%+#"+strconv.Itoa(*maxPkts), "-of", "json", path).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}
	var p struct {
		Streams []struct {
			Width, Height int
			AvgFrameRate  string `json:"avg_frame_rate"`
			Transfer      string `json:"color_transfer"`
			PixFmt        string `json:"pix_fmt"`
		}
		Packets []struct {
			PTS      string `json:"pts_time"`
			Duration string `json:"duration_time"`
			Size     string `json:"size"`
			Flags    string `json:"flags"`
		}
	}
	if err := json.Unmarshal(probe, &p); err != nil || len(p.Streams) == 0 {
		return nil, errors.New("ffprobe gave no stream")
	}
	if len(p.Packets) > *maxPkts {
		p.Packets = p.Packets[:*maxPkts]
	}
	raw, err := exec.Command(filepath.Join(*ffDir, "ffmpeg"), "-v", "error", "-i", path, "-map", "0:v:0", "-c", "copy",
		"-frames:v", strconv.Itoa(len(p.Packets)), "-f", "data", "-").Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg: %w", err)
	}
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	codec, desc, err := config(file)
	if err != nil {
		return nil, err
	}
	s := p.Streams[0]
	v := &video{Name: filepath.Base(path), Codec: codec, Width: s.Width, Height: s.Height, Transfer: s.Transfer, PixFmt: s.PixFmt,
		Description: base64.StdEncoding.EncodeToString(desc), Packets: len(p.Packets), path: path}
	if num, den, ok := strings.Cut(s.AvgFrameRate, "/"); ok {
		n, _ := strconv.ParseFloat(num, 64)
		d, _ := strconv.ParseFloat(den, 64)
		if d > 0 {
			v.FPS = n / d
		}
	}
	// Each packet: its size (u32), whether it's a keyframe (u8), and its
	// time and duration in microseconds (f64), little-endian, then its
	// bytes.
	var buf bytes.Buffer
	off := 0
	for _, pk := range p.Packets {
		size, _ := strconv.Atoi(pk.Size)
		if off+size > len(raw) {
			return nil, errors.New("the packets' sizes run past their data")
		}
		pts, _ := strconv.ParseFloat(pk.PTS, 64)
		dur, _ := strconv.ParseFloat(pk.Duration, 64)
		key := byte(0)
		if strings.Contains(pk.Flags, "K") {
			key = 1
		}
		_ = binary.Write(&buf, binary.LittleEndian, uint32(size))
		buf.WriteByte(key)
		_ = binary.Write(&buf, binary.LittleEndian, math.Round(pts*1e6))
		_ = binary.Write(&buf, binary.LittleEndian, math.Round(dur*1e6))
		buf.Write(raw[off : off+size])
		off += size
	}
	if off != len(raw) {
		return nil, fmt.Errorf("the packets' sizes cover %d of %d bytes", off, len(raw))
	}
	v.data = buf.Bytes()
	return v, nil
}

// config finds a video's avcC or hvcC box and makes the codec string
// WebCodecs names it by.
func config(file []byte) (string, []byte, error) {
	for _, box := range []string{"avcC", "hvcC"} {
		i := bytes.Index(file, []byte(box))
		if i < 4 {
			continue
		}
		size := int(binary.BigEndian.Uint32(file[i-4:]))
		if size < 8 || i-4+size > len(file) {
			continue
		}
		b := file[i+4 : i-4+size]
		if box == "avcC" {
			if len(b) < 4 {
				break
			}
			return fmt.Sprintf("avc1.%02x%02x%02x", b[1], b[2], b[3]), b, nil
		}
		if len(b) < 13 {
			break
		}
		// ISO/IEC 14496-15, E.3: profile space and profile, the
		// compatibility flags bit-reversed, tier and level, and the
		// constraint bytes without trailing zeros.
		space, tier, profile := b[1]>>6, (b[1]>>5)&1, b[1]&31
		compat := binary.BigEndian.Uint32(b[2:6])
		var rev uint32
		for i := 0; i < 32; i++ {
			rev |= ((compat >> i) & 1) << (31 - i)
		}
		s := "hvc1." + []string{"", "A", "B", "C"}[space] + strconv.Itoa(int(profile)) + "." + strconv.FormatUint(uint64(rev), 16)
		s += "." + map[byte]string{0: "L", 1: "H"}[tier] + strconv.Itoa(int(b[12]))
		cons := b[6:12]
		n := len(cons)
		for n > 0 && cons[n-1] == 0 {
			n--
		}
		for _, c := range cons[:n] {
			s += "." + strings.ToUpper(strconv.FormatUint(uint64(c), 16))
		}
		return s, b, nil
	}
	return "", nil, errors.New("no avcC or hvcC box")
}

// score scores a copy against the video's first frames scaled the same
// way, as WebCodecs decodes them: cropped only as the stream says, and not
// turned.
func score(v *video, ivf string, width, height, frames int) (map[string]any, error) {
	ff := filepath.Join(*ffDir, "ffmpeg")
	// Both run in one time base, frame for frame, since the scores pair
	// frames by their times.
	graph := fmt.Sprintf("[1:v]scale=%d:%d:flags=bicubic,format=yuv420p,trim=end_frame=%d,settb=1/30,setpts=N,split=2[r1][r2];"+
		"[0:v]format=yuv420p,trim=end_frame=%d,settb=1/30,setpts=N,split=2[d1][d2];[d1][r1]libvmaf=n_threads=4;[d2][r2]ssim",
		width, height, frames, frames)
	out, err := exec.Command(ff, "-nostdin", "-v", "info", "-i", ivf, "-noautorotate", "-apply_cropping", "codec", "-i", v.path,
		"-lavfi", graph, "-f", "null", "-").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("scoring: %v: %s", err, tail(out))
	}
	res := map[string]any{"file": filepath.Base(ivf)}
	if m := regexp.MustCompile(`VMAF score: ([0-9.]+)`).FindSubmatch(out); m != nil {
		res["vmaf"], _ = strconv.ParseFloat(string(m[1]), 64)
	}
	if m := regexp.MustCompile(`SSIM .* All:([0-9.]+)`).FindSubmatch(out); m != nil {
		res["ssim"], _ = strconv.ParseFloat(string(m[1]), 64)
	}
	return res, nil
}

func tail(b []byte) string {
	if len(b) > 2000 {
		b = b[len(b)-2000:]
	}
	return string(b)
}
