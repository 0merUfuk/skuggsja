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

	"github.com/0merUfuk/skuggsja/internal/audit"
	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/0merUfuk/skuggsja/internal/provider/codex"
)

func TestReleaseRetriesRetainChangedWindowsAndStopAtFirstEquality(t *testing.T) {
	root := t.TempDir()
	evidence := t.TempDir()
	if err := os.Chmod(evidence, 0o700); err != nil {
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
	if err := os.Chmod(evidence, 0o700); err != nil {
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
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private snapshot permissions: info=%v error=%v", info, err)
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
			if err := os.Chmod(evidence, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SKUGGSJA_RELEASE_EVIDENCE_DIR", evidence)
			original := filepath.Join(evidence, "attempt-03-rewind.json")
			data := []byte("{\n  \"synthetic_content_free_report\": true\n}\n")
			if err := os.WriteFile(original, data, 0o600); err != nil {
				t.Fatal(err)
			}
			retainSelectedReleaseReport(t, original, verified, 3, releaseInputs{})
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
