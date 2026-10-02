package benchmark

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestBenchmarkExpectedFactsMatchFixtureSource(t *testing.T) {
	root := benchmarkRoot(t)
	manifest, err := LoadManifest(filepath.Join(root, "tasks.json"))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}

	facts := deriveFixtureFacts(t, filepath.Join(root, manifest.Fixture))
	for _, task := range manifest.Tasks {
		for _, expected := range task.ExpectedFacts {
			if _, ok := facts[expected]; !ok {
				t.Fatalf("task %q expected fact is not supported by fixture source: %s", task.ID, expected)
			}
		}
	}
}

func TestBenchmarkFixtureContainsServiceDecoy(t *testing.T) {
	root := benchmarkRoot(t)
	facts := deriveFixtureFacts(t, filepath.Join(root, "fixture"))

	if _, ok := facts["declares|other/service.go|example.com/bench/other.Service"]; !ok {
		t.Fatal("fixture must retain the other.Service decoy")
	}
	if _, ok := facts["declares|lib/service.go|example.com/bench/lib.Service"]; !ok {
		t.Fatal("fixture must retain the target lib.Service declaration")
	}
}

func benchmarkRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve benchmark test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "benchmarks", "v1"))
}

func deriveFixtureFacts(t *testing.T, root string) map[string]struct{} {
	t.Helper()

	modulePath := readModulePath(t, filepath.Join(root, "go.mod"))
	facts := make(map[string]struct{})
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}

		parsed, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		dir := filepath.ToSlash(filepath.Dir(rel))
		packagePath := modulePath
		if dir != "." {
			packagePath += "/" + dir
		}

		facts["contains|"+packagePath+"|"+rel] = struct{}{}

		for _, declaration := range parsed.Decls {
			gen, ok := declaration.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				facts["declares|"+rel+"|"+packagePath+"."+typeSpec.Name.Name] = struct{}{}
			}
		}

		for _, imp := range parsed.Imports {
			importPath := strings.Trim(imp.Path.Value, """)
			if strings.HasPrefix(importPath, modulePath) {
				facts["imports|"+packagePath+"|"+importPath] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("derive fixture facts: %v", err)
	}
	return facts
}

func readModulePath(t *testing.T, path string) string {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture go.mod: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1]
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read fixture go.mod: %v", err)
	}
	t.Fatal("fixture go.mod has no module directive")
	return ""
}

func sortedFacts(facts map[string]struct{}) []string {
	out := make([]string, 0, len(facts))
	for fact := range facts {
		out = append(out, fact)
	}
	sort.Strings(out)
	return out
}
