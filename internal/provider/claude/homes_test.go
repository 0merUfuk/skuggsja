package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0merUfuk/skuggsja/internal/analytics"
	"github.com/0merUfuk/skuggsja/internal/model"
)

func writeHomeEvidence(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readHomes(t *testing.T, reader Reader) model.ProviderResult {
	t.Helper()
	d, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return reader.Read(context.Background(), d)
}

func TestAdditionalHomesRetainActiveCanonicalAndAlternateEvidence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	canonical, active, alternate := filepath.Join(root, ".claude"), filepath.Join(root, "active"), filepath.Join(root, ".claude-other")
	for _, sample := range []struct{ home, id string }{{canonical, "canonical"}, {active, "active"}, {alternate, "alternate"}} {
		writeHomeEvidence(t, filepath.Join(sample.home, "projects", "project", sample.id+".jsonl"), `{"type":"user","uuid":"`+sample.id+`","sessionId":"`+sample.id+`","timestamp":"2026-07-01T00:00:00Z","message":{"role":"user","content":"Synthetic prompt"}}`+"\n")
	}
	writeHomeEvidence(t, filepath.Join(root, ".claude.json"), `{"projects":{"p":{"lastSessionId":"main-index-only","lastStartTime":1767225600000}}}`)
	writeHomeEvidence(t, filepath.Join(active, ".claude.json"), `{"projects":{"p":{"lastSessionId":"active-index-only","lastStartTime":1767225600000}}}`)
	writeHomeEvidence(t, filepath.Join(alternate, "backups", ".claude.json.backup.123"), `{"projects":{"p":{"lastSessionId":"backup-index-only","lastStartTime":1767225600000}}}`)
	writeHomeEvidence(t, filepath.Join(alternate, "projects", "project", "sessions-index.json"), `{"version":1,"entries":[{"sessionId":"project-index-only","created":"2025-12-01T00:00:00Z","fullPath":"/never/discover/from/index.jsonl"}]}`)
	writeHomeEvidence(t, filepath.Join(alternate, "stats-cache.json"), `{"firstSessionDate":"2025-11-01T00:00:00Z"}`)
	reader := Reader{ProjectsDir: filepath.Join(active, "projects"), GlobalStateFile: filepath.Join(root, ".claude.json"), ExtraHomes: []string{active, canonical, alternate}}
	d, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 3 {
		t.Fatalf("distinct transcript files = %d, want 3", len(d.Files))
	}
	for _, home := range reader.ExtraHomes {
		if !containsPath(d.Roots, home) {
			t.Fatalf("extra home missing from guard declarations: %s", home)
		}
	}
	if containsPath(d.Files, "/never/discover/from/index.jsonl") {
		t.Fatal("stored fullPath became a discovery input")
	}
	result := reader.Read(context.Background(), d)
	if len(result.Sessions) != 3 || result.Coverage.UnmaterializedSessions != 4 {
		t.Fatalf("sessions=%d coverage=%#v", len(result.Sessions), result.Coverage)
	}
	if got := result.Coverage.EarliestLocalEvidence.Format(time.RFC3339); got != "2025-11-01T00:00:00Z" {
		t.Fatalf("earliest local=%s", got)
	}
}

