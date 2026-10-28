package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/Data-Corruption/dens.chat/internal/ui"
)

//go:embed web
var web embed.FS

// serve answers the shell's WebView on localhost with the feature page,
// Dens's own voice worklet and RNNoise's module, and takes the page's
// results into features.json. It's for phones the browser checks can't
// drive over adb; on the others, the checks' driver runs the same page.
func serve(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: aprobe serve PORT")
	}
	page, err := ui.New()
	if err != nil {
		return err
	}
	asset := func(rel, contentType string) http.HandlerFunc {
		a := page.Assets[rel]
		return func(w http.ResponseWriter, r *http.Request) {
			if a == nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", contentType)
			w.Write(a.Data)
		}
	}
	files, err := fs.Sub(web, "web")
	if err != nil {
		return err
	}
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(files))
	mux.HandleFunc("GET /voice.worklet.js", asset("js/voice.worklet.js", "text/javascript"))
	mux.HandleFunc("GET /rnnoise.wasm", asset("wasm/rnnoise.wasm", "application/wasm"))
	mux.HandleFunc("POST /result", func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&got); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		all := map[string]any{}
		if b, err := os.ReadFile("features.json"); err == nil {
			_ = json.Unmarshal(b, &all)
		}
		maps.Copy(all, got)
		b, _ := json.MarshalIndent(all, "", "  ")
		if err := os.WriteFile("features.json", b, 0o600); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(os.Stderr, "features: %d results\n", len(got))
	})
	ln, err := net.Listen("tcp", "127.0.0.1:"+args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "serving the feature page on http://localhost:%s/\n", args[0])
	return http.Serve(ln, mux)
}
