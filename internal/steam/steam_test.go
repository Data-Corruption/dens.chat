package steam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// steamStandIn stands in for Steam: what it answers for each path, how
// often it was asked, and whether a request carried anything about its
// sender. The answers in testdata are Steam's own, recorded.
type steamStandIn struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	answers map[string]http.HandlerFunc
	asked   map[string]int
	queries map[string][]string
}

func newSteam(t *testing.T) *steamStandIn {
	s := &steamStandIn{t: t, answers: map[string]http.HandlerFunc{}, asked: map[string]int{}, queries: map[string][]string{}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range []string{"User-Agent", "Cookie", "Referer"} {
			if _, ok := r.Header[h]; ok {
				t.Errorf("%s asked with a %s header: %q", r.URL.Path, h, r.Header.Get(h))
			}
		}
		s.mu.Lock()
		s.asked[r.URL.Path]++
		s.queries[r.URL.Path] = append(s.queries[r.URL.Path], r.URL.RawQuery)
		h := s.answers[r.URL.Path]
		s.mu.Unlock()
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *steamStandIn) answer(path string, h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.answers[path] = h
}

func (s *steamStandIn) times(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asked[path]
}

func (s *steamStandIn) lookup() *Lookup {
	u := s.srv.URL
	return newLookup(sites{items: u + "/items", price: u + "/price", players: u + "/players", tags: u + "/tags", assets: u + "/assets/"}, s.srv.Client().Transport)
}

// recorded answers with one of Steam's recorded answers.
func recorded(t *testing.T, name string) http.HandlerFunc {
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return jsonAnswer(string(b))
}

func jsonAnswer(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		_, _ = w.Write([]byte(body))
	}
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }
}

// edited answers with a recorded answer for an app, changed by edit.
func edited(t *testing.T, name string, edit func(item map[string]any)) http.HandlerFunc {
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var answer map[string]map[string][]map[string]any
	if err := json.Unmarshal(b, &answer); err != nil {
		t.Fatal(err)
	}
	edit(answer["response"]["store_items"][0])
	out, _ := json.Marshal(answer)
	return jsonAnswer(string(out))
}

func picture(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, x%h, color.RGBA{30, 60, 200, 255})
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

func TestACardFromSteamsAnswers(t *testing.T) {
	s := newSteam(t)
	s.answer("/items", recorded(t, "items-1771300.json"))
	s.answer("/price", recorded(t, "price-1771300.json"))
	s.answer("/players", recorded(t, "players-1771300.json"))
	s.answer("/tags", recorded(t, "tags.json"))
	c, err := s.lookup().Card(context.Background(), "1771300")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Kingdom Come: Deliverance II" || c.Developer != "Warhorse Studios" || c.Kind != "game" || !strings.HasPrefix(c.Description, "A thrilling story-driven action RPG") {
		t.Errorf("card %+v", c)
	}
	if strings.Join(c.Tags, ",") != "RPG,Medieval,Open World" {
		t.Errorf("tags %q", c.Tags)
	}
	if c.Reviews == nil || *c.Reviews != (Reviews{Score: 8, Percent: 93, Count: 148458}) {
		t.Errorf("reviews %+v", c.Reviews)
	}
	if c.Players != 7511 || c.Platforms != (Platforms{Windows: true}) || c.Deck != "playable" || c.Released != 1738683276 || c.ComingSoon || c.Free || c.Adult || c.Shots != 4 {
		t.Errorf("card %+v", c)
	}
	if c.Price == nil || *c.Price != (Price{Final: "$19.79", Original: "$59.99", Discount: 67, Ends: 1796061600}) {
		t.Errorf("price %+v", c.Price)
	}

	// The store is asked in English for the US, and the price for nowhere,
	// which Steam takes to be where the member is.
	var input struct {
		IDs     []map[string]uint32 `json:"ids"`
		Context map[string]string   `json:"context"`
	}
	q := s.queries["/items"][0]
	if err := json.Unmarshal([]byte(must(t, q, "input_json")), &input); err != nil || input.IDs[0]["appid"] != 1771300 || input.Context["country_code"] != "US" || input.Context["language"] != "english" {
		t.Errorf("GetItems asked %q", q)
	}
	if p := s.queries["/price"][0]; must(t, p, "appids") != "1771300" || must(t, p, "filters") != "price_overview" || strings.Contains(p, "cc=") {
		t.Errorf("appdetails asked %q", p)
	}
}

