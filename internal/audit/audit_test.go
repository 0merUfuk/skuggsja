package audit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCompareProvesUnchangedFilesAndListings(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte("synthetic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := Capture(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	after, err := Capture(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	comparison := Compare(before, after)
	if !comparison.Verified || comparison.ChangedFiles != 0 || comparison.DirectoryChanges != 0 {
		t.Fatalf("unchanged comparison = %+v", comparison)
	}
}

func TestCompareDetectsNewSourceDirectoryEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	if err := os.WriteFile(path, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := Capture(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+"-shm", []byte("new sidecar"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := Capture(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	comparison := Compare(before, after)
	if comparison.Verified || comparison.DirectoryChanges == 0 {
		t.Fatalf("changed comparison = %+v", comparison)
	}
}

func TestCompareInventoriesDiscoveryRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	existing := filepath.Join(root, "2025")
	if err := os.Mkdir(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	before, err := Capture(context.Background(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "2026"), 0o700); err != nil {
		t.Fatal(err)
	}
	after, err := Capture(context.Background(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	comparison := Compare(before, after)
	if comparison.DirectoryChanges != 1 {
		t.Fatalf("root directory changes = %+v", comparison)
	}
}

func TestCompareDetectsEntryInNestedSourceDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	month := filepath.Join(root, "2026", "09")
	day := filepath.Join(month, "04")
	if err := os.MkdirAll(day, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(day, "rollout-synthetic.jsonl")
	if err := os.WriteFile(source, []byte("synthetic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := CaptureDiscovered(context.Background(), []string{root}, []string{source})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(month, "05"), 0o700); err != nil {
		t.Fatal(err)
	}
	after, err := CaptureDiscovered(context.Background(), []string{root}, []string{source})
	if err != nil {
		t.Fatal(err)
	}
	comparison := Compare(before, after)
	if comparison.Verified || comparison.DirectoryChanges == 0 {
		t.Fatalf("nested directory change = %+v", comparison)
	}
}

func TestCompareDetectsDisappearingDiscoveredFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(source, []byte("synthetic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := CaptureDiscovered(context.Background(), nil, []string{source})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	after, err := CaptureDiscovered(context.Background(), nil, []string{source})
	if err != nil {
		t.Fatal(err)
	}
	comparison := Compare(before, after)
	if comparison.Verified || comparison.ChangedFiles != 1 {
		t.Fatalf("disappearing file comparison = %+v", comparison)
	}
}

func TestMissingDiscoveredFileKeepsComparisonUnverified(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stable := filepath.Join(dir, "stable.jsonl")
	missing := filepath.Join(dir, "missing.jsonl")
	if err := os.WriteFile(stable, []byte("synthetic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := CaptureDiscovered(context.Background(), nil, []string{stable, missing})
	if err != nil {
		t.Fatal(err)
	}
	after, err := CaptureDiscovered(context.Background(), nil, []string{stable, missing})
	if err != nil {
		t.Fatal(err)
	}
	if comparison := Compare(before, after); comparison.Verified {
		t.Fatalf("missing discovered file was treated as verified: %+v", comparison)
	}
}

func TestOptionalConfiguredAbsenceIsVerifiedAndAppearanceIsDetected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stable := filepath.Join(dir, "stable.jsonl")
	optional := filepath.Join(dir, "future-index.jsonl")
	if err := os.WriteFile(stable, []byte("synthetic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := CaptureConfigured(context.Background(), nil, []string{stable}, []string{optional})
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := CaptureConfigured(context.Background(), nil, []string{stable}, []string{optional})
	if err != nil {
		t.Fatal(err)
	}
	if comparison := Compare(before, unchanged); !comparison.Verified {
		t.Fatalf("stable optional absence was not verified: %+v", comparison)
	}
	if err := os.WriteFile(optional, []byte("appeared\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := CaptureConfigured(context.Background(), nil, []string{stable}, []string{optional})
	if err != nil {
		t.Fatal(err)
	}
	if comparison := Compare(before, after); comparison.Verified || comparison.ChangedFiles != 1 || comparison.DirectoryChanges == 0 {
		t.Fatalf("appearing optional source was not detected: %+v", comparison)
	}
}

func TestConfiguredRootAppearanceIsDetected(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	stable := filepath.Join(base, "stable.jsonl")
	futureRoot := filepath.Join(base, "future-sessions")
	if err := os.WriteFile(stable, []byte("synthetic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := CaptureConfigured(context.Background(), []string{futureRoot}, []string{stable}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(futureRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	after, err := CaptureConfigured(context.Background(), []string{futureRoot}, []string{stable}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if comparison := Compare(before, after); comparison.Verified || comparison.DirectoryChanges == 0 {
		t.Fatalf("appearing configured root was not detected: %+v", comparison)
	}
}

func TestCaptureRejectsSymbolicLinkDiscoveredFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target.jsonl")
	if err := os.WriteFile(target, []byte("synthetic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "session.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := CaptureDiscovered(context.Background(), nil, []string{link}); err == nil {
		t.Fatal("CaptureDiscovered accepted a symbolic-link source")
	}
}

func TestCaptureRejectsSymbolicLinkSQLiteSidecar(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	database := filepath.Join(dir, "state.db")
	if err := os.WriteFile(database, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, database+"-wal"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := CaptureDiscovered(context.Background(), nil, []string{database}); err == nil {
		t.Fatal("CaptureDiscovered accepted a symbolic-link SQLite sidecar")
	}
}