func TestAdditionalHomesCoalesceIdenticalAndPartialPhysicalCopies(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "identical"
		if partial {
			name = "partial"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			primary, extra := filepath.Join(root, "primary"), filepath.Join(root, "extra")
			first := `{"type":"user","uuid":"prompt-1","sessionId":"shared","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"First synthetic prompt"}}` + "\n" + `{"type":"assistant","sessionId":"shared","timestamp":"2026-01-01T00:00:01Z","message":{"role":"assistant","id":"response-1","model":"claude-test","content":[{"type":"tool_use","id":"tool-1"}],"usage":{"input_tokens":10,"output_tokens":2}}}` + "\n"
			second := first
			if partial {
				second = `{"type":"user","uuid":"prompt-1","sessionId":"shared","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"First synthetic prompt"}}` + "\n" + `{"type":"assistant","sessionId":"shared","timestamp":"2026-01-01T00:00:02Z","message":{"role":"assistant","id":"response-1","model":"claude-test","content":[{"type":"tool_use","id":"tool-2"}],"usage":{"input_tokens":10,"output_tokens":5}}}` + "\n" + `{"type":"user","uuid":"prompt-2","sessionId":"shared","timestamp":"2026-01-02T00:00:00Z","message":{"role":"user","content":"Later synthetic prompt"}}` + "\n" + `{"type":"assistant","sessionId":"shared","timestamp":"2026-01-02T00:00:01Z","message":{"role":"assistant","id":"response-2","model":"claude-test","content":[],"usage":{"input_tokens":7,"output_tokens":3}}}` + "\n"
			}
			writeHomeEvidence(t, filepath.Join(primary, "projects", "project", "shared.jsonl"), first)
			writeHomeEvidence(t, filepath.Join(extra, "projects", "project", "shared.jsonl"), second)
			child := `{"type":"user","uuid":"child-prompt","agentId":"child","sessionId":"shared","isSidechain":true,"timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":"Synthetic child instruction"}}` + "\n"
			for _, home := range []string{primary, extra} {
				writeHomeEvidence(t, filepath.Join(home, "projects", "project", "shared", "subagents", "workflows", "task", "agent-child.jsonl"), child)
			}
			result := readHomes(t, Reader{ProjectsDir: filepath.Join(primary, "projects"), ExtraHomes: []string{extra, extra}})
			report := analytics.Build([]model.ProviderResult{result}, analytics.Options{Location: time.UTC})
			wantPrompts, wantTools, wantOutput := 1, int64(1), int64(2)
			if partial {
				wantPrompts, wantTools, wantOutput = 2, 2, 8
			}
			if report.Totals.Sessions != 1 || report.Totals.ChildSessions != 1 || report.Totals.Prompts != wantPrompts || report.Totals.ToolCalls != wantTools {
				t.Fatalf("totals=%#v", report.Totals)
			}
			if report.Providers[0].TokenUsage.Output != wantOutput {
				t.Fatalf("owner output=%d, want %d", report.Providers[0].TokenUsage.Output, wantOutput)
			}
			if len(result.SourceFiles) != 4 || warningCount(result, "copied_session_files_coalesced") != 2 {
				t.Fatalf("sources=%d warnings=%#v", len(result.SourceFiles), result.Warnings)
			}
			if len(result.Sessions) != 2 {
				t.Fatalf("logical sessions=%d", len(result.Sessions))
			}
			var rhythmStarts int
			for _, day := range report.Rhythm.Activity {
				rhythmStarts += day.Count
			}
			if rhythmStarts != 1 {
				t.Fatalf("session-start rhythm=%d, want 1", rhythmStarts)
			}
		})
	}
}

func TestAdditionalHomesMergePartialPromptHistoriesWithoutLineNumberCollisions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	primary, extra := filepath.Join(root, "primary"), filepath.Join(root, "extra")
	a := `{"sessionId":"history-only","timestamp":1767225600000,"display":"Repeat synthetic prompt"}` + "\n"
	b := `{"sessionId":"history-only","timestamp":1767225600000,"display":"Different synthetic prompt"}` + "\n"
	writeHomeEvidence(t, filepath.Join(primary, "history.jsonl"), a+a)
	writeHomeEvidence(t, filepath.Join(extra, "history.jsonl"), b+a+a)
	result := readHomes(t, Reader{HistoryFile: filepath.Join(primary, "history.jsonl"), ExtraHomes: []string{extra}})
	if len(result.Sessions) != 1 || len(result.Sessions[0].Prompts) != 3 || result.Coverage.HistoryOnlySessions != 1 {
		t.Fatalf("sessions=%#v coverage=%#v", result.Sessions, result.Coverage)
	}
	if warningCount(result, "copied_session_prompt_records") != 2 {
		t.Fatalf("warnings=%#v", result.Warnings)
	}
}

func TestAdditionalHomeFailureRetainsAllSourceGuardDeclarations(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	link, other := filepath.Join(root, "linked-home"), filepath.Join(root, "other-home")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	reader := Reader{ProjectsDir: filepath.Join(root, "main-projects"), ExtraHomes: []string{link, other}}
	d, err := reader.Discover(context.Background())
	if err == nil {
		t.Fatal("symlink home accepted")
	}
	for _, path := range []string{reader.ProjectsDir, link, other, filepath.Join(other, "projects")} {
		if !containsPath(d.Roots, path) {
			t.Fatalf("guard root missing after earlier error: %s", path)
		}
	}
	if !containsPath(d.ConfiguredFiles, filepath.Join(other, "history.jsonl")) {
		t.Fatal("unvisited additional source not protected")
	}
	if len(d.Files) != 0 {
		t.Fatal("symlink target was inspected")
	}
}

func TestPhysicalCopiesWithConflictingModelsKeepConcreteUsageModelPair(t *testing.T) {
	t.Parallel()
	first := model.TokenUsage{Available: true, Exact: true, Input: 10, Output: 2, Source: "synthetic"}
	later := first
	later.Output = 5
	result := model.ProviderResult{}
	sessions := coalesceSessionCopies([]model.Session{
		{ID: "same", Calls: []model.CallMetric{{ID: "call", Model: "model-a", Usage: first, ToolIDs: []string{"one"}}}},
		{ID: "same", Calls: []model.CallMetric{{ID: "call", Model: "model-b", Usage: later, ToolIDs: []string{"two"}}}},
	}, &result)
	call := sessions[0].Calls[0]
	if call.Model != "model-a" || call.Usage != first || len(call.ToolIDs) != 2 {
		t.Fatalf("synthesized or lost copied evidence: %#v", call)
	}
	if warningCount(result, "copied_session_model_conflict") != 1 {
		t.Fatalf("warnings=%#v", result.Warnings)
	}
}
