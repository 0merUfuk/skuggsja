package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/0merUfuk/skuggsja/internal/analytics"
)

const artifactName = "rewind.json"

var errOutputOverlapsSource = errors.New("generated artifact location overlaps a configured source location")

// DefaultOutputPath returns the one generated-artifact location for this OS.
func DefaultOutputPath() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}
	return filepath.Join(cache, "skuggsja", artifactName), nil
}

// EnsureOutputSeparate rejects lexical, symlink-resolved, and hard-link
// overlap between the fixed generated artifact and source inputs. Roots are
// treated as directories; files require exact identity.
func EnsureOutputSeparate(output string, roots, files []string) error {
	resolvedOutput, err := canonicalPath(output)
	if err != nil {
		return fmt.Errorf("resolve generated artifact location: %w", err)
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		resolvedRoot, err := canonicalPath(root)
		if err != nil {
			return fmt.Errorf("resolve configured source location: %w", err)
		}
		if containsPath(resolvedRoot, resolvedOutput) {
			return errOutputOverlapsSource
		}
		if sameFileIdentity(root, output) {
			return errOutputOverlapsSource
		}
	}
	for _, source := range files {
		if source == "" {
			continue
		}
		resolvedSource, err := canonicalPath(source)
		if err != nil {
			return fmt.Errorf("resolve discovered source location: %w", err)
		}
		if samePath(resolvedSource, resolvedOutput) || sameFileIdentity(source, output) {
			return errOutputOverlapsSource
		}
	}
	return nil
}

func canonicalPath(input string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(input))
	if err != nil {
		return "", err
	}
	current := absolute
	var suffix []string
	for {
		resolved, evalErr := filepath.EvalSymlinks(current)
		if evalErr == nil {
			parts := append([]string{resolved}, suffix...)
			return filepath.Clean(filepath.Join(parts...)), nil
		}
		if !errors.Is(evalErr, os.ErrNotExist) {
			return "", evalErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return absolute, nil
		}
		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent
	}
}

func containsPath(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func samePath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func sameFileIdentity(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

// WriteReport atomically writes only the aggregate report with private permissions.
func WriteReport(path string, report analytics.Report) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure output directory: %w", err)
	}
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode aggregate report: %w", err)
	}
	payload = append(payload, '\n')
	temporary, err := os.CreateTemp(dir, ".rewind-*.json")
	if err != nil {
		return fmt.Errorf("create temporary report: %w", err)
	}
	temporaryPath := temporary.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure temporary report: %w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write aggregate report: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync aggregate report: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close aggregate report: %w", err)
	}
	if runtime.GOOS == "windows" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("replace prior aggregate report: %w", err)
		}
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install aggregate report: %w", err)
	}
	keep = true
	return nil
}

// Clean removes only Skuggsja's regenerable artifact and then its empty directory.
func Clean(path string) error {
	if filepath.Base(path) != artifactName {
		return errors.New("refusing to clean an unexpected artifact path")
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove aggregate report: %w", err)
	}
	if err := os.Remove(filepath.Dir(path)); err != nil && !errors.Is(err, os.ErrNotExist) {
		// A non-empty product directory is left intact deliberately.
		return nil
	}
	return nil
}
