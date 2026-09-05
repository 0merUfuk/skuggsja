package platform

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestClaudeExtraHomesUsesBoundedNativeHistoryShapes(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	active := filepath.Join(t.TempDir(), "active")
	fixtures := map[string]string{
		".claude-account/history.jsonl": "{}",
		".claude-org/.claude.json":      "{}",
		".claude-nested/projects/project/parent/subagents/workflows/task/agent-child.jsonl": "{}",
		".claude-mem/config.sqlite":              "not a native transcript",
		".claude-router/settings.json":           "{}",
		"unrelated/.claude-hidden/history.jsonl": "{}",
	}
	for rel, data := range fixtures {
		path := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(home, ".claude-account"), filepath.Join(home, ".claude-linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got, err := claudeExtraHomes(home, active)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{active, filepath.Join(home, ".claude"), filepath.Join(home, ".claude-account"), filepath.Join(home, ".claude-org"), filepath.Join(home, ".claude-nested")}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("homes=%q, want %q", got, want)
	}
	got, err = claudeExtraHomes(home, filepath.Join(home, ".claude"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("canonical active extras=%q", got)
	}
}

func TestExplicitClaudeScopeAvoidsNativeHomeInspection(t *testing.T) {
	for _, variable := range []string{"SKUGGSJA_CLAUDE_EXTRA_HOMES", "SKUGGSJA_CLAUDE_PROJECTS"} {
		t.Run(variable, func(t *testing.T) {
			// A native home that cannot be listed makes unintended pre-override
			// discovery observable without touching the operator's real sources.
			home := filepath.Join(t.TempDir(), "absent-native-home")
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			for _, name := range []string{"SKUGGSJA_CLAUDE_EXTRA_HOMES", "SKUGGSJA_CLAUDE_PROJECTS"} {
				t.Setenv(name, "")
				if err := os.Unsetenv(name); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := DefaultPaths(); err == nil {
				t.Fatal("fixture did not make native discovery fail")
			}
			value := ""
			if variable == "SKUGGSJA_CLAUDE_PROJECTS" {
				value = filepath.Join(t.TempDir(), "explicit-projects")
			}
			t.Setenv(variable, value)
			paths, err := DefaultPaths()
			if err != nil {
				t.Fatalf("explicit source scope still inspected inaccessible native home: %v", err)
			}
			if len(paths.ClaudeExtraHomes) != 0 {
				t.Fatalf("auto homes inherited despite explicit scope: %q", paths.ClaudeExtraHomes)
			}
		})
	}
}
