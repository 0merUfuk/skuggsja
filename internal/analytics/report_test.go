package analytics

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/0merUfuk/skuggsja/internal/audit"
	"github.com/0merUfuk/skuggsja/internal/model"
)

func TestBuildKeepsProviderSemanticsAndDiscardsSensitiveContent(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("UTC+3", 3*60*60)
	start := time.Date(2025, 12, 31, 22, 30, 0, 0, time.UTC)
	result := model.ProviderResult{
		Harness: model.Claude, DisplayName: "Claude Code", Status: "supported",
		VerificationLevel: "fixture", ToolCallsAvailable: true,
		SourceFiles: []string{"/Users/private/.claude/projects/secret.jsonl"},
		Sessions: []model.Session{
			{
				Harness: model.Claude, ID: "root", StartedAt: start, EndedAt: start.Add(90 * time.Minute),
				ActivityAt: start, ActivityBasis: "session start", Project: "synthetic-project",
				Prompts: []model.PromptMetric{
					{EventID: "p1", Words: 2, Characters: 11, HasText: true},
					{EventID: "p1", Words: 2, Characters: 11, HasText: true},
				},
				Calls: []model.CallMetric{
					{ID: "m1", Model: "claude-test", Usage: exactUsage(10, 4), ToolIDs: []string{"tool-1"}},
					{ID: "m1", Model: "claude-test", Usage: exactUsage(10, 4), ToolIDs: []string{"tool-1"}},
				},
			},
			{Harness: model.Claude, ID: "sensitive-child-id-9274", IsChild: true, StartedAt: start},
		},
	}
	report := Build([]model.ProviderResult{result}, Options{
		Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Location: location,
		SourceAudit: audit.Comparison{Verified: true, Files: 1, ManifestBefore: "a", ManifestAfter: "a"},
	})

	if report.Totals.Sessions != 1 || report.Totals.ChildSessions != 1 {
		t.Fatalf("session totals = %d roots, %d children", report.Totals.Sessions, report.Totals.ChildSessions)
	}
	if report.Totals.Prompts != 1 || report.Totals.ToolCalls != 1 {
		t.Fatalf("deduplicated totals = %d prompts, %d tools", report.Totals.Prompts, report.Totals.ToolCalls)
	}
	if !report.Providers[0].ToolCallsAvailable {
		t.Fatal("mapped provider tool calls were reported as unavailable")
	}
	if report.Totals.ActiveDays != 1 || report.Rhythm.Activity[0].Date != "2026-01-01" {
		t.Fatalf("local activity = %#v", report.Rhythm.Activity)
	}
	if report.Coverage.Timezone != "UTC+3" {
		t.Fatalf("timezone label = %q", report.Coverage.Timezone)
	}
	if got := report.Providers[0].TokenUsage; got.Input != 10 || got.Output != 4 || !got.Exact {
		t.Fatalf("provider token usage = %#v", got)
	}
	if !report.PromptStyle.Available || report.PromptStyle.MedianWords != 2 {
		t.Fatalf("prompt style = %#v", report.PromptStyle)
	}
	if report.Coverage.CalendarFraming {
		t.Fatal("a short surviving-history span must not use calendar-year framing")
	}

	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"/Users/private", "secret.jsonl", "raw prompt", "root", "sensitive-child-id-9274", "m1", "p1", "tool-1"} {
		if strings.Contains(string(payload), forbidden) {
			t.Errorf("persisted report contains forbidden source value %q", forbidden)
		}
	}
}

func TestCoverageUsesCalendarYearOnlyForHonestSpan(t *testing.T) {
	t.Parallel()
	location := time.UTC
	if got := coverageLabel(
		time.Date(2025, 1, 7, 0, 0, 0, 0, location),
		time.Date(2025, 12, 25, 0, 0, 0, 0, location), location,
	); got != "2025" {
		t.Fatalf("year coverage label = %q", got)
	}
	if got := coverageLabel(
		time.Date(2025, 2, 1, 0, 0, 0, 0, location),
		time.Date(2025, 12, 24, 0, 0, 0, 0, location), location,
	); got == "2025" {
		t.Fatalf("partial coverage mislabeled as calendar year: %q", got)
	}
}

func TestPromptMedianPreservesHalfWord(t *testing.T) {
	t.Parallel()
	style := promptStyle([]model.PromptMetric{
		{Words: 2, Characters: 10, HasText: true},
		{Words: 3, Characters: 15, HasText: true},
	})
	if style.MedianWords != 2.5 {
		t.Fatalf("median words = %v, want 2.5", style.MedianWords)
	}
}

func TestUnavailableProviderDoesNotClaimNoLocalEvidence(t *testing.T) {
	t.Parallel()
	report := Build([]model.ProviderResult{{
		Harness: model.Codex, DisplayName: "Codex", Status: "unavailable",
		Warnings: []model.Warning{{Code: "discovery_failed", Count: 1, Message: "Source could not be inspected."}},
	}}, Options{Now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Location: time.UTC})
	coverage := report.Providers[0].Coverage
	if coverage.Status != "assessment unavailable" || coverage.Confidence != "low" {
		t.Fatalf("coverage = %#v", coverage)
	}
}

func TestFilesystemMtimeIsNotPromotedToDetailedActivity(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	report := Build([]model.ProviderResult{{
		Harness: model.Claude, DisplayName: "Claude Code", Status: "supported with warnings",
		ToolCallsAvailable: true,
		Sessions: []model.Session{{
			Harness: model.Claude, ID: "unanchored", StartedAt: at, EndedAt: at, ActivityAt: at,
			ActivityBasis: "file modification time (no physical-session record)", Unanchored: true, Project: "must-not-count",
			Prompts: []model.PromptMetric{{EventID: "copied-prompt", Words: 2, Characters: 12, HasText: true}},
			Calls:   []model.CallMetric{{ID: "copied-call", Model: "claude-test", Usage: exactUsage(7, 3), ToolIDs: []string{"copied-tool"}}},
		}},
	}}, Options{Now: at, Location: time.UTC})
	provider := report.Providers[0]
	if !provider.Coverage.EarliestDetailedRecord.IsZero() || provider.Sessions != 0 || provider.Projects != 0 ||
		provider.Prompts != 1 || provider.ToolCalls != 1 || provider.TokenUsage.Input != 7 ||
		len(report.Models) != 1 || report.Totals.ActiveDays != 0 || !report.Coverage.Start.IsZero() {
		t.Fatalf("provider/global coverage = %#v / %#v", report.Providers[0].Coverage, report.Coverage)
	}
}

func exactUsage(input, output int64) model.TokenUsage {
	return model.TokenUsage{
		Available: true, Exact: true, Input: input, Output: output, Source: "fixture exact fields",
	}
}

func TestUntimedPhysicalSessionRetainsProjectWithoutRhythm(t *testing.T) {
	t.Parallel()
	report := Build([]model.ProviderResult{{Harness: model.Claude, Status: "supported", Sessions: []model.Session{{Harness: model.Claude, ID: "untimed", Project: "synthetic-project", TimeUnavailable: true}}}}, Options{Now: time.Now(), Location: time.UTC})
	if report.Totals.Sessions != 1 || report.Totals.Projects != 1 || report.Providers[0].Projects != 1 || len(report.Projects) != 1 || report.Totals.ActiveDays != 0 || !report.Coverage.Start.IsZero() || report.Longest.Available {
		t.Fatalf("untimed session incorrectly counted: %#v", report)
	}
}
