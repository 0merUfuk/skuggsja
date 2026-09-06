//go:build !windows

package app_test

import (
	"fmt"
	"os"
)

func secureReleaseEvidenceFixture(path string) error {
	return os.Chmod(path, 0o700)
}

func releaseEvidencePrivacy(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if directory {
		if info.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("directory grants group/other access: %o", info.Mode().Perm())
		}
	} else if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("file permissions = %o, want 600", info.Mode().Perm())
	}
	return nil
}
