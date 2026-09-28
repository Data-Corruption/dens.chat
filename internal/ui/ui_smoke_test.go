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
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"<!DOCTYPE html>", `<div id="app"`, `data-version="v0.0.0-dev"`, `src="/assets/js/output.js"`, "</html>"} {
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

func TestNewRequiresPageAssets(t *testing.T) {
	original := manifestData
	t.Cleanup(func() { manifestData = original })

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
			manifestData = []byte(test.manifest)
			_, err := New()
			if err == nil {
				t.Fatalf("New succeeded without required asset %q", test.missing)
			}
			if !strings.Contains(err.Error(), test.missing) {
				t.Fatalf("New error = %q, want missing asset %q", err, test.missing)
			}
		})
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
