package webassets

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedAssetsHaveNoExternalOrigins(t *testing.T) {
	t.Parallel()

	externalOrigin := regexp.MustCompile(`(?i)(?:https?:|wss?:)?//[a-z0-9]`)
	remoteCSS := regexp.MustCompile(`(?i)@import\s|url\(\s*["']?(?:https?:|//)`)
	networkAPI := regexp.MustCompile(`\b(?:XMLHttpRequest|WebSocket|EventSource|sendBeacon)\b`)
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
	if strings.Count(string(app), `fetch("/api/rewind"`) != 1 {
		t.Fatal("the only permitted fetch must target the same-origin aggregate API")
	}
}
