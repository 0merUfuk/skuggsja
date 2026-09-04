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
// overlap between the fixed generated artifact and source inputs. Source and
// output directories must not contain one another.
func EnsureOutputSeparate(output string, roots, files []string) error {
	outputAbsolute, err := absolutePath(output)
	if err != nil {
		return fmt.Errorf("resolve generated artifact location: %w", err)
	}
	resolvedOutput, err := canonicalPath(output)
	if err != nil {
		return fmt.Errorf("resolve generated artifact location: %w", err)
	}
	outputDirectories, err := pathForms(filepath.Dir(output))
	if err != nil {
		return fmt.Errorf("resolve generated artifact directory: %w", err)
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		if info, statErr := os.Lstat(root); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return errOutputOverlapsSource
		} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("inspect configured source root: %w", statErr)
		}
		rootDirectories, err := pathForms(root)
		if err != nil {
			return fmt.Errorf("resolve configured source location: %w", err)
		}
		for _, outputDirectory := range outputDirectories {
			for _, rootDirectory := range rootDirectories {
				if directoriesOverlap(outputDirectory, rootDirectory) {
					return errOutputOverlapsSource
				}
			}
		}
		if sameFileIdentity(root, output) {
			return errOutputOverlapsSource
		}
	}
	for _, source := range files {
		if source == "" {
			continue
		}
		if info, statErr := os.Lstat(source); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return errOutputOverlapsSource
		} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("inspect discovered source location: %w", statErr)
		}
		sourceAbsolute, err := absolutePath(source)
		if err != nil {
			return fmt.Errorf("resolve discovered source location: %w", err)
		}
		resolvedSource, err := canonicalPath(source)
		if err != nil {
			return fmt.Errorf("resolve discovered source location: %w", err)
		}
		if samePath(sourceAbsolute, outputAbsolute) || samePath(resolvedSource, resolvedOutput) || sameFileIdentity(source, output) {
			return errOutputOverlapsSource
		}
		sourceDirectories, err := pathForms(filepath.Dir(source))
		if err != nil {
			return fmt.Errorf("resolve discovered source directory: %w", err)
		}
		resolvedDirectories, err := pathForms(filepath.Dir(resolvedSource))
		if err != nil {
			return fmt.Errorf("resolve discovered source target directory: %w", err)
		}
		sourceDirectories = append(sourceDirectories, resolvedDirectories...)
		for _, outputDirectory := range outputDirectories {
			for _, sourceDirectory := range sourceDirectories {
				if directoriesOverlap(outputDirectory, sourceDirectory) {
					return errOutputOverlapsSource
				}
			}
		}
	}
	return nil
}

func absolutePath(input string) (string, error) {
	return filepath.Abs(filepath.Clean(input))
}

func pathForms(input string) ([]string, error) {
	absolute, err := absolutePath(input)
	if err != nil {
		return nil, err
	}
	resolved, err := canonicalPath(input)
	if err != nil {
		return nil, err
	}
	if samePath(absolute, resolved) {
		return []string{absolute}, nil
	}
	return []string{absolute, resolved}, nil
}

func canonicalPath(input string) (string, error) {
	absolute, err := absolutePath(input)
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
		if info, statErr := os.Lstat(current); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("path contains a dangling symbolic link")
		} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return absolute, nil
		}
		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent
	}
}

func directoriesOverlap(left, right string) bool {
	return containsPath(left, right) || containsPath(right, left)
}

func containsPath(root, candidate string) bool {
	for current := candidate; ; current = filepath.Dir(current) {
		if samePath(root, current) || sameFileIdentity(root, current) {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
	}
}

func samePath(left, right string) bool {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
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
func WriteReport(path string, report analytics.Report) (returnErr error) {
	dir := filepath.Dir(path)
	if err := secureOutputDirectory(dir); err != nil {
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
			if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				returnErr = errors.Join(returnErr, fmt.Errorf("remove temporary report: %w", err))
			}
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
	directory := filepath.Dir(path)
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect aggregate directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if !info.IsDir() {
		return errors.New("aggregate directory path is not a directory")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("inspect aggregate directory contents: %w", err)
	}
	if len(entries) > 0 {
		return nil
	}
	if err := os.Remove(directory); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove empty aggregate directory: %w", err)
	}
	return nil
}
