package cursor

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/0merUfuk/skuggsja/internal/model"
	_ "modernc.org/sqlite"
)

func TestReaderParsesCurrentComposerSchemaWithoutTokenEstimates(t *testing.T) {
	t.Parallel()
	path := createFixture(t, `
		CREATE TABLE composerHeaders (
			composerId TEXT PRIMARY KEY,
			workspaceId TEXT,
			createdAt INTEGER,
			lastUpdatedAt INTEGER,
			isArchived INTEGER,
			isSubagent INTEGER,
			recency INTEGER,
			checkpointAt INTEGER,
			subagentTypeName TEXT,
			value TEXT
		);
		CREATE INDEX idx_composerHeaders_0
			ON composerHeaders (workspaceId, isSubagent, isArchived, recency);
		CREATE TABLE cursorDiskKV (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB);

		INSERT INTO composerHeaders
			(composerId, workspaceId, createdAt, lastUpdatedAt, isArchived, isSubagent, recency, value)
		VALUES
			('composer-1', 'synthetic', 1767607200000, 1767607800000, 0, 0, 1767607800000,
			 '{"type":"head","composerId":"composer-1","createdAt":1767607200000,"lastUpdatedAt":1767607800000}'),
			('composer-empty', 'synthetic', 1767607200000, 1767607800000, 0, 0, 1767607800000,
			 '{"type":"head","composerId":"composer-empty","createdAt":1767607200000}'),
			('composer-missing-only', 'synthetic', 1767607200000, 1767607800000, 0, 0, 1767607800000,
			 '{"type":"head","composerId":"composer-missing-only","createdAt":1767607200000}'),
			('composer-child', 'synthetic', 1767607200000, 1767607800000, 0, 1, 1767607800000,
			 '{"type":"head","composerId":"composer-child","createdAt":1767607200000,"subagentInfo":{"parentComposerId":"composer-1"}}'),
			('composer-draft', 'synthetic', 1767607200000, 1767607800000, 0, 0, 1767607800000,
			 '{"type":"head","composerId":"composer-draft","createdAt":1767607200000,"isDraft":true}');

		INSERT INTO cursorDiskKV VALUES ('composerData:composer-1',
			'{"_v":18,"composerId":"composer-1","workspaceIdentifier":{"id":"synthetic","uri":"file:///synthetic/cursor-project"},"modelConfig":{"modelName":"configured-not-counted"},"fullConversationHeadersOnly":[{"bubbleId":"bubble-1","type":1,"createdAt":"2026-01-05T10:00:01Z"},{"bubbleId":"bubble-missing","type":1,"createdAt":"2026-01-05T10:00:02Z"},{"bubbleId":"bubble-display","type":1,"isDisplayOnly":true},{"bubbleId":"bubble-ai","type":2}]}');
		INSERT INTO cursorDiskKV VALUES ('composerData:composer-empty',
			'{"_v":18,"composerId":"composer-empty","fullConversationHeadersOnly":[{"bubbleId":"empty-ai","type":2}]}');
		INSERT INTO cursorDiskKV VALUES ('composerData:composer-missing-only',
			'{"_v":18,"composerId":"composer-missing-only","fullConversationHeadersOnly":[{"bubbleId":"never-stored","type":1}]}');
		INSERT INTO cursorDiskKV VALUES ('composerData:composer-child',
			'{"_v":18,"composerId":"composer-child","fullConversationHeadersOnly":[{"bubbleId":"child-human","type":1}]}');
		INSERT INTO cursorDiskKV VALUES ('composerData:composer-draft',
			'{"_v":18,"composerId":"composer-draft","fullConversationHeadersOnly":[{"bubbleId":"draft-human","type":1}]}');

		INSERT INTO cursorDiskKV VALUES ('bubbleId:composer-1:bubble-1',
			'{"_v":3,"bubbleId":"bubble-1","type":1,"text":"Explain the failing synthetic test.","createdAt":"2026-01-05T10:00:01Z","modelInfo":{"modelName":"cursor-synthetic"},"tokenCount":{"inputTokens":999999}}');
		INSERT INTO cursorDiskKV VALUES ('bubbleId:composer-1:bubble-display',
			'{"_v":3,"bubbleId":"bubble-display","type":1,"text":"Must not count."}');
		INSERT INTO cursorDiskKV VALUES ('bubbleId:composer-1:bubble-ai',
			'{"_v":3,"bubbleId":"bubble-ai","type":2,"text":"Synthetic assistant response."}');
		INSERT INTO cursorDiskKV VALUES ('bubbleId:composer-child:child-human',
			'{"_v":3,"bubbleId":"child-human","type":1,"text":"Child prompt."}');
		INSERT INTO cursorDiskKV VALUES ('bubbleId:composer-draft:draft-human',
			'{"_v":3,"bubbleId":"draft-human","type":1,"text":"Draft prompt."}');
	`)

	result := readFixture(t, path)
	if result.Status != "supported with warnings" || len(result.Sessions) != 2 {
		t.Fatalf("result status/sessions = %q/%d, warnings=%v", result.Status, len(result.Sessions), result.Warnings)
	}
	if warningCount(result, "missing_bubble_body") != 2 {
		t.Fatalf("missing-body warnings = %+v", result.Warnings)
	}
	if result.VerificationLevel != "schema-verified with synthetic fixtures" {
		t.Fatalf("verification level = %q", result.VerificationLevel)
	}

	var top, childFound bool
	for _, session := range result.Sessions {
		switch session.ID {
		case "composer-1":
			top = true
			if session.IsChild || session.Project != "cursor-project" {
				t.Fatalf("top-level session = %+v", session)
			}
			if len(session.Prompts) != 1 || session.Prompts[0].Words != 5 {
				t.Fatalf("prompts = %+v", session.Prompts)
			}
			if session.Usage.Available {
				t.Fatal("Cursor token usage must remain unavailable")
			}
			if session.Models["cursor-synthetic"].Turns != 1 || session.Models["configured-not-counted"].Turns != 0 {
				t.Fatalf("model activity = %+v", session.Models)
			}
		case "composer-child":
			childFound = true
			if !session.IsChild || len(session.Prompts) != 1 {
				t.Fatalf("child session = %+v", session)
			}
		default:
			t.Fatalf("unexpected session %q", session.ID)
		}
	}
	if !top || !childFound {
		t.Fatalf("sessions = %+v", result.Sessions)
	}
}

