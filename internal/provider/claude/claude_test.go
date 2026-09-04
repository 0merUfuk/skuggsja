package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReaderExcludesSubagentsAndDerivesMetrics(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..", "testdata", "claude")
	reader := Reader{ProjectsDir: root}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if got, want := len(discovery.Files), 1; got != want {
		t.Fatalf("source file count = %d, want %d", got, want)
	}
	result := reader.Read(context.Background(), discovery)
	if got, want := len(result.Sessions), 1; got != want {
		t.Fatalf("session count = %d, want %d", got, want)
	}
	session := result.Sessions[0]
	if got, want := len(session.Prompts), 2; got != want {
		t.Errorf("prompt count = %d, want %d", got, want)
	}
	if got, want := len(session.Calls), 1; got != want {
		t.Errorf("deduplicated call count = %d, want %d", got, want)
	}
	if got, want := session.Usage.Input, int64(100); got != want {
		t.Errorf("input tokens = %d, want %d", got, want)
	}
	if got, want := session.ToolCalls, int64(1); got != want {
		t.Errorf("tool calls = %d, want %d", got, want)
	}
	if session.Project != "project-one" {
		t.Errorf("project = %q, want project-one", session.Project)
	}
}

func TestMissingUsageRemainsUnavailable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "synthetic-project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, "session.jsonl")
	line := `{"type":"assistant","uuid":"event-1","sessionId":"session-1","cwd":"/synthetic/project","timestamp":"2026-01-02T21:00:03Z","message":{"id":"message-1","role":"assistant","model":"claude-synthetic","content":[]}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := Reader{ProjectsDir: root}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if len(result.Sessions) != 1 {
		t.Fatalf("sessions = %#v", result.Sessions)
	}
	if result.Sessions[0].Usage.Available || result.Sessions[0].Calls[0].Usage.Available {
		t.Fatal("absent message.usage was reported as available")
	}
	if result.Sessions[0].Models["claude-synthetic"].Turns != 1 {
		t.Fatal("model event should remain countable without token usage")
	}
}
