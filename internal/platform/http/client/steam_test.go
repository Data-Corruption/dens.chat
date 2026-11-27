package client

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/steam"

	"github.com/go-chi/chi/v5"
)

type fakeSteam struct {
	trailer steam.Trailer
	init    []byte
	seg     []byte
	mime    string
	err     error
	lastSeg int
}

func (f *fakeSteam) Card(context.Context, string) (steam.Card, error) {
	return steam.Card{}, f.err
}
func (f *fakeSteam) Header(context.Context, string) ([]byte, error) { return nil, f.err }
func (f *fakeSteam) Shot(context.Context, string, int) ([]byte, error) {
	return nil, f.err
}
func (f *fakeSteam) Trailer(context.Context, string, string) (steam.Trailer, error) {
	return f.trailer, f.err
}
func (f *fakeSteam) TrailerInit(context.Context, string, string, string) ([]byte, string, error) {
	return f.init, f.mime, f.err
}
func (f *fakeSteam) TrailerSegment(_ context.Context, _, _, _ string, seg int) ([]byte, string, error) {
	f.lastSeg = seg
	return f.seg, f.mime, f.err
}

func TestSteamTrailerRoutesAndHeaders(t *testing.T) {
	f := &fakeSteam{
		trailer: steam.Trailer{
			Encoding: "h264",
			Duration: 6,
			Video:    []steam.TrailerTrack{{ID: "0", Width: 854, Height: 480, Mime: "video/mp4", Codecs: "avc1.640029", Segments: 2, SegmentDuration: 3}},
			Audio:    &steam.TrailerTrack{ID: "1", Mime: "audio/mp4", Codecs: "mp4a.40.2", Segments: 2, SegmentDuration: 3},
		},
		init: []byte{0, 0, 0, 12, 'f', 't', 'y', 'p', 'i', 's', 'o', '5'},
		seg:  []byte{0, 0, 0, 12, 's', 't', 'y', 'p', 'm', 's', 'd', 'h'},
		mime: "video/mp4",
	}
	r := chi.NewRouter()
	(&router{steam: f}).mountSteam(r)

	// Manifest endpoint returns JSON descriptor with Cache-Control: no-store.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/steam/1771300/trailer/h264", nil))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("manifest: %d Cache-Control=%q", w.Code, w.Header().Get("Cache-Control"))
	}
	var got steam.Trailer
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Encoding != "h264" || len(got.Video) != 1 || got.Audio == nil {
		t.Fatalf("manifest body: %s (%v)", w.Body.String(), err)
	}

	// Init segment endpoint sets strict headers and Content-Type.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/steam/1771300/trailer/h264/0/init", nil))
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), f.init) {
		t.Fatalf("init: %d len=%d", w.Code, w.Body.Len())
	}
	for k, want := range map[string]string{
		"Cache-Control":           "no-store",
		"Content-Security-Policy": "default-src 'none'; sandbox",
		"X-Content-Type-Options":  "nosniff",
		"Content-Type":            "video/mp4",
	} {
		if got := w.Header().Get(k); got != want {
			t.Errorf("init header %s = %q, want %q", k, got, want)
		}
	}

	// Media chunk endpoint routes independently from /init and serves audio/mp4 too.
	f.mime = "audio/mp4"
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/steam/1771300/trailer/h264/1/2", nil))
	if w.Code != http.StatusOK || f.lastSeg != 2 || !bytes.Equal(w.Body.Bytes(), f.seg) || w.Header().Get("Content-Type") != "audio/mp4" {
		t.Fatalf("segment: %d seg=%d ct=%q", w.Code, f.lastSeg, w.Header().Get("Content-Type"))
	}
	if w.Header().Get("Content-Security-Policy") != "default-src 'none'; sandbox" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("segment headers missing: %+v", w.Header())
	}

	// Bad segment indices get 404 before calling Steam.
	for _, bad := range []string{"0", "-1", "01", "abc", "1000000"} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/steam/1771300/trailer/h264/0/"+bad, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("seg %q: %d, want 404", bad, w.Code)
		}
	}

	// Error mapping: ErrBadID -> 400, ErrNotFound -> 404, ErrUnavailable -> 502.
	for _, tc := range []struct {
		err  error
		want int
	}{
		{steam.ErrBadID, http.StatusBadRequest},
		{steam.ErrNotFound, http.StatusNotFound},
		{steam.ErrUnavailable, http.StatusBadGateway},
	} {
		f.err = tc.err
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/steam/1771300/trailer/h264", nil))
		if w.Code != tc.want {
			t.Errorf("%v: %d, want %d", tc.err, w.Code, tc.want)
		}
	}
}
