package platform

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// claudeExtraHomes only inspects the user's immediate .claude-* directories and
// recognized Claude storage markers. It never searches unrelated home contents.
func claudeExtraHomes(home, configured string) ([]string, error) {
	canonical := filepath.Join(home, ".claude")
	seen := make(map[string]struct{})
	if filepath.Clean(configured) != canonical {
		// A custom active home must not hide the surviving canonical history. The
		// active root is repeated here to include its root-local .claude.json, while
		// the primary global-state path continues to cover ~/.claude.json.
		seen[canonical] = struct{}{}
		seen[filepath.Clean(configured)] = struct{}{}
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasPrefix(entry.Name(), ".claude-") {
			continue
		}
		root := filepath.Join(home, entry.Name())
		recognized, err := hasClaudeStorage(root)
		if err != nil {
			return nil, err
		}
		if recognized {
			seen[root] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for root := range seen {
		if root != canonical || filepath.Clean(configured) != canonical {
			out = append(out, root)
		}
	}
	sort.Strings(out)
	return out, nil
}

func hasClaudeStorage(root string) (bool, error) {
	for _, name := range []string{"history.jsonl", "stats-cache.json", ".claude.json"} {
		info, err := os.Lstat(filepath.Join(root, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if info.Mode().IsRegular() {
			return true, nil
		}
	}
	projects := filepath.Join(root, "projects")
	info, err := os.Lstat(projects)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}
	found := false
	err = filepath.WalkDir(projects, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		relative, err := filepath.Rel(projects, path)
		if err != nil {
			return err
		}
		parts := strings.Split(relative, string(filepath.Separator))
		direct := len(parts) == 2 && (filepath.Ext(entry.Name()) == ".jsonl" || entry.Name() == "sessions-index.json")
		child := len(parts) >= 4 && parts[2] == "subagents" && strings.HasPrefix(entry.Name(), "agent-") && filepath.Ext(entry.Name()) == ".jsonl"
		if direct || child {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found, err
}
