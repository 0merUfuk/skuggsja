package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/0merUfuk/skuggsja/internal/app"
	"github.com/0merUfuk/skuggsja/internal/audit"
	"github.com/0merUfuk/skuggsja/internal/platform"
	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/0merUfuk/skuggsja/internal/provider/claude"
	"github.com/0merUfuk/skuggsja/internal/provider/codex"
	"github.com/0merUfuk/skuggsja/internal/provider/cursor"
	"github.com/0merUfuk/skuggsja/internal/provider/hermes"
)

// TestRealDataFullRunLeavesSourcesUnchanged is a release-only equality check.
// Runtime source-write protection is independent of this check: owning harnesses
// normally continue writing while Skuggsja reads. This test performs one Generate
// after a continuous quiet preflight and requires equality only for its explicitly
// declared live scope. A self-hosted Codex store can be snapshotted and excluded
// from that equality scope; ingestion still uses every original source.
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
	live := all
	liveReaders := readers
	var excluded releaseInputs
	excludeCodex := os.Getenv("SKUGGSJA_RELEASE_SNAPSHOT_CODEX") == "1"
	if excludeCodex {
		// Codex's shared history, index and SQLite files belong to the same store
		// as this verification agent's rollout. Excluding one rollout would leave
		// self-generated writes in the release equality window.
		liveReaders = nil
		var excludedReaders []provider.Reader
		for _, reader := range readers {
			if reader.Harness() == "codex" {
				excludedReaders = append(excludedReaders, reader)
			} else {
				liveReaders = append(liveReaders, reader)
			}
		}
		live, err = discoverReleaseInputs(ctx, liveReaders)
		if err != nil {
			t.Fatal(err)
		}
		excluded, err = discoverReleaseInputs(ctx, excludedReaders)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("release_scope excluded_codex_store=%t ingestion=all_original_sources other_exclusions=0 generation_limit=1", excludeCodex)
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

	quietCtx, quietCancel := context.WithTimeout(ctx, 10*time.Minute)
	err = waitForContinuousQuiet(quietCtx, live.roots, append(append([]string(nil), live.files...), live.configured...), 60*time.Second, 2*time.Second, func(elapsed, quietFor time.Duration) {
		t.Logf("release_quiet_preflight elapsed=%s continuous_quiet=%s required=1m0s generation_started=false", elapsed.Round(time.Second), quietFor.Round(time.Second))
	})
	quietCancel()
	if err != nil {
		t.Fatalf("release equality check not started: no genuine quiet window: %v", err)
	}
	live, err = discoverReleaseInputs(ctx, liveReaders)
	if err != nil {
		t.Fatal(err)
	}
	beforeAt := time.Now().UTC()
	before, err := live.capture(ctx)
	if err != nil {
		t.Fatal("capture release before snapshot")
	}
	writeReleaseSnapshot(t, "live-before.json", live, before, beforeAt, all)

	started := time.Now()
	t.Logf("release_generation attempt=1 started_at=%s", started.UTC().Format(time.RFC3339Nano))
	generation, err := app.Generate(ctx, app.GenerateOptions{
		Readers: readers, Location: time.Local,
		OutputPath: filepath.Join(t.TempDir(), "rewind.json"), AuditSources: true,
	})
	if err != nil {
		t.Fatalf("generate Rewind: %v", err)
	}
	afterInputs, err := discoverReleaseInputs(ctx, liveReaders)
	if err != nil {
		t.Fatal(err)
	}
	afterAt := time.Now().UTC()
	after, err := afterInputs.capture(ctx)
	if err != nil {
		t.Fatal("capture release after snapshot")
	}
	writeReleaseSnapshot(t, "live-after.json", afterInputs, after, afterAt, all)
	comparison := audit.Compare(before, after)
	internal := generation.Report.Privacy.SourceAudit
	t.Logf(
		"outer_source_audit attempt=1 files_before=%d files_after=%d directories_before=%d directories_after=%d complete_before=%t complete_after=%t before=%s after=%s changed_files=%d directory_changes=%d verified=%t generation_elapsed=%s outer_window_elapsed=%s excluded_codex_store=%t",
		len(before.Files), len(after.Files), len(before.Directories), len(after.Directories), before.Complete, after.Complete,
		comparison.ManifestBefore, comparison.ManifestAfter,
		comparison.ChangedFiles, comparison.DirectoryChanges, comparison.Verified,
		generation.Duration.Round(time.Millisecond), time.Since(started).Round(time.Millisecond), excludeCodex,
	)
	t.Logf(
		"internal_source_observation files=%d before=%s after=%s changed_files=%d directory_changes=%d unchanged=%t release_gate=false includes_original_codex=true",
		internal.Files, internal.ManifestBefore, internal.ManifestAfter,
		internal.ChangedFiles, internal.DirectoryChanges, internal.Verified,
	)
	logChangedProviders(t, comparison, before, after, paths)
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
		t.Fatal("release equality check failed: a source in the declared live scope changed or could not be completely observed; this is independent of Skuggsja's source-write protection")
	}
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
	dir := os.Getenv("SKUGGSJA_RELEASE_EVIDENCE_DIR")
	if dir == "" {
		if os.Getenv("SKUGGSJA_RELEASE_SNAPSHOT_CODEX") == "1" {
			t.Fatal("explicit Codex exclusion requires SKUGGSJA_RELEASE_EVIDENCE_DIR for its exact private inventory")
		}
		return
	}
	path := filepath.Join(dir, name)
	if err := app.EnsureOutputSeparate(path, append(append([]string(nil), all.roots...), all.protected...), append(append([]string(nil), all.files...), all.configured...)); err != nil {
		t.Fatal("release evidence must be separate from every source")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		t.Fatal("release evidence requires an existing private directory with no group/other permissions")
	}
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
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal("create private release snapshot")
	}
	_, writeErr := file.Write(append(data, '\n'))
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal("persist private release snapshot")
	}
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

