package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/0merUfuk/skuggsja/internal/provider/hermes"
)

func TestGenerateRejectsWorkspaceInsideStandaloneSQLiteStoreBeforeCreation(t *testing.T) {
	for _, audited := range []bool{false, true} {
		store := t.TempDir()
		source := filepath.Join(store, "custom-history-name")
		if err := os.WriteFile(source, []byte("source sentinel"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Generate(context.Background(), GenerateOptions{
			Readers:    []provider.Reader{hermes.Reader{DatabasePath: source}},
			OutputPath: filepath.Join(t.TempDir(), artifactName), TempParent: store, AuditSources: audited,
		})
		if !errors.Is(err, errOutputOverlapsSource) {
			t.Fatalf("SQLite-store workspace accepted: %v", err)
		}
		entries, err := os.ReadDir(store)
		if err != nil || len(entries) != 1 || entries[0].Name() != "custom-history-name" {
			t.Fatalf("SQLite source directory changed: %v", err)
		}
		data, err := os.ReadFile(source)
		if err != nil || string(data) != "source sentinel" {
			t.Fatalf("SQLite source bytes changed: %v", err)
		}
	}
}

func TestPrivateWorkspaceNormalizesRelativeParentAndCleansUp(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("private-temp", 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, err := newPrivateWorkspace("private-temp", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(workspace.directory) {
		t.Fatal("private copy context would receive a relative workspace")
	}
	if err := workspace.close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir("private-temp")
	if err != nil || len(entries) != 0 {
		t.Fatalf("workspace remained after cleanup: %v", err)
	}
}

func TestGenerateRejectsPrivateWorkspaceWithinAnySourceBeforeCreation(t *testing.T) {
	for _, audited := range []bool{false, true} {
		t.Run(map[bool]string{false: "observation disabled", true: "observation enabled"}[audited], func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "history.jsonl")
			if err := os.WriteFile(source, []byte("preserve source"), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Generate(context.Background(), GenerateOptions{
				Readers:    []provider.Reader{fixtureReader{discovery: provider.Discovery{Roots: []string{root}, Files: []string{source}}}},
				OutputPath: filepath.Join(t.TempDir(), artifactName), TempParent: root, AuditSources: audited,
			})
			if !errors.Is(err, errOutputOverlapsSource) {
				t.Fatalf("workspace inside source accepted: %v", err)
			}
			after, err := os.ReadDir(root)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("source directory changed: %v", err)
			}
			data, err := os.ReadFile(source)
			if err != nil || string(data) != "preserve source" {
				t.Fatalf("source bytes changed: %v", err)
			}
		})
	}
}

func TestConcurrentSourceActivityIsNeutralAndIndependentOfReadOnlyAccess(t *testing.T) {
	t.Parallel()
	sourceDir, tempParent := t.TempDir(), t.TempDir()
	source := filepath.Join(sourceDir, "history.jsonl")
	if err := os.WriteFile(source, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	start, changed := make(chan struct{}), make(chan error, 1)
	// This writer is outside the provider: it models the owning harness changing
	// a source while the production generation pipeline only reads that source.
	go func() { <-start; changed <- os.WriteFile(source, []byte("after external update"), 0o600) }()
	reader := &concurrentFixtureReader{source: source, start: start, changed: changed}
	generation, err := Generate(context.Background(), GenerateOptions{
		Readers: []provider.Reader{reader}, TempParent: tempParent,
		OutputPath: filepath.Join(t.TempDir(), artifactName), AuditSources: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reader.err != nil {
		t.Fatal(reader.err)
	}
	p := generation.Report.Privacy
	if p.SourceAccess != "read-only" || p.SourceObservation != "observed" || p.SourceAudit.ChangedFiles != 1 || p.SourceAudit.Verified {
		t.Fatalf("independent access/activity evidence = %+v", p)
	}
	if len(generation.Report.Providers) != 1 || len(generation.Report.Warnings) != 0 || generation.AuditError != nil {
		t.Fatalf("concurrency became provider failure or warning: %+v", generation.Report.Warnings)
	}
	entries, err := os.ReadDir(tempParent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("private workspace was not removed: %v", err)
	}
	serialized, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(serialized, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["source_access"] != "read-only" || fields["source_observation"] != "observed" {
		t.Fatalf("JSON access/activity fields = %s", serialized)
	}
}

func TestUnavailableSourceObservationDoesNotAddIntegrityWarning(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "disappeared.jsonl")
	for _, audited := range []bool{false, true} {
		generation, err := Generate(context.Background(), GenerateOptions{
			Readers: []provider.Reader{fixtureReader{
				discovery: provider.Discovery{Files: []string{missing}},
				result:    model.ProviderResult{Harness: model.Claude, Status: "supported"},
			}}, OutputPath: filepath.Join(t.TempDir(), artifactName), AuditSources: audited,
		})
		if err != nil {
			t.Fatal(err)
		}
		expected := "disabled"
		if audited {
			expected = "unavailable"
		}
		if generation.Report.Privacy.SourceAccess != "read-only" || generation.Report.Privacy.SourceObservation != expected || len(generation.Report.Warnings) != 0 || len(generation.Report.Providers) != 1 {
			t.Fatalf("observation status affected source guarantee/providers: %+v", generation.Report.Privacy)
		}
	}
}

type concurrentFixtureReader struct {
	source  string
	start   chan struct{}
	changed chan error
	err     error
}

func (r *concurrentFixtureReader) Harness() model.Harness { return model.Claude }
func (r *concurrentFixtureReader) DisplayName() string    { return "Concurrent fixture" }
func (r *concurrentFixtureReader) Discover(context.Context) (provider.Discovery, error) {
	return provider.Discovery{Harness: model.Claude, Roots: []string{filepath.Dir(r.source)}, Files: []string{r.source}}, nil
}
func (r *concurrentFixtureReader) Read(context.Context, provider.Discovery) model.ProviderResult {
	close(r.start)
	r.err = <-r.changed
	return model.ProviderResult{Harness: model.Claude, Status: "supported", SourceFiles: []string{r.source}}
}
