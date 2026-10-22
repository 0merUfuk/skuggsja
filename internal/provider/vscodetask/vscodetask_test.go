package vscodetask

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/0merUfuk/skuggsja/internal/model"
)

func task(t *testing.T, root, id, ui, meta string) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, uiMessagesFile), []byte(ui), 0o600); err != nil {
		t.Fatal(err)
	}
	if meta != "" {
		if err := os.WriteFile(filepath.Join(dir, metadataFile), []byte(meta), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func read(t *testing.T, cfg Config) model.ProviderResult {
	t.Helper()
	r := Reader{Config: cfg}
	d, err := r.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return r.Read(context.Background(), d)
}

const clineTask = `[
	{"ts":1789000000000,"type":"say","say":"task","text":"Add a health check endpoint."},
	{"ts":1789000001000,"type":"say","say":"api_req_started","text":"{\"tokensIn\":1000,\"tokensOut\":200,\"cacheWrites\":10,\"cacheReads\":300,\"cost\":0.01}"},
	{"ts":1789000002000,"type":"say","say":"text","text":"I will add the handler."},
	{"ts":1789000060000,"type":"say","say":"user_feedback","text":"Also add a test"},
	{"ts":1789000061000,"type":"say","say":"api_req_started","text":"{\"tokensIn\":500,\"tokensOut\":100}"},
	{"ts":1789000062000,"type":"say","say":"deleted_api_reqs","text":"{\"tokensIn\":50,\"tokensOut\":5}"}
]`

func TestTaskMarkerFamilyCountsPromptsTimesAndUsage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	task(t, root, "t1", clineTask, `{"model_usage":[{"ts":1,"model_id":"claude-sonnet-5","model_provider_id":"anthropic","mode":"act"}]}`)
	out := read(t, Config{Harness: model.Cline, DisplayName: "Cline", TaskRoots: []string{root}, ReadsModelMetadata: true})
	if out.Status != "supported" || len(out.Sessions) != 1 {
		t.Fatalf("status=%q sessions=%d warnings=%v", out.Status, len(out.Sessions), out.Warnings)
	}
	s := out.Sessions[0]
	if len(s.Prompts) != 2 || s.Prompts[0].Words != 5 || s.Prompts[1].Words != 4 {
		t.Fatalf("prompts wrong (assistant 'text' must not count): %#v", s.Prompts)
	}
	if s.StartedAt.Unix() != 1789000000 || s.EndedAt.Unix() != 1789000062 || s.TimeUnavailable {
		t.Fatalf("message timestamps not used: %v..%v", s.StartedAt, s.EndedAt)
	}
	if s.Usage.Input != 1550 || s.Usage.Output != 305 || s.Usage.CacheRead != 300 || s.Usage.CacheWrite != 10 {
		t.Fatalf("usage not summed including deleted requests: %#v", s.Usage)
	}
	if a, ok := s.Models["claude-sonnet-5"]; !ok || a.Turns != 2 {
		t.Fatalf("single-model task not attributed: %#v", s.Models)
	}
}

func TestLegacyFirstTextMessageIsTheTaskPrompt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	task(t, root, "t1", `[
		{"ts":1789000000000,"type":"say","say":"text","text":"Refactor the parser please"},
		{"ts":1789000001000,"type":"say","say":"text","text":"Sure, starting now."},
		{"ts":1789000002000,"type":"say","say":"user_feedback","text":"thanks"}
	]`, "")
	s := read(t, Config{Harness: model.Roo, DisplayName: "Roo Code", TaskRoots: []string{root}}).Sessions[0]
	if len(s.Prompts) != 2 || s.Prompts[0].Words != 4 {
		t.Fatalf("legacy task prompt wrong: %#v", s.Prompts)
	}
	if len(s.Models) != 0 {
		t.Fatalf("model invented for a family without model metadata: %#v", s.Models)
	}
}

func TestModelSwitchingTaskIsNotAttributed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	task(t, root, "t1", clineTask, `{"model_usage":[{"model_id":"a"},{"model_id":"b"}]}`)
	s := read(t, Config{Harness: model.Cline, DisplayName: "Cline", TaskRoots: []string{root}, ReadsModelMetadata: true}).Sessions[0]
	if len(s.Models) != 0 {
		t.Fatalf("ambiguous model switch was attributed: %#v", s.Models)
	}
}

func TestTasksWithoutUserPromptsAndMalformedFilesAreSkipped(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	task(t, root, "empty", `[{"ts":1,"type":"say","say":"api_req_started","text":"{}"}]`, "")
	task(t, root, "bad", `{nope`, "")
	task(t, root, "good", clineTask, "")
	out := read(t, Config{Harness: model.Cline, DisplayName: "Cline", TaskRoots: []string{root}})
	if len(out.Sessions) != 1 || out.Sessions[0].ID != "good" || out.Status != "supported with warnings" {
		t.Fatalf("status=%q sessions=%v warnings=%v", out.Status, out.Sessions, out.Warnings)
	}
}

func TestPartialMessagesAndMissingRootsAreIgnored(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	task(t, root, "t1", `[
		{"ts":1789000000000,"type":"say","say":"task","text":"one two three"},
		{"ts":1789000001000,"type":"say","say":"user_feedback","text":"streaming","partial":true}
	]`, "")
	out := read(t, Config{Harness: model.Cline, DisplayName: "Cline", TaskRoots: []string{root, filepath.Join(root, "absent"), ""}})
	if len(out.Sessions) != 1 || len(out.Sessions[0].Prompts) != 1 {
		t.Fatalf("partial feedback counted: %#v", out.Sessions)
	}
	none := read(t, Config{Harness: model.Roo, DisplayName: "Roo Code", TaskRoots: nil})
	if none.Status != "not found" {
		t.Fatalf("status = %q", none.Status)
	}
}
