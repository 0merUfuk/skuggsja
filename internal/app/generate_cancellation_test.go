package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/provider"
)

func TestGenerateCancellationPreservesPreviousReport(t *testing.T) {
	for _, auditSources := range []bool{false, true} {
		for _, phase := range []string{"before generation", "during discovery", "during reader", "before persistence"} {
			t.Run(fmt.Sprintf("audit=%t/%s", auditSources, phase), func(t *testing.T) {
				t.Parallel()
				output := filepath.Join(t.TempDir(), artifactName)
				const previous = "previous complete report must remain byte-for-byte intact\n"
				if err := os.WriteFile(output, []byte(previous), 0o600); err != nil {
					t.Fatal(err)
				}
				sourceDir, tempParent := t.TempDir(), t.TempDir()
				source := filepath.Join(sourceDir, "history.jsonl")
				const original = "synthetic history; no harness data\n"
				if err := os.WriteFile(source, []byte(original), 0o600); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				reader := &cancellationReader{source: source, phase: phase, cancel: cancel}
				laterReader := &cancellationReader{source: source}
				if phase == "before generation" {
					cancel()
				}
				now := func() time.Time {
					if phase == "before persistence" {
						cancel()
					}
					return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
				}
				generation, err := Generate(ctx, GenerateOptions{
					Readers: []provider.Reader{reader, laterReader}, OutputPath: output,
					TempParent: tempParent, AuditSources: auditSources, Now: now,
				})
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("Generate() error = %v, want context.Canceled", err)
				}
				if generation.OutputPath != "" || generation.Report.SchemaVersion != 0 {
					t.Fatalf("canceled generation was reported as ready: %#v", generation)
				}
				got, err := os.ReadFile(output)
				if err != nil || string(got) != previous {
					t.Fatalf("cancellation replaced the previous report: unchanged=%t error=%v", string(got) == previous, err)
				}
				entries, err := os.ReadDir(tempParent)
				if err != nil || len(entries) != 0 {
					t.Fatalf("cancellation left a private workspace: entries=%d error=%v", len(entries), err)
				}
				got, err = os.ReadFile(source)
				if err != nil || string(got) != original {
					t.Fatalf("cancellation changed source content: unchanged=%t error=%v", string(got) == original, err)
				}
				if phase == "before generation" && (reader.discoveries != 0 || reader.reads != 0) {
					t.Fatal("pre-canceled generation inspected source data")
				}
				if phase == "during discovery" && reader.reads != 0 {
					t.Fatal("generation continued parsing after discovery canceled")
				}
				if (phase == "before generation" || phase == "during discovery") && laterReader.discoveries != 0 {
					t.Fatal("generation invoked another discovery after cancellation")
				}
				if phase != "before persistence" && laterReader.reads != 0 {
					t.Fatal("generation invoked another reader after cancellation")
				}
			})
		}
	}
}

type cancellationReader struct {
	source      string
	phase       string
	cancel      context.CancelFunc
	discoveries int
	reads       int
}

func (r *cancellationReader) Harness() model.Harness { return model.Claude }
func (r *cancellationReader) DisplayName() string    { return "Synthetic cancellation reader" }
func (r *cancellationReader) Discover(context.Context) (provider.Discovery, error) {
	r.discoveries++
	if r.phase == "during discovery" {
		r.cancel()
	}
	return provider.Discovery{Harness: model.Claude, Roots: []string{filepath.Dir(r.source)}, Files: []string{r.source}}, nil
}
func (r *cancellationReader) Read(context.Context, provider.Discovery) model.ProviderResult {
	r.reads++
	if r.phase == "during reader" {
		r.cancel()
	}
	return model.ProviderResult{Harness: model.Claude, Status: "supported", SourceFiles: []string{r.source}}
}
