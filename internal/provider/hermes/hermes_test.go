package hermes

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDiscoverRetainsDatabaseParentProtectionOnFailure(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	path := filepath.Join(parent, "database_without_extension")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	d, err := (Reader{DatabasePath: path}).Discover(context.Background())
	if err == nil || len(d.ProtectedDirectories) != 1 || d.ProtectedDirectories[0] != parent {
		t.Fatalf("invalid database lost source-parent protection: %#v error=%v", d, err)
	}
	if len(d.Roots) != 0 || len(d.AuditFiles) != 0 {
		t.Fatalf("write protection expanded audit inventory: %#v", d)
	}
	empty, err := (Reader{}).Discover(context.Background())
	if err != nil || len(empty.ProtectedDirectories) != 0 || len(empty.ConfiguredFiles) != 0 {
		t.Fatalf("empty database path protected unrelated paths: %#v error=%v", empty, err)
	}
}

func TestReaderUsesCanonicalSessionAndModelUsageTables(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	schema := `
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY, parent_session_id TEXT, started_at REAL NOT NULL,
			ended_at REAL, last_activity_at REAL, git_repo_root TEXT, cwd TEXT,
			tool_call_count INTEGER, model TEXT, api_call_count INTEGER,
			input_tokens INTEGER, output_tokens INTEGER, cache_read_tokens INTEGER,
			cache_write_tokens INTEGER, reasoning_tokens INTEGER
		);
		CREATE TABLE messages (
			id INTEGER PRIMARY KEY, session_id TEXT, role TEXT, content TEXT,
			timestamp REAL, _compressed_summary INTEGER
		);
		CREATE TABLE session_model_usage (
			session_id TEXT, model TEXT, api_call_count INTEGER, input_tokens INTEGER,
			output_tokens INTEGER, cache_read_tokens INTEGER, cache_write_tokens INTEGER,
			reasoning_tokens INTEGER
		);
		INSERT INTO sessions VALUES ('root', NULL, 1767520800, 1767521400, 1767521400,
			'/synthetic/hermes-project', '/synthetic/hermes-project', 3, 'hermes-synthetic', 2,
			110, 25, 30, 5, 4);
		INSERT INTO messages VALUES (1, 'root', 'user', 'Review the synthetic patch.', 1767520801, 0);
		INSERT INTO session_model_usage VALUES ('root', 'hermes-synthetic', 2, 110, 25, 30, 5, 4);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reader := Reader{DatabasePath: path}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if result.Status != "supported" {
		t.Fatalf("status = %q, warnings = %v", result.Status, result.Warnings)
	}
	if len(result.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(result.Sessions))
	}
	session := result.Sessions[0]
	if len(session.Prompts) != 1 || session.Usage.Input != 110 || session.ToolCalls != 3 {
		t.Fatalf("unexpected metrics: %s", fmt.Sprintf("prompts=%d input=%d tools=%d", len(session.Prompts), session.Usage.Input, session.ToolCalls))
	}
}

func TestDiscoverRejectsSymbolicLinkDatabase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target.db")
	if err := os.WriteFile(target, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "state.db")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := (Reader{DatabasePath: link}).Discover(context.Background())
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Discover() error = %v, want symbolic-link rejection", err)
	}
}
