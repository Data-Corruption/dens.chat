package denclient

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
