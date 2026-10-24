package copilot

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDiscoverRetainsDatabaseParentProtectionOnFailure(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	path := filepath.Join(parent, "session-store.db")
	discovery, err := (Reader{DatabasePath: path}).Discover(context.Background())
	if err != nil || len(discovery.Files) != 0 || len(discovery.ProtectedDirectories) != 1 || discovery.ProtectedDirectories[0] != parent {
		t.Fatalf("missing database lost source-parent protection: %#v error=%v", discovery, err)
	}
	empty, err := (Reader{}).Discover(context.Background())
	if err != nil || len(empty.ProtectedDirectories) != 0 || len(empty.ConfiguredFiles) != 0 {
		t.Fatalf("empty database path protected unrelated paths: %#v error=%v", empty, err)
	}
}

func TestReaderCountsTurnsAsPromptsAndDropsEmptySessions(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "session-store.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	schema := `
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY, cwd TEXT, repository TEXT, host_type TEXT,
			branch TEXT, summary TEXT, agent_name TEXT, agent_description TEXT,
			created_at TEXT, updated_at TEXT
		);
		CREATE TABLE turns (
			id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL,
			turn_index INTEGER NOT NULL, user_message TEXT, assistant_response TEXT,
			timestamp TEXT
		);
		INSERT INTO sessions (id, cwd, created_at, updated_at) VALUES
			('with-turns', '/synthetic/copilot-project', '2026-09-20T10:00:00.000Z', '2026-09-20T10:05:00.000Z'),
			('empty-shell', '/synthetic/copilot-project', '2026-09-20T11:00:00.000Z', '2026-09-20T11:00:00.000Z');
		INSERT INTO turns (session_id, turn_index, user_message, assistant_response, timestamp) VALUES
			('with-turns', 0, 'Add a health check endpoint.', 'Added the handler.', '2026-09-20T10:00:05.000Z'),
			('with-turns', 1, 'Now write a test for it.', 'Added the test.', '2026-09-20T10:04:00.000Z');
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
		t.Fatalf("sessions = %d, want 1 (the empty-shell session must be dropped)", len(result.Sessions))
	}
	session := result.Sessions[0]
	if session.ID != "with-turns" || len(session.Prompts) != 2 {
		t.Fatalf("unexpected session: id=%q prompts=%d", session.ID, len(session.Prompts))
	}
	if !session.Prompts[0].HasText || session.Prompts[0].Words == 0 {
		t.Fatalf("first prompt metric not derived from user_message: %#v", session.Prompts[0])
	}
	if session.Project != "copilot-project" {
		t.Fatalf("project = %q, want copilot-project", session.Project)
	}
}

func TestReaderReportsUnsupportedSchemaWithoutCrashing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "session-store.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE unrelated (id TEXT)`); err != nil {
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
	if result.Status != "unsupported schema" {
		t.Fatalf("status = %q, want unsupported schema", result.Status)
	}
}
