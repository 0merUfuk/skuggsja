package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/klauspost/compress/zstd"
)

func TestReaderUsesModeAwarePromptEvents(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..", "testdata", "codex", "sessions")
	reader := Reader{SessionsDir: root, ArchivedDir: filepath.Join(t.TempDir(), "missing")}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	result := reader.Read(context.Background(), discovery)
	if got, want := len(result.Sessions), 3; got != want {
		t.Fatalf("session count = %d, want %d", got, want)
	}
	rootSession := sessionByID(result, "root-1")
	childSession := sessionByID(result, "child-1")
	paginatedSession := sessionByID(result, "page-root-1")
	if rootSession == nil || childSession == nil || paginatedSession == nil {
		t.Fatalf("missing fixture session: root=%v child=%v paginated=%v", rootSession != nil, childSession != nil, paginatedSession != nil)
	}
	if rootSession.IsChild || !childSession.IsChild || paginatedSession.IsChild {
		t.Fatalf("root/child classification = %v/%v/%v", rootSession.IsChild, childSession.IsChild, paginatedSession.IsChild)
	}
	if got, want := len(rootSession.Prompts), 1; got != want {
		t.Fatalf("legacy prompts = %d, want %d", got, want)
	}
	if got, want := rootSession.Prompts[0].Words, 4; got != want {
		t.Errorf("legacy prompt words = %d, want %d", got, want)
	}
	if got, want := rootSession.Usage.Input, int64(100); got != want {
		t.Errorf("final cumulative input tokens = %d, want %d", got, want)
	}
	if got, want := rootSession.ToolCalls, int64(1); got != want {
		t.Errorf("legacy tool calls = %d, want %d", got, want)
	}
	if got, want := len(childSession.Prompts), 0; got != want {
		t.Errorf("response_item-only child prompts = %d, want %d", got, want)
	}
	if got, want := len(paginatedSession.Prompts), 1; got != want {
		t.Fatalf("paginated prompts = %d, want %d", got, want)
	}
	if got, want := paginatedSession.Prompts[0].Words, 4; got != want {
		t.Errorf("paginated prompt words = %d, want %d", got, want)
	}
	if got, want := paginatedSession.ToolCalls, int64(1); got != want {
		t.Errorf("paginated tool calls = %d, want %d", got, want)
	}
	if got, want := childSession.StartedAt, time.Date(2026, 1, 3, 1, 0, 2, 0, time.UTC); !got.Equal(want) {
		t.Errorf("child physical start = %s, want %s", got, want)
	}
	if got, want := childSession.EndedAt, time.Date(2026, 1, 3, 1, 0, 4, 0, time.UTC); !got.Equal(want) {
		t.Errorf("child end = %s, want %s", got, want)
	}
}

func TestDiscoverPrefersPlainSiblingAndReadsZstd(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions", "2026", "01", "03")
	archivedDir := filepath.Join(root, "archived_sessions")
	for _, dir := range []string{sessionsDir, archivedDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll(%q): %v", dir, err)
		}
	}

	plainPath := filepath.Join(sessionsDir, "rollout-plain.jsonl")
	if err := os.WriteFile(plainPath, syntheticLegacyRollout("plain", "Count only this plain prompt."), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", plainPath, err)
	}
	// A corrupt compressed sibling proves discovery selected the plain file.
	if err := os.WriteFile(plainPath+".zst", []byte("not a zstd frame"), 0o600); err != nil {
		t.Fatalf("WriteFile(compressed sibling): %v", err)
	}

	compressedPath := filepath.Join(archivedDir, "rollout-compressed.jsonl.zst")
	writeZstd(t, compressedPath, syntheticLegacyRollout("compressed", "Read this compressed prompt."))

	reader := Reader{SessionsDir: filepath.Join(root, "sessions"), ArchivedDir: archivedDir}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if got, want := len(discovery.Files), 2; got != want {
		t.Fatalf("discovered files = %d, want %d: %v", got, want, discovery.Files)
	}
	for _, path := range discovery.Files {
		if path == plainPath+".zst" {
			t.Fatalf("selected compressed sibling instead of plain file: %v", discovery.Files)
		}
	}

	result := reader.Read(context.Background(), discovery)
	if got, want := len(result.Sessions), 2; got != want {
		t.Fatalf("parsed sessions = %d, want %d; warnings = %v", got, want, result.Warnings)
	}
	for _, id := range []string{"plain", "compressed"} {
		session := sessionByID(result, id)
		if session == nil {
			t.Fatalf("missing %q session", id)
		}
		if got, want := len(session.Prompts), 1; got != want {
			t.Errorf("%s prompts = %d, want %d", id, got, want)
		}
	}
}

func sessionByID(result model.ProviderResult, id string) *model.Session {
	for i := range result.Sessions {
		if result.Sessions[i].ID == id {
			return &result.Sessions[i]
		}
	}
	return nil
}

func syntheticLegacyRollout(id, prompt string) []byte {
	return []byte(fmt.Sprintf(
		"{\"timestamp\":\"2026-01-03T03:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":%q,\"session_id\":%q,\"cwd\":\"/synthetic/zstd\",\"source\":\"cli\",\"history_mode\":\"legacy\"}}\n"+
			"{\"timestamp\":\"2026-01-03T03:00:01Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":%q}}\n",
		id, id, prompt,
	))
}

func writeZstd(t *testing.T, path string, data []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(%q): %v", path, err)
	}
	encoder, err := zstd.NewWriter(f, zstd.WithEncoderConcurrency(1))
	if err != nil {
		_ = f.Close()
		t.Fatalf("NewWriter(%q): %v", path, err)
	}
	if _, err := encoder.Write(data); err != nil {
		encoder.Close()
		_ = f.Close()
		t.Fatalf("zstd write: %v", err)
	}
	if err := encoder.Close(); err != nil {
		_ = f.Close()
		t.Fatalf("zstd close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}
}
