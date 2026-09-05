package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0merUfuk/skuggsja/internal/app"
	"github.com/0merUfuk/skuggsja/internal/platform"
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

func TestCleanRefusesConfiguredSQLiteSidecarAlias(t *testing.T) {
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
	const sentinel = "synthetic source target, not a generated report"
	if err := os.WriteFile(artifact, []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(root, "isolated-history", "state.db")
	if err := os.MkdirAll(filepath.Dir(database), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(artifact, database+"-wal"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv("SKUGGSJA_HERMES_DATABASE", database)

	command := New("test")
	command.SetArgs([]string{"clean"})
	if err := command.ExecuteContext(context.Background()); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("clean error = %v, want sidecar-overlap refusal", err)
	}
	got, err := os.ReadFile(artifact)
	if err != nil || string(got) != sentinel {
		t.Fatalf("configured sidecar target changed: content=%q error=%v", got, err)
	}
}

func TestCleanAllowsGlobalStateInHomeAncestor(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "local-app-data"))
	t.Setenv("APPDATA", filepath.Join(root, "app-data"))

	globalState := filepath.Join(root, ".claude.json")
	if err := os.WriteFile(globalState, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact, err := app.DefaultOutputPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(artifact), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("synthetic aggregate"), 0o600); err != nil {
		t.Fatal(err)
	}

	command := New("test")
	command.SetArgs([]string{"clean"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("clean error = %v", err)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("artifact still exists: %v", err)
	}
	if got, err := os.ReadFile(globalState); err != nil || string(got) != "{}\n" {
		t.Fatalf("global state changed: content=%q error=%v", got, err)
	}
}

func TestExplicitClaudeProjectsScopeClearsAutomaticExtraHomes(t *testing.T) {
	root := t.TempDir()
	paths := platform.Paths{ClaudeExtraHomes: []string{filepath.Join(root, "automatic")}}
	t.Setenv("SKUGGSJA_CLAUDE_PROJECTS", filepath.Join(root, "isolated-projects"))
	t.Setenv("SKUGGSJA_CLAUDE_EXTRA_HOMES", "")
	if err := os.Unsetenv("SKUGGSJA_CLAUDE_EXTRA_HOMES"); err != nil {
		t.Fatal(err)
	}
	applyPathOverrides(&paths)
	if len(paths.ClaudeExtraHomes) != 0 {
		t.Fatalf("inherited homes=%q", paths.ClaudeExtraHomes)
	}
	t.Setenv("SKUGGSJA_CLAUDE_EXTRA_HOMES", filepath.Join(root, "explicit-a")+string(os.PathListSeparator)+filepath.Join(root, "explicit-b"))
	t.Setenv("SKUGGSJA_CODEX_RECOVERY", filepath.Join(root, "isolated-recovery"))
	applyPathOverrides(&paths)
	if len(paths.ClaudeExtraHomes) != 2 || paths.CodexRecovery != filepath.Join(root, "isolated-recovery") {
		t.Fatalf("explicit source overrides=%#v", paths)
	}
}
