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
)

func TestOpenRejectsPrivateCopyInsideSourceDirectory(t *testing.T) {
	for _, placement := range []string{"same directory", "nested absent directory", "symlink alias"} {
		t.Run(placement, func(t *testing.T) {
			sourceDir := t.TempDir()
			source := filepath.Join(sourceDir, "state.db")
			if err := os.WriteFile(source, []byte("synthetic source must remain untouched"), 0o600); err != nil {
				t.Fatal(err)
			}
			parent := sourceDir
			switch placement {
			case "nested absent directory":
				parent = filepath.Join(sourceDir, "absent", "copies")
			case "symlink alias":
				parent = filepath.Join(t.TempDir(), "source-alias")
				if err := os.Symlink(sourceDir, parent); err != nil {
					if runtime.GOOS == "windows" {
						t.Skipf("symlinks unavailable: %v", err)
					}
					t.Fatal(err)
				}
			}
			assertCopyLocationRejectedWithoutWrites(t, WithTempDir(context.Background(), parent), sourceDir, source)
		})
	}
}

func TestOpenRejectsInheritedTempDirectoryInsideSource(t *testing.T) {
	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "state.db")
	if err := os.WriteFile(source, []byte("synthetic source must remain untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Setenv("TMP", sourceDir)
		t.Setenv("TEMP", sourceDir)
	} else {
		t.Setenv("TMPDIR", sourceDir)
	}
	assertCopyLocationRejectedWithoutWrites(t, context.Background(), sourceDir, source)
}

func assertCopyLocationRejectedWithoutWrites(t *testing.T, ctx context.Context, sourceDir, source string) {
	t.Helper()
	before, err := audit.CaptureConfigured(ctx, []string{sourceDir}, []string{source}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, source); err == nil || !strings.Contains(err.Error(), "overlaps the source directory") {
		t.Fatalf("Open() error = %v, want source-directory overlap rejection before copying", err)
	}
	after, err := audit.CaptureConfigured(ctx, []string{sourceDir}, []string{source}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if comparison := audit.Compare(before, after); !comparison.Verified {
		t.Fatalf("rejected copy changed source content or directory membership: %+v", comparison)
	}
}

func TestOpenUsesContextWorkspaceBesideSourceDirectory(t *testing.T) {
	t.Parallel()
	sharedParent := t.TempDir()
	sourceDir := filepath.Join(sharedParent, "history")
	if err := os.Mkdir(sourceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(sourceDir, "state.db")
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE sessions (id TEXT); INSERT INTO sessions VALUES ('synthetic')"); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := WithTempDir(context.Background(), sharedParent)
	before, err := audit.CaptureConfigured(ctx, []string{sourceDir}, []string{source}, nil)
	if err != nil {
		t.Fatal(err)
	}
	copyDB, err := Open(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	if filepath.Dir(copyDB.dir) != sharedParent || filepath.Dir(copyDB.dir) == sourceDir {
		t.Fatalf("private copy was not allocated beside the source in the explicit workspace: %q", copyDB.dir)
	}
	var count int
	if err := copyDB.DB.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count); err != nil || count != 1 {
		t.Fatalf("private copy query: count=%d error=%v", count, err)
	}
	if err := copyDB.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(sharedParent)
	if err != nil || len(entries) != 1 || entries[0].Name() != "history" {
		t.Fatalf("private copy cleanup left unexpected directory entries: entries=%v error=%v", entries, err)
	}
	after, err := audit.CaptureConfigured(ctx, []string{sourceDir}, []string{source}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if comparison := audit.Compare(before, after); !comparison.Verified {
		t.Fatalf("private sibling copy changed source: %+v", comparison)
	}
}
