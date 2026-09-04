// Command verification-fixtures creates disposable synthetic SQLite histories
// for the runtime-isolation harness. It is not part of release builds.
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: verification-fixtures HERMES_DB CURSOR_DB")
		os.Exit(2)
	}
	if err := createHermes(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "create synthetic Hermes fixture:", err)
		os.Exit(1)
	}
	if err := createCursor(os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "create synthetic Cursor fixture:", err)
		os.Exit(1)
	}
}

func createHermes(path string) error {
	return createDatabase(path, `
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
		INSERT INTO sessions VALUES ('synthetic-hermes', NULL, 1767520800, 1767521400, 1767521400,
			'/synthetic/hermes-project', '/synthetic/hermes-project', 2, 'hermes-synthetic', 1,
			80, 20, 10, 2, 1);
		INSERT INTO messages VALUES (1, 'synthetic-hermes', 'user', 'Review the synthetic patch.', 1767520801, 0);
		INSERT INTO session_model_usage VALUES ('synthetic-hermes', 'hermes-synthetic', 1, 80, 20, 10, 2, 1);
	`)
}

func createCursor(path string) error {
	return createDatabase(path, `
		CREATE TABLE composerHeaders (
			composerId TEXT PRIMARY KEY, createdAt INTEGER, lastUpdatedAt INTEGER,
			isSubagent INTEGER, value TEXT
		);
		CREATE TABLE cursorDiskKV (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB);
		INSERT INTO composerHeaders VALUES ('synthetic-cursor', 1767607200000, 1767607800000, 0,
			'{"composerId":"synthetic-cursor","createdAt":1767607200000,"lastUpdatedAt":1767607800000}');
		INSERT INTO cursorDiskKV VALUES ('composerData:synthetic-cursor',
			'{"composerId":"synthetic-cursor","workspaceIdentifier":{"uri":"file:///synthetic/cursor-project"},"fullConversationHeadersOnly":[{"bubbleId":"synthetic-bubble","type":1,"createdAt":"2026-01-05T10:00:01Z"}]}');
		INSERT INTO cursorDiskKV VALUES ('bubbleId:synthetic-cursor:synthetic-bubble',
			'{"bubbleId":"synthetic-bubble","type":1,"text":"Explain the synthetic fixture.","createdAt":"2026-01-05T10:00:01Z","modelInfo":{"modelName":"cursor-synthetic"}}');
	`)
}

func createDatabase(path, schema string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	if _, err := database.Exec(schema); err != nil {
		_ = database.Close()
		return err
	}
	return database.Close()
}
