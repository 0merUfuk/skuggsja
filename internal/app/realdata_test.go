package app_test

import (
	"context"
	"crypto/sha256"
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

// TestRealDataFullRunLeavesSourcesUnchanged is opt-in because it reads the
// operator's actual histories. Its outer snapshots bracket Generate in full,
// including persistence of the aggregate artifact to an isolated temp path.
func TestRealDataFullRunLeavesSourcesUnchanged(t *testing.T) {
	if os.Getenv("SKUGGSJA_VERIFY_REAL_DATA") != "1" {
		t.Skip("set SKUGGSJA_VERIFY_REAL_DATA=1 to run against local histories")
	}
	paths, err := platform.DefaultPaths()
	if err != nil {
		t.Fatal("resolve default source locations")
	}
	readers := []provider.Reader{
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var generation app.Generation
	verified := false
	for attempt := 1; attempt <= 3; attempt++ {
		var roots, files, configured []string
		for _, reader := range readers {
			discovery, discoverErr := reader.Discover(ctx)
			if discoverErr != nil {
				t.Fatalf("discover %s: source location unavailable", reader.Harness())
			}
			roots = append(roots, discovery.Roots...)
			files = append(files, discovery.Files...)
			files = append(files, discovery.AuditFiles...)
			configured = append(configured, discovery.ConfiguredFiles...)
		}
		if err := waitForQuietSourceMetadata(ctx, roots, append(append([]string(nil), files...), configured...)); err != nil {
			t.Logf("active_source_window attempt=%d warning=%q; proceeding with strict before/after captures", attempt, err)
		}
		before, err := audit.CaptureConfigured(ctx, roots, files, configured)
		if err != nil {
			t.Fatal("capture outer before snapshot")
		}

		started := time.Now()
		generation, err = app.Generate(ctx, app.GenerateOptions{
			Readers: readers, Location: time.Local,
			OutputPath: filepath.Join(t.TempDir(), "rewind.json"), AuditSources: true,
		})
		if err != nil {
			t.Fatalf("generate Rewind: %v", err)
		}
		after, err := audit.CaptureConfigured(ctx, roots, files, configured)
		if err != nil {
			t.Fatal("capture outer after snapshot")
		}
		comparison := audit.Compare(before, after)
		internal := generation.Report.Privacy.SourceAudit
		t.Logf(
			"outer_source_audit attempt=%d files_before=%d files_after=%d directories_before=%d directories_after=%d complete_before=%t complete_after=%t before=%s after=%s changed_files=%d directory_changes=%d verified=%t generation_elapsed=%s outer_window_elapsed=%s",
			attempt, len(before.Files), len(after.Files), len(before.Directories), len(after.Directories), before.Complete, after.Complete,
			comparison.ManifestBefore, comparison.ManifestAfter,
			comparison.ChangedFiles, comparison.DirectoryChanges, comparison.Verified,
			generation.Duration.Round(time.Millisecond), time.Since(started).Round(time.Millisecond),
		)
		t.Logf(
			"internal_source_audit files=%d before=%s after=%s changed_files=%d directory_changes=%d verified=%t",
			internal.Files, internal.ManifestBefore, internal.ManifestAfter,
			internal.ChangedFiles, internal.DirectoryChanges, internal.Verified,
		)
		logChangedProviders(t, comparison, before, after, paths)
		if comparison.Verified && internal.Verified {
			verified = true
			break
		}
	}

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
	if !verified {
		t.Fatal("no complete audited full-run window retained identical source hashes and directory listings after three attempts")
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

func waitForQuietSourceMetadata(ctx context.Context, roots, files []string) error {
	previous, err := sourceMetadataSignature(roots, files)
	if err != nil {
		return err
	}
	for check := 0; check < 5; check++ {
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		current, err := sourceMetadataSignature(roots, files)
		if err != nil {
			return err
		}
		if current == previous {
			return nil
		}
		previous = current
	}
	return errors.New("source metadata remained active for ten seconds")
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
