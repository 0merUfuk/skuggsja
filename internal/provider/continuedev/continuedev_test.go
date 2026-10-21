package continuedev

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverSkipsTheIndexAndMissingRoots(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, dir, "a.json", "{}")
	write(t, dir, "sessions.json", "[]")
	write(t, dir, "notes.txt", "x")
	d, err := (Reader{SessionsRoot: dir}).Discover(context.Background())
	if err != nil || len(d.Files) != 1 || filepath.Base(d.Files[0]) != "a.json" || len(d.AuditFiles) != 1 {
		t.Fatalf("unexpected discovery %#v err=%v", d, err)
	}
	for _, r := range []Reader{{}, {SessionsRoot: filepath.Join(dir, "absent")}} {
		d, err := r.Discover(context.Background())
		if err != nil || len(d.Files) != 0 {
			t.Fatalf("absent root must discover nothing without error: %#v err=%v", d, err)
		}
	}
}

func TestReaderCountsUserMessagesAndUsesIndexTimes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, dir, "sessions.json", `[{"sessionId":"s1","title":"t","dateCreated":"1789000000000","workspaceDirectory":"/synthetic/continue-app","messageCount":2}]`)
	write(t, dir, "s1.json", `{
		"sessionId":"s1","title":"t","workspaceDirectory":"/synthetic/continue-app","chatModelTitle":"Claude Sonnet",
		"history":[
			{"message":{"role":"user","content":"Add a health check endpoint."},"contextItems":[]},
			{"message":{"role":"assistant","content":"Done."},"contextItems":[]},
			{"message":{"role":"user","content":[{"type":"text","text":"Now test it"},{"type":"imageUrl","imageUrl":{"url":"x"}}]},"contextItems":[]},
			{"message":{"role":"assistant","content":"Tested."},"contextItems":[]}
		],
		"usage":{"promptTokens":1200,"completionTokens":300,"totalCost":0.02,
			"promptTokensDetails":{"cachedTokens":200,"cacheWriteTokens":50},
			"completionTokensDetails":{"reasoningTokens":40}}
	}`)
	write(t, dir, "empty.json", `{"sessionId":"empty","history":[{"message":{"role":"assistant","content":"hi"}}]}`)
	r := Reader{SessionsRoot: dir}
	d, _ := r.Discover(context.Background())
	out := r.Read(context.Background(), d)
	if out.Status != "supported" || len(out.Sessions) != 1 {
		t.Fatalf("status=%q sessions=%d warnings=%v", out.Status, len(out.Sessions), out.Warnings)
	}
	s := out.Sessions[0]
	if s.ID != "s1" || s.Project != "continue-app" || len(s.Prompts) != 2 {
		t.Fatalf("unexpected session %#v", s)
	}
	if s.Prompts[1].Words != 3 {
		t.Fatalf("array-form content not flattened to text parts: %#v", s.Prompts[1])
	}
	if s.StartedAt.Unix() != 1789000000 || s.TimeUnavailable {
		t.Fatalf("index creation time lost: %v unavailable=%v", s.StartedAt, s.TimeUnavailable)
	}
	if !s.Usage.Available || s.Usage.Input != 1200 || s.Usage.Output != 300 || s.Usage.CacheRead != 200 || s.Usage.CacheWrite != 50 || s.Usage.Reasoning != 40 {
		t.Fatalf("usage mapping wrong: %#v", s.Usage)
	}
	if activity, ok := s.Models["Claude Sonnet"]; !ok || activity.Turns != 2 {
		t.Fatalf("model activity wrong: %#v", s.Models)
	}
}

func TestSessionMissingFromIndexHasNoInventedTime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, dir, "sessions.json", `[]`)
	write(t, dir, "orphan.json", `{"sessionId":"orphan","history":[{"message":{"role":"user","content":"hello there"}}]}`)
	r := Reader{SessionsRoot: dir}
	d, _ := r.Discover(context.Background())
	out := r.Read(context.Background(), d)
	if len(out.Sessions) != 1 || !out.Sessions[0].TimeUnavailable || !out.Sessions[0].StartedAt.IsZero() {
		t.Fatalf("time was invented for an unindexed session: %#v", out.Sessions)
	}
	if out.Sessions[0].Usage.Available {
		t.Fatal("usage appeared without a usage block")
	}
}

func TestMalformedSessionIsSkippedWithAWarning(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, dir, "bad.json", `{not json`)
	write(t, dir, "good.json", `{"sessionId":"g","history":[{"message":{"role":"user","content":"one"}}]}`)
	r := Reader{SessionsRoot: dir}
	d, _ := r.Discover(context.Background())
	out := r.Read(context.Background(), d)
	if len(out.Sessions) != 1 || out.Status != "supported with warnings" {
		t.Fatalf("status=%q sessions=%d", out.Status, len(out.Sessions))
	}
}