func TestReaderFeatureDetectsModernValueOnlyHeaders(t *testing.T) {
	t.Parallel()
	path := createFixture(t, `
		CREATE TABLE composerHeaders (composerId TEXT PRIMARY KEY, value TEXT);
		CREATE TABLE cursorDiskKV (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB);
		INSERT INTO composerHeaders VALUES ('composer-value',
			'{"composerId":"composer-value","createdAt":1767607200000,"lastUpdatedAt":1767607800000,"isBestOfNSubcomposer":true}');
		INSERT INTO cursorDiskKV VALUES ('bubbleId:composer-value:bubble-value',
			'{"bubbleId":"bubble-value","type":1,"text":"Synthetic value-only header."}');
	`)
	result := readFixture(t, path)
	if result.Status != "supported" || len(result.Sessions) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if !result.Sessions[0].IsChild || result.Sessions[0].StartedAt.IsZero() {
		t.Fatalf("session = %+v", result.Sessions[0])
	}
}

func TestReaderFallsBackToLegacyHeaderBlobsAndDeduplicates(t *testing.T) {
	t.Parallel()
	path := createFixture(t, `
		CREATE TABLE ItemTable (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB);
		CREATE TABLE cursorDiskKV (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB);
		INSERT INTO ItemTable VALUES ('composer.composerHeaders',
			'{"allComposers":[{"composerId":"legacy-1","name":"Synthetic","createdAt":1735689600000},{"composerId":"legacy-empty","name":"Empty","createdAt":1735689600000}]}');
		INSERT INTO ItemTable VALUES ('composer.composerData',
			'{"allComposers":[{"composerId":"legacy-1","name":"Duplicate","createdAt":1735689600000,"subagentInfo":{"parentComposerId":"must-not-win"}}]}');
		INSERT INTO cursorDiskKV VALUES ('bubbleId:legacy-1:legacy-bubble',
			'{"bubbleId":"legacy-bubble","type":1,"text":"Legacy synthetic prompt."}');
	`)
	result := readFixture(t, path)
	if result.Status != "supported" || len(result.Sessions) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.Sessions[0].ID != "legacy-1" || result.Sessions[0].IsChild || len(result.Sessions[0].Prompts) != 1 {
		t.Fatalf("legacy session = %+v", result.Sessions[0])
	}
}

func TestReaderReportsUnknownSchema(t *testing.T) {
	t.Parallel()
	path := createFixture(t, "CREATE TABLE unrelated (value TEXT);")
	result := readFixture(t, path)
	if result.Status != "unsupported schema" || warningCount(result, "unknown_schema") != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestDiscoverRejectsSymbolicLinkDatabase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target.vscdb")
	if err := os.WriteFile(target, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "state.vscdb")
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatal(err)
	}
	_, err := (Reader{DatabasePath: link}).Discover(context.Background())
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Discover() error = %v", err)
	}
}

func createFixture(t *testing.T, schema string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFixture(t *testing.T, path string) model.ProviderResult {
	t.Helper()
	reader := Reader{DatabasePath: path}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return reader.Read(context.Background(), discovery)
}

func warningCount(result model.ProviderResult, code string) int {
	for _, warning := range result.Warnings {
		if warning.Code == code {
			return warning.Count
		}
	}
	return 0
}
