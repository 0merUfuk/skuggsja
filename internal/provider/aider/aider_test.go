package aider

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func history(t *testing.T, dir, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, historyName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const sample = "\n# aider chat started at 2026-10-20 10:00:00\n\n" +
	"> Aider v0.90\n> Model: some-model\n\n" +
	"#### Add a health check endpoint.  \n\n" +
	"I added it.\n\n#### Heading in the assistant reply\n\n" +
	"#### Now write a test  \n#### covering the error path  \n\n" +
	"Done.\n\n" +
	"\n# aider chat started at 2026-10-21 09:30:00\n\n" +
	"#### <blank>  \n"

func TestSessionsAndMultilinePromptsAreParsedFromTheMarkdown(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	history(t, filepath.Join(root, "my-repo"), sample)
	r := Reader{Roots: []string{root}}
	d, err := r.Discover(context.Background())
	if err != nil || len(d.Files) != 1 || len(d.Roots) != 0 {
		t.Fatalf("discovery wrong (roots must not be audited): %#v err=%v", d, err)
	}
	out := r.Read(context.Background(), d)
	if out.Status != "supported" || len(out.Sessions) != 2 {
		t.Fatalf("status=%q sessions=%d warnings=%v", out.Status, len(out.Sessions), out.Warnings)
	}
	first, second := out.Sessions[0], out.Sessions[1]
	if first.Project != "my-repo" || len(first.Prompts) != 2 {
		t.Fatalf("assistant heading counted as a prompt or project lost: %#v", first)
	}
	if first.Prompts[0].Words != 5 || first.Prompts[1].Words != 8 {
		t.Fatalf("multi-line prompt not joined: %#v", first.Prompts)
	}
	want := time.Date(2026, 10, 20, 10, 0, 0, 0, time.Local).UTC()
	if !first.StartedAt.Equal(want) {
		t.Fatalf("session start = %v, want %v", first.StartedAt, want)
	}
	if len(second.Prompts) != 1 || second.Prompts[0].HasText {
		t.Fatalf("<blank> input must not count as text: %#v", second.Prompts)
	}
}

func TestDiscoveryChecksRootAndDirectChildrenOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	history(t, root, sample)
	history(t, filepath.Join(root, "a"), sample)
	history(t, filepath.Join(root, "a", "deep"), sample)
	explicit := history(t, t.TempDir(), sample)
	d, err := (Reader{Roots: []string{root, explicit, filepath.Join(root, "absent"), ""}}).Discover(context.Background())
	if err != nil || len(d.Files) != 3 {
		t.Fatalf("expected root, direct child and explicit file only: %v err=%v", d.Files, err)
	}
	empty, _ := (Reader{}).Discover(context.Background())
	if len(empty.Files) != 0 {
		t.Fatalf("no roots must discover nothing: %#v", empty)
	}
}

func TestUnparsableStartTimeIsCountedWithoutATimestamp(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	history(t, filepath.Join(root, "r"), "# aider chat started at yesterday\n\n#### hello there  \n")
	r := Reader{Roots: []string{root}}
	d, _ := r.Discover(context.Background())
	out := r.Read(context.Background(), d)
	if len(out.Sessions) != 1 || !out.Sessions[0].TimeUnavailable || !out.Sessions[0].StartedAt.IsZero() {
		t.Fatalf("time was invented: %#v", out.Sessions)
	}
	if out.Status != "supported with warnings" {
		t.Fatalf("status = %q", out.Status)
	}
}
