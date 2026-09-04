// Package audit proves that discovered source files and their directories are
// unchanged across a run.
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// FileState is an in-memory digest of one source file.
type FileState struct {
	SHA256 string
	Size   int64
}

// Snapshot contains content hashes and directory membership. Absolute paths
// are intentionally kept internal and must never be serialized.
type Snapshot struct {
	Files       map[string]FileState
	Directories map[string][]string
	Manifest    string
	TotalBytes  int64
}

// Comparison summarizes a before/after pair without revealing paths.
type Comparison struct {
	Verified         bool   `json:"verified"`
	Files            int    `json:"files"`
	ManifestBefore   string `json:"manifest_before"`
	ManifestAfter    string `json:"manifest_after"`
	ChangedFiles     int    `json:"changed_files"`
	DirectoryChanges int    `json:"directory_changes"`
}

// Capture hashes every existing discovered file, existing SQLite sidecars, and
// the directory listing containing each source.
func Capture(ctx context.Context, paths []string) (Snapshot, error) {
	files := existingFilesWithSidecars(paths)
	snapshot := Snapshot{Files: make(map[string]FileState), Directories: make(map[string][]string)}

	for _, dir := range directoriesToInventory(paths, files) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return Snapshot{}, fmt.Errorf("list source directory: %w", err)
		}
		listing := make([]string, 0, len(entries))
		for _, entry := range entries {
			kind := "f"
			if entry.IsDir() {
				kind = "d"
			} else if entry.Type()&os.ModeSymlink != 0 {
				kind = "l"
			}
			listing = append(listing, kind+":"+entry.Name())
		}
		sort.Strings(listing)
		snapshot.Directories[dir] = listing
	}

	type result struct {
		path  string
		state FileState
		err   error
	}
	jobs := make(chan string)
	results := make(chan result)
	workers := min(6, len(files))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				digest, size, err := DigestFile(ctx, path)
				results <- result{path: path, state: FileState{SHA256: digest, Size: size}, err: err}
			}
		}()
	}
	go func() {
		defer close(results)
		for _, path := range files {
			jobs <- path
		}
		close(jobs)
		wg.Wait()
	}()
	var firstHashError error
	for item := range results {
		if item.err != nil {
			if firstHashError == nil {
				firstHashError = item.err
			}
			continue
		}
		snapshot.Files[item.path] = item.state
		snapshot.TotalBytes += item.state.Size
	}
	if firstHashError != nil {
		return Snapshot{}, fmt.Errorf("hash source file: %w", firstHashError)
	}
	snapshot.Manifest = manifest(snapshot)
	return snapshot, nil
}

func directoriesToInventory(paths, files []string) []string {
	seen := make(map[string]struct{})
	for _, path := range files {
		seen[filepath.Dir(path)] = struct{}{}
	}
	for _, path := range paths {
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		switch {
		case err == nil && info.IsDir():
			seen[path] = struct{}{}
		case err == nil && info.Mode().IsRegular():
			seen[filepath.Dir(path)] = struct{}{}
		case errors.Is(err, os.ErrNotExist):
			// Missing optional roots remain absent from both snapshots.
		}
	}
	directories := make([]string, 0, len(seen))
	for path := range seen {
		directories = append(directories, path)
	}
	sort.Strings(directories)
	return directories
}

// Compare returns aggregate proof suitable for the generated artifact.
func Compare(before, after Snapshot) Comparison {
	comparison := Comparison{
		Files:          len(before.Files),
		ManifestBefore: before.Manifest,
		ManifestAfter:  after.Manifest,
	}
	allFiles := make(map[string]struct{}, len(before.Files)+len(after.Files))
	for path := range before.Files {
		allFiles[path] = struct{}{}
	}
	for path := range after.Files {
		allFiles[path] = struct{}{}
	}
	for path := range allFiles {
		if before.Files[path] != after.Files[path] {
			comparison.ChangedFiles++
		}
	}
	allDirs := make(map[string]struct{}, len(before.Directories)+len(after.Directories))
	for path := range before.Directories {
		allDirs[path] = struct{}{}
	}
	for path := range after.Directories {
		allDirs[path] = struct{}{}
	}
	for path := range allDirs {
		if strings.Join(before.Directories[path], "\x00") != strings.Join(after.Directories[path], "\x00") {
			comparison.DirectoryChanges++
		}
	}
	comparison.Verified = comparison.Files > 0 && comparison.ChangedFiles == 0 && comparison.DirectoryChanges == 0 &&
		comparison.ManifestBefore != "" && comparison.ManifestBefore == comparison.ManifestAfter
	return comparison
}

// DigestFile calculates a source file's SHA-256 using an O_RDONLY handle.
func DigestFile(ctx context.Context, path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1024*1024)
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			return "", size, err
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			size += int64(n)
			if _, err := h.Write(buf[:n]); err != nil {
				return "", size, err
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return "", size, readErr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

func existingFilesWithSidecars(paths []string) []string {
	seen := make(map[string]struct{})
	for _, path := range paths {
		if path == "" {
			continue
		}
		candidates := []string{path}
		if isSQLite(path) {
			candidates = append(candidates, path+"-wal", path+"-shm", path+"-journal")
		}
		for _, candidate := range candidates {
			info, err := os.Stat(candidate)
			if err == nil && info.Mode().IsRegular() {
				seen[candidate] = struct{}{}
			}
		}
	}
	files := make([]string, 0, len(seen))
	for path := range seen {
		files = append(files, path)
	}
	sort.Strings(files)
	return files
}

func isSQLite(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".db") || strings.HasSuffix(lower, ".sqlite") || strings.HasSuffix(lower, ".vscdb")
}

func manifest(snapshot Snapshot) string {
	h := sha256.New()
	filePaths := make([]string, 0, len(snapshot.Files))
	for path := range snapshot.Files {
		filePaths = append(filePaths, path)
	}
	sort.Strings(filePaths)
	for _, path := range filePaths {
		state := snapshot.Files[path]
		fmt.Fprintf(h, "file\x00%s\x00%s\x00%d\n", path, state.SHA256, state.Size)
	}
	dirs := make([]string, 0, len(snapshot.Directories))
	for path := range snapshot.Directories {
		dirs = append(dirs, path)
	}
	sort.Strings(dirs)
	for _, path := range dirs {
		fmt.Fprintf(h, "dir\x00%s\x00%s\n", path, strings.Join(snapshot.Directories[path], "\x00"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
