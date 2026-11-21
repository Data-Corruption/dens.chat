// Package ui embeds the page and serves its assets under hashed names: the
// shell's templates, and the stylesheet, script and manifest the build
// writes. Where the build wrote no manifest, as in a clone, it serves a
// placeholder page, which a binary a script builds refuses.
//
// The page is a Preact app, its JSX under src/ (grouped by area), its
// stylesheet source at css/input.css, and its tests under test/; each source
// file opens with a comment saying what it holds. Beside them under assets/
// are the built bundle, the workers and worklets for voice, sealed calls and
// Relay; RNNoise's module (assets/wasm/rnnoise.wasm, which scripts/rnnoise.sh
// builds with the header in rnnoise); the list of Unicode's emoji
// (assets/emoji.json, which scripts/emoji.sh writes); the page's sounds
// (assets/sounds, Kenney's, CC0); and the names' fonts, which the build
// copies into assets/fonts.
package ui

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/build"
)

//go:embed templates/*.html
var templateFS embed.FS

// The build writes the page's stylesheet, script and manifest into the
// embedded directory. Read at start rather than embedded by name, so a
// clone builds and tests before anything has written them.
//
//go:embed assets
var assetsFS embed.FS

// unbuilt stands for every asset's hash on the placeholder page.
const unbuilt = "unbuilt"

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

// unreadDot is the red dot the tab's icon takes while a DM or a mention
// waits, ringed in white so it reads on the mark's magenta.
const unreadDot = `<circle cx="37" cy="11" r="9" fill="#E11D48" stroke="#FFFFFF" stroke-width="3"/>`

// iconURL is an SVG as a data URL a link's href takes.
func iconURL(svg string) template.URL {
	return template.URL("data:image/svg+xml," + strings.NewReplacer("#", "%23", `"`, "'").Replace(svg))
}

// PageData returns the fields shared by every HTML page.
func (ui *UI) PageData(title, version string) map[string]any {
	return map[string]any{
		"CSS":           ui.CSS.URLPath,
		"JS":            ui.JS.URLPath,
		"Favicon":       iconURL(build.Mark),
		"FaviconUnread": iconURL(strings.Replace(build.Mark, "</svg>", unreadDot+"</svg>", 1)),
		"Title":         title,
		"Version":       version,
	}
}

// New parses all embedded templates and loads static assets from the
// manifest the build wrote. Without one it serves a placeholder page, unless
// the binary requires the page.
func New() (*UI, error) {
	manifest, err := assetsFS.ReadFile("assets/manifest.json")
	if errors.Is(err, fs.ErrNotExist) {
		manifest = nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to read asset manifest: %w", err)
	}
	return load(manifest, build.PageRequired())
}

// placeholder lists what the build's manifest would, every embedded asset
// but manifest.json, with the stylesheet and script that the build writes
// listed whether or not they're there.
func placeholder() (map[string]string, error) {
	manifest := map[string]string{"css/output.css": unbuilt, "js/output.js": unbuilt}
	err := fs.WalkDir(assetsFS, "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(path, "assets/")
		if rel != "manifest.json" {
			manifest[rel] = unbuilt
		}
		return nil
	})
	return manifest, err
}

// load makes the UI from manifestData, which lists by relative path the
// assets the page loads, with a hash of each; nil is the placeholder page,
// whose missing stylesheet and script are empty.
func load(manifestData []byte, pageRequired bool) (*UI, error) {
	var manifest map[string]string // relPath -> hash
	if manifestData == nil {
		if pageRequired {
			return nil, errors.New("the page isn't built: build with ./scripts/build.sh")
		}
		var err error
		if manifest, err = placeholder(); err != nil {
			return nil, fmt.Errorf("failed to list the placeholder page's assets: %w", err)
		}
	} else if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse asset manifest: %w", err)
	}
	for _, required := range []string{"css/output.css", "js/output.js"} {
		if _, ok := manifest[required]; !ok {
			return nil, fmt.Errorf("asset manifest is missing required asset %q", required)
		}
	}

	// Build asset map
	assets := make(map[string]*Asset)
	routeMap := make(map[string]*Asset)

	for relPath, hash := range manifest {
		// Read file data
		data, err := assetsFS.ReadFile("assets/" + relPath)
		if manifestData == nil && errors.Is(err, fs.ErrNotExist) {
			data, err = nil, nil
		}
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
		CSS:       assets["css/output.css"],
		JS:        assets["js/output.js"],
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
	case ".ogg":
		return "audio/ogg"
	case ".ttf":
		return "font/ttf"
	case ".eot":
		return "application/vnd.ms-fontobject"
	default:
		return "application/octet-stream"
	}
}
