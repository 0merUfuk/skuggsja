package gemini

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, root, project, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, project, "chats")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const jsonl = `{"sessionId":"s1","projectHash":"abc","startTime":"2026-10-20T10:00:00.000Z","lastUpdated":"2026-10-20T10:00:00.000Z"}
{"id":"m1","timestamp":"2026-10-20T10:00:01.000Z","type":"user","content":[{"text":"Add a health check endpoint."}]}
{"id":"m2","timestamp":"2026-10-20T10:00:05.000Z","type":"gemini","content":"Done.","model":"gemini-3-pro","tokens":{"input":1000,"output":200,"cached":300,"thoughts":40,"tool":0,"total":1240},"toolCalls":[{"id":"t1","name":"write_file"},{"id":"t2","name":"run_shell_command"}]}
{"id":"m3","timestamp":"2026-10-20T10:01:00.000Z","type":"user","content":"Now add a test"}
{"id":"m4","timestamp":"2026-10-20T10:01:30.000Z","type":"gemini","content":"Added.","model":"gemini-3-pro","tokens":{"input":500,"output":100,"cached":0,"total":600}}
{"$set":{"lastUpdated":"2026-10-20T10:02:00.000Z"}}
`

func TestJSONLRecordingCountsPromptsToolsTokensAndModel(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	put(t, root, "my-app", "session-2026-10-20T10-00-abc.jsonl", jsonl)
	r := Reader{TmpRoot: root}
	d, err := r.Discover(context.Background())
	if err != nil || len(d.Files) != 1 {
		t.Fatalf("discovery %#v err=%v", d, err)
	}
	out := r.Read(context.Background(), d)
	if out.Status != "supported" || len(out.Sessions) != 1 {
		t.Fatalf("status=%q sessions=%d warnings=%v", out.Status, len(out.Sessions), out.Warnings)
	}
	s := out.Sessions[0]
	if s.ID != "s1" || s.Project != "my-app" || len(s.Prompts) != 2 || s.Prompts[0].Words != 5 {
		t.Fatalf("unexpected session %#v", s)
	}
	if s.ToolCalls != 2 || s.Usage.Input != 1500 || s.Usage.Output != 300 || s.Usage.CacheRead != 300 || s.Usage.Reasoning != 40 {
		t.Fatalf("tools/usage wrong: tools=%d usage=%#v", s.ToolCalls, s.Usage)
	}
	if a := s.Models["gemini-3-pro"]; a.Turns != 2 || a.Usage.Input != 1500 {
		t.Fatalf("model activity wrong: %#v", s.Models)
	}
	if s.EndedAt.Format("15:04:05") != "10:02:00" {
		t.Fatalf("$set lastUpdated ignored: %v", s.EndedAt)
	}
}

func TestRewindPatchAndRemoveFollowGeminiLoaderSemantics(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	put(t, root, "app", "session-1.jsonl", `{"sessionId":"s","projectHash":"h","startTime":"2026-10-20T10:00:00.000Z"}
{"id":"m1","timestamp":"2026-10-20T10:00:01.000Z","type":"user","content":"first prompt here"}
{"id":"m2","timestamp":"2026-10-20T10:00:02.000Z","type":"gemini","content":"ok"}
{"id":"m3","timestamp":"2026-10-20T10:00:03.000Z","type":"user","content":"rewound away"}
{"id":"m4","timestamp":"2026-10-20T10:00:04.000Z","type":"user","content":"also removed"}
{"$rewindTo":"m3"}
{"id":"m5","timestamp":"2026-10-20T10:00:05.000Z","type":"user","content":"x"}
{"$patch":{"id":"m5","content":"five word patched user prompt"}}
{"$patch":{"removeIds":["m2"]}}
`)
	r := Reader{TmpRoot: root}
	d, _ := r.Discover(context.Background())
	s := r.Read(context.Background(), d).Sessions[0]
	if len(s.Prompts) != 2 || s.Prompts[0].Words != 3 || s.Prompts[1].Words != 5 {
		t.Fatalf("rewind/patch not applied: %#v", s.Prompts)
	}
}

func TestLegacyJSONIsReadAndJSONLWinsOnSameStem(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	put(t, root, "app", "session-a.json", `{"sessionId":"legacy","startTime":"2026-10-20T10:00:00.000Z","lastUpdated":"2026-10-20T10:05:00.000Z",
		"messages":[{"id":"m1","timestamp":"2026-10-20T10:00:01.000Z","type":"user","content":"legacy prompt text"}]}`)
	put(t, root, "app", "session-b.json", `{"sessionId":"dup-old","messages":[{"id":"m1","type":"user","content":"stale copy"}]}`)
	put(t, root, "app", "session-b.jsonl", `{"sessionId":"dup-new","projectHash":"h"}`+"\n"+`{"id":"m1","timestamp":"2026-10-20T11:00:00.000Z","type":"user","content":"current copy here"}`+"\n")
	r := Reader{TmpRoot: root}
	d, _ := r.Discover(context.Background())
	out := r.Read(context.Background(), d)
	ids := map[string]bool{}
	for _, s := range out.Sessions {
		ids[s.ID] = true
	}
	if len(out.Sessions) != 2 || !ids["legacy"] || !ids["dup-new"] || ids["dup-old"] {
		t.Fatalf("unexpected sessions %v", ids)
	}
}

func TestHashFolderHasNoProjectAndSubagentIsAChild(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	hash := strings.Repeat("ab", 32)
	put(t, root, hash, "session-1.jsonl", `{"sessionId":"main","projectHash":"h"}`+"\n"+`{"id":"m1","timestamp":"2026-10-20T10:00:00.000Z","type":"user","content":"hello world"}`+"\n")
	sub := filepath.Join(root, "app", "chats", "parent-session")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "session-sub.jsonl"), []byte(`{"sessionId":"child","projectHash":"h","kind":"subagent"}`+"\n"+`{"id":"m1","timestamp":"2026-10-20T10:00:00.000Z","type":"user","content":"delegated work item"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := Reader{TmpRoot: root}
	d, _ := r.Discover(context.Background())
	byID := map[string]bool{}
	for _, s := range r.Read(context.Background(), d).Sessions {
		switch s.ID {
		case "main":
			if s.Project != "" {
				t.Fatalf("hash folder leaked as project %q", s.Project)
			}
		case "child":
			byID["child"] = s.IsChild && s.ParentID == "parent-session" && s.Project == "app"
		}
	}
	if !byID["child"] {
		t.Fatal("subagent recording not linked to its parent")
	}
}

func TestMalformedRecordsAreCountedNotFatal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	put(t, root, "app", "session-1.jsonl", "{not json\n"+`{"id":"m1","timestamp":"2026-10-20T10:00:00.000Z","type":"user","content":"still counted"}`+"\n")
	r := Reader{TmpRoot: root}
	d, _ := r.Discover(context.Background())
	out := r.Read(context.Background(), d)
	if len(out.Sessions) != 1 || out.Status != "supported with warnings" {
		t.Fatalf("status=%q sessions=%d", out.Status, len(out.Sessions))
	}
	none, _ := (Reader{}).Discover(context.Background())
	if len(none.Files) != 0 || len(none.Roots) != 0 || len(none.ProtectedDirectories) != 0 {
		t.Fatalf("empty root discovered paths: %#v", none)
	}
}
