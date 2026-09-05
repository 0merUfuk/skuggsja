package codex

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/0merUfuk/skuggsja/internal/audit"
	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/klauspost/compress/zstd"
)

func TestThreadHistoryDatabaseIsAuditedWithoutParsingOrInventingUsage(t *testing.T) {
	t.Parallel()
	for _, present := range []bool{false, true} {
		t.Run(fmt.Sprintf("present=%t", present), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "thread_history_1.sqlite")
			const sentinel = "synthetic malformed SQLite; never pass this source to SQLite"
			if present {
				if err := os.WriteFile(path, []byte(sentinel), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			reader := Reader{ThreadHistoryDatabase: path}
			discovery, err := reader.Discover(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, configured := range codexSQLitePaths(path) {
				if !slices.Contains(discovery.ConfiguredFiles, configured) {
					t.Errorf("configured database/sidecar path was omitted: %s", filepath.Base(configured))
				}
			}
			if len(discovery.Files) != 0 || slices.Contains(discovery.AuditFiles, path) != present {
				t.Fatalf("database discovery = %#v, want audit-only iff present", discovery)
			}
			result := reader.Read(context.Background(), discovery)
			if len(result.Sessions) != 0 || result.ToolCallsAvailable {
				t.Fatalf("unparsed database fabricated usage: sessions=%d tools_available=%t", len(result.Sessions), result.ToolCallsAvailable)
			}
			if hasWarningCode(result.Warnings, "unparsed_thread_history_database") != present {
				t.Fatalf("warnings = %#v, want unparsed-database warning iff present", result.Warnings)
			}
			if present {
				if result.Status != "supported with warnings" || result.Coverage.Status != "coverage assessment incomplete" || result.Coverage.Confidence != "low" {
					t.Fatalf("unparsed database coverage = %#v, status=%q", result.Coverage, result.Status)
				}
				got, err := os.ReadFile(path)
				if err != nil || string(got) != sentinel {
					t.Fatalf("source changed: read_error=%v sentinel_retained=%t", err, string(got) == sentinel)
				}
			} else if result.Status != "not found" {
				t.Fatalf("absent database status = %q, want not found", result.Status)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			wantFiles := 0
			if present {
				wantFiles = 1
			}
			if len(entries) != wantFiles {
				t.Fatalf("database read created source-directory files: got=%d want=%d", len(entries), wantFiles)
			}
		})
	}
}

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

func TestDiscoverRejectsSymbolicLinkRollout(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "rollout-target.jsonl")
	if err := os.WriteFile(target, syntheticLegacyRollout("target", "Synthetic prompt."), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "rollout-link.jsonl")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := (Reader{SessionsDir: root}).Discover(context.Background())
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Discover() error = %v, want symbolic-link rejection", err)
	}
}

