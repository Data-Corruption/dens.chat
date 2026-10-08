package youtube

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const id = "dQw4w9WgXcQ"

// youTube stands in for YouTube: what it answers for each path, how often
// it was asked, and whether a request carried anything about its sender.
type youTube struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	answers map[string]http.HandlerFunc
	asked   map[string]int
	// hold, when set, keeps every answer back until it's closed.
	hold chan struct{}
}

func newYouTube(t *testing.T) *youTube {
	y := &youTube{t: t, answers: map[string]http.HandlerFunc{}, asked: map[string]int{}}
	y.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range []string{"User-Agent", "Cookie", "Referer"} {
			if _, ok := r.Header[h]; ok {
				t.Errorf("%s asked with a %s header: %q", r.URL.Path, h, r.Header.Get(h))
			}
		}
		y.mu.Lock()
		y.asked[r.URL.Path]++
		h, hold := y.answers[r.URL.Path], y.hold
		y.mu.Unlock()
		if hold != nil {
			<-hold
		}
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(y.srv.Close)
	return y
}

func (y *youTube) answer(path string, h http.HandlerFunc) {
	y.mu.Lock()
	defer y.mu.Unlock()
	y.answers[path] = h
}

// holdAnswers keeps every answer back until the channel it returns is
// closed.
func (y *youTube) holdAnswers() chan struct{} {
	y.mu.Lock()
	defer y.mu.Unlock()
	y.hold = make(chan struct{})
	return y.hold
}

func (y *youTube) times(path string) int {
	y.mu.Lock()
	defer y.mu.Unlock()
	return y.asked[path]
}

func (y *youTube) lookup() *Lookup {
	return newLookup(y.srv.URL+"/oembed", y.srv.URL+"/vi/", y.srv.Client().Transport)
}

// oEmbed answers as YouTube's oEmbed does for the one video it knows.
func oEmbed(t *testing.T, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query(); got.Get("url") != "https://www.youtube.com/watch?v="+id || got.Get("format") != "json" {
			t.Errorf("oEmbed asked for %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }
}

func picture(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, x%h, color.RGBA{200, 30, 30, 255})
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func serve(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(body)
	}
}

func TestACoverShowsTheTitleAndChannelAsNames(t *testing.T) {
	y := newYouTube(t)
	long := strings.Repeat("ab ", 150)
	y.answer("/oembed", oEmbed(t, `{"type":"video","title":"Never ‮gonna​  give","author_name":"Rick\nAstley `+long+`",`+
		`"html":"<iframe src=\"https://evil.example\"></iframe>","thumbnail_url":"https://evil.example/x.jpg"}`))
	v, err := y.lookup().Video(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if v.Title != "Never gonna give" || !v.Playable {
		t.Errorf("video %+v", v)
	}
	if n := len([]rune(v.Channel)); n > maxChannel || n < maxChannel-1 || !strings.HasPrefix(v.Channel, "Rick Astley ab ab") || !strings.HasSuffix(v.Channel, "…") {
		t.Errorf("channel of %d characters: %q", n, v.Channel)
	}
}

func TestVideosYouTubeWontDescribe(t *testing.T) {
	for _, c := range []struct {
		name   string
		answer http.HandlerFunc
		want   error
	}{
		{"embedding turned off", status(http.StatusUnauthorized), nil},
		{"forbidden", status(http.StatusForbidden), nil},
		{"no such video", status(http.StatusNotFound), ErrNotFound},
		{"a malformed one", status(http.StatusBadRequest), ErrNotFound},
		{"YouTube down", status(http.StatusServiceUnavailable), ErrUnavailable},
		{"a redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		}, ErrUnavailable},
		{"not JSON", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`{"type":"video","title":"x"}`))
		}, ErrUnavailable},
		{"not a video", oEmbed(t, `{"type":"rich","title":"x"}`), ErrUnavailable},
		{"too long", oEmbed(t, `{"type":"video","title":"`+strings.Repeat("x", maxAnswer)+`"}`), ErrUnavailable},
		{"broken JSON", oEmbed(t, `{"type":"video",`), ErrUnavailable},
	} {
		t.Run(c.name, func(t *testing.T) {
			y := newYouTube(t)
			y.answer("/oembed", c.answer)
			y.answer("/elsewhere", oEmbed(t, `{"type":"video","title":"followed"}`))
			v, err := y.lookup().Video(context.Background(), id)
			if !errors.Is(err, c.want) {
				t.Fatalf("error %v, want %v", err, c.want)
			}
			if c.want == nil && (v.Playable || v.Title != "") {
				t.Errorf("video %+v, want one that plays only on YouTube", v)
			}
			if err != nil && strings.Contains(err.Error(), id) {
				t.Errorf("the error names the video, which would reach the log: %v", err)
			}
			if y.times("/elsewhere") != 0 {
				t.Error("the redirect was followed")
			}
		})
	}
}

