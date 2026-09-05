package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0merUfuk/skuggsja/internal/analytics"
	"github.com/0merUfuk/skuggsja/internal/model"
)

func TestReaderIncludesSubagentsSeparatelyAndDerivesMetrics(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..", "testdata", "claude")
	reader := Reader{ProjectsDir: root}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if got, want := len(discovery.Files), 2; got != want {
		t.Fatalf("source file count = %d, want %d", got, want)
	}
	result := reader.Read(context.Background(), discovery)
	if got, want := len(result.Sessions), 2; got != want {
		t.Fatalf("session count = %d, want %d", got, want)
	}
	var session, child *model.Session
	for index := range result.Sessions {
		if result.Sessions[index].IsChild {
			child = &result.Sessions[index]
		} else {
			session = &result.Sessions[index]
		}
	}
	if session == nil || child == nil || child.ID != "a" || child.ParentID != "session-1" {
		t.Fatalf("root/child classification = root:%v child:%#v", session != nil, child)
	}
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

func TestDiscoverRejectsSymbolicLinkSession(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "synthetic-project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target.jsonl")
	if err := os.WriteFile(target, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(project, "session.jsonl")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := (Reader{ProjectsDir: root}).Discover(context.Background())
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Discover() error = %v, want symbolic-link rejection", err)
	}
}

func TestMissingUsageRemainsUnavailable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "synthetic-project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, "session-1.jsonl")
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

