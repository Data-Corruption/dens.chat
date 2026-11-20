package ui

import (
	"bytes"
	"html/template"
	"regexp"
	"strings"
	"testing"
)

func TestAppShellRenders(t *testing.T) {
	u, err := New()
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}
	var buf bytes.Buffer
	err = u.Execute(&buf, "app.html", map[string]any{
		"Title":    "Dens",
		"CSS":      "/assets/css/output.css",
		"JS":       "/assets/js/output.js",
		"Favicon":  template.URL("data:,"),
		"Version":  "v0.0.0-dev",
		"Instance": `second"><script>`,
		"Sounds":   `{"joined":"/assets/sounds/joined.a1b2.ogg"}`,
		"Fonts":    `{"pacifico":"/assets/fonts/pacifico.c3d4.woff2"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// The page's sounds' addresses go as JSON in an attribute (M14.2), and
	// the names' fonts'.
	for _, want := range []string{"<!DOCTYPE html>", `<div id="app"`, `data-version="v0.0.0-dev"`, `src="/assets/js/output.js"`, "</html>",
		`data-sounds="{&#34;joined&#34;:&#34;/assets/sounds/joined.a1b2.ogg&#34;}"`,
		`data-fonts="{&#34;pacifico&#34;:&#34;/assets/fonts/pacifico.c3d4.woff2&#34;}"`} {
		if !strings.Contains(out, want) {
			t.Errorf("app shell lacks %q", want)
		}
	}
	if strings.Contains(out, `second"><script>`) {
		t.Error("the instance name reached the page unescaped")
	}
}

// TestTemplatesNeedNoInlineCode keeps every page compatible with the CSP,
// which allows scripts and styles only from files.
func TestTemplatesNeedNoInlineCode(t *testing.T) {
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		t.Fatal(err)
	}
	inlineHandler := regexp.MustCompile(`\son[a-z]+\s*=`)
	for _, entry := range entries {
		source, err := templateFS.ReadFile("templates/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		for _, script := range regexp.MustCompile(`<script[^>]*>`).FindAllString(text, -1) {
			if !strings.Contains(script, "src=") {
				t.Errorf("%s has an inline script: %s", entry.Name(), script)
			}
		}
		if strings.Contains(text, "<style") || strings.Contains(text, " style=") {
			t.Errorf("%s has inline styles", entry.Name())
		}
		if inlineHandler.MatchString(text) {
			t.Errorf("%s has an inline event handler", entry.Name())
		}
	}
}

func TestLoadRequiresPageAssets(t *testing.T) {
	for _, test := range []struct {
		name     string
		manifest string
		missing  string
	}{
		{name: "empty manifest", manifest: `{}`, missing: "css/output.css"},
		{name: "missing CSS", manifest: `{"js/output.js":"test"}`, missing: "css/output.css"},
		{name: "missing JavaScript", manifest: `{"css/output.css":"test"}`, missing: "js/output.js"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := load([]byte(test.manifest), false)
			if err == nil {
				t.Fatalf("load succeeded without required asset %q", test.missing)
			}
			if !strings.Contains(err.Error(), test.missing) {
				t.Fatalf("load error = %q, want missing asset %q", err, test.missing)
			}
		})
	}
}

// TestPlaceholderPage keeps a clone building and testing before the page is
// built, and a binary that requires the page from starting without it.
func TestPlaceholderPage(t *testing.T) {
	if _, err := load(nil, true); err == nil || !strings.Contains(err.Error(), "isn't built") {
		t.Fatalf("load without a manifest, the page required: %v, want a refusal", err)
	}
	u, err := load(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	data := u.PageData("Dens", "v0.0.0-dev")
	if err := u.Execute(&buf, "app.html", data); err != nil {
		t.Fatalf("the placeholder lacks an asset the page names: %v", err)
	}
	for _, rel := range []string{"css/output.css", "js/output.js", "sounds/joined.ogg"} {
		if u.Assets[rel] == nil {
			t.Errorf("the placeholder lacks %s", rel)
		}
	}
	for rel := range u.Assets {
		if rel == "css/input.css" || strings.HasPrefix(rel, "js/src/") {
			t.Errorf("the placeholder serves the source %s", rel)
		}
	}
}

// TestScriptsNeverBuildHTMLFromStrings keeps every den-supplied string on
// the text path: the page is the most valuable target, and anything a den
// sends is hostile. Preact renders text as text; these are the ways around it.
func TestScriptsNeverBuildHTMLFromStrings(t *testing.T) {
	entries, err := assetsFS.ReadDir("assets/js/src")
	if err != nil {
		t.Fatal(err)
	}
	banned := []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "dangerouslySetInnerHTML",
		"document.write", "eval(", "new Function", "createContextualFragment", "srcdoc"}
	for _, entry := range entries {
		source, err := assetsFS.ReadFile("assets/js/src/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range banned {
			if strings.Contains(string(source), b) {
				t.Errorf("%s uses %s", entry.Name(), b)
			}
		}
	}
}