// waitForContinuousQuiet requires the entire interval, rather than one equal
// pair of samples. A deadline leaves Generate unstarted; it never waives scope.
func waitForContinuousQuiet(ctx context.Context, roots, files []string, required, interval time.Duration, progress func(time.Duration, time.Duration)) error {
	previous, err := sourceMetadataSignature(roots, files)
	if err != nil {
		return err
	}
	started := time.Now()
	quietSince := started
	lastProgress := started
	if progress != nil {
		progress(0, 0)
	}
	for {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("continuous quiet preflight: %w", ctx.Err())
		case <-timer.C:
		}
		current, err := sourceMetadataSignature(roots, files)
		if err != nil {
			return err
		}
		now := time.Now()
		if current != previous {
			quietSince = now
		}
		previous = current
		quietFor := now.Sub(quietSince)
		if progress != nil && (now.Sub(lastProgress) >= 15*time.Second || quietFor >= required) {
			progress(now.Sub(started), quietFor)
			lastProgress = now
		}
		if quietFor >= required {
			return nil
		}
	}
}

func sourceMetadataSignature(roots, files []string) ([sha256.Size]byte, error) {
	records := make(map[string]string)
	record := func(path string) error {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			records[path] = "missing"
			return nil
		}
		if err != nil {
			return err
		}
		records[path] = info.Mode().String() + "\x00" + strconv.FormatInt(info.Size(), 10) + "\x00" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
		return nil
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		if err := record(root); err != nil {
			return [sha256.Size]byte{}, err
		}
		if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path == root {
				return nil
			}
			return record(path)
		}); err != nil {
			return [sha256.Size]byte{}, err
		}
	}
	for _, path := range files {
		if path == "" {
			continue
		}
		for _, candidate := range []string{path, path + "-wal", path + "-shm", path + "-journal"} {
			if err := record(candidate); err != nil {
				return [sha256.Size]byte{}, err
			}
		}
	}
	keys := make([]string, 0, len(records))
	for path := range records {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	hashInput := strings.Builder{}
	for _, path := range keys {
		fmt.Fprintf(&hashInput, "%s\x00%s\n", path, records[path])
	}
	return sha256.Sum256([]byte(hashInput.String())), nil
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
