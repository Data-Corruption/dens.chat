// Command serve hosts the RNNoise spike's page under Dens's own CSP, with
// 'wasm-unsafe-eval' added, and collects what each browser measures. Under
// /strict/ the page gets the CSP as it is today, to show the module can't
// compile without that addition.
//
// Run from the repository's root, after spikes/rnnoise/build.sh:
//
//	(cd spikes && go run ./rnnoise/serve -addr 127.0.0.1:38590)
package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The CSP internal/platform/http/guard sets on the page.
const csp = "default-src 'none'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data:; media-src 'self'; font-src 'self'; connect-src 'self'; form-action 'self'; " +
	"frame-ancestors 'none'; base-uri 'none'"

func main() {
	addr := flag.String("addr", "127.0.0.1:38590", "where to listen")
	root := flag.String("root", "..", "the repository's root")
	flag.Parse()
	page := filepath.Join(*root, "spikes/rnnoise/page")
	built := filepath.Join(*root, "out/spikes/rnnoise")

	var mu sync.Mutex
	results := map[string]json.RawMessage{}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /results", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var head struct{ Label string }
		if err := json.Unmarshal(body, &head); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		results[head.Label] = body
		mu.Unlock()
		log.Printf("results from %s", head.Label)
	})
	mux.HandleFunc("POST /audio", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Query().Get("name"))
		if !strings.HasSuffix(name, ".wav") {
			http.Error(w, "a .wav name", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
		if err == nil {
			err = os.MkdirAll(filepath.Join(built, "listen"), 0o755)
		}
		if err == nil {
			err = os.WriteFile(filepath.Join(built, "listen", name), body, 0o644)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("GET /results", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		body, ok := results[r.URL.Query().Get("label")]
		mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		policy := strings.Replace(csp, "script-src 'self'", "script-src 'self' 'wasm-unsafe-eval'", 1)
		path := r.URL.Path
		if rest, ok := strings.CutPrefix(path, "/strict"); ok && (rest == "" || rest == "/") {
			policy, path = csp, "/"
		}
		h := w.Header()
		h.Set("Content-Security-Policy", policy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=(self), payment=(), usb=()")
		h.Set("Cache-Control", "no-store")
		var file string
		switch name := strings.TrimPrefix(path, "/"); {
		case name == "":
			file = filepath.Join(page, "index.html")
		case name == "spike.js" || name == "worklet.js":
			file = filepath.Join(page, name)
		case strings.HasSuffix(name, ".wasm") || strings.HasSuffix(name, ".wav"):
			file = filepath.Join(built, filepath.Base(name))
		default:
			http.NotFound(w, r)
			return
		}
		switch filepath.Ext(file) {
		case ".js":
			h.Set("Content-Type", "text/javascript")
		case ".wasm":
			h.Set("Content-Type", "application/wasm")
		case ".wav":
			h.Set("Content-Type", "audio/wav")
		case ".html":
			h.Set("Content-Type", "text/html; charset=utf-8")
		}
		data, err := os.ReadFile(file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	})
	log.Printf("serving the RNNoise spike on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
