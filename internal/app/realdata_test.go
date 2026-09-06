package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0merUfuk/skuggsja/internal/app"
	"github.com/0merUfuk/skuggsja/internal/audit"
	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/platform"
	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/0merUfuk/skuggsja/internal/provider/claude"
	"github.com/0merUfuk/skuggsja/internal/provider/codex"
	"github.com/0merUfuk/skuggsja/internal/provider/cursor"
	"github.com/0merUfuk/skuggsja/internal/provider/hermes"
)

// TestRealDataFullRunLeavesSourcesUnchanged is a release-only equality check.
// Runtime source-write protection is independent of this check: owning harnesses
// normally continue writing while Skuggsja reads. This test directly attempts
// bounded generation windows and stops at the first equal before/after manifest
// pair for its explicitly declared live scope. A self-hosted Codex store can be
// snapshotted and excluded from equality; ingestion uses every original source.
func TestRealDataFullRunLeavesSourcesUnchanged(t *testing.T) {
	if os.Getenv("SKUGGSJA_VERIFY_REAL_DATA") != "1" {
		t.Skip("set SKUGGSJA_VERIFY_REAL_DATA=1 to run against local histories")
	}
	paths, err := platform.DefaultPaths()
	if err != nil {
		t.Fatal("resolve default source locations")
	}
	readers := realDataReaders(paths)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	all, err := discoverReleaseInputs(ctx, readers)
	if err != nil {
		t.Fatal(err)
	}
	excludeCodex := os.Getenv("SKUGGSJA_RELEASE_SNAPSHOT_CODEX") == "1"
	fallback := os.Getenv("SKUGGSJA_RELEASE_CLAUDE_CURSOR_FALLBACK") == "1"
	scope, err := selectReleaseScope(readers, excludeCodex, fallback)
	if err != nil {
		t.Fatal(err)
	}
	liveReaders := scope.equality
	excluded, err := discoverReleaseInputs(ctx, scope.snapshot)
	if err != nil {
		t.Fatal(err)
	}
	const maxAttempts = 8
	t.Logf("release_scope name=%s excluded_codex_store=%t ingestion=all_original_sources generation_limit=%d preflight=none hermes_live_equality=%s", scope.name, excludeCodex, maxAttempts, scope.hermesEquality())
	if fallback {
		t.Log("release_scope_limitation equality_harnesses=claude,cursor hermes_live_equality=unmeasured complete_scope_result=not_measured_in_this_run; previous_complete_scope_evidence_is_not_replaced")
	}
	writeReleaseScope(t, scope, excludeCodex, all)
	if excludeCodex {
		t.Log("release_exclusion_reason=verification_is_self_hosted_in_Codex; shared_rollouts_history_indexes_and_SQLite_are_snapshotted_separately; original_Codex_store_immutability_is_not_claimed")
		capturedAt := time.Now().UTC()
		snapshot, captureErr := excluded.capture(ctx)
		if captureErr != nil {
			t.Fatalf("snapshot explicitly excluded Codex store: %v", captureErr)
		}
		if !snapshot.Complete {
			t.Fatal("explicitly excluded Codex store snapshot is incomplete; no source may be silently omitted")
		}
		writeReleaseSnapshot(t, "excluded-codex-before.json", excluded, snapshot, capturedAt, all)
		t.Logf("excluded_codex_snapshot files=%d directories=%d complete=%t manifest=%s captured_at=%s", len(snapshot.Files), len(snapshot.Directories), snapshot.Complete, snapshot.Manifest, capturedAt.Format(time.RFC3339Nano))
	}

	var generation app.Generation
	attempts, comparison, runErr := runReleaseEqualityAttempts(ctx, maxAttempts, func(ctx context.Context, attempt int) (audit.Comparison, error) {
		beforeInputs, err := discoverReleaseInputs(ctx, liveReaders)
		if err != nil {
			return audit.Comparison{}, err
		}
		beforeAt := time.Now().UTC()
		before, err := beforeInputs.capture(ctx)
		if err != nil {
			return audit.Comparison{}, fmt.Errorf("capture release before snapshot: %w", err)
		}
		writeReleaseSnapshot(t, fmt.Sprintf("%s-attempt-%02d-before.json", scope.manifestPrefix(), attempt), beforeInputs, before, beforeAt, all)

		outputPath := releaseEvidencePath(t, scope.attemptReportName(attempt), all)
		if outputPath == "" {
			outputPath = filepath.Join(t.TempDir(), "rewind.json")
		}
		if _, err := os.Lstat(outputPath); !errors.Is(err, os.ErrNotExist) {
			return audit.Comparison{}, errors.New("release attempt report destination must be new")
		}
		started := time.Now()
		t.Logf("release_generation attempt=%d started_at=%s", attempt, started.UTC().Format(time.RFC3339Nano))
		var generationErr error
		generation, generationErr = app.Generate(ctx, app.GenerateOptions{
			Readers: scope.ingestion, Location: time.Local,
			OutputPath: outputPath, AuditSources: true,
		})
		// Capture the after state even if generation failed. Every completed
		// comparison is retained; retries never overwrite unsuccessful evidence.
		afterInputs, err := discoverReleaseInputs(ctx, liveReaders)
		if err != nil {
			return audit.Comparison{}, errors.Join(generationErr, err)
		}
		afterAt := time.Now().UTC()
		after, err := afterInputs.capture(ctx)
		if err != nil {
			return audit.Comparison{}, errors.Join(generationErr, fmt.Errorf("capture release after snapshot: %w", err))
		}
		writeReleaseSnapshot(t, fmt.Sprintf("%s-attempt-%02d-after.json", scope.manifestPrefix(), attempt), afterInputs, after, afterAt, all)
		comparison := audit.Compare(before, after)
		internal := generation.Report.Privacy.SourceAudit
		t.Logf(
			"outer_source_audit scope=%s attempt=%d files_before=%d files_after=%d directories_before=%d directories_after=%d complete_before=%t complete_after=%t before=%s after=%s changed_files=%d directory_changes=%d verified=%t generation_elapsed=%s outer_window_elapsed=%s excluded_codex_store=%t",
			scope.name, attempt, len(before.Files), len(after.Files), len(before.Directories), len(after.Directories), before.Complete, after.Complete,
			comparison.ManifestBefore, comparison.ManifestAfter,
			comparison.ChangedFiles, comparison.DirectoryChanges, comparison.Verified,
			generation.Duration.Round(time.Millisecond), time.Since(started).Round(time.Millisecond), excludeCodex,
		)
		t.Logf(
			"internal_source_observation attempt=%d files=%d before=%s after=%s changed_files=%d directory_changes=%d unchanged=%t release_gate=false includes_original_codex=true",
			attempt, internal.Files, internal.ManifestBefore, internal.ManifestAfter,
			internal.ChangedFiles, internal.DirectoryChanges, internal.Verified,
		)
		logChangedProviders(t, comparison, before, after, paths)
		if generationErr == nil {
			digest, _, err := audit.DigestFile(ctx, generation.OutputPath)
			if err != nil {
				return comparison, fmt.Errorf("hash retained release report: %w", err)
			}
			t.Logf("release_report attempt=%d filename=%s sha256=%s release_equality=%t", attempt, filepath.Base(generation.OutputPath), digest, comparison.Verified)
		}
		return comparison, generationErr
	})
	t.Logf("release_generation_result scope=%s attempts=%d limit=%d verified=%t", scope.name, attempts, maxAttempts, runErr == nil && comparison.Verified)
	if runErr != nil {
		t.Fatalf("release generation window could not be completed: %v", runErr)
	}
	retainSelectedReleaseReport(t, generation.OutputPath, comparison.Verified, attempts, all, scope.name)
	for _, summary := range generation.Report.Providers {
		t.Logf(
			"provider=%s status=%q verification=%q source_files=%d sessions=%d child_sessions=%d prompts=%d warnings=%d span_start=%s span_end=%s coverage_status=%q confidence=%q earliest_local=%s earliest_detail=%s history_only=%d unmaterialized=%d",
			summary.ID, summary.Status, summary.Verification, summary.SourceFileCount,
			summary.Sessions, summary.ChildSessions, summary.Prompts, len(summary.Warnings),
			summary.SpanStart.Format(time.RFC3339), summary.SpanEnd.Format(time.RFC3339),
			summary.Coverage.Status, summary.Coverage.Confidence,
			summary.Coverage.EarliestLocalEvidence.Format(time.RFC3339), summary.Coverage.EarliestDetailedRecord.Format(time.RFC3339),
			summary.Coverage.HistoryOnlySessions, summary.Coverage.UnmaterializedSessions,
		)
		for _, warning := range summary.Warnings {
			t.Logf("provider_warning provider=%s code=%s count=%d", summary.ID, warning.Code, warning.Count)
		}
	}
	if !comparison.Verified {
		t.Fatalf("release equality check did not find an unchanged complete generation window after %d attempts; all comparisons are retained, and this is independent of Skuggsja's source-write protection", attempts)
	}
}

