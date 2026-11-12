package ops_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Ops run behind both front ends, so they must not reach for the terminal,
// git, local config, or either front end's framework.
var forbiddenImports = []string{
	"github.com/spf13/cobra",
	"github.com/modelcontextprotocol/go-sdk",
	"github.com/kabilan108/atlas/internal/cli",
	"github.com/kabilan108/atlas/internal/config",
	"github.com/kabilan108/atlas/internal/git",
	"github.com/kabilan108/atlas/internal/mcpserver",
	"github.com/kabilan108/atlas/internal/update",
	"golang.org/x/term",
}

func TestOpsLayering(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			importPath, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			for _, forbidden := range forbiddenImports {
				if importPath == forbidden || strings.HasPrefix(importPath, forbidden+"/") {
					t.Errorf("%s imports %s", path, importPath)
				}
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "os" && (sel.Sel.Name == "Stdout" || sel.Sel.Name == "Stderr") {
					t.Errorf("%s uses os.%s", fset.Position(sel.Pos()), sel.Sel.Name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
