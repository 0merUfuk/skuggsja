package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// privateWorkspace contains all SQLite copies for one generation. The random
// path is checked before creation, so a source-root TMPDIR cannot receive even
// a transient directory. The source handles themselves remain read-only.
type privateWorkspace struct{ directory string }

func newPrivateWorkspace(parent string, roots, files []string) (privateWorkspace, error) {
	if parent == "" {
		parent = os.TempDir()
	}
	var err error
	parent, err = filepath.Abs(parent)
	if err != nil {
		return privateWorkspace{}, fmt.Errorf("resolve private workspace parent: %w", err)
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return privateWorkspace{}, fmt.Errorf("name private workspace: %w", err)
	}
	w := privateWorkspace{directory: filepath.Join(parent, "skuggsja-run-"+hex.EncodeToString(nonce[:]))}
	if err := w.ensureSeparate(roots, files); err != nil {
		return privateWorkspace{}, fmt.Errorf("private workspace overlaps source: %w", err)
	}
	if err := rejectOutputSymlinkComponents(w.directory); err != nil {
		return privateWorkspace{}, fmt.Errorf("validate private workspace: %w", err)
	}
	if err := os.Mkdir(w.directory, 0o700); err != nil {
		return privateWorkspace{}, fmt.Errorf("create private workspace: %w", err)
	}
	return w, nil
}

func (w privateWorkspace) ensureSeparate(roots, files []string) error {
	return EnsureOutputSeparate(filepath.Join(w.directory, ".copy-area"), roots, files)
}

func (w privateWorkspace) close() error {
	if err := os.RemoveAll(w.directory); err != nil {
		return fmt.Errorf("remove private workspace: %w", err)
	}
	return nil
}
