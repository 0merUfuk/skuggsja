package sqlitecopy

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/0merUfuk/skuggsja/internal/audit"
	_ "modernc.org/sqlite"
)

func TestOpenReadsLiveWALWithoutChangingSource(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "state.db")
	writer, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE sessions (id TEXT PRIMARY KEY); INSERT INTO sessions VALUES ('synthetic-session');"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(source + "-wal"); err != nil || info.Size() <= 32 {
		t.Fatalf("fixture does not contain a live WAL: info=%v err=%v", info, err)
	}
	transaction, err := writer.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	if _, err := transaction.Exec("INSERT INTO sessions VALUES ('uncommitted-must-not-appear')"); err != nil {
		t.Fatal(err)
	}
	before, err := audit.Capture(context.Background(), []string{source})
	if err != nil {
		t.Fatal(err)
	}
	copyDB, err := Open(context.Background(), source)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	tempDir := copyDB.dir
	var count int
	if err := copyDB.DB.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("copied row count = %d, want 1", count)
	}
	var queryOnly, trustedSchema int
	if err := copyDB.DB.QueryRow("PRAGMA query_only").Scan(&queryOnly); err != nil || queryOnly != 1 {
		t.Fatalf("query_only = %d, err=%v", queryOnly, err)
	}
	if err := copyDB.DB.QueryRow("PRAGMA trusted_schema").Scan(&trustedSchema); err != nil || trustedSchema != 0 {
		t.Fatalf("trusted_schema = %d, err=%v", trustedSchema, err)
	}
	if _, err := copyDB.DB.Exec("INSERT INTO sessions VALUES ('must-not-write')"); err == nil {
		t.Fatal("query-only copied database accepted a write")
	}
	if err := copyDB.DB.QueryRow(`SELECT "missing_column" FROM sessions`).Scan(new(string)); err == nil {
		t.Fatal("double-quoted string compatibility remained enabled")
	}
	if _, err := copyDB.DB.Exec("PRAGMA writable_schema=ON"); err != nil {
		t.Fatal(err)
	}
	var writableSchema int
	if err := copyDB.DB.QueryRow("PRAGMA writable_schema").Scan(&writableSchema); err != nil || writableSchema != 0 {
		t.Fatalf("writable_schema = %d, err=%v", writableSchema, err)
	}
	if err := copyDB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Fatalf("temporary directory remains after Close: %v", err)
	}
	after, err := audit.Capture(context.Background(), []string{source})
	if err != nil {
		t.Fatal(err)
	}
	if comparison := audit.Compare(before, after); !comparison.Verified {
		t.Fatalf("source changed: %+v", comparison)
	}
}

func TestOpenRejectsSymbolicLinkSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target.db")
	if err := os.WriteFile(target, []byte("not opened"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "state.db")
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), link); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Open() error = %v, want symbolic-link rejection", err)
	}
}

func TestOpenRejectsSymbolicLinkPersistentSidecar(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "state.db")
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE sessions (id TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "not-a-wal")
	if err := os.WriteFile(target, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, source+"-wal"); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), source); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Open() error = %v, want sidecar symbolic-link rejection", err)
	}
}

func TestPrivateTempDirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows DACL validation has a platform-specific test")
	}
	dir, err := makePrivateTempDir()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if permissions := info.Mode().Perm(); permissions != 0o700 {
		t.Fatalf("private temp directory permissions = %o, want 700", permissions)
	}
}
