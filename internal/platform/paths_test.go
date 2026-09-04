package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPathsHonorsHarnessHomeOverrides(t *testing.T) {
	root := t.TempDir()
	claudeHome := filepath.Join(root, "claude-home")
	codexHome := filepath.Join(root, "codex-home")
	hermesHome := filepath.Join(root, "hermes-home")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("HERMES_HOME", hermesHome)

	paths, err := DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if paths.ClaudeProjects != filepath.Join(claudeHome, "projects") {
		t.Errorf("Claude path = %q", paths.ClaudeProjects)
	}
	if paths.CodexSessions != filepath.Join(codexHome, "sessions") ||
		paths.CodexArchived != filepath.Join(codexHome, "archived_sessions") {
		t.Errorf("Codex paths = %q, %q", paths.CodexSessions, paths.CodexArchived)
	}
	if paths.HermesDatabase != filepath.Join(hermesHome, "state.db") {
		t.Errorf("Hermes path = %q", paths.HermesDatabase)
	}
}

func TestDefaultPathsReturnsAbsoluteNativeLocations(t *testing.T) {
	for _, name := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "HERMES_HOME"} {
		t.Setenv(name, "")
	}
	paths, err := DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"Claude": paths.ClaudeProjects, "Codex sessions": paths.CodexSessions,
		"Codex archive": paths.CodexArchived, "Hermes": paths.HermesDatabase,
		"Cursor": paths.CursorStateDB,
	} {
		if !filepath.IsAbs(path) {
			t.Errorf("%s path %q is not absolute", name, path)
		}
		if _, err := os.Stat(filepath.Dir(path)); err != nil && !os.IsNotExist(err) {
			t.Errorf("inspect %s parent: %v", name, err)
		}
	}
}
