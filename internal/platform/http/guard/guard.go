// Package guard holds the request checks every Dens listener applies.
package guard

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// SecurityHeaders sets a strict policy on every response: scripts, styles,
// media and connections only from the page's own origin, no framing, no
// referrer, and no MIME sniffing.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; "+
			"img-src 'self' data:; media-src 'self'; font-src 'self'; connect-src 'self'; form-action 'self'; "+
			"frame-ancestors 'none'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), geolocation=(), payment=(), usb=()")
		next.ServeHTTP(w, r)
	})
}

// LoopbackHosts are the Host header values a browser sends for the client
// listener on port. An exact match defeats DNS rebinding: a hostile page
// that rebinds its own name to 127.0.0.1 still sends its own name.
func LoopbackHosts(port int) map[string]bool {
	p := strconv.Itoa(port)
	return map[string]bool{
		"127.0.0.1:" + p: true,
		"localhost:" + p: true,
		"[::1]:" + p:     true,
	}
}

// Host rejects requests whose Host header isn't in allowed.
func Host(allowed map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !allowed[strings.ToLower(r.Host)] {
				http.Error(w, "unexpected Host header", http.StatusMisdirectedRequest)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SameOrigin rejects state-changing requests that don't come from the page
// itself: they must carry an Origin equal to the request's own origin, must
// not be marked cross-site by Sec-Fetch-Site, and must send JSON, or raw
// bytes for an upload. Neither is a type a form can send, and both make
// another site's script ask permission first, which it never gets.
func SameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		origin, err := url.Parse(r.Header.Get("Origin"))
		if err != nil || origin.Scheme != "http" || !strings.EqualFold(origin.Host, r.Host) || origin.Path != "" {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
			http.Error(w, "cross-site request rejected", http.StatusForbidden)
			return
		}
		if ct := r.Header.Get("Content-Type"); r.ContentLength != 0 && !strings.HasPrefix(ct, "application/json") && ct != "application/octet-stream" {
			http.Error(w, "expected application/json", http.StatusUnsupportedMediaType)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// IsLoopback reports whether addr (host:port) is a loopback address.
func IsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
