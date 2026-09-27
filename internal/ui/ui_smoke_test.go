package ui

import (
	"bytes"
	"html/template"
	"regexp"
	"strings"
	"testing"
)

func TestTemplatesRender(t *testing.T) {
	u, err := New()
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}
	base := map[string]any{
		"CSS":      "/assets/css/output.css",
		"JS":       "/assets/js/output.js",
		"Favicon":  template.URL("data:,"),
		"Version":  "v0.0.0-dev",
		"Instance": "main",
	}
	pages := []struct {
		name  string
		extra map[string]any
		want  string
	}{
		{"index.html", map[string]any{"Title": "Dens"}, "Pair this browser"},
		{"index.html", map[string]any{"Title": "Dens", "Paired": true}, "Set a local password"},
		{"index.html", map[string]any{"Title": "Dens", "Paired": true, "PasswordSet": true, "ShowNav": true,
			"UpdateVersion": "v1.2.3", "UpdateCommand": "sudo dens update"}, "Dens is running"},
		{"settings.html", map[string]any{"Title": "Settings", "LogLevel": "info", "UpdatesManaged": true, "ShowNav": true}, "Change password"},
	}
	for _, page := range pages {
		data := map[string]any{}
		for k, v := range base {
			data[k] = v
		}
		for k, v := range page.extra {
			data[k] = v
		}
		var buf bytes.Buffer
		if err := u.Execute(&buf, page.name, data); err != nil {
			t.Fatalf("render %s: %v", page.name, err)
		}
		out := buf.String()
		if !strings.Contains(out, "<!DOCTYPE html>") || !strings.Contains(out, "</html>") {
			t.Fatalf("render %s: missing page shell", page.name)
		}
		if !strings.Contains(out, page.want) {
			t.Fatalf("render %s: missing %q", page.name, page.want)
		}
		if strings.Count(out, `id="action-dialog"`) != 1 {
			t.Fatalf("render %s: shared action dialog missing or duplicated", page.name)
		}
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

func TestSharedDialogUsesTextContentForDynamicMessages(t *testing.T) {
	source, err := assetsFS.ReadFile("assets/js/src/ui.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, "msgEl.textContent = message") {
		t.Fatal("shared dialog does not assign dynamic messages through textContent")
	}
	if strings.Contains(text, "msgEl.innerHTML") {
		t.Fatal("shared dialog assigns dynamic messages through innerHTML")
	}
}
