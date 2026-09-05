// Package sqlitecopy opens SQLite history without letting SQLite touch the
// irreplaceable source directory. It copies a stable DB/WAL snapshot into a
// private temporary directory before the SQLite library sees it.
package sqlitecopy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const maxCopyAttempts = 5

var (
	persistentSuffixes = []string{"", "-wal", "-journal"}
	errSourceChanged   = errors.New("sqlite source changed while being copied")
)

// Database owns a read-query-only SQLite connection and its private copy.
type Database struct {
	DB       *sql.DB
	dir      string
	closeMu  sync.Once
	closeErr error
}

// Open copies source with raw read handles, validates a stable before/copied/
// after hash set, opens only the copy, and runs quick_check before returning.
func Open(ctx context.Context, source string) (*Database, error) {
	if _, err := regularFileInfo(source); err != nil {
		return nil, fmt.Errorf("inspect sqlite source: %w", err)
	}
	if err := ensureTempSeparate(ctx, source); err != nil {
		return nil, err
	}

	for attempt := 1; attempt <= maxCopyAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dir, err := makePrivateTempDir(ctx)
		if err != nil {
			return nil, fmt.Errorf("create private sqlite copy directory: %w", err)
		}

		copyPath, stable, err := stableCopy(ctx, source, dir)
		if err != nil {
			return nil, cleanupError(dir, err)
		}
		if !stable {
			if err := cleanupError(dir, nil); err != nil {
				return nil, err
			}
			if attempt < maxCopyAttempts {
				if err := waitBeforeRetry(ctx, attempt); err != nil {
					return nil, err
				}
			}
			continue
		}
		database, err := openCopy(ctx, copyPath, dir)
		if err != nil {
			return nil, cleanupError(dir, err)
		}
		return database, nil
	}
	return nil, fmt.Errorf("sqlite source remained active while copying; skipped after %d stable-copy attempts", maxCopyAttempts)
}

// Close closes SQLite before removing the private copy. Cleanup failures are
// returned because a leftover copy may contain sensitive source material.
func (d *Database) Close() error {
	d.closeMu.Do(func() {
		closeErr := d.DB.Close()
		removeErr := os.RemoveAll(d.dir)
		if removeErr != nil {
			removeErr = fmt.Errorf("remove private sqlite copy: %w", removeErr)
		}
		d.closeErr = errors.Join(closeErr, removeErr)
	})
	return d.closeErr
}

type fileState struct {
	info   os.FileInfo
	digest string
	size   int64
}

type copiedState struct {
	digest string
	size   int64
}

func stableCopy(ctx context.Context, source, dir string) (string, bool, error) {
	before, err := inspectSourceSet(ctx, source)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, errSourceChanged) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("hash sqlite source before copy: %w", err)
	}

	base := filepath.Base(source)
	copied := make(map[string]copiedState, len(before))
	for _, suffix := range persistentSuffixes {
		state, exists := before[suffix]
		if !exists {
			continue
		}
		destination := filepath.Join(dir, base+suffix)
		digest, size, err := copyRegularFile(ctx, source+suffix, destination, state.info)
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, errSourceChanged) {
			return "", false, nil
		}
		if err != nil {
			return "", false, fmt.Errorf("copy sqlite source: %w", err)
		}
		copied[suffix] = copiedState{digest: digest, size: size}
	}

	after, err := inspectSourceSet(ctx, source)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, errSourceChanged) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("hash sqlite source after copy: %w", err)
	}
	stable := equalSourceSets(before, after) && copiedMatchesSource(before, copied)
	return filepath.Join(dir, base), stable, nil
}

func inspectSourceSet(ctx context.Context, source string) (map[string]fileState, error) {
	set := make(map[string]fileState, len(persistentSuffixes))
	for _, suffix := range persistentSuffixes {
		path := source + suffix
		info, err := regularFileInfo(path)
		if errors.Is(err, os.ErrNotExist) && suffix != "" {
			continue
		}
		if err != nil {
			return nil, err
		}
		digest, size, err := digestRegularFile(ctx, path, info)
		if err != nil {
			return nil, err
		}
		if size != info.Size() {
			return nil, errSourceChanged
		}
		set[suffix] = fileState{info: info, digest: digest, size: size}
	}
	return set, nil
}

func regularFileInfo(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("sqlite file is a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("sqlite file is not a regular file")
	}
	return info, nil
}

func digestRegularFile(ctx context.Context, path string, expected os.FileInfo) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	info, statErr := f.Stat()
	if statErr != nil || !info.Mode().IsRegular() || !os.SameFile(expected, info) {
		return "", 0, errors.Join(statErr, f.Close(), errSourceChanged)
	}

	h := sha256.New()
	size, readErr := copyWithContext(ctx, h, f)
	closeErr := f.Close()
	return hex.EncodeToString(h.Sum(nil)), size, errors.Join(readErr, closeErr)
}

