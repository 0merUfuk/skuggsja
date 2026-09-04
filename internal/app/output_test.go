package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanLeavesProductDirectorySymlink(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "skuggsja")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	artifact := filepath.Join(link, artifactName)
	if err := os.WriteFile(artifact, []byte("synthetic aggregate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Clean(artifact); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("product-directory symlink was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, artifactName)); !os.IsNotExist(err) {
		t.Fatalf("aggregate artifact still exists: %v", err)
	}
}

func TestCleanLeavesNonemptyProductDirectory(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	artifact := filepath.Join(directory, artifactName)
	if err := os.WriteFile(artifact, []byte("synthetic aggregate"), 0o600); err != nil {
		t.Fatal(err)
	}
	neighbor := filepath.Join(directory, "keep.txt")
	if err := os.WriteFile(neighbor, []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Clean(artifact); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(neighbor); err != nil || string(got) != "unrelated" {
		t.Fatalf("neighbor changed: %q, %v", got, err)
	}
}
