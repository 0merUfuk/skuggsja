package webassets

import (
	"io/fs"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func TestSourceActivityPresentation(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is needed for the production JavaScript presentation test")
	}
	output, err := exec.Command(node, "--test", "source_activity_test.cjs", "usage_chart_test.cjs").CombinedOutput()
	if err != nil {
		t.Fatalf("source activity presentation: %v\n%s", err, output)
	}
	t.Logf("production JavaScript presentation checks:\n%s", output)
}

// bundledTextAssets hold the UI files whose text is scanned for network
// references. The webfonts are binary and are covered by
// TestEmbeddedFontsAreBundledAndReferenced instead.
var bundledTextAssets = []string{"index.html", "styles.css", "app.js"}

// bundledFontAssets are the exact same-origin webfont files the UI may serve.
var bundledFontAssets = []string{
	"fonts/OFL.txt",
	"fonts/newsreader-italic-latin-var.woff2",
	"fonts/newsreader-latin-var.woff2",
	"fonts/plex-mono-latin-400.woff2",
	"fonts/plex-mono-latin-600.woff2",
	"fonts/plex-mono-latin-ext-400.woff2",
	"fonts/plex-mono-latin-ext-600.woff2",
	"fonts/plex-sans-latin-ext-var.woff2",
	"fonts/plex-sans-latin-var.woff2",
}

func TestEmbeddedAssetsHaveNoExternalOrigins(t *testing.T) {
	t.Parallel()

	externalOrigin := regexp.MustCompile(`(?i)(?:https?:|wss?:)?//[a-z0-9]`)
	remoteCSS := regexp.MustCompile(`(?i)@import\s|url\(\s*["']?(?:https?:|//)`)
	networkAPI := regexp.MustCompile(`\b(?:XMLHttpRequest|WebSocket|EventSource|sendBeacon|RTCPeerConnection|SharedWorker|ServiceWorker)\b`)
	fetchAPI := regexp.MustCompile(`\bfetch\s*\(`)
	want := map[string]bool{}
	for _, asset := range append(append([]string{}, bundledTextAssets...), bundledFontAssets...) {
		want[asset] = true
	}
	textAssets := map[string]bool{}
	for _, asset := range bundledTextAssets {
		textAssets[asset] = true
	}

	err := fs.WalkDir(Files, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !want[path] {
			t.Errorf("unexpected embedded asset %q", path)
		}
		delete(want, path)
		if !textAssets[path] {
			return nil
		}
		body, err := fs.ReadFile(Files, path)
		if err != nil {
			return err
		}
		text := string(body)
		if externalOrigin.MatchString(text) {
			t.Errorf("%s references an external origin", path)
		}
		if remoteCSS.MatchString(text) {
			t.Errorf("%s contains a remote CSS import", path)
		}
		if networkAPI.MatchString(text) {
			t.Errorf("%s contains a browser network API", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for missing := range want {
		t.Errorf("required asset %q is not embedded", missing)
	}

	app, err := fs.ReadFile(Files, "app.js")
	if err != nil {
		t.Fatal(err)
	}
	appText := string(app)
	if len(fetchAPI.FindAllStringIndex(appText, -1)) != 1 || strings.Count(appText, `fetch("/api/rewind"`) != 1 {
		t.Fatal("the only permitted fetch must target the same-origin aggregate API")
	}
}

// TestEmbeddedFontsAreBundledAndReferenced keeps the typographic contract and
// the binary contract in step: every font the stylesheet asks for must ship in
// the embedded filesystem, and the licence must travel with it.
func TestEmbeddedFontsAreBundledAndReferenced(t *testing.T) {
	t.Parallel()
	styles, err := fs.ReadFile(Files, "styles.css")
	if err != nil {
		t.Fatal(err)
	}
	referenced := map[string]bool{}
	for _, match := range regexp.MustCompile(`url\(\s*["']?([^"')]+)["']?\s*\)`).FindAllStringSubmatch(string(styles), -1) {
		referenced[strings.TrimPrefix(match[1], "./")] = true
	}
	if len(referenced) != len(bundledFontAssets)-1 {
		t.Errorf("styles.css references %d assets, want %d bundled webfonts", len(referenced), len(bundledFontAssets)-1)
	}
	for _, asset := range bundledFontAssets {
		if asset == "fonts/OFL.txt" {
			continue
		}
		if !referenced[asset] {
			t.Errorf("styles.css does not reference the bundled asset %q", asset)
		}
		if _, err := fs.ReadFile(Files, asset); err != nil {
			t.Errorf("referenced font %q is not embedded: %v", asset, err)
		}
		delete(referenced, asset)
	}
	for asset := range referenced {
		t.Errorf("styles.css references %q, which is not a bundled webfont", asset)
	}
	licence, err := fs.ReadFile(Files, "fonts/OFL.txt")
	if err != nil {
		t.Fatalf("bundled fonts must embed their licence: %v", err)
	}
	if !strings.Contains(string(licence), "SIL Open Font License, Version 1.1") {
		t.Error("embedded font licence is not the SIL Open Font License 1.1")
	}
	for _, holder := range []string{"The Newsreader Project Authors", "IBM Corp."} {
		if !strings.Contains(string(licence), holder) {
			t.Errorf("embedded font licence does not credit %q", holder)
		}
	}
}

func TestEmbeddedReportMakesLocalCoverageExplicit(t *testing.T) {
	t.Parallel()
	index, err := fs.ReadFile(Files, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	app, err := fs.ReadFile(Files, "app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"Locally recovered sessions",
		"Local history is not lifetime usage.",
		`id="coverage-notice"`,
	} {
		if !strings.Contains(string(index), required) {
			t.Errorf("index.html is missing coverage language %q", required)
		}
	}
	for _, required := range []string{
		"Data coverage",
		"Earliest local evidence",
		"Earliest detailed record",
		"History-only sessions",
		"Known refs without detail",
		"recoverable local records",
	} {
		if !strings.Contains(string(app), required) {
			t.Errorf("app.js is missing coverage language %q", required)
		}
	}
}
