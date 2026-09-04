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
	Exists bool
}

// Snapshot contains content hashes and directory membership. Absolute paths
// are intentionally kept internal and must never be serialized.
type Snapshot struct {
	Files       map[string]FileState
	Directories map[string][]string
	Manifest    string
	TotalBytes  int64
	Complete    bool
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

// Capture classifies paths for callers that do not already distinguish source
// roots from files.
func Capture(ctx context.Context, paths []string) (Snapshot, error) {
	var roots, files []string
	for _, path := range paths {
		if path == "" {
			continue
		}
		info, err := os.Lstat(path)
		if err == nil && info.IsDir() {
			roots = append(roots, path)
			continue
		}
		files = append(files, path)
	}
	return CaptureDiscovered(ctx, roots, files)
}

// CaptureDiscovered hashes every discovered source file and present SQLite
// sidecar, recursively inventories source roots, and inventories each source
// file's containing directory. Missing discovered files make the snapshot
// incomplete instead of silently shrinking the audited set.
func CaptureDiscovered(ctx context.Context, roots, discoveredFiles []string) (Snapshot, error) {
	snapshot := Snapshot{
		Files: make(map[string]FileState), Directories: make(map[string][]string), Complete: true,
	}
	for _, root := range uniqueSorted(roots) {
		if err := inventoryTree(ctx, root, &snapshot); err != nil {
			return Snapshot{}, fmt.Errorf("inventory source root: %w", err)
		}
	}

	files, missing, err := filesWithSidecars(discoveredFiles)
	if err != nil {
		return Snapshot{}, err
	}
	for _, path := range missing {
		snapshot.Files[path] = FileState{}
		snapshot.Complete = false
	}
	for _, dir := range containingDirectories(append(append([]string(nil), files...), missing...)) {
		if err := inventoryDirectory(dir, &snapshot); err != nil {
			return Snapshot{}, fmt.Errorf("list source directory: %w", err)
		}
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
				results <- result{path: path, state: FileState{SHA256: digest, Size: size, Exists: true}, err: err}
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

func inventoryTree(ctx context.Context, root string, snapshot *Snapshot) error {
	if root == "" {
		return nil
	}
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("source root is not a directory")
	}
	var walk func(string) error
	walk = func(dir string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := inventoryDirectoryEntries(dir, snapshot)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
				if err := walk(filepath.Join(dir, entry.Name())); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(root)
}

func inventoryDirectory(dir string, snapshot *Snapshot) error {
	_, err := inventoryDirectoryEntries(dir, snapshot)
	if errors.Is(err, os.ErrNotExist) {
		snapshot.Complete = false
		return nil
	}
	return err
}

func inventoryDirectoryEntries(dir string, snapshot *Snapshot) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
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
	return entries, nil
}

// Compare returns aggregate proof suitable for the generated artifact.
func Compare(before, after Snapshot) Comparison {
	comparison := Comparison{
		Files:          existingFileCount(before.Files),
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
	comparison.Verified = before.Complete && after.Complete && comparison.Files > 0 && comparison.ChangedFiles == 0 && comparison.DirectoryChanges == 0 &&
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

func filesWithSidecars(paths []string) ([]string, []string, error) {
	seen := make(map[string]struct{})
	missing := make(map[string]struct{})
	for _, path := range paths {
		if path == "" {
			continue
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			missing[path] = struct{}{}
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("inspect discovered source file: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("discovered source is not a regular file")
		}
		seen[path] = struct{}{}
		if isSQLite(path) {
			for _, candidate := range []string{path + "-wal", path + "-shm", path + "-journal"} {
				info, err := os.Lstat(candidate)
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil {
					return nil, nil, fmt.Errorf("inspect SQLite sidecar: %w", err)
				}
				if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
					return nil, nil, fmt.Errorf("SQLite sidecar is not a regular file")
				}
				seen[candidate] = struct{}{}
			}
		}
	}
	return sortedKeys(seen), sortedKeys(missing), nil
}

func containingDirectories(paths []string) []string {
	seen := make(map[string]struct{})
	for _, path := range paths {
		seen[filepath.Dir(path)] = struct{}{}
	}
	return sortedKeys(seen)
}

func uniqueSorted(paths []string) []string {
	seen := make(map[string]struct{})
	for _, path := range paths {
		if path != "" {
			seen[path] = struct{}{}
		}
	}
	return sortedKeys(seen)
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}

func existingFileCount(files map[string]FileState) int {
	count := 0
	for _, state := range files {
		if state.Exists {
			count++
		}
	}
	return count
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
		fmt.Fprintf(h, "file\x00%s\x00%t\x00%s\x00%d\n", path, state.Exists, state.SHA256, state.Size)
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
