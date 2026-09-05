package claude

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDiscoverProjectSessionIndexesExactDepth(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{
		"sessions-index.json", "a/sessions-index.json", "z/sessions-index.json",
		"a/nested/sessions-index.json", "a/other.json", "a/session/subagents/sessions-index.json",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"version":1,"entries":[]}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := discoverProjectSessionIndexes(root)
	want := []string{filepath.Join(root, "a", "sessions-index.json"), filepath.Join(root, "z", "sessions-index.json")}
	if err != nil || !reflect.DeepEqual(paths, want) {
		t.Fatalf("discoverProjectSessionIndexes() = %v, %v; want %v", paths, err, want)
	}
	for _, missing := range []string{"", filepath.Join(t.TempDir(), "missing")} {
		if paths, err := discoverProjectSessionIndexes(missing); err != nil || len(paths) != 0 {
			t.Fatalf("missing root = %v, %v", paths, err)
		}
	}
}

func TestDiscoverProjectSessionIndexesRejectsLinks(t *testing.T) {
	t.Parallel()
	for _, location := range []string{"root", "project", "index"} {
		t.Run(location, func(t *testing.T) {
			root := t.TempDir()
			target := t.TempDir()
			link := filepath.Join(root, "project")
			if location == "root" {
				root = filepath.Join(root, "root-link")
				link = root
			} else if location == "index" {
				if err := os.Mkdir(link, 0o700); err != nil {
					t.Fatal(err)
				}
				link = filepath.Join(link, "sessions-index.json")
				target = filepath.Join(target, "index.json")
				if err := os.WriteFile(target, []byte(`{"version":1,"entries":[]}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if _, err := discoverProjectSessionIndexes(root); err == nil || !strings.Contains(err.Error(), "symbolic link") {
				t.Fatalf("discovery error = %v, want symbolic-link rejection", err)
			}
		})
	}
}

func TestProjectSessionIndexesRetainOnlyCoverageEvidence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first := filepath.Join(root, "first.json")
	second := filepath.Join(root, "second.json")
	content := `{"version":1,"entries":[{"sessionId":"indexed-session","created":"2025-12-31T21:14:05.160+03:00","modified":"2026-01-01T00:00:00Z","fullPath":"/unreadable/nonexistent/session.jsonl","firstPrompt":{"unsupported":"sensitive"},"summary":["sensitive"],"messageCount":"not-a-count"}]}`
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ids, earliest, malformed, oversize := readProjectSessionIndexes([]string{first, second})
	want := time.Date(2025, 12, 31, 18, 14, 5, 160000000, time.UTC)
	if len(ids) != 1 || !earliest.Equal(want) || earliest.Location() != time.UTC || malformed != 0 || oversize != 0 {
		t.Fatalf("coverage = %v, %v, malformed %d, oversize %d", ids, earliest, malformed, oversize)
	}
	if _, ok := ids["indexed-session"]; !ok {
		t.Fatal("session reference was lost")
	}
	got, err := os.ReadFile(first)
	if err != nil || string(got) != content {
		t.Fatalf("source changed: %v", err)
	}
}

func TestProjectSessionIndexesRejectUnsupportedAndMalformedEvidence(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, content string
		wantIDs       int
		wantMalformed int
	}{
		{"unsupported version", `{"version":2,"entries":[{"sessionId":"future"}]}`, 0, 1},
		{"missing version", `{"entries":[{"sessionId":"unknown"}]}`, 0, 1},
		{"missing entries", `{"version":1}`, 0, 1},
		{"null entries", `{"version":1,"entries":null}`, 0, 1},
		{"empty entries", `{"version":1,"entries":[]}`, 0, 0},
		{"broken JSON", `{"version":1`, 0, 1},
		{"entry wrong type", `{"version":1,"entries":[42]}`, 0, 1},
		{"timestamp wrong type", `{"version":1,"entries":[{"sessionId":"bad","created":42}]}`, 0, 1},
		{"invalid timestamp", `{"version":1,"entries":[{"sessionId":"retained","created":"invalid","modified":"bad"}]}`, 1, 1},
		{"missing ID ignores dates", `{"version":1,"entries":[{"created":"2000-01-01T00:00:00Z"},{"sessionId":"retained"}]}`, 1, 1},
		{"absent date retains reference", `{"version":1,"entries":[{"sessionId":"retained"}]}`, 1, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "index.json")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			ids, earliest, malformed, oversize := readProjectSessionIndexes([]string{path})
			if len(ids) != test.wantIDs || malformed != test.wantMalformed || oversize != 0 || !earliest.IsZero() {
				t.Fatalf("coverage = %v, %v, malformed %d, oversize %d", ids, earliest, malformed, oversize)
			}
		})
	}
}

func TestProjectSessionIndexesBoundFileSizeAndMissingSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "oversize.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxRecordBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	ids, earliest, malformed, oversize := readProjectSessionIndexes([]string{path, filepath.Join(root, "missing.json")})
	if len(ids) != 0 || !earliest.IsZero() || malformed != 1 || oversize != 1 {
		t.Fatalf("coverage = %v, %v, malformed %d, oversize %d", ids, earliest, malformed, oversize)
	}
}