func copyRegularFile(ctx context.Context, source, destination string, expected os.FileInfo) (string, int64, error) {
	in, err := os.Open(source)
	if err != nil {
		return "", 0, err
	}
	info, statErr := in.Stat()
	if statErr != nil || !info.Mode().IsRegular() || !os.SameFile(expected, info) {
		return "", 0, errors.Join(statErr, in.Close(), errSourceChanged)
	}

	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, errors.Join(err, in.Close())
	}
	h := sha256.New()
	size, copyErr := copyWithContext(ctx, io.MultiWriter(out, h), in)
	if size != info.Size() && copyErr == nil {
		copyErr = errSourceChanged
	}
	syncErr := out.Sync()
	outCloseErr := out.Close()
	inCloseErr := in.Close()
	return hex.EncodeToString(h.Sum(nil)), size, errors.Join(copyErr, syncErr, outCloseErr, inCloseErr)
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 1024*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			written, writeErr := destination.Write(buffer[:read])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != read {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			return total, readErr
		}
	}
}

func equalSourceSets(left, right map[string]fileState) bool {
	if len(left) != len(right) {
		return false
	}
	for suffix, before := range left {
		after, exists := right[suffix]
		if !exists || !os.SameFile(before.info, after.info) || before.size != after.size || before.digest != after.digest {
			return false
		}
	}
	return true
}

func copiedMatchesSource(source map[string]fileState, copied map[string]copiedState) bool {
	if len(source) != len(copied) {
		return false
	}
	for suffix, original := range source {
		copy, exists := copied[suffix]
		if !exists || original.size != copy.size || original.digest != copy.digest {
			return false
		}
	}
	return true
}

func openCopy(ctx context.Context, path, dir string) (*Database, error) {
	// The first private connection may write only to the private copy so SQLite
	// can recover a copied hot journal or WAL. It never sees the source path.
	recovery, err := openConnection(path, false)
	if err != nil {
		return nil, err
	}
	if err := recovery.PingContext(ctx); err != nil {
		return nil, closeDatabase(recovery, fmt.Errorf("read private sqlite copy: %w", err))
	}
	if err := quickCheck(ctx, recovery); err != nil {
		return nil, closeDatabase(recovery, err)
	}
	if err := recovery.Close(); err != nil {
		return nil, fmt.Errorf("close private sqlite recovery connection: %w", err)
	}

	// Reopen read-only with query_only in the DSN so every connection created by
	// database/sql receives the setting, including a transparent replacement.
	db, err := openConnection(path, true)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, closeDatabase(db, fmt.Errorf("open query-only sqlite copy: %w", err))
	}
	var queryOnly int
	if err := db.QueryRowContext(ctx, "PRAGMA query_only").Scan(&queryOnly); err != nil || queryOnly != 1 {
		if err == nil {
			err = errors.New("query_only was not enabled")
		}
		return nil, closeDatabase(db, fmt.Errorf("verify private sqlite copy protections: %w", err))
	}
	return &Database{DB: db, dir: dir}, nil
}

func openConnection(path string, queryOnly bool) (*sql.DB, error) {
	query := url.Values{
		"mode":          {"rw"},
		"_defensive":    {"1"},
		"_dqs":          {"0"},
		"_busy_timeout": {"2000"},
		"_pragma":       {"trusted_schema(OFF)", "temp_store(MEMORY)"},
	}
	if queryOnly {
		query.Set("mode", "ro")
		query.Set("_query_only", "1")
	}
	dsnURL := &url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}
	db, err := sql.Open("sqlite", dsnURL.String())
	if err != nil {
		return nil, fmt.Errorf("open private sqlite copy: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

func quickCheck(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "PRAGMA quick_check(1)")
	if err != nil {
		return fmt.Errorf("validate private sqlite copy: %w", err)
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return fmt.Errorf("validate private sqlite copy: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("validate private sqlite copy: %w", err)
	}
	if len(values) != 1 || values[0] != "ok" {
		return errors.New("validate private sqlite copy: quick_check did not return exactly one ok row")
	}
	return nil
}

func closeDatabase(db *sql.DB, cause error) error {
	return errors.Join(cause, db.Close())
}

func cleanupError(dir string, cause error) error {
	if err := os.RemoveAll(dir); err != nil {
		return errors.Join(cause, fmt.Errorf("remove private sqlite copy: %w", err))
	}
	return cause
}

func waitBeforeRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(attempt) * 10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