func must(t *testing.T, query, name string) string {
	t.Helper()
	for _, p := range strings.Split(query, "&") {
		if k, v, ok := strings.Cut(p, "="); ok && k == name {
			out, err := url.QueryUnescape(v)
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
	}
	return ""
}

func TestAFreeGameOnLinux(t *testing.T) {
	s := newSteam(t)
	s.answer("/items", recorded(t, "items-730.json"))
	s.answer("/price", recorded(t, "price-730.json"))
	s.answer("/tags", recorded(t, "tags.json"))
	// The count fails, and the card goes without it.
	s.answer("/players", status(http.StatusServiceUnavailable))
	c, err := s.lookup().Card(context.Background(), "730")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Counter-Strike 2" || !c.Free || c.Price != nil || c.Players != 0 || c.Platforms != (Platforms{Windows: true, Linux: true}) || strings.Join(c.Tags, ",") != "FPS,Shooter,Multiplayer" {
		t.Errorf("card %+v", c)
	}
}

func TestAGameComingSoon(t *testing.T) {
	s := newSteam(t)
	s.answer("/items", recorded(t, "items-5503390.json"))
	s.answer("/price", jsonAnswer(`{"5503390":{"success":true,"data":[]}}`))
	s.answer("/players", recorded(t, "players-5503390.json"))
	c, err := s.lookup().Card(context.Background(), "5503390")
	if err != nil {
		t.Fatal(err)
	}
	if !c.ComingSoon || c.ReleaseNote != "Coming soon" || c.Released != 0 || c.Reviews != nil || c.Players != 0 || c.Deck != "" || c.Price != nil || c.Tags != nil {
		t.Errorf("card %+v", c)
	}
}

func TestAnAppSteamDoesntHave(t *testing.T) {
	s := newSteam(t)
	s.answer("/items", recorded(t, "items-99999999.json"))
	l := s.lookup()
	for range 2 {
		if _, err := l.Card(context.Background(), "99999999"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a missing app: %v", err)
		}
	}
	// It's kept as an app would be, and nothing more is asked about it.
	if s.times("/items") != 1 || s.times("/price") != 0 {
		t.Errorf("asked %d times, and for its price %d", s.times("/items"), s.times("/price"))
	}
}

func TestOnlyAppIDsAreAskedFor(t *testing.T) {
	s := newSteam(t)
	l := s.lookup()
	for _, id := range []string{"", "0", "01", "abc", "-1", "1e3", "12 ", "4294967296", "12345678901", "730/../1", "７３０"} {
		if _, err := l.Card(context.Background(), id); !errors.Is(err, ErrBadID) {
			t.Errorf("%q: %v", id, err)
		}
		if _, err := l.Header(context.Background(), id); !errors.Is(err, ErrBadID) {
			t.Errorf("%q's header: %v", id, err)
		}
	}
	if _, err := l.Card(context.Background(), "4294967295"); errors.Is(err, ErrBadID) {
		t.Error("Steam's largest ID was refused")
	}
	if s.times("/items") != 1 {
		t.Errorf("asked %d times", s.times("/items"))
	}
}

// What Steam says is shown to members, so it's taken as hostile: text is
// cleaned as names are, and numbers that can't be what they say are left
// out.
func TestSteamsTextAndNumbersAreChecked(t *testing.T) {
	s := newSteam(t)
	s.answer("/items", edited(t, "items-1771300.json", func(it map[string]any) {
		it["name"] = "Kingdom‮ Come​\n II"
		it["basic_info"].(map[string]any)["short_description"] = "Tom &amp; Jerry\x00 " + strings.Repeat("long ", 200)
		it["basic_info"].(map[string]any)["developers"] = []any{map[string]any{"name": "⁧Warhorse⁩"}}
		it["reviews"] = map[string]any{"summary_filtered": map[string]any{"review_count": 10, "percent_positive": 150, "review_score": 8}}
		it["release"] = map[string]any{"steam_release_date": 99999999999, "custom_release_date_message": "<b>soon</b>‮"}
		it["platforms"] = map[string]any{"windows": true, "steam_deck_compat_category": 9}
		it["content_descriptorids"] = []any{1, 3}
		it["type"] = 77
	}))
	s.answer("/price", jsonAnswer(`{"1771300":{"success":true,"data":{"price_overview":{"discount_percent":400,"initial_formatted":"$1","final_formatted":"`+"‮$9.99"+strings.Repeat("9", 60)+`"}}}}`))
	s.answer("/players", jsonAnswer(`{"response":{"player_count":-5,"result":1}}`))
	s.answer("/tags", jsonAnswer(`{"response":{"tags":[{"tagid":122,"name":"R‮PG"},{"tagid":4172,"name":""},{"tagid":-1,"name":"x"}]}}`))
	c, err := s.lookup().Card(context.Background(), "1771300")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Kingdom Come II" || c.Developer != "Warhorse" || c.ReleaseNote != "<b>soon</b>" || c.Kind != "" || c.Deck != "" || !c.Adult {
		t.Errorf("card %+v", c)
	}
	if !strings.HasPrefix(c.Description, "Tom & Jerry long") || len([]rune(c.Description)) > maxDescription {
		t.Errorf("description %q", c.Description)
	}
	if c.Reviews != nil || c.Released != 0 || c.Players != 0 || strings.Join(c.Tags, ",") != "RPG" {
		t.Errorf("card %+v", c)
	}
	if c.Price == nil || c.Price.Discount != 0 || c.Price.Original != "" || c.Price.Ends != 0 || !strings.HasPrefix(c.Price.Final, "$9.99") || len([]rune(c.Price.Final)) > maxPriceText {
		t.Errorf("price %+v", c.Price)
	}
}

