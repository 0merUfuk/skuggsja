package grok

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverFindsPromptHistoryUnderEachProjectDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	withHistory := filepath.Join(root, "%2FUsers%2Fjane%2Fapp")
	if err := os.MkdirAll(withHistory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withHistory, "prompt_history.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withoutHistory := filepath.Join(root, "%2FUsers%2Fjane%2Fempty")
	if err := os.MkdirAll(withoutHistory, 0o700); err != nil {
		t.Fatal(err)
	}
	discovery, err := (Reader{SessionsRoot: root}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Files) != 1 || discovery.Files[0] != filepath.Join(withHistory, "prompt_history.jsonl") {
		t.Fatalf("unexpected discovered files: %#v", discovery.Files)
	}

	empty, err := (Reader{}).Discover(context.Background())
	if err != nil || len(empty.Files) != 0 || len(empty.Roots) != 0 {
		t.Fatalf("empty sessions root discovered unrelated paths: %#v error=%v", empty, err)
	}

	missing, err := (Reader{SessionsRoot: filepath.Join(root, "does-not-exist")}).Discover(context.Background())
	if err != nil || len(missing.Files) != 0 {
		t.Fatalf("missing sessions root should discover nothing, not error: %#v error=%v", missing, err)
	}
}

func TestReaderGroupsPromptsBySessionAndDerivesSpan(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	projectDir := filepath.Join(root, "%2FUsers%2Fjane%2Fapp")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	history := `{"is_bash":false,"prompt":"Add a health check endpoint.","session_id":"sess-1","timestamp":"2026-09-20T10:00:00Z"}
{"is_bash":true,"prompt":"go test ./...","session_id":"sess-1","timestamp":"2026-09-20T10:05:00Z"}
{"is_bash":false,"prompt":"Now do the same for the other service.","session_id":"sess-2","timestamp":"2026-09-21T09:00:00Z"}
`
	path := filepath.Join(projectDir, "prompt_history.jsonl")
	if err := os.WriteFile(path, []byte(history), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := Reader{SessionsRoot: root}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if result.Status != "supported" {
		t.Fatalf("status = %q, warnings = %v", result.Status, result.Warnings)
	}
	if len(result.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(result.Sessions))
	}
	first := result.Sessions[0]
	if first.ID != "sess-1" || len(first.Prompts) != 2 {
		t.Fatalf("unexpected first session: id=%q prompts=%d", first.ID, len(first.Prompts))
	}
	if first.Project != "app" {
		t.Fatalf("project = %q, want app (decoded from the URL-encoded directory name)", first.Project)
	}
	if !first.EndedAt.After(first.StartedAt) {
		t.Fatalf("span not derived from prompt timestamps: started=%v ended=%v", first.StartedAt, first.EndedAt)
	}
}

func TestReaderSkipsMalformedAndUnidentifiedLinesWithoutCrashing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	projectDir := filepath.Join(root, "%2FUsers%2Fjane%2Fapp")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	history := "not json at all\n" +
		`{"is_bash":false,"prompt":"missing a session id","timestamp":"2026-09-20T10:00:00Z"}` + "\n" +
		`{"is_bash":false,"prompt":"valid record","session_id":"sess-1","timestamp":"2026-09-20T10:00:00Z"}` + "\n"
	path := filepath.Join(projectDir, "prompt_history.jsonl")
	if err := os.WriteFile(path, []byte(history), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := Reader{SessionsRoot: root}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if result.Status != "supported with warnings" {
		t.Fatalf("status = %q, want supported with warnings", result.Status)
	}
	if len(result.Sessions) != 1 || len(result.Sessions[0].Prompts) != 1 {
		t.Fatalf("unexpected sessions after skipping bad lines: %#v", result.Sessions)
	}
}