func TestRecoveryDiscoveryRetainsAuditSourcesWithoutInflatingCopiedUsage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	recovery := filepath.Join(root, "recovery", "saved", "nested")
	for _, dir := range []string{sessions, recovery} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	data := syntheticLegacyRollout("copied", "Count this prompt once.")
	canonical := filepath.Join(sessions, "rollout-canonical.jsonl")
	copied := filepath.Join(recovery, "rollout-recovered.jsonl")
	for _, path := range []string{canonical, copied} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reader := Reader{SessionsDir: sessions}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	baseline := reader.Read(context.Background(), discovery)
	reader.RecoveryDir = filepath.Join(root, "recovery")
	discovery, err = reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if !reflect.DeepEqual(result.Sessions, baseline.Sessions) || !reflect.DeepEqual(result.Coverage, baseline.Coverage) {
		t.Fatalf("byte-identical recovery changed usage or coverage: sessions=%#v coverage=%#v", result.Sessions, result.Coverage)
	}
	if len(discovery.Files) != 2 || len(result.SourceFiles) != 2 || !slices.Contains(result.SourceFiles, canonical) || !slices.Contains(result.SourceFiles, copied) {
		t.Fatalf("physical audit sources were lost: discovery=%#v sources=%v", discovery, result.SourceFiles)
	}
	if warningCount(result, "identical_rollout_copy_suppressed") != 1 || warningCount(result, "ambiguous_history_chain_suppressed") != 0 || warningCount(result, "unvalidated_same_id_rollout_suppressed") != 0 {
		t.Fatalf("recovery warnings = %#v", result.Warnings)
	}
	for _, path := range []string{canonical, copied} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(data) {
			t.Fatalf("source changed: path=%s error=%v", path, err)
		}
	}
	// A transcript that survives only in recovery must contribute its evidence.
	unique := filepath.Join(recovery, "rollout-unique.jsonl")
	if err := os.WriteFile(unique, syntheticLegacyRollout("recovery-only", "Recovered unique prompt."), 0o600); err != nil {
		t.Fatal(err)
	}
	discovery, err = reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result = reader.Read(context.Background(), discovery)
	if len(result.Sessions) != 2 || sessionByID(result, "recovery-only") == nil || len(result.SourceFiles) != 3 {
		t.Fatalf("unique recovery evidence missing: sessions=%d sources=%d", len(result.Sessions), len(result.SourceFiles))
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
	if len(discovery.AuditFiles) != 1 || discovery.AuditFiles[0] != plainPath+".zst" {
		t.Fatalf("suppressed sibling was omitted from audit sources: %v", discovery.AuditFiles)
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
	if len(result.SourceFiles) != 3 || !slices.Contains(result.SourceFiles, plainPath+".zst") {
		t.Fatalf("physical source coverage = %v, want all three files", result.SourceFiles)
	}
	before, err := audit.CaptureConfigured(context.Background(), discovery.Roots, result.SourceFiles, discovery.ConfiguredFiles)
	if err != nil {
		t.Fatal(err)
	}
	// Same path and length, different bytes: directory membership alone cannot
	// detect a change to this suppressed source.
	if err := os.WriteFile(plainPath+".zst", []byte("NOT a zstd frame"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := audit.CaptureConfigured(context.Background(), discovery.Roots, result.SourceFiles, discovery.ConfiguredFiles)
	if err != nil {
		t.Fatal(err)
	}
	comparison := audit.Compare(before, after)
	if comparison.Verified || comparison.ChangedFiles != 1 || comparison.DirectoryChanges != 0 || comparison.ManifestBefore == comparison.ManifestAfter {
		t.Fatalf("suppressed sibling change escaped content audit: %#v", comparison)
	}
}

func TestReaderStitchesValidatedPaginatedHistorySegments(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	basePrefix := "" +
		`{"ordinal":0,"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"chain","session_id":"chain","cwd":"/synthetic/chain","history_mode":"paginated"}}` + "\n" +
		`{"ordinal":1,"timestamp":"2026-01-01T00:00:01Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"prompt-base","content":[{"type":"input_text","text":"base prompt"}]}}}` + "\n" +
		`{"ordinal":1,"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":8,"output_tokens":2,"total_tokens":10}}}}` + "\n"
	offset := len(basePrefix)
	base := basePrefix +
		`{"ordinal":2,"timestamp":"2026-01-01T00:00:03Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"stale","content":[{"type":"input_text","text":"superseded branch"}]}}}` + "\n" +
		`{"timestamp":"2026-01-01T00:00:04Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"ordinal-less-stale","content":[{"type":"input_text","text":"also superseded"}]}}}` + "\n"
	continuation := fmt.Sprintf(
		`{"ordinal":2,"timestamp":"2026-01-02T00:00:00Z","type":"session_meta","payload":{"id":"chain","session_id":"chain","cwd":"/synthetic/chain","history_mode":"paginated","history_base":{"thread_id":"chain","end_byte_offset":%d,"end_ordinal_exclusive":2}}}`+"\n"+
			`{"ordinal":3,"timestamp":"2026-01-02T00:00:01Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"prompt-tail","content":[{"type":"input_text","text":"tail prompt"}]}}}`+"\n"+
			`{"ordinal":4,"timestamp":"2026-01-02T00:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":4,"output_tokens":1,"total_tokens":5}}}}`+"\n",
		offset,
	)
	shadow := "not-json\n" +
		`{"ordinal":0,"timestamp":"2026-01-03T00:00:00Z","type":"session_meta","payload":{"id":"chain","session_id":"chain","history_mode":"paginated"}}` + "\n" +
		`{"ordinal":1,"timestamp":"2026-01-03T00:00:01Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"shadow","content":[{"type":"input_text","text":"must not merge"}]}}}` + "\n"
	for name, data := range map[string]string{
		"rollout-base.jsonl": base, "rollout-continuation.jsonl": continuation, "rollout-shadow.jsonl": shadow,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Identical copies of either physical segment must not break the validated
	// chain or create another prompt/token contribution.
	recovery := filepath.Join(t.TempDir(), "recovery", "nested")
	if err := os.MkdirAll(recovery, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"rollout-base-copy.jsonl": base, "rollout-tail-copy.jsonl": continuation} {
		if err := os.WriteFile(filepath.Join(recovery, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reader := Reader{SessionsDir: root, RecoveryDir: filepath.Dir(recovery)}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if got, want := len(result.Sessions), 1; got != want {
		t.Fatalf("sessions = %d, want %d; warnings=%#v", got, want, result.Warnings)
	}
	session := result.Sessions[0]
	if got, want := len(session.Prompts), 2; got != want {
		t.Fatalf("prompts = %d, want %d", got, want)
	}
	if got, want := session.Usage.Input, int64(12); got != want {
		t.Errorf("stitched input tokens = %d, want %d", got, want)
	}
	if warningCount(result, "stitched_history_segments") != 1 {
		t.Errorf("warnings = %#v", result.Warnings)
	}
	if warningCount(result, "unvalidated_same_id_rollout_suppressed") != 1 || warningCount(result, "history_chain_inspection_failed") != 1 {
		t.Errorf("unvalidated duplicate warnings = %#v", result.Warnings)
	}
	if warningCount(result, "identical_rollout_copy_suppressed") != 2 || warningCount(result, "ambiguous_history_chain_suppressed") != 0 || len(result.SourceFiles) != 5 {
		t.Errorf("copied chain warnings/sources = %#v / %d", result.Warnings, len(result.SourceFiles))
	}
}

func TestHistoryBoundaryValidatesEOFOrdinal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "rollout-base.jsonl")
	data := []byte(`{"ordinal":0,"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"chain"}}` + "\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if !validateHistoryBoundary(path, int64(len(data)), 1) {
		t.Fatal("valid EOF boundary was rejected")
	}
	if validateHistoryBoundary(path, int64(len(data)), 5) {
		t.Fatal("gapped EOF boundary was accepted")
	}
	empty := filepath.Join(root, "rollout-empty.jsonl")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if validateHistoryBoundary(empty, 0, 3) {
		t.Fatal("empty base with nonzero ordinal was accepted")
	}
}

func TestLonePaginatedContinuationIsMarkedIncomplete(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rollout := `{"ordinal":8,"timestamp":"2026-01-02T00:00:00Z","type":"session_meta","payload":{"id":"partial","session_id":"partial","history_mode":"paginated","history_base":{"thread_id":"partial","end_byte_offset":100,"end_ordinal_exclusive":8}}}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "rollout-partial.jsonl"), []byte(rollout), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := Reader{SessionsDir: root}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if warningCount(result, "missing_history_base") != 1 || result.Coverage.Status != "known incomplete" {
		t.Fatalf("warning/coverage = %#v / %#v", result.Warnings, result.Coverage)
	}
}

func TestHistoryAndSessionIndexExposeMissingRollouts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	if err := os.Mkdir(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	detailedRollout := strings.ReplaceAll(string(syntheticLegacyRollout("detailed", "rollout prompt")), "2026-01-03", "2026-07-03")
	if err := os.WriteFile(filepath.Join(sessionsDir, "rollout-detailed.jsonl"), []byte(detailedRollout), 0o600); err != nil {
		t.Fatal(err)
	}
	historyPath := filepath.Join(root, "history.jsonl")
	history := `{"session_id":"detailed","text":"duplicate","ts":1782864000}` + "\n" +
		`{"session_id":"history-only","text":"older prompt","ts":1777593600}` + "\n"
	if err := os.WriteFile(historyPath, []byte(history), 0o600); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, "session_index.jsonl")
	index := `{"id":"detailed","updated_at":"2026-07-01T00:00:00Z"}` + "\n" +
		`{"id":"history-only","updated_at":"2026-05-01T00:00:00Z"}` + "\n" +
		`{"id":"index-only","updated_at":"2026-04-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(indexPath, []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := Reader{SessionsDir: sessionsDir, HistoryFile: historyPath, SessionIndexFile: indexPath}
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
}

func TestStateDatabaseExposesMissingRolloutsWithoutCountingUsage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	if err := os.Mkdir(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rolloutPath := filepath.Join(sessionsDir, "rollout-detailed.jsonl")
	if err := os.WriteFile(rolloutPath, syntheticLegacyRollout("detailed", "rollout prompt"), 0o600); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(root, "state_5.sqlite")
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE threads (id TEXT PRIMARY KEY, rollout_path TEXT NOT NULL, created_at INTEGER NOT NULL, created_at_ms INTEGER)`); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO threads VALUES ('detailed', ?, 1782864000, NULL)`, rolloutPath); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO threads VALUES ('state-only', ?, 1777593600, NULL)`, filepath.Join(sessionsDir, "rollout-missing.jsonl")); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reader := Reader{SessionsDir: sessionsDir, StateDatabase: databasePath}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if got, want := len(result.Sessions), 1; got != want {
		t.Fatalf("sessions = %d, want %d", got, want)
	}
	if result.Coverage.UnmaterializedSessions != 1 || result.Coverage.Status != "known incomplete" ||
		warningCount(result, "missing_state_rollout_paths") != 1 || warningCount(result, "state_only_sessions") != 1 {
		t.Fatalf("warnings/coverage = %#v / %#v", result.Warnings, result.Coverage)
	}
}

func TestStateDatabaseDoesNotProbePathsOutsideConfiguredRoots(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	if err := os.Mkdir(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rolloutPath := filepath.Join(sessionsDir, "rollout-detailed.jsonl")
	if err := os.WriteFile(rolloutPath, syntheticLegacyRollout("detailed", "rollout prompt"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(root, "outside", "rollout-detailed.jsonl")
	databasePath := filepath.Join(root, "state_5.sqlite")
	database := createStateDatabase(t, databasePath)
	if _, err := database.Exec(`INSERT INTO threads VALUES ('detailed', ?, 1782864000, NULL)`, outsidePath); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO threads VALUES ('relative', ?, 1777593600, NULL)`, filepath.Join("sessions", "rollout-relative.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsDir, "rollout-relative.jsonl"), syntheticLegacyRollout("relative", "relative prompt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reader := Reader{SessionsDir: sessionsDir, StateDatabase: databasePath}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if warningCount(result, "state_rollouts_outside_configured_roots") != 1 ||
		warningCount(result, "existing_state_rollouts_not_discovered") != 0 {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestCatalogDatabaseAddsAccountAndMissingLocalCoverageOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	if err := os.Mkdir(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	detailedRollout := strings.ReplaceAll(string(syntheticLegacyRollout("detailed", "rollout prompt")), "2026-01-03", "2026-07-03")
	if err := os.WriteFile(filepath.Join(sessionsDir, "rollout-detailed.jsonl"), []byte(detailedRollout), 0o600); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(root, "catalog.db")
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE local_thread_catalog (thread_id TEXT, source_created_at REAL NOT NULL, host_id TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE local_thread_catalog_hosts (host_id TEXT PRIMARY KEY, host_kind TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO local_thread_catalog_hosts VALUES ('local-host', 'local')`,
		`INSERT INTO local_thread_catalog_hosts VALUES ('account-a', 'chatgpt')`,
		`INSERT INTO local_thread_catalog_hosts VALUES ('account-b', 'chatgpt')`,
		`INSERT INTO local_thread_catalog_hosts VALUES ('future-host', 'unknown')`,
		`INSERT INTO local_thread_catalog VALUES ('detailed', 1782864000.5, 'local-host')`,
		`INSERT INTO local_thread_catalog VALUES ('catalog-only', 1777593600.25, 'local-host')`,
		`INSERT INTO local_thread_catalog VALUES ('remote-a', 1775001600, 'account-a')`,
		`INSERT INTO local_thread_catalog VALUES ('remote-b', 1775001600, 'account-b')`,
		`INSERT INTO local_thread_catalog VALUES ('unknown', 1770000000, 'future-host')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, "session_index.jsonl")
	if err := os.WriteFile(indexPath, []byte(`{"id":"catalog-only","updated_at":"2026-06-01T00:00:00Z"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := Reader{SessionsDir: sessionsDir, SessionIndexFile: indexPath, CatalogDatabase: databasePath}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if len(result.Sessions) != 1 || result.Coverage.UnmaterializedSessions != 1 ||
		warningCount(result, "account_workspace_catalogs") != 2 ||
		warningCount(result, "remote_catalog_sessions_excluded") != 2 ||
		warningCount(result, "local_catalog_only_sessions") != 1 {
		t.Fatalf("result = %#v", result)
	}
	wantEarliest := time.Unix(1777593600, 250000000).UTC()
	if !result.Coverage.EarliestLocalEvidence.Equal(wantEarliest) {
		t.Fatalf("earliest local evidence = %s, want %s", result.Coverage.EarliestLocalEvidence, wantEarliest)
	}
}

func TestInvalidOrMissingSessionMetadataIsWarnedAndNotCounted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	invalid := `{"timestamp":"not-a-time","type":"session_meta","payload":{"id":"invalid","session_id":"invalid","history_mode":"legacy"}}` + "\n" +
		`{"timestamp":"2026-01-03T03:00:01Z","type":"event_msg","payload":{"type":"user_message","message":"must not count"}}` + "\n"
	missing := `{"timestamp":"2026-01-03T03:00:01Z","type":"event_msg","payload":{"type":"user_message","message":"must not count"}}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "rollout-invalid.jsonl"), []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rollout-missing.jsonl"), []byte(missing), 0o600); err != nil {
		t.Fatal(err)
	}
	result := readTestRoot(t, root)
	if len(result.Sessions) != 0 || warningCount(result, "invalid_timestamp") != 1 ||
		warningCount(result, "missing_or_invalid_session_meta") != 2 || result.ToolCallsAvailable {
		t.Fatalf("result = %#v", result)
	}
}

func TestOversizeExternalImportIndexIsBoundedAndWarned(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "external_imports.json")
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
	reader := Reader{ExternalImportsFile: path}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := reader.Read(context.Background(), discovery)
	if warningCount(result, "oversize_external_imports") != 1 || result.Coverage.Status != "coverage assessment incomplete" {
		t.Fatalf("result = %#v", result)
	}
}

func createStateDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE threads (id TEXT PRIMARY KEY, rollout_path TEXT NOT NULL, created_at INTEGER NOT NULL, created_at_ms INTEGER)`); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	return database
}

func readTestRoot(t *testing.T, root string) model.ProviderResult {
	t.Helper()
	reader := Reader{SessionsDir: root}
	discovery, err := reader.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return reader.Read(context.Background(), discovery)
}

func warningCount(result model.ProviderResult, code string) int {
	for _, warning := range result.Warnings {
		if warning.Code == code {
			return warning.Count
		}
	}
	return 0
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
