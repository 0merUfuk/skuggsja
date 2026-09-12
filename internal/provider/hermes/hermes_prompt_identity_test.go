package hermes

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

const promptFixtureSchema = `
	CREATE TABLE sessions (
		id TEXT PRIMARY KEY, parent_session_id TEXT, started_at REAL NOT NULL,
		ended_at REAL, last_activity_at REAL, git_repo_root TEXT, cwd TEXT,
		tool_call_count INTEGER, model TEXT, api_call_count INTEGER,
		input_tokens INTEGER, output_tokens INTEGER, cache_read_tokens INTEGER,
		cache_write_tokens INTEGER, reasoning_tokens INTEGER
	);
	CREATE TABLE messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT, role TEXT, content TEXT,
		tool_calls TEXT, timestamp REAL NOT NULL, platform_message_id TEXT,
		active INTEGER NOT NULL DEFAULT 1, compacted INTEGER NOT NULL DEFAULT 0,
		_compressed_summary INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE session_model_usage (
		session_id TEXT, model TEXT, api_call_count INTEGER, input_tokens INTEGER,
		output_tokens INTEGER, cache_read_tokens INTEGER, cache_write_tokens INTEGER,
		reasoning_tokens INTEGER
	);
`

// newPromptFixture builds the real Hermes message shape with one root session
// whose stored tool counter is `toolCalls`.
func newPromptFixture(t *testing.T, toolCalls int64) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(promptFixtureSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO sessions (id, parent_session_id, started_at, ended_at, last_activity_at,
			git_repo_root, cwd, tool_call_count, model, api_call_count,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens)
		VALUES ('root', NULL, 1767520800, 1767521400, 1767521400,
			'/synthetic/hermes-project', '/synthetic/hermes-project', ?, 'hermes-synthetic', 2,
			110, 25, 30, 5, 4)`, toolCalls); err != nil {
		t.Fatal(err)
	}
	return path, db
}

func insertMessage(t *testing.T, db *sql.DB, id int, role, content string, timestamp float64, active, compacted, summary int) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO messages (id, session_id, role, content, timestamp, active, compacted, _compressed_summary)
		VALUES (?, 'root', ?, ?, ?, ?, ?, ?)`, id, role, content, timestamp, active, compacted, summary); err != nil {
		t.Fatal(err)
	}
}

func readPromptsFromFixture(t *testing.T, path string) (int, int64) {
	t.Helper()
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
	return len(result.Sessions[0].Prompts), result.Sessions[0].ToolCalls
}

func TestReaderCountsCompactionCopyOnce(t *testing.T) {
	t.Parallel()
	path, db := newPromptFixture(t, 0)
	// Hermes compaction archives the carried-tail original (active=0, compacted=0)
	// and inserts a byte-exact clone with a fresh row id (active=1, compacted=0).
	insertMessage(t, db, 2, "user", "Owner action", 1767520801.25, 0, 0, 0)
	insertMessage(t, db, 3, "user", "[compressed summary]", 1767520802, 1, 1, 1)
	insertMessage(t, db, 4, "user", "Owner action", 1767520801.25, 1, 0, 0)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	prompts, toolCalls := readPromptsFromFixture(t, path)
	if prompts != 1 || toolCalls != 0 {
		t.Fatalf("prompts = %d, tool calls = %d; want 1 prompt and 0 tool calls", prompts, toolCalls)
	}
}

func TestReaderKeepsArchivedOnlyPrompt(t *testing.T) {
	t.Parallel()
	path, db := newPromptFixture(t, 0)
	// A summarized-away prompt keeps its archived original and has no live twin.
	insertMessage(t, db, 2, "user", "Owner action", 1767520801.25, 0, 1, 0)
	insertMessage(t, db, 3, "user", "[compressed summary]", 1767520802, 1, 1, 1)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	prompts, _ := readPromptsFromFixture(t, path)
	if prompts != 1 {
		t.Fatalf("prompts = %d, want the archived original counted once", prompts)
	}
}

