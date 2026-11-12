package ops_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
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

// forbiddenStdImports are matched exactly, so log/slog stays usable while the
// standard logger, which writes to stderr, does not.
var forbiddenStdImports = []string{"log"}

// forbiddenCalls are package members ops must not use, by import path: the
// process's streams, environment, working directory, and file system belong
// to the CLI. Errors such as os.ErrNotExist stay usable.
var forbiddenCalls = map[string][]string{
	"os": {
		"Stdout", "Stderr", "Stdin", "Exit",
		"Getenv", "LookupEnv", "Environ", "Setenv", "Unsetenv", "ExpandEnv",
		"Getwd", "Chdir", "UserHomeDir", "UserCacheDir", "UserConfigDir",
		"Create", "CreateTemp", "Open", "OpenFile", "ReadFile", "WriteFile", "ReadDir",
		"Mkdir", "MkdirAll", "MkdirTemp", "Remove", "RemoveAll", "Rename",
	},
	"fmt": {"Print", "Printf", "Println"},
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
		for _, problem := range layeringProblems(fset, file) {
			t.Error(problem)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// layeringProblems reports forbidden imports and uses of forbiddenCalls
// however the package is imported: by its name, an alias, or a dot import.
func layeringProblems(fset *token.FileSet, file *ast.File) []string {
	var problems []string
	localNames := map[string]string{}
	for _, imp := range file.Imports {
		importPath, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		for _, forbidden := range forbiddenImports {
			if importPath == forbidden || strings.HasPrefix(importPath, forbidden+"/") {
				problems = append(problems, fmt.Sprintf("%s imports %s", fset.Position(imp.Pos()), importPath))
			}
		}
		if slices.Contains(forbiddenStdImports, importPath) {
			problems = append(problems, fmt.Sprintf("%s imports %s", fset.Position(imp.Pos()), importPath))
		}
		if _, watched := forbiddenCalls[importPath]; !watched {
			continue
		}
		name := importPath
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "." {
			problems = append(problems, fmt.Sprintf("%s dot-imports %s", fset.Position(imp.Pos()), importPath))
			continue
		}
		localNames[name] = importPath
	}
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		importPath, ok := localNames[pkg.Name]
		if !ok {
			return true
		}
		for _, member := range forbiddenCalls[importPath] {
			if sel.Sel.Name == member {
				problems = append(problems, fmt.Sprintf("%s uses %s.%s", fset.Position(sel.Pos()), importPath, member))
			}
		}
		return true
	})
	return problems
}

func TestLayeringCatchesAliasesAndDotImports(t *testing.T) {
	sources := map[string]string{
		"plain":     "package x\nimport \"os\"\nvar _ = os.Stdout",
		"alias":     "package x\nimport stdos \"os\"\nvar _ = stdos.Stderr",
		"dot":       "package x\nimport . \"os\"\nvar _ = Stdout",
		"env":       "package x\nimport \"os\"\nvar _ = os.Getenv(\"HOME\")",
		"file":      "package x\nimport o \"os\"\nvar _ = o.WriteFile",
		"print":     "package x\nimport f \"fmt\"\nfunc g() { f.Println() }",
		"log":       "package x\nimport \"log\"\nfunc g() { log.Print() }",
		"forbidden": "package x\nimport \"github.com/kabilan108/atlas/internal/config\"",
	}
	for name, src := range sources {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name+".go", src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		if problems := layeringProblems(fset, file); len(problems) == 0 {
			t.Errorf("%s: no problem found in %q", name, src)
		}
	}
	allowed := "package x\nimport (\n\t\"errors\"\n\t\"fmt\"\n\t\"log/slog\"\n\t\"os\"\n)\nvar _ = errors.Is(nil, os.ErrNotExist)\nvar _ = fmt.Sprintf(\"\")\nvar _ = slog.Default"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "ok.go", allowed, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	if problems := layeringProblems(fset, file); len(problems) != 0 {
		t.Errorf("allowed source flagged: %v", problems)
	}
}
