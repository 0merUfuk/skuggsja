package sqlitecopy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type tempDirContextKey struct{}

// WithTempDir chooses the parent of private SQLite copies without modifying
// process-global environment variables. The caller must keep this workspace
// separate from every configured source; Open additionally protects the
// directory containing its own source database. This function creates nothing.
func WithTempDir(ctx context.Context, parent string) context.Context {
	return context.WithValue(ctx, tempDirContextKey{}, parent)
}

func tempParent(ctx context.Context) string {
	if parent, ok := ctx.Value(tempDirContextKey{}).(string); ok {
		return parent
	}
	return os.TempDir()
}

// ensureTempSeparate runs before MkdirTemp so even a deliberately misplaced
// TMPDIR cannot cause temporary files to be created inside the source store.
// An ancestor such as /tmp is valid when the database lives in /tmp/history:
// the random private directory will be its sibling, not inside history.
func ensureTempSeparate(ctx context.Context, source string) error {
	parent := tempParent(ctx)
	if parent == "" || !filepath.IsAbs(parent) {
		return errors.New("private sqlite workspace must be an absolute directory")
	}
	resolvedParent, err := resolveExistingAncestor(parent)
	if err != nil {
		return fmt.Errorf("resolve private sqlite workspace: %w", err)
	}
	resolvedSource, err := resolveExistingAncestor(filepath.Dir(source))
	if err != nil {
		return fmt.Errorf("resolve sqlite source directory: %w", err)
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		resolvedParent = strings.ToLower(resolvedParent)
		resolvedSource = strings.ToLower(resolvedSource)
	}
	relative, err := filepath.Rel(resolvedSource, resolvedParent)
	if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
		return errors.New("private sqlite workspace overlaps the source directory")
	}
	return nil
}

func resolveExistingAncestor(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(absolute)
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			return filepath.Join(append([]string{resolved}, suffix...)...), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if info, statErr := os.Lstat(current); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("path contains a dangling symbolic link")
		} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent
	}
}
