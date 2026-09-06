package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/0merUfuk/skuggsja/internal/app"
	"github.com/0merUfuk/skuggsja/internal/audit"
	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/0merUfuk/skuggsja/internal/provider/codex"
)

func TestReleaseRetriesRetainChangedWindowsAndStopAtFirstEquality(t *testing.T) {
	root := t.TempDir()
	evidence := t.TempDir()
	if err := secureReleaseEvidenceFixture(evidence); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKUGGSJA_RELEASE_EVIDENCE_DIR", evidence)
	file := filepath.Join(root, "source.jsonl")
	if err := os.WriteFile(file, []byte("initial source record\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inputs := releaseInputs{roots: []string{root}, files: []string{file}}
	var comparisons []audit.Comparison
	attempts, comparison, err := runReleaseEqualityAttempts(context.Background(), 8, func(ctx context.Context, attempt int) (audit.Comparison, error) {
		before, err := inputs.capture(ctx)
		if err != nil {
			return audit.Comparison{}, err
		}
		writeReleaseSnapshot(t, fmt.Sprintf("live-attempt-%02d-before.json", attempt), inputs, before, time.Now().UTC(), inputs)
		if attempt < 3 {
			// A disposable source mutation models another process writing during
			// two windows. The third window is equal and must end the retries.
			if err := os.WriteFile(file, fmt.Appendf(nil, "source revision %d\n", attempt), 0o600); err != nil {
				return audit.Comparison{}, err
			}
		}
		after, err := inputs.capture(ctx)
		if err != nil {
			return audit.Comparison{}, err
		}
		writeReleaseSnapshot(t, fmt.Sprintf("live-attempt-%02d-after.json", attempt), inputs, after, time.Now().UTC(), inputs)
		comparison := audit.Compare(before, after)
		comparisons = append(comparisons, comparison)
		return comparison, nil
	})
	if err != nil || attempts != 3 || !comparison.Verified || len(comparisons) != 3 {
		t.Fatalf("attempts=%d verified=%t comparisons=%d error=%v", attempts, comparison.Verified, len(comparisons), err)
	}
	entries, err := os.ReadDir(evidence)
	if err != nil || len(entries) != 6 {
		t.Fatalf("retained attempt snapshots=%d error=%v, want 6", len(entries), err)
	}
	for index, comparison := range comparisons {
		expectedChanges := 1
		if index == 2 {
			expectedChanges = 0
		}
		if comparison.Verified != (index == 2) || comparison.ChangedFiles != expectedChanges {
			t.Fatalf("attempt %d unexpectedly discarded or changed its source comparison: %+v", index+1, comparison)
		}
		for suffix, expected := range map[string]string{"before": comparison.ManifestBefore, "after": comparison.ManifestAfter} {
			data, err := os.ReadFile(filepath.Join(evidence, fmt.Sprintf("live-attempt-%02d-%s.json", index+1, suffix)))
			if err != nil {
				t.Fatal(err)
			}
			var stored struct {
				Manifest string `json:"manifest"`
			}
			if err := json.Unmarshal(data, &stored); err != nil || stored.Manifest != expected {
				t.Fatalf("attempt %d %s manifest was not retained: error=%v", index+1, suffix, err)
			}
		}
	}
}

func TestReleaseRetriesStayBoundedWhenEveryWindowChanges(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "source.jsonl")
	if err := os.WriteFile(file, []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inputs := releaseInputs{roots: []string{root}, files: []string{file}}
	calls := 0
	attempts, comparison, err := runReleaseEqualityAttempts(context.Background(), 8, func(ctx context.Context, attempt int) (audit.Comparison, error) {
		calls++
		before, err := inputs.capture(ctx)
		if err != nil {
			return audit.Comparison{}, err
		}
		if err := os.WriteFile(file, fmt.Appendf(nil, "source revision %d\n", attempt), 0o600); err != nil {
			return audit.Comparison{}, err
		}
		after, err := inputs.capture(ctx)
		if err != nil {
			return audit.Comparison{}, err
		}
		return audit.Compare(before, after), nil
	})
	if err != nil || attempts != 8 || calls != 8 || comparison.Verified || comparison.ChangedFiles != 1 {
		t.Fatalf("attempts=%d calls=%d comparison=%+v error=%v", attempts, calls, comparison, err)
	}
}

func TestReleaseRetriesHonorCancellationAndOperationalErrors(t *testing.T) {
	t.Parallel()
	t.Run("cancelled before generation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		attempts, _, err := runReleaseEqualityAttempts(ctx, 8, func(context.Context, int) (audit.Comparison, error) {
			t.Fatal("generation started after cancellation")
			return audit.Comparison{}, nil
		})
		if attempts != 0 || !errors.Is(err, context.Canceled) {
			t.Fatalf("attempts=%d error=%v", attempts, err)
		}
	})
	t.Run("operational failure cannot pass or retry", func(t *testing.T) {
		failure := errors.New("synthetic generation failure")
		attempts, _, err := runReleaseEqualityAttempts(context.Background(), 8, func(context.Context, int) (audit.Comparison, error) {
			return audit.Comparison{Verified: true}, failure
		})
		if attempts != 1 || !errors.Is(err, failure) {
			t.Fatalf("attempts=%d error=%v", attempts, err)
		}
	})
}

