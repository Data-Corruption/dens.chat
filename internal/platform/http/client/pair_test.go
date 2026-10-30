package client

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/layout"
)

// The page says how to pair a browser: the command that opens the default
// one, which the Flatpak's apps menu is, and the one that prints the link
// for any other (M9.3). The Android app pairs its own page (M12).
func TestPairCommands(t *testing.T) {
	for _, c := range []struct {
		form       layout.Form
		instance   string
		open, link string
	}{
		{layout.Installed, "main", "dens open", "dens open --print"},
		{layout.Development, "second", "dens open --instance second", "dens open --instance second --print"},
		{layout.Flatpak, "main", "", "flatpak run --command=dens chat.dens.Dens open --print"},
		{layout.Android, "main", "", ""},
	} {
		a := app.New(build.BuildInfo{Name: "dens", Version: "v1.1.0"})
		a.Layout = layout.Layout{Form: c.form, Instance: c.instance}
		open, link := (&router{a: a}).pairCommands()
		if open != c.open || link != c.link {
			t.Errorf("%s, %s: %q and %q; want %q and %q", c.form, c.instance, open, link, c.open, c.link)
		}
	}
}

// The page may start audio without a click in the Android app's WebView
// alone, where it then makes its call's audio once the microphone is open
// (M12).
func TestAutoplaysInTheAndroidAppAlone(t *testing.T) {
	for form, want := range map[layout.Form]bool{layout.Installed: false, layout.Development: false, layout.Flatpak: false, layout.Android: true} {
		a := app.New(build.BuildInfo{Name: "dens", Version: "v1.1.0"})
		a.Layout = layout.Layout{Form: form, Instance: "main"}
		if got := (&router{a: a}).autoplays(); got != want {
			t.Errorf("%s: autoplays %t, want %t", form, got, want)
		}
	}
}

// A request from a page another version of Dens served, as one open across
// an update, is refused with the header that has the page load again; one
// from this version's page, or naming none, as a script's, passes (M9.3).
func TestRequestsFromAnotherVersionsPage(t *testing.T) {
	a := app.New(build.BuildInfo{Name: "dens", Version: "v1.2.0"})
	handler := (&router{a: a}).requireThisVersion(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, c := range []struct {
		page   string
		code   int
		reload string
	}{
		{"", http.StatusNoContent, ""},
		{"v1.2.0", http.StatusNoContent, ""},
		{"v1.1.0", http.StatusConflict, "v1.2.0"},
	} {
		req := httptest.NewRequest("GET", "/api/status", nil)
		if c.page != "" {
			req.Header.Set(pageVersionHeader, c.page)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != c.code || rec.Header().Get(reloadHeader) != c.reload {
			t.Errorf("a page of %q: %d, reload %q; want %d, %q", c.page, rec.Code, rec.Header().Get(reloadHeader), c.code, c.reload)
		}
	}
}

// The page's Install hands the Android app's update to its shell only
// once the updater has one ready (M12.4); elsewhere the address is the
// Flatpak's alone.
func TestInstallingAnUpdateByForm(t *testing.T) {
	for _, c := range []struct {
		form layout.Form
		code int
	}{
		{layout.Android, http.StatusConflict},
		{layout.Installed, http.StatusNotFound},
	} {
		a := app.New(build.BuildInfo{Name: "dens", Version: "v1.5.0"})
		a.Layout = layout.Layout{Form: c.form, Instance: "main"}
		w := httptest.NewRecorder()
		(&router{a: a}).handleUpdate(w, httptest.NewRequest(http.MethodPost, "/api/update", nil))
		if w.Code != c.code {
			t.Errorf("%s with no update ready: %d %s, want %d", c.form, w.Code, w.Body, c.code)
		}
	}
}
