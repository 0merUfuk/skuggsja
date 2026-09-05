//go:build !windows

package sqlitecopy

import (
	"context"
	"fmt"
	"os"
)

func makePrivateTempDir(ctx context.Context) (string, error) {
	dir, err := os.MkdirTemp(tempParent(ctx), "skuggsja-sqlite-")
	if err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", cleanupError(dir, fmt.Errorf("set private directory permissions: %w", err))
	}
	return dir, nil
}
