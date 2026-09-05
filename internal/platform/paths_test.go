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
	if paths.ClaudeHistory != filepath.Join(claudeHome, "history.jsonl") ||
		paths.ClaudeStats != filepath.Join(claudeHome, "stats-cache.json") {
		t.Errorf("Claude supplemental paths = %q, %q", paths.ClaudeHistory, paths.ClaudeStats)
	}
	if paths.CodexSessions != filepath.Join(codexHome, "sessions") ||
		paths.CodexArchived != filepath.Join(codexHome, "archived_sessions") ||
		paths.CodexHistory != filepath.Join(codexHome, "history.jsonl") ||
		paths.CodexSessionIndex != filepath.Join(codexHome, "session_index.jsonl") ||
		paths.CodexStateDatabase != filepath.Join(codexHome, "state_5.sqlite") ||
		paths.CodexCatalogDatabase != filepath.Join(codexHome, "sqlite", "codex-dev.db") ||
		paths.CodexThreadHistoryDatabase != filepath.Join(codexHome, "thread_history_1.sqlite") {
		t.Errorf("Codex paths = %q, %q, %q, %q, %q, %q", paths.CodexSessions, paths.CodexArchived, paths.CodexHistory, paths.CodexSessionIndex, paths.CodexStateDatabase, paths.CodexCatalogDatabase)
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
		"Claude": paths.ClaudeProjects, "Claude history": paths.ClaudeHistory,
		"Claude stats": paths.ClaudeStats, "Claude global state": paths.ClaudeGlobalState,
		"Codex sessions": paths.CodexSessions, "Codex archive": paths.CodexArchived,
		"Codex history": paths.CodexHistory, "Codex index": paths.CodexSessionIndex,
		"Codex imports": paths.CodexExternalImports, "Codex state": paths.CodexStateDatabase, "Hermes": paths.HermesDatabase,
		"Codex catalog":        paths.CodexCatalogDatabase,
		"Codex thread history": paths.CodexThreadHistoryDatabase,
		"Cursor":               paths.CursorStateDB,
	} {
		if !filepath.IsAbs(path) {
			t.Errorf("%s path %q is not absolute", name, path)
		}
		if _, err := os.Stat(filepath.Dir(path)); err != nil && !os.IsNotExist(err) {
			t.Errorf("inspect %s parent: %v", name, err)
		}
	}
	for name, path := range map[string]string{
		"Claude Desktop sessions":   paths.ClaudeDesktopSessions,
		"Claude Code session index": paths.ClaudeCodeSessions,
	} {
		if path != "" && !filepath.IsAbs(path) {
			t.Errorf("%s path %q is not absolute", name, path)
		}
	}
}
