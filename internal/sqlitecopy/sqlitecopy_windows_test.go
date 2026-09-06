package sqlitecopy

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/0merUfuk/skuggsja/internal/audit"
	"golang.org/x/sys/windows"
)

func TestWindowsLockedSHMCannotProduceEqualityEvidence(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "state.db")
	writer, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE sessions (id TEXT); INSERT INTO sessions VALUES ('committed');"); err != nil {
		t.Fatal(err)
	}
	transaction, err := writer.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	if _, err := transaction.Exec("INSERT INTO sessions VALUES ('uncommitted')"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(source + "-shm"); err != nil || info.Size() == 0 {
		t.Fatalf("normal WAL fixture must retain a real SHM file: info=%v error=%v", info, err)
	}
	before, err := audit.Capture(context.Background(), []string{source})
	if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) || before.Complete || before.Manifest != "" {
		t.Fatalf("locked SHM must make full equality capture unavailable: snapshot=%+v error=%v", before, err)
	}
	// Normal ingestion copies DB/WAL/journal and reconstructs its own SHM.
	// This is independent of whether a full source equality audit is possible.
	copyDB, err := Open(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	var count int
	if err := copyDB.DB.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count); err != nil || count != 1 {
		t.Fatalf("normal WAL copy must expose only committed rows: count=%d error=%v", count, err)
	}
	if err := copyDB.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := audit.Capture(context.Background(), []string{source})
	if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) || after.Complete || after.Manifest != "" {
		t.Fatalf("full capture must remain unavailable after copying: snapshot=%+v error=%v", after, err)
	}
	if comparison := audit.Compare(before, after); comparison.Verified {
		t.Fatalf("failed full captures must never be promoted to equality: %+v", comparison)
	}
}