func TestOnlyVideoIDsAreAskedFor(t *testing.T) {
	y := newYouTube(t)
	l := y.lookup()
	for _, bad := range []string{"", "dQw4w9WgXc", "dQw4w9WgXcQQ", "dQw4w9WgX/Q", "../../oembe", "dQw4w9WgX%51", "dQw4w9WgXc?"} {
		if _, err := l.Video(context.Background(), bad); !errors.Is(err, ErrBadID) {
			t.Errorf("Video(%q): %v", bad, err)
		}
		if _, err := l.Picture(context.Background(), bad); !errors.Is(err, ErrBadID) {
			t.Errorf("Picture(%q): %v", bad, err)
		}
	}
	if n := y.times("/oembed"); n != 0 {
		t.Errorf("YouTube was asked %d times", n)
	}
}

func TestAPictureIsAJPEGWithoutItsMetadata(t *testing.T) {
	y := newYouTube(t)
	plain := picture(t, 480, 360)
	// A comment segment, right after the start marker, stands in for
	// whatever the picture might carry.
	tagged := append([]byte{0xFF, 0xD8, 0xFF, 0xFE, 0x00, 0x15}, []byte("taken at 51 Main St")...)
	tagged = append(tagged, plain[2:]...)
	y.answer("/vi/"+id+"/hqdefault.jpg", serve(tagged))
	got, err := y.lookup().Picture(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte("Main St")) {
		t.Error("the picture kept its comment")
	}
	img, err := jpeg.Decode(bytes.NewReader(got))
	if err != nil || img.Bounds().Dx() != 480 || img.Bounds().Dy() != 360 {
		t.Fatalf("the picture doesn't decode as the one sent: %v", err)
	}
}

func TestPicturesThatArentServed(t *testing.T) {
	var asPNG bytes.Buffer
	if err := png.Encode(&asPNG, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	huge := picture(t, 480, 360)
	huge = append(huge, make([]byte, maxPicture)...)
	for _, c := range []struct {
		name   string
		answer http.HandlerFunc
		want   error
	}{
		{"none", status(http.StatusNotFound), ErrNotFound},
		{"a PNG", serve(asPNG.Bytes()), ErrUnavailable},
		{"HTML", serve([]byte("<html><script>alert(1)</script></html>")), ErrUnavailable},
		{"a broken JPEG", serve(picture(t, 480, 360)[:300]), ErrUnavailable},
		{"too large", serve(huge), ErrUnavailable},
		{"too wide", serve(picture(t, 2000, 20)), ErrUnavailable},
		{"a redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		}, ErrUnavailable},
	} {
		t.Run(c.name, func(t *testing.T) {
			y := newYouTube(t)
			y.answer("/vi/"+id+"/hqdefault.jpg", c.answer)
			y.answer("/elsewhere", serve(picture(t, 480, 360)))
			if _, err := y.lookup().Picture(context.Background(), id); !errors.Is(err, c.want) {
				t.Fatalf("error %v, want %v", err, c.want)
			}
			if y.times("/elsewhere") != 0 {
				t.Error("the redirect was followed")
			}
		})
	}
}

func TestCoversAskingTogetherFetchOnce(t *testing.T) {
	y := newYouTube(t)
	y.answer("/oembed", oEmbed(t, `{"type":"video","title":"Once"}`))
	hold := y.holdAnswers()
	l := y.lookup()
	var wg sync.WaitGroup
	var got atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v, err := l.Video(context.Background(), id); err == nil && v.Title == "Once" {
				got.Add(1)
			}
		}()
	}
	// Each cover is waiting before YouTube answers any of them.
	for deadline := time.Now().Add(5 * time.Second); y.times("/oembed") == 0 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(hold)
	wg.Wait()
	if got.Load() != 8 || y.times("/oembed") != 1 {
		t.Errorf("%d covers got the video from %d fetches", got.Load(), y.times("/oembed"))
	}
}

