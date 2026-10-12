package denclient

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func TestExplain(t *testing.T) {
	secure := httptest.NewTLSServer(http.NotFoundHandler())
	defer secure.Close()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := "https://" + ln.Addr().String()
	ln.Close()

	client := &http.Client{Transport: &http.Transport{}}
	for url, want := range map[string]string{
		secure.URL:                     "isn't from an authority this computer trusts",
		closed:                         "Nothing is answering",
		"https://den.invalid.example/": "doesn't resolve",
	} {
		_, err := client.Get(url)
		if got := Explain(err); !strings.Contains(got, want) {
			t.Errorf("%s: explained %q (%v), want %q", url, got, err, want)
		}
	}
}

func TestExplainStatus(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusBadGateway, "", "the den behind it doesn't (HTTP 502)"},
		{http.StatusForbidden, "Forbidden", "doesn't answer like a den (HTTP 403)"},
		{http.StatusNotFound, `{"error": {"code": "not_found"}}`, ""},
	} {
		rec := httptest.NewRecorder()
		rec.WriteHeader(tc.status)
		rec.WriteString(tc.body)
		perr := denproto.ReadError(rec.Result()).(*denproto.Error)
		got, ok := ExplainStatus(perr)
		if ok != (tc.want != "") || !strings.Contains(got, tc.want) {
			t.Errorf("%d %q: explained %q %v, want %q", tc.status, tc.body, got, ok, tc.want)
		}
	}
}

func TestDescribeMoved(t *testing.T) {
	down := &denproto.Error{Status: http.StatusBadGateway, Code: "http_502"}
	got := describe(&MovedError{URL: "https://new.example.com", Err: down})
	if !strings.HasPrefix(got, "The den moved to https://new.example.com. Something answers at the den's address") {
		t.Fatalf("%q", got)
	}
}

// TestUnknownCodesArentShown checks that a den's error code reaches the
// member only when it's one of the protocol's (M8): the code is the den's
// own text, which could say anything.
func TestUnknownCodesArentShown(t *testing.T) {
	for body, want := range map[string]string{
		`{"error": {"code": "banned"}}`:                                       "The den refused the connection (banned).",
		`{"error": {"code": "your_session_expired_sign_in_at_evil.example"}}`: "The den refused the connection (HTTP 403).",
		`{"error": {"code": "` + strings.Repeat("x", 4096) + `"}}`:            "The den refused the connection (HTTP 403).",
	} {
		rec := httptest.NewRecorder()
		rec.WriteHeader(http.StatusForbidden)
		rec.WriteString(body)
		if got := describe(denproto.ReadError(rec.Result())); got != want {
			t.Errorf("%.60s: described as %q, want %q", body, got, want)
		}
	}
}

// TestLoggedErrors checks that a network error goes into a log line without
// the address Go's error names (M8), and any other error as it is.
func TestLoggedErrors(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	_, err = (&http.Client{}).Get("http://" + addr)
	if err == nil || !strings.Contains(err.Error(), addr) {
		t.Fatalf("Go's error doesn't name the address: %v", err)
	}
	if got := fmt.Sprint(logged(fmt.Errorf("reach the den: %w", err))); strings.Contains(got, "127.0.0.1") {
		t.Fatalf("logged %q", got)
	}
	other := errors.New("the den's response is malformed")
	if got := logged(other); got != other {
		t.Fatalf("logged %v", got)
	}
}
