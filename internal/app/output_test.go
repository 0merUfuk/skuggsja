package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/0merUfuk/skuggsja/internal/analytics"
)

func TestExplicitOutputDirectoryRetainsSourceProtection(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "history")
	t.Setenv("SKUGGSJA_OUTPUT_DIRECTORY", source)
	output, err := DefaultOutputPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureOutputSeparate(output, []string{source}, nil); !errors.Is(err, errOutputOverlapsSource) {
		t.Fatalf("explicit output inside source accepted: %v", err)
	}
	if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output resolution changed absent source: %v", err)
	}
	t.Setenv("SKUGGSJA_OUTPUT_DIRECTORY", "relative-output")
	if _, err := DefaultOutputPath(); err == nil {
		t.Fatal("relative output directory accepted")
	}
}

func TestEnsureOutputSeparateRejectsAbsentSourceFileAboveOutput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "future-source")
	output := filepath.Join(source, "nested", artifactName)
	if err := EnsureOutputSeparate(output, nil, []string{source}); !errors.Is(err, errOutputOverlapsSource) {
		t.Fatalf("EnsureOutputSeparate() error = %v, want output/source overlap", err)
	}
	if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("separation check changed absent source path: %v", err)
	}
}

func TestWriteReportRefusesProductDirectorySymlink(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	targetArtifact := filepath.Join(target, artifactName)
	if err := os.WriteFile(targetArtifact, []byte("preserve target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "skuggsja")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := WriteReport(filepath.Join(link, artifactName), analytics.Report{}); err == nil {
		t.Fatal("WriteReport accepted a symbolic-link product directory")
	}
	if got, err := os.ReadFile(targetArtifact); err != nil || string(got) != "preserve target" {
		t.Fatalf("symlink target changed: content=%q error=%v", got, err)
	}
}

func TestWriteReportRefusesIntermediateDirectorySymlink(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	targetParent := filepath.Join(base, "target-parent")
	targetOutput := filepath.Join(targetParent, "skuggsja")
	if err := os.MkdirAll(targetOutput, 0o700); err != nil {
		t.Fatal(err)
	}
	targetArtifact := filepath.Join(targetOutput, artifactName)
	if err := os.WriteFile(targetArtifact, []byte("preserve target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "redirect")
	if err := os.Symlink(targetParent, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := WriteReport(filepath.Join(link, "skuggsja", artifactName), analytics.Report{}); err == nil {
		t.Fatal("WriteReport accepted an intermediate symbolic-link path component")
	}
	if got, err := os.ReadFile(targetArtifact); err != nil || string(got) != "preserve target" {
		t.Fatalf("intermediate symlink target changed: content=%q error=%v", got, err)
	}
}

func TestCleanRefusesProductDirectorySymlink(t *testing.T) {
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
	if err := Clean(artifact); err == nil {
		t.Fatal("Clean accepted a symbolic-link product directory")
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("product-directory symlink was removed: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(target, artifactName)); err != nil || string(got) != "synthetic aggregate" {
		t.Fatalf("symlink target changed: content=%q error=%v", got, err)
	}
}

func TestCleanRefusesIntermediateDirectorySymlink(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	targetParent := filepath.Join(base, "target-parent")
	targetOutput := filepath.Join(targetParent, "skuggsja")
	if err := os.MkdirAll(targetOutput, 0o700); err != nil {
		t.Fatal(err)
	}
	targetArtifact := filepath.Join(targetOutput, artifactName)
	if err := os.WriteFile(targetArtifact, []byte("preserve target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "redirect")
	if err := os.Symlink(targetParent, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := Clean(filepath.Join(link, "skuggsja", artifactName)); err == nil {
		t.Fatal("Clean accepted an intermediate symbolic-link path component")
	}
	if got, err := os.ReadFile(targetArtifact); err != nil || string(got) != "preserve target" {
		t.Fatalf("intermediate symlink target changed: content=%q error=%v", got, err)
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