// releaseSourceScope separates equality observation from ingestion. The only
// fallback is the explicitly requested Claude+Cursor pair; there is no generic
// provider filter and all original reader objects remain in the ingestion set.
type releaseSourceScope struct {
	name                          string
	ingestion, equality, snapshot []provider.Reader
}

func selectReleaseScope(readers []provider.Reader, excludeCodex, fallback bool) (releaseSourceScope, error) {
	scope := releaseSourceScope{name: "complete", ingestion: append([]provider.Reader(nil), readers...)}
	if fallback && !excludeCodex {
		return scope, errors.New("Claude+Cursor fallback requires the explicit own-Codex snapshot exclusion")
	}
	if fallback {
		scope.name = "claude_cursor_fallback"
		counts := make(map[model.Harness]int)
		for _, reader := range readers {
			counts[reader.Harness()]++
		}
		if len(counts) != 4 || counts[model.Claude] != 1 || counts[model.Codex] != 1 || counts[model.Cursor] != 1 || counts[model.Hermes] != 1 {
			return scope, errors.New("Claude+Cursor fallback requires exactly one original reader for each of Claude, Codex, Cursor and Hermes")
		}
	}
	for _, reader := range readers {
		if excludeCodex && reader.Harness() == model.Codex {
			scope.snapshot = append(scope.snapshot, reader)
			continue
		}
		if fallback && reader.Harness() == model.Hermes {
			continue
		}
		scope.equality = append(scope.equality, reader)
	}
	return scope, nil
}