// A sale's end is the store's in the US, so it's shown only where the
// member's discount is the same one.
func TestASalesEndOnlyWithItsDiscount(t *testing.T) {
	s := newSteam(t)
	s.answer("/items", recorded(t, "items-1771300.json"))
	s.answer("/price", jsonAnswer(`{"1771300":{"success":true,"data":{"price_overview":{"discount_percent":50,"initial_formatted":"59,99€","final_formatted":"29,99€"}}}}`))
	c, err := s.lookup().Card(context.Background(), "1771300")
	if err != nil {
		t.Fatal(err)
	}
	if c.Price == nil || *c.Price != (Price{Final: "29,99€", Original: "59,99€", Discount: 50}) {
		t.Errorf("price %+v", c.Price)
	}
}

func TestAnswersThatArentSteams(t *testing.T) {
	big := `{"response":{"store_items":[{"id":730,"success":1,"name":"` + strings.Repeat("x", maxItems) + `"}]}}`
	for name, h := range map[string]http.HandlerFunc{
		"down": status(http.StatusServiceUnavailable),
		"a redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://evil.example/", http.StatusFound)
		},
		"HTML": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>"))
		},
		"too long":     jsonAnswer(big),
		"wrong types":  jsonAnswer(`{"response":{"store_items":[{"id":730,"success":1,"name":42}]}}`),
		"not JSON":     jsonAnswer(`{"response":`),
		"another app":  jsonAnswer(`{"response":{"store_items":[{"id":570,"success":1,"name":"Dota 2"}]}}`),
		"two apps":     jsonAnswer(`{"response":{"store_items":[{"id":730,"success":1,"name":"A"},{"id":730,"success":1,"name":"B"}]}}`),
		"none":         jsonAnswer(`{"response":{}}`),
		"no name":      jsonAnswer(`{"response":{"store_items":[{"id":730,"success":1,"name":"‮​"}]}}`),
		"absurd count": jsonAnswer(`{"response":{"store_items":[{"id":730,"success":1,"name":"A","reviews":{"summary_filtered":{"review_count":1e30}}}]}}`),
	} {
		t.Run(name, func(t *testing.T) {
			s := newSteam(t)
			s.answer("/items", h)
			if _, err := s.lookup().Card(context.Background(), "730"); !errors.Is(err, ErrUnavailable) {
				t.Errorf("%v", err)
			}
		})
	}
}

func TestTheHeaderIsTwiceTheSizeWhereSteamHasIt(t *testing.T) {
	s := newSteam(t)
	s.answer("/items", recorded(t, "items-730.json"))
	large, small := picture(t, 920, 430), picture(t, 460, 215)
	dir := "/assets/steam/apps/730/162664aa5da85f418105350c5d67ca565f6c3713/"
	s.answer(dir+"header_2x.jpg", serve(large))
	s.answer(dir+"header.jpg", serve(small))
	l := s.lookup()
	got, err := l.Header(context.Background(), "730")
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _ := jpeg.DecodeConfig(bytes.NewReader(got)); cfg.Width != 920 {
		t.Errorf("header %d wide", cfg.Width)
	}
	if s.queries[dir+"header_2x.jpg"][0] != "t=1793054556" {
		t.Errorf("asked with %q", s.queries[dir+"header_2x.jpg"])
	}

	// Without the larger one, the usual one.
	s2 := newSteam(t)
	s2.answer("/items", recorded(t, "items-730.json"))
	s2.answer(dir+"header.jpg", serve(small))
	got, err = s2.lookup().Header(context.Background(), "730")
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _ := jpeg.DecodeConfig(bytes.NewReader(got)); cfg.Width != 460 {
		t.Errorf("header %d wide", cfg.Width)
	}
}

func TestScreenshotsAreThumbnailsForAllAges(t *testing.T) {
	s := newSteam(t)
	s.answer("/items", edited(t, "items-1771300.json", func(it map[string]any) {
		it["screenshots"].(map[string]any)["mature_content_screenshots"] = []any{map[string]any{"filename": "steam/apps/1771300/ss_mature.jpg"}}
	}))
	shot := picture(t, 600, 338)
	s.answer("/assets/steam/apps/1771300/ss_709f3329562cd2cfe6db70bac908b3e72eaef83b.600x338.jpg", serve(shot))
	l := s.lookup()
	got, err := l.Shot(context.Background(), "1771300", 2)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _ := jpeg.DecodeConfig(bytes.NewReader(got)); cfg.Width != 600 {
		t.Errorf("shot %d wide", cfg.Width)
	}
	for _, n := range []int{0, 5, -1} {
		if _, err := l.Shot(context.Background(), "1771300", n); !errors.Is(err, ErrNotFound) {
			t.Errorf("shot %d: %v", n, err)
		}
	}
	for path := range s.asked {
		if strings.Contains(path, "mature") {
			t.Errorf("asked for %s", path)
		}
	}
}

