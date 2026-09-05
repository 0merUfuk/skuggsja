// Package platform resolves per-OS history locations without probing the network.
package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Paths contains all candidate source locations. Tests can construct it directly.
type Paths struct {
	ClaudeProjects             string
	ClaudeHistory              string
	ClaudeStats                string
	ClaudeGlobalState          string
	ClaudeDesktopSessions      string
	ClaudeCodeSessions         string
	CodexSessions              string
	CodexArchived              string
	CodexHistory               string
	CodexSessionIndex          string
	CodexExternalImports       string
	CodexStateDatabase         string
	CodexCatalogDatabase       string
	CodexThreadHistoryDatabase string
	HermesDatabase             string
	CursorStateDB              string
	CursorConversationDB       string
	CursorWorkspaceRoot        string
}

// DefaultPaths returns native harness locations for the running OS.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home directory: %w", err)
	}

	claudeHome := os.Getenv("CLAUDE_CONFIG_DIR")
	if claudeHome == "" {
		claudeHome = filepath.Join(home, ".claude")
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	hermesHome := os.Getenv("HERMES_HOME")
	if hermesHome == "" {
		if runtime.GOOS == "windows" {
			hermesHome = os.Getenv("LOCALAPPDATA")
			if hermesHome != "" {
				hermesHome = filepath.Join(hermesHome, "hermes")
			} else {
				hermesHome = filepath.Join(home, "AppData", "Local", "hermes")
			}
		} else {
			hermesHome = filepath.Join(home, ".hermes")
		}
	}

	cursorUser := cursorUserDir(home)
	claudeDesktopSessions := ""
	claudeCodeSessions := ""
	if runtime.GOOS == "darwin" {
		claudeDesktopSessions = filepath.Join(home, "Library", "Application Support", "Claude", "local-agent-mode-sessions")
		claudeCodeSessions = filepath.Join(home, "Library", "Application Support", "Claude", "claude-code-sessions")
	}
	return Paths{
		ClaudeProjects:             filepath.Join(claudeHome, "projects"),
		ClaudeHistory:              filepath.Join(claudeHome, "history.jsonl"),
		ClaudeStats:                filepath.Join(claudeHome, "stats-cache.json"),
		ClaudeGlobalState:          filepath.Join(home, ".claude.json"),
		ClaudeDesktopSessions:      claudeDesktopSessions,
		ClaudeCodeSessions:         claudeCodeSessions,
		CodexSessions:              filepath.Join(codexHome, "sessions"),
		CodexArchived:              filepath.Join(codexHome, "archived_sessions"),
		CodexHistory:               filepath.Join(codexHome, "history.jsonl"),
		CodexSessionIndex:          filepath.Join(codexHome, "session_index.jsonl"),
		CodexExternalImports:       filepath.Join(codexHome, "external_agent_session_imports.json"),
		CodexStateDatabase:         filepath.Join(codexHome, "state_5.sqlite"),
		CodexCatalogDatabase:       filepath.Join(codexHome, "sqlite", "codex-dev.db"),
		CodexThreadHistoryDatabase: filepath.Join(codexHome, "thread_history_1.sqlite"),
		HermesDatabase:             filepath.Join(hermesHome, "state.db"),
		CursorStateDB:              filepath.Join(cursorUser, "globalStorage", "state.vscdb"),
		CursorConversationDB:       filepath.Join(cursorUser, "globalStorage", "conversation-search.db"),
		CursorWorkspaceRoot:        filepath.Join(cursorUser, "workspaceStorage"),
	}, nil
}

func cursorUserDir(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Cursor", "User")
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "Cursor", "User")
		}
		return filepath.Join(home, "AppData", "Roaming", "Cursor", "User")
	default:
		if config := os.Getenv("XDG_CONFIG_HOME"); config != "" {
			return filepath.Join(config, "Cursor", "User")
		}
		return filepath.Join(home, ".config", "Cursor", "User")
	}
}