func (scope releaseSourceScope) hermesEquality() string {
	if scope.name == "claude_cursor_fallback" {
		return "unmeasured"
	}
	return "in_scope"
}

func (scope releaseSourceScope) manifestPrefix() string {
	if scope.name == "claude_cursor_fallback" {
		return "claude-cursor"
	}
	return "live"
}

func (scope releaseSourceScope) attemptReportName(attempt int) string {
	if scope.name == "claude_cursor_fallback" {
		return fmt.Sprintf("claude-cursor-attempt-%02d-rewind.json", attempt)
	}
	return fmt.Sprintf("attempt-%02d-rewind.json", attempt)
}

func writeReleaseScope(t *testing.T, scope releaseSourceScope, excludedCodex bool, all releaseInputs) {
	t.Helper()
	ids := func(readers []provider.Reader) []model.Harness {
		values := make([]model.Harness, 0, len(readers))
		for _, reader := range readers {
			values = append(values, reader.Harness())
		}
		return values
	}
	payload := map[string]any{
		"name": scope.name, "ingested_harnesses": ids(scope.ingestion),
		"live_equality_harnesses": ids(scope.equality), "excluded_codex_store": excludedCodex,
		"hermes_live_equality": scope.hermesEquality(), "source_write_protection_scope": "all_original_sources",
	}
	if scope.name == "claude_cursor_fallback" {
		payload["complete_scope_result"] = "not_measured_in_this_run"
		payload["limitation"] = "Only Claude and Cursor participate in this live equality comparison. Hermes is ingested but its live equality is unmeasured. Previous complete-scope evidence is not superseded."
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal("serialize release scope")
	}
	writeReleaseEvidence(t, "release-scope.json", append(data, '\n'), all)
}

func realDataReaders(paths platform.Paths) []provider.Reader {
	return []provider.Reader{
		claude.Reader{
			ProjectsDir: paths.ClaudeProjects, HistoryFile: paths.ClaudeHistory, ExtraHomes: paths.ClaudeExtraHomes,
			StatsFile: paths.ClaudeStats, GlobalStateFile: paths.ClaudeGlobalState,
			DesktopSessionsDir: paths.ClaudeDesktopSessions, CodeSessionsDir: paths.ClaudeCodeSessions,
		},
		codex.Reader{
			SessionsDir: paths.CodexSessions, ArchivedDir: paths.CodexArchived, RecoveryDir: paths.CodexRecovery,
			HistoryFile: paths.CodexHistory, SessionIndexFile: paths.CodexSessionIndex,
			ExternalImportsFile: paths.CodexExternalImports, StateDatabase: paths.CodexStateDatabase,
			CatalogDatabase: paths.CodexCatalogDatabase, ThreadHistoryDatabase: paths.CodexThreadHistoryDatabase,
		},
		hermes.Reader{DatabasePath: paths.HermesDatabase},
		cursor.Reader{DatabasePath: paths.CursorStateDB},
	}
}

