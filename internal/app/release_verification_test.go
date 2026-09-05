package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/0merUfuk/skuggsja/internal/provider/codex"
)

func TestReleaseQuietGateRestartsAfterSourceActivity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "source.jsonl")
	if err := os.WriteFile(file, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const required = 80 * time.Millisecond
	var changed time.Time
	var once sync.Once
	progress := func(time.Duration, time.Duration) {
		once.Do(func() {
			changed = time.Now()
			if err := os.WriteFile(file, []byte("second source record\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := waitForContinuousQuiet(ctx, []string{root}, []string{file}, required, 5*time.Millisecond, progress); err != nil {
		t.Fatal(err)
	}
	if changed.IsZero() {
		t.Fatal("source mutation did not run after the initial quiet-gate sample")
	}
	if elapsed := time.Since(changed); elapsed < required {
		t.Fatalf("quiet gate returned %s after activity; required %s", elapsed, required)
	}
}

func TestReleaseQuietGateDeadlineDoesNotWaiveWindow(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := waitForContinuousQuiet(ctx, []string{t.TempDir()}, nil, time.Second, 5*time.Millisecond, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("quiet gate error = %v, want deadline exceeded", err)
	}
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
