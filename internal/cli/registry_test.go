package cli

import (
	"context"
	"testing"

	"github.com/0merUfuk/skuggsja/internal/model"
	"github.com/0merUfuk/skuggsja/internal/platform"
	"github.com/0merUfuk/skuggsja/internal/provider"
	"github.com/0merUfuk/skuggsja/internal/provider/claude"
)

func TestReadersUsesTheRegistryInDeterministicOrder(t *testing.T) {
	t.Parallel()
	got := readers(platform.Paths{})
	want := []model.Harness{model.Claude, model.Codex, model.Hermes, model.Cursor}
	if len(got) != len(want) {
		t.Fatalf("readers() returned %d providers, want %d", len(got), len(want))
	}
	for i, reader := range got {
		if reader.Harness() != want[i] {
			t.Errorf("readers()[%d].Harness() = %q, want %q (terminal output and the UI both depend on this order staying stable)", i, reader.Harness(), want[i])
		}
	}
}

// stubReader is a minimal Reader used only to prove buildReaders works for
// any registered factory, not just today's four shipped harnesses.
type stubReader struct{ harness model.Harness }

func (s stubReader) Harness() model.Harness { return s.harness }
func (stubReader) DisplayName() string      { return "Stub Harness" }
func (stubReader) Discover(context.Context) (provider.Discovery, error) {
	return provider.Discovery{}, nil
}
func (stubReader) Read(context.Context, provider.Discovery) model.ProviderResult {
	return model.ProviderResult{}
}

func TestBuildReadersAppliesAnyRegisteredFactoryNotJustTheShippedFour(t *testing.T) {
	t.Parallel()
	const newHarness model.Harness = "example-harness"
	factories := []func(platform.Paths) provider.Reader{
		claude.New,
		func(platform.Paths) provider.Reader { return stubReader{harness: newHarness} },
	}
	got := buildReaders(platform.Paths{ClaudeProjects: "/configured/only"}, factories)
	if len(got) != 2 {
		t.Fatalf("buildReaders() returned %d readers, want 2", len(got))
	}
	if got[0].Harness() != model.Claude {
		t.Errorf("readers[0].Harness() = %q, want %q", got[0].Harness(), model.Claude)
	}
	if got[1].Harness() != newHarness {
		t.Errorf("readers[1].Harness() = %q, want %q (adding one factory must be enough to register a harness)", got[1].Harness(), newHarness)
	}
}