type releaseInputs struct {
	roots, files, configured, protected []string
}

func discoverReleaseInputs(ctx context.Context, readers []provider.Reader) (releaseInputs, error) {
	var inputs releaseInputs
	for _, reader := range readers {
		discovery, err := reader.Discover(ctx)
		if err != nil {
			return inputs, fmt.Errorf("discover %s: source location unavailable", reader.Harness())
		}
		inputs.roots = append(inputs.roots, discovery.Roots...)
		inputs.protected = append(inputs.protected, discovery.ProtectedDirectories...)
		inputs.files = append(inputs.files, discovery.Files...)
		inputs.files = append(inputs.files, discovery.AuditFiles...)
		inputs.configured = append(inputs.configured, discovery.ConfiguredFiles...)
	}
	return inputs, nil
}

func (inputs releaseInputs) capture(ctx context.Context) (audit.Snapshot, error) {
	return audit.CaptureConfigured(ctx, inputs.roots, inputs.files, inputs.configured)
}

// Absolute paths are confined to opt-in private release evidence. They never
// enter the Rewind artifact or normal terminal/UI output.
func writeReleaseSnapshot(t *testing.T, name string, inputs releaseInputs, snapshot audit.Snapshot, started time.Time, all releaseInputs) {
	t.Helper()
	payload := struct {
		StartedAt       time.Time                  `json:"started_at"`
		FinishedAt      time.Time                  `json:"finished_at"`
		ConfiguredRoots []string                   `json:"configured_roots"`
		DiscoveredFiles []string                   `json:"discovered_files"`
		ConfiguredFiles []string                   `json:"configured_files"`
		Files           map[string]audit.FileState `json:"files"`
		Directories     map[string][]string        `json:"directories"`
		Roots           map[string]bool            `json:"roots"`
		Manifest        string                     `json:"manifest"`
		TotalBytes      int64                      `json:"total_bytes"`
		Complete        bool                       `json:"complete"`
	}{started, time.Now().UTC(), inputs.roots, inputs.files, inputs.configured, snapshot.Files, snapshot.Directories, snapshot.Roots, snapshot.Manifest, snapshot.TotalBytes, snapshot.Complete}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal("serialize private release snapshot")
	}
	writeReleaseEvidence(t, name, append(data, '\n'), all)
}

func releaseEvidencePath(t *testing.T, name string, all releaseInputs) string {
	t.Helper()
	dir := os.Getenv("SKUGGSJA_RELEASE_EVIDENCE_DIR")
	if dir == "" {
		if os.Getenv("SKUGGSJA_RELEASE_SNAPSHOT_CODEX") == "1" {
			t.Fatal("explicit Codex exclusion requires SKUGGSJA_RELEASE_EVIDENCE_DIR for its exact private inventory")
		}
		return ""
	}
	path := filepath.Join(dir, name)
	if err := app.EnsureOutputSeparate(path, append(append([]string(nil), all.roots...), all.protected...), append(append([]string(nil), all.files...), all.configured...)); err != nil {
		t.Fatal("release evidence must be separate from every source")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		t.Fatal("release evidence requires an existing private directory with no group/other permissions")
	}
	return path
}

func writeReleaseEvidence(t *testing.T, name string, data []byte, all releaseInputs) {
	t.Helper()
	path := releaseEvidencePath(t, name, all)
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal("create private release evidence")
	}
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal("persist private release evidence")
	}
}

func retainSelectedReleaseReport(t *testing.T, generatedPath string, verified bool, attempt int, all releaseInputs, scopeName string) {
	t.Helper()
	name := "rewind-observed-changing.json"
	if verified {
		name = "rewind.json"
	}
	if scopeName == "claude_cursor_fallback" {
		name = "rewind-claude-cursor-observed-changing.json"
		if verified {
			name = "rewind-claude-cursor-verified.json"
		}
	}
	if releaseEvidencePath(t, name, all) == "" {
		return
	}
	// Copy only the content-free report already emitted by Generate. Exact bytes
	// preserve provenance between the release window and a later browser run.
	data, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatal("read generated release report")
	}
	writeReleaseEvidence(t, name, data, all)
	t.Logf("release_selected_report scope=%s attempt=%d filename=%s sha256=%x release_equality=%t", scopeName, attempt, name, sha256.Sum256(data), verified)
}

