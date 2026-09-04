package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/provider"
)

func TestGenerateRunsAuditedContentFreePipeline(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := filepath.Join(dir, "synthetic-history.jsonl")
	secret := "synthetic prompt that must never survive"
	if err := os.WriteFile(source, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), artifactName)
	at := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	reader := fixtureReader{
		discovery: provider.Discovery{Harness: model.Claude, Files: []string{source}},
		result: model.ProviderResult{
			Harness: model.Claude, DisplayName: "Fixture", Status: "supported",
			VerificationLevel: "synthetic fixture", ToolCallsAvailable: true, SourceFiles: []string{source},
			Sessions: []model.Session{{
				Harness: model.Claude, ID: "private-source-id", StartedAt: at, EndedAt: at,
				ActivityAt: at, ActivityBasis: "session start", Project: "fixture-project",
				Prompts: []model.PromptMetric{{EventID: "private-event-id", Words: 7, Characters: len(secret), HasText: true}},
			}},
		},
	}

	generation, err := Generate(context.Background(), GenerateOptions{
		Readers: []provider.Reader{reader}, Location: time.UTC,
		Now: func() time.Time { return at.Add(time.Hour) }, OutputPath: output, AuditSources: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !generation.Report.Privacy.SourceAudit.Verified {
		t.Fatalf("source audit = %#v", generation.Report.Privacy.SourceAudit)
	}
	if generation.Report.Privacy.SourceAudit.ChangedFiles != 0 || generation.Report.Privacy.SourceAudit.DirectoryChanges != 0 {
		t.Fatalf("source changes = %#v", generation.Report.Privacy.SourceAudit)
	}
	payload, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{secret, source, "private-source-id", "private-event-id"} {
		if strings.Contains(string(payload), forbidden) {
			t.Errorf("generated artifact contains private value %q", forbidden)
		}
	}
	if got, err := os.ReadFile(source); err != nil || string(got) != secret+"\n" {
		t.Fatalf("source changed: content=%q error=%v", got, err)
	}
}

func TestGenerateDegradesOneProviderWithoutStoppingOthers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	good := fixtureReader{result: model.ProviderResult{
		Harness: model.Claude, DisplayName: "Claude Code", Status: "not found",
	}}
	bad := fixtureReader{harness: model.Cursor, displayName: "Cursor", discoverErr: errors.New("fixture failure")}

	generation, err := Generate(context.Background(), GenerateOptions{
		Readers: []provider.Reader{bad, good}, OutputPath: filepath.Join(dir, artifactName), AuditSources: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(generation.Report.Providers) != 2 {
		t.Fatalf("providers = %#v", generation.Report.Providers)
	}
	if len(generation.Report.Warnings) != 1 || generation.Report.Warnings[0].Code != "discovery_failed" {
		t.Fatalf("warnings = %#v", generation.Report.Warnings)
	}
}

func TestGenerateRejectsArtifactInsideSourceRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "session.jsonl")
	if err := os.WriteFile(source, []byte("synthetic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, artifactName)
	reader := fixtureReader{discovery: provider.Discovery{
		Harness: model.Claude, Roots: []string{root}, Files: []string{source},
	}}
	if _, err := Generate(context.Background(), GenerateOptions{
		Readers: []provider.Reader{reader}, OutputPath: output, AuditSources: true,
	}); !errors.Is(err, errOutputOverlapsSource) {
		t.Fatalf("Generate() error = %v, want output/source overlap", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("overlapping artifact was created: %v", err)
	}
	if got, err := os.ReadFile(source); err != nil || string(got) != "synthetic\n" {
		t.Fatalf("source changed: content=%q error=%v", got, err)
	}
}

func TestEnsureOutputSeparateRejectsHardLinkAlias(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	output := filepath.Join(dir, artifactName)
	if err := os.WriteFile(source, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source, output); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if err := EnsureOutputSeparate(output, nil, []string{source}); !errors.Is(err, errOutputOverlapsSource) {
		t.Fatalf("EnsureOutputSeparate() error = %v", err)
	}
}

func TestEnsureOutputSeparateRejectsSourceSymlinkIntoOutputDirectory(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	sourceDirectory := filepath.Join(base, "histories")
	outputDirectory := filepath.Join(base, "generated")
	if err := os.MkdirAll(sourceDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outputDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outputDirectory, "source.jsonl")
	if err := os.WriteFile(target, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sourceDirectory, "session.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := EnsureOutputSeparate(filepath.Join(outputDirectory, artifactName), nil, []string{link}); !errors.Is(err, errOutputOverlapsSource) {
		t.Fatalf("EnsureOutputSeparate() error = %v, want symlink-target directory overlap", err)
	}
}

func TestEnsureOutputSeparateRejectsSQLiteSidecarAlias(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	sourceDirectory := filepath.Join(base, "histories")
	outputDirectory := filepath.Join(base, "generated")
	if err := os.MkdirAll(sourceDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outputDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(outputDirectory, artifactName)
	if err := os.WriteFile(output, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(sourceDirectory, "state.db-wal")
	if err := os.Symlink(output, sidecar); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := EnsureOutputSeparate(output, nil, []string{sidecar}); !errors.Is(err, errOutputOverlapsSource) {
		t.Fatalf("EnsureOutputSeparate() error = %v, want sidecar alias overlap", err)
	}
}

func TestGenerateRejectsDanglingConfiguredSidecarAtArtifact(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	sourceDirectory := filepath.Join(base, "histories")
	outputDirectory := filepath.Join(base, "generated")
	if err := os.MkdirAll(sourceDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outputDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(outputDirectory, artifactName)
	database := filepath.Join(sourceDirectory, "state.db")
	if err := os.Symlink(output, database+"-wal"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	reader := fixtureReader{discovery: provider.Discovery{
		Harness: model.Hermes,
		ConfiguredFiles: []string{
			database, database + "-wal", database + "-shm", database + "-journal",
		},
	}}
	if _, err := Generate(context.Background(), GenerateOptions{
		Readers: []provider.Reader{reader}, OutputPath: output, AuditSources: true,
	}); !errors.Is(err, errOutputOverlapsSource) {
		t.Fatalf("Generate() error = %v, want dangling sidecar refusal", err)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("overlapping artifact was created: %v", err)
	}
}

func TestEnsureOutputSeparateRejectsDanglingIntermediateSymlink(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	outputDirectory := filepath.Join(base, "generated")
	link := filepath.Join(base, "histories-link")
	if err := os.Symlink(outputDirectory, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	source := filepath.Join(link, artifactName)
	output := filepath.Join(outputDirectory, artifactName)
	if err := EnsureOutputSeparate(output, nil, []string{source}); err == nil {
		t.Fatal("EnsureOutputSeparate accepted a dangling intermediate symlink")
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact was unexpectedly created: %v", err)
	}
}

func TestEnsureOutputSeparateRejectsArtifactBesideSourceFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "state.db")
	if err := os.WriteFile(source, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureOutputSeparate(filepath.Join(dir, artifactName), nil, []string{source}); !errors.Is(err, errOutputOverlapsSource) {
		t.Fatalf("EnsureOutputSeparate() error = %v, want output/source overlap", err)
	}
}

func TestEnsureOutputSeparateRejectsSourceRootInsideOutputDirectory(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	outputDirectory := filepath.Join(base, "generated")
	sourceRoot := filepath.Join(outputDirectory, "histories")
	if err := os.MkdirAll(sourceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := EnsureOutputSeparate(filepath.Join(outputDirectory, artifactName), []string{sourceRoot}, nil); !errors.Is(err, errOutputOverlapsSource) {
		t.Fatalf("EnsureOutputSeparate() error = %v, want reverse output/source overlap", err)
	}
}

func TestEnsureOutputSeparateAllowsSiblingDirectories(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	sourceRoot := filepath.Join(base, "histories")
	outputDirectory := filepath.Join(base, "generated")
	if err := os.MkdirAll(sourceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := EnsureOutputSeparate(filepath.Join(outputDirectory, artifactName), []string{sourceRoot}, nil); err != nil {
		t.Fatalf("EnsureOutputSeparate() error = %v", err)
	}
}

func TestEnsureOutputSeparateRejectsDarwinCaseAlias(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" {
		t.Skip("macOS path comparison")
	}
	root := t.TempDir()
	caseAlias := strings.ToUpper(root)
	if caseAlias == root {
		t.Skip("temporary path has no letters")
	}
	if err := EnsureOutputSeparate(filepath.Join(caseAlias, artifactName), []string{root}, nil); !errors.Is(err, errOutputOverlapsSource) {
		t.Fatalf("EnsureOutputSeparate() error = %v, want case-equivalent overlap", err)
	}
}

func TestGenerateChecksConfiguredFilesFromFailedDiscovery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	output := filepath.Join(dir, artifactName)
	if err := os.WriteFile(output, []byte("synthetic source"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := fixtureReader{
		discovery:   provider.Discovery{Harness: model.Cursor, ConfiguredFiles: []string{output}},
		discoverErr: errors.New("synthetic discovery failure"),
	}
	if _, err := Generate(context.Background(), GenerateOptions{
		Readers: []provider.Reader{reader}, OutputPath: output, AuditSources: true,
	}); !errors.Is(err, errOutputOverlapsSource) {
		t.Fatalf("Generate() error = %v, want output/source overlap", err)
	}
	content, err := os.ReadFile(output)
	if err != nil || string(content) != "synthetic source" {
		t.Fatalf("configured source changed: content=%q error=%v", content, err)
	}
}

type fixtureReader struct {
	harness     model.Harness
	displayName string
	discovery   provider.Discovery
	discoverErr error
	result      model.ProviderResult
}

func (r fixtureReader) Harness() model.Harness {
	if r.harness != "" {
		return r.harness
	}
	return model.Claude
}

func (r fixtureReader) DisplayName() string {
	if r.displayName != "" {
		return r.displayName
	}
	return "Fixture"
}

func (r fixtureReader) Discover(context.Context) (provider.Discovery, error) {
	return r.discovery, r.discoverErr
}

func (r fixtureReader) Read(context.Context, provider.Discovery) model.ProviderResult {
	return r.result
}
