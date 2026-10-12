// Package ui provides embedded frontend templates and assets.
package ui

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/build"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed assets
var assetsFS embed.FS

// manifestData lists, by relative path, the assets the page loads, with a
// hash of each; the build writes it, leaving out the sources.
//
//go:embed assets/manifest.json
var manifestData []byte

// Asset represents an embedded static asset with cache busting.
type Asset struct {
	RelPath     string // original relative path, e.g. "css/output.css"
	URLPath     string // cache-busted URL path, e.g. "/assets/css/output.a1b2c3d4.css"
	Data        []byte
	ContentType string
}

// Handler returns an http.HandlerFunc that serves this asset with
// appropriate caching headers (1 year, immutable).
func (a *Asset) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", a.ContentType)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Write(a.Data)
	}
}

// UI holds parsed templates and static assets.
// Create once at app startup via New().
type UI struct {
	templates *template.Template
	Assets    map[string]*Asset // keyed by relative path (e.g. "css/output.css")

	// Convenience shortcuts to common assets
	CSS *Asset
	JS  *Asset

	// URL path -> Asset for routing
	routeMap map[string]*Asset
}

// PageData returns the fields shared by every HTML page.
func (ui *UI) PageData(title, version string) map[string]any {
	return map[string]any{
		"CSS":     ui.CSS.URLPath,
		"JS":      ui.JS.URLPath,
		"Favicon": template.URL("data:image/svg+xml," + strings.NewReplacer("#", "%23", `"`, "'").Replace(build.Mark)),
		"Title":   title,
		"Version": version,
	}
}

// New parses all embedded templates and loads static assets from the manifest.
func New() (*UI, error) {
	// Load manifest
	var manifest map[string]string // relPath -> hash
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse asset manifest: %w", err)
	}

	// Build asset map
	assets := make(map[string]*Asset)
	routeMap := make(map[string]*Asset)

	for relPath, hash := range manifest {
		// Read file data
		data, err := assetsFS.ReadFile("assets/" + relPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read asset %s: %w", relPath, err)
		}

		// Build cache-busted URL path
		ext := filepath.Ext(relPath)
		base := strings.TrimSuffix(relPath, ext)
		urlPath := fmt.Sprintf("/assets/%s.%s%s", base, hash, ext)

		asset := &Asset{
			RelPath:     relPath,
			URLPath:     urlPath,
			Data:        data,
			ContentType: detectContentType(relPath),
		}

		assets[relPath] = asset
		routeMap[urlPath] = asset
	}

	css := assets["css/output.css"]
	if css == nil {
		return nil, fmt.Errorf("asset manifest is missing required asset %q", "css/output.css")
	}
	js := assets["js/output.js"]
	if js == nil {
		return nil, fmt.Errorf("asset manifest is missing required asset %q", "js/output.js")
	}

	// assetPath gives a template an asset's cache-busted path. Assets are
	// served only there, so one the manifest lacks fails the page.
	assetPath := func(relPath string) (string, error) {
		if asset, ok := assets[relPath]; ok {
			return asset.URLPath, nil
		}
		return "", fmt.Errorf("the asset manifest has no %s", relPath)
	}

	t, err := template.New("").Funcs(template.FuncMap{"assetPath": assetPath}).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	return &UI{
		templates: t,
		Assets:    assets,
		routeMap:  routeMap,
		CSS:       css,
		JS:        js,
	}, nil
}

// Execute renders a template by name to the writer.
func (ui *UI) Execute(w io.Writer, name string, data any) error {
	return ui.templates.ExecuteTemplate(w, name, data)
}

// ServeAsset returns an http.HandlerFunc that routes to the correct asset
// based on the URL path. Mount this at "/assets/*".
func (ui *UI) ServeAsset(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if asset, ok := ui.routeMap[path]; ok {
		asset.Handler()(w, r)
		return
	}
	http.NotFound(w, r)
}

// detectContentType returns the MIME type based on file extension.
func detectContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".wasm":
		return "application/wasm"
	case ".ttf":
		return "font/ttf"
	case ".eot":
		return "application/vnd.ms-fontobject"
	default:
		return "application/octet-stream"
	}
}
