package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0merUfuk/skuggsja/internal/app"
)

func TestCleanRefusesConfiguredSourceAlias(t *testing.T) {
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
	const sentinel = "synthetic source, not a generated report"
	if err := os.WriteFile(artifact, []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKUGGSJA_HERMES_DATABASE", artifact)

	command := New("test")
	command.SetArgs([]string{"clean"})
	if err := command.ExecuteContext(context.Background()); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("clean error = %v, want source-overlap refusal", err)
	}
	got, err := os.ReadFile(artifact)
	if err != nil || string(got) != sentinel {
		t.Fatalf("configured source changed: content=%q error=%v", got, err)
	}
}
