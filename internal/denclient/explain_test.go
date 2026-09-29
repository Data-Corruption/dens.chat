package denclient

import (
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
