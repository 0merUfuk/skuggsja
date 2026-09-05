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
	output, err := exec.Command(node, "--test", "source_activity_test.cjs").CombinedOutput()
	if err != nil {
		t.Fatalf("source activity presentation: %v\n%s", err, output)
	}
	t.Logf("production JavaScript presentation checks:\n%s", output)
}

func TestEmbeddedAssetsHaveNoExternalOrigins(t *testing.T) {
	t.Parallel()

	externalOrigin := regexp.MustCompile(`(?i)(?:https?:|wss?:)?//[a-z0-9]`)
	remoteCSS := regexp.MustCompile(`(?i)@import\s|url\(\s*["']?(?:https?:|//)`)
	networkAPI := regexp.MustCompile(`\b(?:XMLHttpRequest|WebSocket|EventSource|sendBeacon|RTCPeerConnection|SharedWorker|ServiceWorker)\b`)
	fetchAPI := regexp.MustCompile(`\bfetch\s*\(`)
	want := map[string]bool{"index.html": true, "styles.css": true, "app.js": true}

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
