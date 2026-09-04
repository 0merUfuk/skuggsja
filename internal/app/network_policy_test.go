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

// TestProductionNetworkSurfaceIsOnlyTheLoopbackServer keeps dormant client,
// telemetry, and updater code out of production, including code paths the
// runtime isolation check might not execute.
func TestProductionNetworkSurfaceIsOnlyTheLoopbackServer(t *testing.T) {
	t.Parallel()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	allowedFile := filepath.Join(repository, "internal", "app", "server.go")
	forbiddenImports := map[string]bool{
		"crypto/tls": true,
		"net":        true,
		"net/http":   true,
		"net/rpc":    true,
		"net/smtp":   true,
		"os/exec":    true,
	}

	err := filepath.WalkDir(repository, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "dist" || entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") ||
			strings.Contains(path, filepath.Join("scripts", "network-probe")) {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range parsed.Imports {
			name, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if (forbiddenImports[name] || strings.HasPrefix(name, "golang.org/x/net/")) && path != allowedFile {
				t.Errorf("production network-capable import %q outside loopback server in %s", name, filepath.Base(path))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), allowedFile, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbiddenSelectors := map[string]bool{
		"http.Get": true, "http.Post": true, "http.PostForm": true,
		"http.NewRequest": true, "http.NewRequestWithContext": true,
		"net.Dial": true, "net.DialTimeout": true, "net.ResolveIPAddr": true,
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		identifier, ok := selector.X.(*ast.Ident)
		if ok && forbiddenSelectors[identifier.Name+"."+selector.Sel.Name] {
			t.Errorf("loopback server contains outbound client call %s.%s", identifier.Name, selector.Sel.Name)
		}
		return true
	})
}