func TestPicturesThatArentServed(t *testing.T) {
	var png8 bytes.Buffer
	_ = png.Encode(&png8, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	for name, h := range map[string]http.HandlerFunc{
		"a PNG":     serve(png8.Bytes()),
		"too large": serve(append(picture(t, 460, 215), make([]byte, maxPicture)...)),
		"too wide":  serve(picture(t, 3000, 100)),
		"a redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://evil.example/x.jpg", http.StatusFound)
		},
		"down": status(http.StatusBadGateway),
	} {
		t.Run(name, func(t *testing.T) {
			s := newSteam(t)
			s.answer("/items", recorded(t, "items-1771300.json"))
			s.answer("/assets/steam/apps/1771300/header.jpg", h)
			if _, err := s.lookup().Header(context.Background(), "1771300"); !errors.Is(err, ErrUnavailable) {
				t.Errorf("%v", err)
			}
		})
	}
}

// A picture's path comes from Steam's answer, so it's taken only inside
// the app's directory on the store's host.
func TestPicturePathsStayInTheAppsDirectory(t *testing.T) {
	for _, tc := range []struct{ format, name, want string }{
		{"steam/apps/730/${FILENAME}?t=12", "header.jpg", "steam/apps/730/header.jpg?t=12"},
		{"steam/apps/730/${FILENAME}", "abc123/header_2x.jpg", "steam/apps/730/abc123/header_2x.jpg"},
		{"steam/apps/730/${FILENAME}", "../../570/header.jpg", ""},
		{"steam/apps/730/${FILENAME}", "a/../header.jpg", ""},
		{"steam/apps/730/${FILENAME}", "a/b/header.jpg", ""},
		{"steam/apps/730/${FILENAME}", "header.png", ""},
		{"steam/apps/730/${FILENAME}", "", ""},
		{"steam/apps/730/${FILENAME}", "x/.jpg", ""},
		{"steam/apps/7300/${FILENAME}", "header.jpg", ""},
		{"steam/apps/730${FILENAME}", "/header.jpg", "steam/apps/730/header.jpg"},
		{"//evil.example/${FILENAME}", "header.jpg", ""},
		{"steam/apps/730/${FILENAME}?q=1", "header.jpg", ""},
		{"steam/apps/730/${FILENAME}?t=12&u=1", "header.jpg", ""},
		{"steam/apps/730/${FILENAME}${FILENAME}", "header.jpg", ""},
		{"steam/apps/730/${FILENAME}", "he%2fader.jpg", ""},
		{"steam/apps/730/${FILENAME}", "head er.jpg", ""},
	} {
		if got := assetPath("730", tc.format, tc.name); got != tc.want {
			t.Errorf("%q with %q: %q, want %q", tc.format, tc.name, got, tc.want)
		}
	}
	if got := thumbnail("730", "steam/apps/730/abc/ss_abc.jpg?t=1"); got != "steam/apps/730/abc/ss_abc.600x338.jpg?t=1" {
		t.Errorf("thumbnail %q", got)
	}
	if got := thumbnail("730", "steam/apps/570/ss_abc.jpg"); got != "" {
		t.Errorf("another app's thumbnail %q", got)
	}
}

func TestTheCountIsKeptTenMinutesAndTheRestAnHour(t *testing.T) {
	s := newSteam(t)
	s.answer("/items", recorded(t, "items-1771300.json"))
	s.answer("/players", recorded(t, "players-1771300.json"))
	l := s.lookup()
	now := time.Now()
	l.now = func() time.Time { return now }
	for _, step := range []struct {
		after          time.Duration
		items, players int
	}{{0, 1, 1}, {9 * time.Minute, 1, 1}, {2 * time.Minute, 1, 2}, {50 * time.Minute, 2, 3}} {
		now = now.Add(step.after)
		if _, err := l.Card(context.Background(), "1771300"); err != nil {
			t.Fatal(err)
		}
		if s.times("/items") != step.items || s.times("/players") != step.players {
			t.Fatalf("after %v: the app asked %d times and the count %d", step.after, s.times("/items"), s.times("/players"))
		}
	}
	// The tag list failed each time, and was asked again each minute at
	// most.
	if n := s.times("/tags"); n != 4 {
		t.Errorf("the tags asked %d times", n)
	}
}
