package claude

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/0merUfuk/skuggsja/internal/provider"
)

// discoverProjectSessionIndexes only accepts the canonical project-level index.
// Its stored fullPath values are never discovery inputs.
func discoverProjectSessionIndexes(root string) ([]string, error) {
	if root == "" {
		return nil, nil
	}
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("Claude project index root is a symbolic link")
	}
	if !info.IsDir() {
		return nil, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, errors.New("Claude project index directory is a symbolic link")
		}
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "sessions-index.json")
		indexInfo, statErr := os.Lstat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return nil, statErr
		}
		if indexInfo.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("Claude project session index is a symbolic link")
		}
		if !indexInfo.Mode().IsRegular() {
			return nil, fmt.Errorf("Claude project session index is not a regular file")
		}
		paths = append(paths, path)
	}
	return paths, nil // os.ReadDir returns entries sorted by name.
}

// readProjectSessionIndexes returns coverage evidence, never usage or sessions.
// Only version 1's IDs and stored dates are retained; fullPath, firstPrompt,
// summary, messageCount, and other fields have no representation here.
// malformed counts files containing any unusable schema, entry, or date.
func readProjectSessionIndexes(paths []string) (map[string]struct{}, time.Time, int, int) {
	ids := make(map[string]struct{})
	var earliest time.Time
	malformed, oversize := 0, 0
	for _, path := range paths {
		var index struct {
			Version int `json:"version"`
			Entries *[]struct {
				SessionID string `json:"sessionId"`
				Created   string `json:"created"`
				Modified  string `json:"modified"`
			} `json:"entries"`
		}
		tooLong, err := provider.DecodeJSONFile(path, maxRecordBytes, &index)
		if tooLong {
			oversize++
			continue
		}
		if err != nil || index.Version != 1 || index.Entries == nil {
			malformed++
			continue
		}
		badEntry := false
		for _, entry := range *index.Entries {
			if strings.TrimSpace(entry.SessionID) == "" {
				badEntry = true
				continue
			}
			ids[entry.SessionID] = struct{}{}
			for _, raw := range []string{entry.Created, entry.Modified} {
				if raw == "" {
					continue
				}
				at, parseErr := time.Parse(time.RFC3339Nano, raw)
				if parseErr != nil {
					badEntry = true
					continue
				}
				if earliest.IsZero() || at.Before(earliest) {
					earliest = at.UTC()
				}
			}
		}
		if badEntry {
			malformed++
		}
	}
	return ids, earliest, malformed, oversize
}
