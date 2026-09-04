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
