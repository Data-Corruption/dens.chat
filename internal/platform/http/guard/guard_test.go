package guard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var ok = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })

func TestHostGuardRequiresExactLoopbackHost(t *testing.T) {
	h := Host(LoopbackHosts(8484))(ok)
	for host, want := range map[string]int{
		"127.0.0.1:8484":        http.StatusNoContent,
		"localhost:8484":        http.StatusNoContent,
		"LOCALHOST:8484":        http.StatusNoContent,
		"[::1]:8484":            http.StatusNoContent,
		"127.0.0.1:9999":        http.StatusMisdirectedRequest,
		"evil.example:8484":     http.StatusMisdirectedRequest,
		"127.0.0.1":             http.StatusMisdirectedRequest,
		"rebind.attacker.test":  http.StatusMisdirectedRequest,
		"127.0.0.1.nip.io:8484": http.StatusMisdirectedRequest,
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("Host %q: %d, want %d", host, w.Code, want)
		}
	}
}

func TestSameOriginGuard(t *testing.T) {
	h := SameOrigin(ok)
	cases := []struct {
		name    string
		method  string
		origin  string
		site    string
		ctype   string
		body    string
		wantErr bool
	}{
		{"get needs nothing", http.MethodGet, "", "", "", "", false},
		{"same origin json", http.MethodPost, "http://127.0.0.1:8484", "same-origin", "application/json", "{}", false},
		{"missing origin", http.MethodPost, "", "", "application/json", "{}", true},
		{"other origin", http.MethodPost, "http://evil.example", "", "application/json", "{}", true},
		{"other port", http.MethodPost, "http://127.0.0.1:9999", "", "application/json", "{}", true},
		{"https origin", http.MethodPost, "https://127.0.0.1:8484", "", "application/json", "{}", true},
		{"null origin", http.MethodPost, "null", "", "application/json", "{}", true},
		{"cross-site fetch", http.MethodPost, "http://127.0.0.1:8484", "cross-site", "application/json", "{}", true},
		{"form post", http.MethodPost, "http://127.0.0.1:8484", "same-origin", "application/x-www-form-urlencoded", "a=b", true},
		{"upload", http.MethodPost, "http://127.0.0.1:8484", "same-origin", "application/octet-stream", "\xff\xd8", false},
		{"multipart form", http.MethodPost, "http://127.0.0.1:8484", "same-origin", "multipart/form-data; boundary=x", "--x", true},
		{"text form", http.MethodPost, "http://127.0.0.1:8484", "same-origin", "text/plain", "hi", true},
		{"upload from elsewhere", http.MethodPost, "http://evil.example", "cross-site", "application/octet-stream", "x", true},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, "/api/x", strings.NewReader(c.body))
		r.Host = "127.0.0.1:8484"
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.site != "" {
			r.Header.Set("Sec-Fetch-Site", c.site)
		}
		if c.ctype != "" {
			r.Header.Set("Content-Type", c.ctype)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if gotErr := w.Code != http.StatusNoContent; gotErr != c.wantErr {
			t.Errorf("%s: status %d", c.name, w.Code)
		}
	}
}

// The page compiles RNNoise, so its policy allows WebAssembly, but not
// eval or inline code; the den's never serves a page, and allows neither.
func TestPageSecurityHeaders(t *testing.T) {
	csp := func(h func(http.Handler) http.Handler) string {
		w := httptest.NewRecorder()
		h(ok).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		return w.Header().Get("Content-Security-Policy")
	}
	page := csp(PageSecurityHeaders)
	if !strings.Contains(page, "script-src 'self' 'wasm-unsafe-eval';") {
		t.Errorf("the page's CSP doesn't allow WebAssembly: %q", page)
	}
	for _, not := range []string{"'unsafe-eval'", "unsafe-inline"} {
		if strings.Contains(page, not) {
			t.Errorf("the page's CSP allows %s: %q", not, page)
		}
	}
	if den := csp(SecurityHeaders); strings.Contains(den, "wasm") {
		t.Errorf("the den's CSP allows WebAssembly: %q", den)
	}
	// The page's one frame is YouTube's player (M4.1); the den frames
	// nothing.
	if !strings.Contains(page, "; frame-src https://www.youtube-nocookie.com;") || strings.Count(page, "frame-src") != 1 {
		t.Errorf("the page's CSP should frame YouTube's player alone: %q", page)
	}
	if den := csp(SecurityHeaders); strings.Contains(den, "frame-src") {
		t.Errorf("the den's CSP allows frames: %q", den)
	}
}

func TestSecurityHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	SecurityHeaders(ok).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "script-src 'self'", "media-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q missing %q", csp, want)
		}
	}
	if strings.Contains(csp, "unsafe-inline") {
		t.Errorf("CSP allows inline code: %q", csp)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("nosniff missing")
	}
	// Calls take the microphone, for the page alone; nothing takes the camera.
	policy := w.Header().Get("Permissions-Policy")
	for _, want := range []string{"microphone=(self)", "camera=()"} {
		if !strings.Contains(policy, want) {
			t.Errorf("Permissions-Policy %q missing %q", policy, want)
		}
	}
}