func TestACoverThatLeavesFailsNobodyElse(t *testing.T) {
	y := newYouTube(t)
	y.answer("/oembed", oEmbed(t, `{"type":"video","title":"Still here"}`))
	hold := y.holdAnswers()
	l := y.lookup()
	gone, leave := context.WithCancel(context.Background())
	left := make(chan error, 1)
	go func() {
		_, err := l.Video(gone, id)
		left <- err
	}()
	for deadline := time.Now().Add(5 * time.Second); y.times("/oembed") == 0 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	leave()
	if err := <-left; !errors.Is(err, context.Canceled) {
		t.Fatalf("the cover that left got %v", err)
	}
	stayed := make(chan Video, 1)
	go func() {
		v, _ := l.Video(context.Background(), id)
		stayed <- v
	}()
	close(hold)
	if v := <-stayed; v.Title != "Still here" || y.times("/oembed") != 1 {
		t.Errorf("the cover that stayed got %+v after %d fetches", v, y.times("/oembed"))
	}
}

func TestWhatWasFoundIsKeptAnHourAndAFailureAMinute(t *testing.T) {
	y := newYouTube(t)
	y.answer("/oembed", oEmbed(t, `{"type":"video","title":"Kept"}`))
	l := y.lookup()
	now := time.Now()
	l.now = func() time.Time { return now }
	ask := func() error {
		_, err := l.Video(context.Background(), id)
		return err
	}
	for _, step := range []struct {
		after time.Duration
		times int
	}{{0, 1}, {59 * time.Minute, 1}, {2 * time.Minute, 2}} {
		now = now.Add(step.after)
		if err := ask(); err != nil {
			t.Fatal(err)
		}
		if n := y.times("/oembed"); n != step.times {
			t.Fatalf("after %v: asked %d times, want %d", step.after, n, step.times)
		}
	}

	y.answer("/oembed", status(http.StatusServiceUnavailable))
	now = now.Add(2 * time.Hour)
	for _, step := range []struct {
		after time.Duration
		times int
	}{{0, 3}, {30 * time.Second, 3}, {time.Minute, 4}} {
		now = now.Add(step.after)
		if err := ask(); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("after %v: %v", step.after, err)
		}
		if n := y.times("/oembed"); n != step.times {
			t.Fatalf("after %v: asked %d times, want %d", step.after, n, step.times)
		}
	}
}

func TestTheCacheStaysWithinItsBudget(t *testing.T) {
	l := newLookup("", "", nil)
	for i := range maxEntries + 10 {
		l.mu.Lock()
		l.store(&entry{key: string(rune('a'+i%26)) + strings.Repeat("x", i), until: time.Now().Add(time.Hour), size: 64})
		l.mu.Unlock()
	}
	if l.order.Len() != maxEntries || len(l.entries) != maxEntries {
		t.Errorf("%d entries kept, want %d", l.order.Len(), maxEntries)
	}
	big := make([]byte, maxPicture)
	for i := range maxBytes/maxPicture + 5 {
		l.mu.Lock()
		l.store(&entry{key: "picture " + strings.Repeat("y", i), value: big, until: time.Now().Add(time.Hour), size: len(big)})
		l.mu.Unlock()
	}
	if l.bytes > maxBytes {
		t.Errorf("%d bytes kept, over %d", l.bytes, maxBytes)
	}
}