func TestReaderNeverMergesSimultaneouslyLiveRows(t *testing.T) {
	t.Parallel()
	path, db := newPromptFixture(t, 0)
	insertMessage(t, db, 2, "user", "Owner action", 1767520801.25, 1, 0, 0)
	insertMessage(t, db, 3, "user", "Owner action", 1767520801.25, 1, 0, 0)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	prompts, _ := readPromptsFromFixture(t, path)
	if prompts != 2 {
		t.Fatalf("prompts = %d, want two live rows kept separate", prompts)
	}
}

func TestReaderSeparatesRepeatedTextAtDistinctTimes(t *testing.T) {
	t.Parallel()
	path, db := newPromptFixture(t, 0)
	insertMessage(t, db, 2, "user", "continue", 1767520801.25, 0, 0, 0)
	insertMessage(t, db, 3, "user", "continue", 1767520999.75, 1, 0, 0)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	prompts, _ := readPromptsFromFixture(t, path)
	if prompts != 2 {
		t.Fatalf("prompts = %d, want repeated text at different times kept separate", prompts)
	}
}

func TestReaderReportsStoredActiveToolCounter(t *testing.T) {
	t.Parallel()
	path, db := newPromptFixture(t, 9)
	// Six tool-call entries survive on disk, one row archived; the stored counter
	// is Hermes's active-transcript value and is reported as-is.
	if _, err := db.Exec(`
		INSERT INTO messages (id, session_id, role, content, tool_calls, timestamp, active, compacted, _compressed_summary)
		VALUES (10, 'root', 'assistant', 'calls', '[{"id":"c1"},{"id":"c2"},{"id":"c3"}]', 1767520803, 1, 0, 0),
		       (11, 'root', 'assistant', 'calls', '[{"id":"c4"},{"id":"c5"},{"id":"c6"}]', 1767520804, 0, 1, 0)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, toolCalls := readPromptsFromFixture(t, path)
	if toolCalls != 9 {
		t.Fatalf("tool calls = %d, want the stored active counter 9", toolCalls)
	}
}

func TestReaderFallsBackOnSchemaWithoutActivityFlags(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	legacySchema := `
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY, parent_session_id TEXT, started_at REAL NOT NULL,
			ended_at REAL, last_activity_at REAL, git_repo_root TEXT, cwd TEXT,
			tool_call_count INTEGER, model TEXT, api_call_count INTEGER,
			input_tokens INTEGER, output_tokens INTEGER, cache_read_tokens INTEGER,
			cache_write_tokens INTEGER, reasoning_tokens INTEGER
		);
		CREATE TABLE messages (
			id INTEGER PRIMARY KEY, session_id TEXT, role TEXT, content TEXT,
			timestamp REAL NOT NULL, _compressed_summary INTEGER
		);
		CREATE TABLE session_model_usage (session_id TEXT, model TEXT, api_call_count INTEGER,
			input_tokens INTEGER, output_tokens INTEGER, cache_read_tokens INTEGER,
			cache_write_tokens INTEGER, reasoning_tokens INTEGER);
		INSERT INTO sessions (id, started_at, ended_at, tool_call_count)
			VALUES ('root', 1767520800, 1767521400, 4);
		INSERT INTO messages (id, session_id, role, content, timestamp, _compressed_summary)
			VALUES (1, 'root', 'user', 'Owner action', 1767520801, 0),
			       (2, 'root', 'user', 'Owner action', 1767520801, 0);
	`
	if _, err := db.Exec(legacySchema); err != nil {
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
	if result.Status != "supported with warnings" {
		t.Fatalf("status = %q, want an explicit warning instead of a hard failure", result.Status)
	}
	warned := false
	for _, warning := range result.Warnings {
		if warning.Code == "prompt_identity_unavailable" {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("warnings = %#v, want prompt_identity_unavailable", result.Warnings)
	}
	if len(result.Sessions) != 1 || len(result.Sessions[0].Prompts) != 2 || result.Sessions[0].ToolCalls != 4 {
		t.Fatalf("legacy fallback = %#v", result.Sessions)
	}
}
