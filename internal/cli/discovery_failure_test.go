package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0merUfuk/skuggsja/internal/app"
)

func TestCleanPreservesConfiguredSourceWhenDiscoveryFails(t *testing.T) {
	for _, harness := range []string{"CLAUDE", "CODEX"} {
		t.Run(harness, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
			t.Setenv("LOCALAPPDATA", filepath.Join(root, "local-app-data"))
			t.Setenv("APPDATA", filepath.Join(root, "app-data"))
			artifact, err := app.DefaultOutputPath()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(artifact), 0o700); err != nil {
				t.Fatal(err)
			}
			const sentinel = "synthetic configured history, not a generated report"
			if err := os.WriteFile(artifact, []byte(sentinel), 0o600); err != nil {
				t.Fatal(err)
			}
			sourceRoot := filepath.Join(root, "source")
			project := filepath.Join(sourceRoot, "synthetic-project")
			if err := os.MkdirAll(project, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(artifact, filepath.Join(project, "rollout-synthetic.jsonl")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			rootVariable := "SKUGGSJA_" + harness + "_SESSIONS"
			if harness == "CLAUDE" {
				rootVariable = "SKUGGSJA_CLAUDE_PROJECTS"
			}
			t.Setenv(rootVariable, sourceRoot)
			t.Setenv("SKUGGSJA_"+harness+"_HISTORY", artifact)
			command := New("test")
			command.SetArgs([]string{"clean"})
			if err := command.ExecuteContext(context.Background()); err == nil || !strings.Contains(err.Error(), "discover") {
				t.Fatalf("clean error = %v, want discovery failure", err)
			}
			if got, err := os.ReadFile(artifact); err != nil || string(got) != sentinel {
				t.Fatalf("clean changed configured source: read_error=%v sentinel_retained=%t", err, string(got) == sentinel)
			}
		})
	}
}