func TestReleaseSnapshotRetainsExactPrivateInventoryAndAbsences(t *testing.T) {
	source := t.TempDir()
	evidence := t.TempDir()
	if err := secureReleaseEvidenceFixture(evidence); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKUGGSJA_RELEASE_EVIDENCE_DIR", evidence)
	existing := filepath.Join(source, "rollout-synthetic.jsonl")
	missing := filepath.Join(source, "absent-history.jsonl")
	content := []byte("synthetic private source\n")
	if err := os.WriteFile(existing, content, 0o600); err != nil {
		t.Fatal(err)
	}
	inputs := releaseInputs{roots: []string{source}, files: []string{existing}, configured: []string{missing}}
	snapshot, err := inputs.capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	writeReleaseSnapshot(t, "excluded-codex-before.json", inputs, snapshot, time.Now().UTC(), inputs)
	path := filepath.Join(evidence, "excluded-codex-before.json")
	if err := releaseEvidencePrivacy(path, false); err != nil {
		t.Fatalf("private snapshot access controls: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		ConfiguredFiles []string `json:"configured_files"`
		Files           map[string]struct {
			SHA256 string
			Exists bool
		} `json:"files"`
		Manifest string `json:"manifest"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	expectedHash := sha256.Sum256(content)
	if stored.Files[existing].SHA256 != hex.EncodeToString(expectedHash[:]) || !stored.Files[existing].Exists {
		t.Fatal("source path and exact source hash were not retained")
	}
	if state, found := stored.Files[missing]; !found || state.Exists {
		t.Fatal("configured source absence was not retained")
	}
	if len(stored.ConfiguredFiles) != 1 || stored.ConfiguredFiles[0] != missing || stored.Manifest != snapshot.Manifest {
		t.Fatal("configured inventory or aggregate manifest changed during serialization")
	}
}

func TestReleaseProtectedDirectoriesDoNotExpandEqualityScope(t *testing.T) {
	t.Parallel()
	store := t.TempDir()
	database := filepath.Join(store, "state.sqlite")
	nested := filepath.Join(store, "unrelated-runtime")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(database, []byte("synthetic database bytes; discovery only"), 0o600); err != nil {
		t.Fatal(err)
	}
	inputs, err := discoverReleaseInputs(context.Background(), []provider.Reader{codex.Reader{StateDatabase: database}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(inputs.protected, store) || slices.Contains(inputs.roots, store) {
		t.Fatal("the SQLite store must be protected from writes without becoming a recursive audit root")
	}
	snapshot, err := inputs.capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, captured := snapshot.Directories[nested]; captured {
		t.Fatal("a write-protected directory expanded the release equality scope")
	}
}

func TestReleaseSelectedReportPreservesExactBytesAndEqualityLabel(t *testing.T) {
	for _, verified := range []bool{true, false} {
		t.Run(fmt.Sprintf("verified=%t", verified), func(t *testing.T) {
			evidence := t.TempDir()
			if err := secureReleaseEvidenceFixture(evidence); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SKUGGSJA_RELEASE_EVIDENCE_DIR", evidence)
			original := filepath.Join(evidence, "attempt-03-rewind.json")
			data := []byte("{\n  \"synthetic_content_free_report\": true\n}\n")
			if err := os.WriteFile(original, data, 0o600); err != nil {
				t.Fatal(err)
			}
			retainSelectedReleaseReport(t, original, verified, 3, releaseInputs{}, "complete")
			name := "rewind-observed-changing.json"
			absent := "rewind.json"
			if verified {
				name, absent = absent, name
			}
			retained, err := os.ReadFile(filepath.Join(evidence, name))
			if err != nil || string(retained) != string(data) {
				t.Fatalf("selected report bytes changed: error=%v", err)
			}
			if _, err := os.Stat(filepath.Join(evidence, absent)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("report was also retained under a contradictory equality label")
			}
		})
	}
}

type releaseCountingReader struct {
	id    model.Harness
	file  string
	reads int
}

func (reader *releaseCountingReader) Harness() model.Harness { return reader.id }
func (reader *releaseCountingReader) DisplayName() string    { return string(reader.id) }
func (reader *releaseCountingReader) Discover(context.Context) (provider.Discovery, error) {
	return provider.Discovery{Harness: reader.id, Roots: []string{filepath.Dir(reader.file)}, Files: []string{reader.file}}, nil
}
func (reader *releaseCountingReader) Read(context.Context, provider.Discovery) model.ProviderResult {
	reader.reads++
	return model.ProviderResult{Harness: reader.id, DisplayName: string(reader.id), Status: "supported", SourceFiles: []string{reader.file}}
}

func TestReleaseFallbackMeasuresExactlyClaudeCursorAndIngestsEveryOriginalReader(t *testing.T) {
	root := t.TempDir()
	evidence := t.TempDir()
	if err := secureReleaseEvidenceFixture(evidence); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKUGGSJA_RELEASE_EVIDENCE_DIR", evidence)
	var readers []provider.Reader
	for _, id := range []model.Harness{model.Claude, model.Codex, model.Hermes, model.Cursor} {
		dir := filepath.Join(root, string(id))
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, "source.jsonl")
		if err := os.WriteFile(file, []byte("synthetic source\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		readers = append(readers, &releaseCountingReader{id: id, file: file})
	}
	scope, err := selectReleaseScope(readers, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(scope.equality) != 2 || scope.equality[0] != readers[0] || scope.equality[1] != readers[3] || len(scope.snapshot) != 1 || scope.snapshot[0] != readers[1] {
		t.Fatal("fallback changed the exact Claude+Cursor equality or own-Codex snapshot scope")
	}
	for i, reader := range readers {
		if scope.ingestion[i] != reader {
			t.Fatal("fallback replaced an original ingestion reader")
		}
	}
	if scope.hermesEquality() != "unmeasured" {
		t.Fatal("fallback claimed Hermes equality")
	}
	fullScope, err := selectReleaseScope(readers, true, false)
	if err != nil {
		t.Fatal(err)
	}
	all, err := discoverReleaseInputs(context.Background(), readers)
	if err != nil {
		t.Fatal(err)
	}
	full, err := discoverReleaseInputs(context.Background(), fullScope.equality)
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := discoverReleaseInputs(context.Background(), scope.equality)
	if err != nil {
		t.Fatal(err)
	}
	beforeFull, err := full.capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	beforeFallback, err := fallback.capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// External activity in a disposable Hermes source makes complete equality
	// fail while the explicitly narrower Claude+Cursor comparison can pass.
	if err := os.WriteFile(readers[2].(*releaseCountingReader).file, []byte("changed synthetic Hermes source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	generation, err := app.Generate(context.Background(), app.GenerateOptions{Readers: scope.ingestion, OutputPath: filepath.Join(t.TempDir(), "rewind.json"), AuditSources: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(generation.Report.Providers) != 4 {
		t.Fatal("fallback narrowed generated provider output")
	}
	for _, reader := range readers {
		if reader.(*releaseCountingReader).reads != 1 {
			t.Fatalf("original %s reader was not ingested once", reader.Harness())
		}
	}
	afterFull, err := full.capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	afterFallback, err := fallback.capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if audit.Compare(beforeFull, afterFull).Verified || !audit.Compare(beforeFallback, afterFallback).Verified {
		t.Fatal("fallback result was conflated with the complete-scope result")
	}
	writeReleaseScope(t, scope, true, all)
	data, err := os.ReadFile(filepath.Join(evidence, "release-scope.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Equality []model.Harness `json:"live_equality_harnesses"`
		Ingested []model.Harness `json:"ingested_harnesses"`
		Hermes   string          `json:"hermes_live_equality"`
		Complete string          `json:"complete_scope_result"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(record.Equality, []model.Harness{model.Claude, model.Cursor}) || len(record.Ingested) != 4 || record.Hermes != "unmeasured" || record.Complete != "not_measured_in_this_run" {
		t.Fatal("private scope record does not preserve the fallback limitation")
	}
}

func TestReleaseFallbackRejectsFurtherNarrowingAndUnknownReaders(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		ids          []model.Harness
		excludeCodex bool
	}{
		{"missing explicit Codex snapshot", []model.Harness{model.Claude, model.Codex, model.Hermes, model.Cursor}, false},
		{"missing Claude", []model.Harness{model.Codex, model.Hermes, model.Cursor}, true},
		{"missing Cursor", []model.Harness{model.Claude, model.Codex, model.Hermes}, true},
		{"missing Hermes ingestion", []model.Harness{model.Claude, model.Codex, model.Cursor}, true},
		{"missing Codex ingestion", []model.Harness{model.Claude, model.Hermes, model.Cursor}, true},
		{"duplicate Claude", []model.Harness{model.Claude, model.Codex, model.Hermes, model.Cursor, model.Claude}, true},
		{"unknown reader", []model.Harness{model.Claude, model.Codex, model.Hermes, model.Cursor, "unknown"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var readers []provider.Reader
			for _, id := range test.ids {
				readers = append(readers, &releaseCountingReader{id: id})
			}
			if _, err := selectReleaseScope(readers, test.excludeCodex, true); err == nil {
				t.Fatal("accepted an unauthorized fallback scope")
			}
		})
	}
}

func TestReleaseFallbackReportNamesCannotImplyCompleteScopeEquality(t *testing.T) {
	for _, verified := range []bool{true, false} {
		t.Run(fmt.Sprintf("verified=%t", verified), func(t *testing.T) {
			evidence := t.TempDir()
			if err := secureReleaseEvidenceFixture(evidence); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SKUGGSJA_RELEASE_EVIDENCE_DIR", evidence)
			scope := releaseSourceScope{name: "claude_cursor_fallback"}
			if scope.manifestPrefix() != "claude-cursor" || scope.attemptReportName(2) != "claude-cursor-attempt-02-rewind.json" {
				t.Fatal("fallback evidence filenames overlap complete-scope evidence")
			}
			original := filepath.Join(evidence, scope.attemptReportName(2))
			if err := os.WriteFile(original, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			retainSelectedReleaseReport(t, original, verified, 2, releaseInputs{}, scope.name)
			name := "rewind-claude-cursor-observed-changing.json"
			if verified {
				name = "rewind-claude-cursor-verified.json"
			}
			if _, err := os.Stat(filepath.Join(evidence, name)); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(evidence, "rewind.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("fallback report used the complete-scope verified name")
			}
		})
	}
}