func TestRepeatedAssistantUpdatesKeepLatestUsageAndAllTools(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, "current.jsonl")
	data := "" +
		`{"type":"assistant","sessionId":"current","timestamp":"2026-01-02T21:00:00Z","message":{"id":"response","role":"assistant","model":"claude-test","content":[{"type":"tool_use","id":"tool-1"}],"usage":{"input_tokens":10,"output_tokens":2,"output_tokens_details":{"thinking_tokens":1}}}}` + "\n" +
		`{"type":"assistant","sessionId":"current","timestamp":"2026-01-02T21:00:01Z","message":{"id":"response","role":"assistant","model":"claude-test","content":[{"type":"tool_use","id":"tool-2"}],"usage":{"input_tokens":10,"output_tokens":5,"output_tokens_details":{"thinking_tokens":3}}}}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	result := readTestRoot(t, root)
	if got, want := len(result.Sessions[0].Calls), 1; got != want {
		t.Fatalf("calls = %d, want %d", got, want)
	}
	if got, want := result.Sessions[0].ToolCalls, int64(2); got != want {
		t.Errorf("tool calls = %d, want %d", got, want)
	}
	if got, want := result.Sessions[0].Usage.Output, int64(5); got != want {
		t.Errorf("output tokens = %d, want %d", got, want)
	}
	if got, want := result.Sessions[0].Usage.Reasoning, int64(3); got != want {
		t.Errorf("thinking tokens = %d, want %d", got, want)
	}
	if warningCount(result, "assistant_usage_advanced") != 1 {
		t.Errorf("warnings = %#v", result.Warnings)
	}
}

func TestRepeatedAssistantUpdateCompletesUsageAndReportsModelConflict(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"type":"assistant","sessionId":"current","timestamp":"2026-01-02T21:00:00Z","message":{"id":"response","role":"assistant","model":"claude-a","content":[]}}` + "\n" +
		`{"type":"assistant","sessionId":"current","timestamp":"2026-01-02T21:00:01Z","message":{"id":"response","role":"assistant","model":"claude-b","content":[],"usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"
	if err := os.WriteFile(filepath.Join(project, "current.jsonl"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	result := readTestRoot(t, root)
	if warningCount(result, "assistant_usage_completed") != 1 || warningCount(result, "assistant_update_model_conflict") != 1 {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	if result.Sessions[0].Usage.Input != 10 || result.Sessions[0].Models["claude-b"].Turns != 1 {
		t.Fatalf("session = %#v", result.Sessions[0])
	}
}

func TestPhysicalFileIdentityAnchorsCopiedHistory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, "current.jsonl")
	data := `{"type":"user","uuid":"copied","sessionId":"inherited","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"copied"}}` + "\n" +
		`{"type":"user","uuid":"current-event","sessionId":"current","timestamp":"2026-01-03T00:00:00Z","message":{"role":"user","content":"current"}}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	result := readTestRoot(t, root)
	session := result.Sessions[0]
	if session.ID != "current" {
		t.Fatalf("session ID = %q", session.ID)
	}
	want := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	if !session.StartedAt.Equal(want) {
		t.Fatalf("physical start = %s, want %s", session.StartedAt, want)
	}
}

func TestHistoryAndGlobalStateExposeTruncationWithoutDoubleCounting(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "projects", "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	transcript := `{"type":"user","uuid":"prompt","sessionId":"detailed","timestamp":"2026-07-01T00:00:00Z","message":{"role":"user","content":"detailed prompt"}}` + "\n"
	if err := os.WriteFile(filepath.Join(project, "detailed.jsonl"), []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	history := `{"display":"duplicate prompt","project":"/tmp/project","sessionId":"detailed","timestamp":1782864000000}` + "\n" +
		`{"display":"older prompt","project":"/tmp/older","sessionId":"history-only","timestamp":1777593600000}` + "\n"
	historyPath := filepath.Join(root, "history.jsonl")
	if err := os.WriteFile(historyPath, []byte(history), 0o600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, ".claude.json")
	state := `{"projects":{"a":{"lastSessionId":"detailed"},"b":{"lastSessionId":"history-only"},"c":{"lastSessionId":"index-only","lastStartTime":1775001600000}}}`
	if err := os.WriteFile(statePath, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := Reader{ProjectsDir: filepath.Join(root, "projects"), HistoryFile: historyPath, GlobalStateFile: statePath}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if got, want := len(result.Sessions), 2; got != want {
		t.Fatalf("sessions = %d, want %d", got, want)
	}
	if result.Coverage.HistoryOnlySessions != 1 || result.Coverage.UnmaterializedSessions != 1 || result.Coverage.Status != "known incomplete" {
		t.Fatalf("coverage = %#v", result.Coverage)
	}
	if warningCount(result, "history_records_reconciled") != 1 {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestCopiedDetailedPromptReconcilesHistoryWhenOriginalTranscriptIsGone(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "projects", "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	// This physical file has no event for its own ID. Its inherited event is
	// useful detailed evidence for the deleted "original" session, but its
	// filesystem mtime must not become an activity date.
	transcript := `{"type":"user","uuid":"copied-prompt","sessionId":"original","timestamp":"2026-06-01T00:00:00Z","message":{"role":"user","content":"surviving copied detail"}}` + "\n" +
		`{"type":"assistant","uuid":"copied-assistant","sessionId":"original","timestamp":"2026-06-01T00:00:01Z","message":{"id":"copied-response","role":"assistant","model":"claude-test","content":[{"type":"tool_use","id":"copied-tool"}],"usage":{"input_tokens":7,"output_tokens":3}}}` + "\n"
	if err := os.WriteFile(filepath.Join(project, "current.jsonl"), []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	history := `{"display":"same logical prompt","project":"/tmp/project","sessionId":"original","timestamp":1780272000000}` + "\n" +
		`{"display":"physical session prompt","project":"/tmp/project","sessionId":"current","timestamp":1780358400000}` + "\n"
	historyPath := filepath.Join(root, "history.jsonl")
	if err := os.WriteFile(historyPath, []byte(history), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := Reader{ProjectsDir: filepath.Join(root, "projects"), HistoryFile: historyPath}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if warningCount(result, "copied_history_prompt_records_reconciled") != 1 {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	if result.Coverage.HistoryOnlySessions != 2 || result.Coverage.UnmaterializedSessions != 0 {
		t.Fatalf("coverage = %#v", result.Coverage)
	}
	prompts, unanchored, historyOnly := 0, 0, 0
	for _, session := range result.Sessions {
		prompts += len(session.Prompts)
		if session.Unanchored {
			unanchored++
		}
		if session.HistoryOnly {
			historyOnly++
		}
	}
	if prompts != 2 || unanchored != 1 || historyOnly != 2 {
		t.Fatalf("sessions = %#v", result.Sessions)
	}
	report := analytics.Build([]model.ProviderResult{result}, analytics.Options{Now: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)})
	if got := report.Providers[0]; got.Sessions != 2 || got.Prompts != 2 || got.Projects != 1 ||
		got.ToolCalls != 1 || got.TokenUsage.Input != 7 {
		t.Fatalf("summary counted unanchored physical activity: %#v", got)
	}
}

func TestDesktopLocalAgentTranscriptIsAuditedButNotCountedAsClaudeCode(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	projects := filepath.Join(root, "account", "organization", "local-session", ".claude", "projects", "-synthetic")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projects, "desktop-session.jsonl")
	line := `{"type":"user","uuid":"desktop-prompt","sessionId":"desktop-session","timestamp":"2026-02-01T00:00:00Z","message":{"role":"user","content":"Cowork prompt"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := Reader{DesktopSessionsDir: root}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Files) != 0 || len(discovery.AuditFiles) != 1 {
		t.Fatalf("discovery = %#v", discovery)
	}
	result := reader.Read(context.Background(), discovery)
	if len(result.Sessions) != 0 || warningCount(result, "desktop_local_agent_sessions_excluded") != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestStatsOnlyEvidenceMeansKnownIncompleteCoverage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	statsPath := filepath.Join(root, "stats-cache.json")
	if err := os.WriteFile(statsPath, []byte(`{"firstSessionDate":"2025-12-31T18:14:05.160Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := Reader{StatsFile: statsPath}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if result.Coverage.Status != "known incomplete" || !result.Coverage.EarliestDetailedRecord.IsZero() {
		t.Fatalf("coverage = %#v", result.Coverage)
	}
}

func TestOversizeStatsCacheIsBoundedAndWarned(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "stats-cache.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxRecordBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	reader := Reader{StatsFile: path}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if warningCount(result, "oversize_stats_cache") != 1 || result.Coverage.Status != "coverage assessment incomplete" {
		t.Fatalf("result = %#v", result)
	}
}

func TestClaudeDesktopCodeIndexAddsCoverageEvidenceOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "projects", "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","uuid":"prompt","sessionId":"detailed","timestamp":"2026-07-01T00:00:00Z","message":{"role":"user","content":"prompt"}}` + "\n"
	if err := os.WriteFile(filepath.Join(project, "detailed.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	codeDir := filepath.Join(root, "code-sessions", "account", "organization")
	if err := os.MkdirAll(codeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	index := `{"cliSessionId":"detailed","priorCliSessionIds":["missing"],"bridgeSessionIds":["session_bridge"],"forkedFromSessionId":"desktop-wrapper","createdAt":1782864000000,"lastActivityAt":1782864001000}`
	if err := os.WriteFile(filepath.Join(codeDir, "local_wrapper.json"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := Reader{ProjectsDir: filepath.Join(root, "projects"), CodeSessionsDir: filepath.Join(root, "code-sessions")}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if len(result.Sessions) != 1 || result.Coverage.UnmaterializedSessions != 1 ||
		warningCount(result, "code_session_index_reconciled") != 1 || warningCount(result, "code_session_index_unmaterialized") != 1 ||
		warningCount(result, "desktop_session_references_unmapped") != 2 {
		t.Fatalf("result = %#v", result)
	}
	for _, warning := range result.Warnings {
		if warning.Message == "" {
			t.Fatalf("empty warning message: %#v", warning)
		}
	}
}

func TestDiscoverIncludesWorkflowNestedChild(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "project", "session", "subagents", "workflows", "workflow")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","agentId":"child","sessionId":"parent","timestamp":"2026-07-01T00:00:00Z","message":{"id":"response","role":"assistant","model":"claude-test","content":[]}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "agent-child.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "journal.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	discovery, err := (Reader{ProjectsDir: root}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Files) != 1 || filepath.Base(discovery.Files[0]) != "agent-child.jsonl" {
		t.Fatalf("files = %#v", discovery.Files)
	}
	result := (Reader{ProjectsDir: root}).Read(context.Background(), discovery)
	if len(result.Sessions) != 1 || !result.Sessions[0].IsChild || result.Sessions[0].ParentID != "session" {
		t.Fatalf("child = %#v", result.Sessions)
	}
}

func TestChildWithoutAgentIDUsesPathIdentityAndParent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "project", "parent", "subagents")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","sessionId":"parent","timestamp":"2026-07-01T00:00:00Z","message":{"id":"response","role":"assistant","model":"claude-test","content":[]}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "agent-child.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	result := readTestRoot(t, root)
	if len(result.Sessions) != 1 || !result.Sessions[0].IsChild || result.Sessions[0].ParentID != "parent" || result.Sessions[0].Unanchored {
		t.Fatalf("child = %#v", result.Sessions)
	}
}

func TestClaudePotentialDataLossAndDedupUncertaintyAreWarned(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"type":"user","sessionId":"current","timestamp":"not-a-time","message":{"role":"user","content":{"unexpected":true}}}` + "\n" +
		`{"type":"user","sessionId":"current","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"missing prompt id"}}` + "\n" +
		`{"type":"assistant","sessionId":"current","timestamp":"not-a-time","message":{"role":"assistant","model":"claude-test","content":"unsupported assistant content"}}` + "\n" +
		`{"type":"assistant","sessionId":"current","timestamp":"2026-01-01T00:00:01Z","message":{"id":"response-a","role":"assistant","model":"claude-test","content":[{"type":"tool_use"},{"type":"tool_use","id":"same-tool"}]}}` + "\n" +
		`{"type":"assistant","sessionId":"current","timestamp":"2026-01-01T00:00:02Z","message":{"id":"response-b","role":"assistant","model":"claude-test","content":[{"type":"tool_use","id":"same-tool"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(project, "current.jsonl"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	result := readTestRoot(t, root)
	for code, want := range map[string]int{
		"invalid_record_timestamp": 2, "unparseable_owner_prompt": 1, "unparseable_assistant_content": 1,
		"owner_prompt_missing_id": 1, "assistant_response_missing_id": 1, "tool_use_missing_id": 1, "duplicate_tool_use": 1,
	} {
		if got := warningCount(result, code); got != want {
			t.Errorf("warning %s = %d, want %d; all=%#v", code, got, want, result.Warnings)
		}
	}
}

func TestPhysicalSessionWithOnlyInvalidTimesIsCountedWithoutInventingActivity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","uuid":"prompt","sessionId":"current","timestamp":"not-a-time","cwd":"/synthetic/project","message":{"role":"user","content":"countable prompt"}}` + "\n"
	if err := os.WriteFile(filepath.Join(project, "current.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	result := readTestRoot(t, root)
	if len(result.Sessions) != 1 || !result.Sessions[0].TimeUnavailable || result.Sessions[0].Unanchored ||
		result.Coverage.UnmaterializedSessions != 0 || warningCount(result, "physical_session_time_unavailable") != 1 {
		t.Fatalf("result = %#v", result)
	}
	report := analytics.Build([]model.ProviderResult{result}, analytics.Options{Now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	if report.Providers[0].Sessions != 1 || report.Providers[0].Prompts != 1 || report.Totals.ActiveDays != 0 || !report.Coverage.Start.IsZero() {
		t.Fatalf("report = %#v", report)
	}
}

func TestCopiedModelConflictIsSeparateFromUsageConflict(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, modelName := range map[string]string{"one": "claude-a", "two": "claude-b"} {
		line := `{"type":"assistant","sessionId":"` + name + `","timestamp":"2026-01-01T00:00:00Z","message":{"id":"copied-response","role":"assistant","model":"` + modelName + `","content":[],"usage":{"input_tokens":7,"output_tokens":3}}}` + "\n"
		if err := os.WriteFile(filepath.Join(project, name+".jsonl"), []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result := readTestRoot(t, root)
	if warningCount(result, "copied_responses") != 1 || warningCount(result, "conflicting_copied_models") != 1 || warningCount(result, "conflicting_usage") != 0 {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func readTestRoot(t *testing.T, root string) model.ProviderResult {
	t.Helper()
	reader := Reader{ProjectsDir: root}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if len(result.Sessions) == 0 {
		t.Fatal("no sessions parsed")
	}
	return result
}

func warningCount(result model.ProviderResult, code string) int {
	for _, warning := range result.Warnings {
		if warning.Code == code {
			return warning.Count
		}
	}
	return 0
}

func TestPhysicalSidechainFlagExcludesDirectTranscriptFromOwnerUsage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "synthetic-project")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, "direct-child.jsonl")
	data := `{"type":"user","uuid":"synthetic-prompt","sessionId":"direct-child","isSidechain":true,"timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"child instruction"}}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	r := Reader{ProjectsDir: root}
	d, err := r.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	report := analytics.Build([]model.ProviderResult{r.Read(context.Background(), d)}, analytics.Options{Now: time.Now(), Location: time.UTC})
	if report.Totals.Sessions != 0 || report.Totals.Prompts != 0 || report.Totals.ChildSessions != 1 {
		t.Fatalf("physical sidechain counted as owner usage: %#v", report.Totals)
	}
}

func TestProjectIndexOnlyReferencesAffectCoverageWithoutInventingUsage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := filepath.Join(root, "synthetic-project")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, "sessions-index.json")
	data := `{"version":1,"entries":[{"sessionId":"missing-session","created":"2025-12-31T18:14:05Z","fullPath":"/not-a-source/never-open","firstPrompt":"must not count","messageCount":999}]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	r := Reader{ProjectsDir: root}
	d, err := r.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 0 || len(d.AuditFiles) != 1 || !containsPath(d.ConfiguredFiles, path) {
		t.Fatalf("index discovery = %#v", d)
	}
	result := r.Read(context.Background(), d)
	if len(result.Sessions) != 0 || result.Coverage.UnmaterializedSessions != 1 || result.Coverage.Status != "known incomplete" || warningCount(result, "project_index_only_sessions") != 1 {
		t.Fatalf("index invented usage or hid coverage: %#v", result)
	}
}
