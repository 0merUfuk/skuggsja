package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// This policy covers dormant production paths as well as the paths exercised
// by runtime verification. Providers and audit code can open read handles;
// mutations belong only to the separately reviewed artifact/private-copy code.
// It is an architectural regression check, not an OS sandbox claim.
func TestProductionSourceAccessHasNoWriteSurface(t *testing.T) {
	t.Parallel()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	reviewedWriters := map[string]bool{
		"internal/app/output.go":                 true,
		"internal/app/output_other.go":           true,
		"internal/app/output_windows.go":         true,
		"internal/app/workspace.go":              true,
		"internal/sqlitecopy/sqlitecopy.go":      true,
		"internal/sqlitecopy/tempdir_other.go":   true,
		"internal/sqlitecopy/tempdir_windows.go": true,
	}
	osWriteSurface := map[string]bool{
		"OpenFile": true, "Create": true, "CreateTemp": true,
		"WriteFile": true, "Mkdir": true, "MkdirAll": true, "MkdirTemp": true,
		"Remove": true, "RemoveAll": true, "Rename": true,
		"Chmod": true, "Chown": true, "Lchown": true, "Chtimes": true,
		"Link": true, "Symlink": true, "Truncate": true,
		"NewFile": true, "OpenRoot": true,
	}
	fileWriteSurface := map[string]bool{
		"Write": true, "WriteAt": true, "WriteString": true, "ReadFrom": true,
		"Truncate": true, "Chmod": true, "Chown": true,
	}
	for _, subtree := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(repository, subtree), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			relative, err := filepath.Rel(repository, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			writer := reviewedWriters[relative]
			sourceReader := strings.HasPrefix(relative, "internal/provider/") || strings.HasPrefix(relative, "internal/audit/")
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			imports := make(map[string]string)
			for _, spec := range parsed.Imports {
				importPath, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}
				alias := filepath.Base(importPath)
				if spec.Name != nil {
					alias = spec.Name.Name
				}
				imports[alias] = importPath
				if alias == "." && (importPath == "os" || importPath == "io/ioutil" || importPath == "database/sql") {
					t.Errorf("%s hides filesystem or database calls behind a dot import", relative)
				}
				if !writer && ((importPath == "syscall" && relative != "cmd/skuggsja/main.go") || strings.HasPrefix(importPath, "golang.org/x/sys/")) {
					t.Errorf("%s imports a direct syscall capability outside reviewed writers: %s", relative, importPath)
				}
				if sourceReader && (importPath == "os/exec" || importPath == "modernc.org/sqlite") {
					t.Errorf("%s bypasses read-only source access with import %s", relative, importPath)
				}
			}
			if writer {
				return nil
			}
			readHandles := make(map[*ast.Object]bool)
			ast.Inspect(parsed, func(node ast.Node) bool {
				assignment, ok := node.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for index, right := range assignment.Rhs {
					call, ok := right.(*ast.CallExpr)
					if !ok || index >= len(assignment.Lhs) {
						continue
					}
					selector, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || selector.Sel.Name != "Open" {
						continue
					}
					name, ok := selector.X.(*ast.Ident)
					if !ok || imports[name.Name] != "os" {
						continue
					}
					if handle, ok := assignment.Lhs[index].(*ast.Ident); ok && handle.Obj != nil {
						readHandles[handle.Obj] = true
					}
				}
				return true
			})
			ast.Inspect(parsed, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				name, ok := selector.X.(*ast.Ident)
				if !ok {
					return true
				}
				importPath := imports[name.Name]
				if (importPath == "os" && osWriteSurface[selector.Sel.Name]) ||
					(importPath == "syscall" && selector.Sel.Name != "SIGTERM") ||
					(importPath == "io/ioutil" && (selector.Sel.Name == "WriteFile" || selector.Sel.Name == "TempFile" || selector.Sel.Name == "TempDir")) ||
					(importPath == "database/sql" && (selector.Sel.Name == "Open" || selector.Sel.Name == "OpenDB")) ||
					(readHandles[name.Obj] && fileWriteSurface[selector.Sel.Name]) {
					t.Errorf("%s exposes a write capability outside reviewed writers: %s.%s", relative, name.Name, selector.Sel.Name)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