func logChangedProviders(t *testing.T, comparison audit.Comparison, before, after audit.Snapshot, paths platform.Paths) {
	t.Helper()
	changedByProvider := map[string]int{"claude": 0, "codex": 0, "cursor": 0, "hermes": 0, "other": 0}
	seenChanged := make(map[string]struct{}, len(before.Files)+len(after.Files))
	for path := range before.Files {
		seenChanged[path] = struct{}{}
	}
	for path := range after.Files {
		seenChanged[path] = struct{}{}
	}
	for path := range seenChanged {
		if before.Files[path] == after.Files[path] {
			continue
		}
		switch {
		case path == paths.HermesDatabase || path == paths.HermesDatabase+"-wal" || path == paths.HermesDatabase+"-shm" || path == paths.HermesDatabase+"-journal":
			changedByProvider["hermes"]++
		case path == paths.CursorStateDB || path == paths.CursorStateDB+"-wal" || path == paths.CursorStateDB+"-shm" || path == paths.CursorStateDB+"-journal":
			changedByProvider["cursor"]++
		case withinRoot(paths.ClaudeProjects, path) || withinAnyRoot(paths.ClaudeExtraHomes, path):
			changedByProvider["claude"]++
		case withinRoot(paths.ClaudeDesktopSessions, path) || withinRoot(paths.ClaudeCodeSessions, path) || path == paths.ClaudeHistory || path == paths.ClaudeStats || path == paths.ClaudeGlobalState:
			changedByProvider["claude"]++
		case withinRoot(paths.CodexSessions, path) || withinRoot(paths.CodexArchived, path) || withinRoot(paths.CodexRecovery, path):
			changedByProvider["codex"]++
		case path == paths.CodexHistory || path == paths.CodexSessionIndex || path == paths.CodexExternalImports:
			changedByProvider["codex"]++
		case path == paths.CodexStateDatabase || path == paths.CodexStateDatabase+"-wal" || path == paths.CodexStateDatabase+"-shm" || path == paths.CodexStateDatabase+"-journal":
			changedByProvider["codex"]++
		case path == paths.CodexThreadHistoryDatabase || path == paths.CodexThreadHistoryDatabase+"-wal" || path == paths.CodexThreadHistoryDatabase+"-shm" || path == paths.CodexThreadHistoryDatabase+"-journal":
			changedByProvider["codex"]++
		case path == paths.CodexCatalogDatabase || path == paths.CodexCatalogDatabase+"-wal" || path == paths.CodexCatalogDatabase+"-shm" || path == paths.CodexCatalogDatabase+"-journal":
			changedByProvider["codex"]++
		default:
			changedByProvider["other"]++
		}
	}
	t.Logf(
		"changed_by_provider claude=%d codex=%d cursor=%d hermes=%d other=%d",
		changedByProvider["claude"], changedByProvider["codex"], changedByProvider["cursor"],
		changedByProvider["hermes"], changedByProvider["other"],
	)
}

// runReleaseEqualityAttempts applies the release gate to actual generation
// windows. Activity before a window is irrelevant. A changed comparison permits
// another bounded attempt; operational errors stop instead of hiding missing
// evidence. The callback owns fresh captures and retains each attempt's evidence.
func runReleaseEqualityAttempts(ctx context.Context, limit int, run func(context.Context, int) (audit.Comparison, error)) (int, audit.Comparison, error) {
	var comparison audit.Comparison
	for attempt := 1; attempt <= limit; attempt++ {
		if err := ctx.Err(); err != nil {
			return attempt - 1, comparison, err
		}
		var err error
		comparison, err = run(ctx, attempt)
		if err != nil || comparison.Verified {
			return attempt, comparison, err
		}
	}
	return limit, comparison, nil
}

func withinRoot(root, path string) bool {
	if root == "" || path == root {
		return path == root
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && relative != "." && !filepath.IsAbs(relative) &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func withinAnyRoot(roots []string, path string) bool {
	for _, root := range roots {
		if withinRoot(root, path) {
			return true
		}
	}
	return false
}
