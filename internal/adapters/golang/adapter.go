package golang

import (
	"context"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/graph"
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/snapshot"
)

const extractorName = "go/packages-v1"

func Extract(ctx context.Context, root string, sources []snapshot.Source) graph.Result {
	allowed := allowedGoSources(sources)
	if len(allowed) == 0 {
		return graph.Result{}
	}

	result := graph.Result{Detected: true}
	cfg := &packages.Config{
		Context: ctx,
		Dir:     root,
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedImports |
			packages.NeedTypes |
			packages.NeedTypesSizes |
			packages.NeedSyntax |
			packages.NeedModule,
		Env:        safeEnvironment(),
		BuildFlags: []string{"-mod=readonly"},
		Tests:      false,
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		result.Warnings = []string{fmt.Sprintf("go adapter load failed: %v", err)}
		return result
	}

	nodes := make(map[string]graph.Node)
	edges := make(map[string]graph.Edge)
	warnings := make(map[string]struct{})

	for _, pkg := range pkgs {
		if pkg == nil || pkg.PkgPath == "" {
			continue
		}
		if len(pkg.Errors) != 0 {
			for _, pkgErr := range pkg.Errors {
				warnings[fmt.Sprintf("go package %s: %s", pkg.PkgPath, pkgErr.Error())] = struct{}{}
			}
			continue
		}

		localFiles, excluded, err := packageFiles(root, pkg.GoFiles, pkg.CompiledGoFiles, allowed)
		if err != nil {
			warnings[fmt.Sprintf("go package %s: %v", pkg.PkgPath, err)] = struct{}{}
			continue
		}
		if excluded != "" {
			warnings[fmt.Sprintf(
				"go package %s uses file outside snapshot universe: %s",
				pkg.PkgPath,
				excluded,
			)] = struct{}{}
			continue
		}
		if len(localFiles) == 0 {
			continue
		}
		if pkg.Types == nil || pkg.Fset == nil {
			warnings[fmt.Sprintf("go package %s has incomplete type information", pkg.PkgPath)] = struct{}{}
			continue
		}

		packageID := packageNodeID(pkg.PkgPath)
		nodes[packageID] = graph.Node{
			ID:          packageID,
			Kind:        "PACKAGE",
			Name:        pkg.Name,
			Language:    "go",
			PackagePath: pkg.PkgPath,
		}

		for _, path := range localFiles {
			fileID := fileNodeID(path)
			nodes[fileID] = graph.Node{
				ID:       fileID,
				Kind:     "FILE",
				Name:     filepath.Base(path),
				Path:     path,
				Language: "go",
			}
			addEdge(edges, graph.Edge{
				From:       packageID,
				To:         fileID,
				Relation:   "CONTAINS",
				Evidence:   "OBSERVED",
				Resolution: "semantic",
				Extractor:  extractorName,
				SourcePath: path,
			})
		}

		scope := pkg.Types.Scope()
		names := scope.Names()
		sort.Strings(names)
		for _, name := range names {
			obj := scope.Lookup(name)
			if obj == nil || obj.Pos() == 0 {
				continue
			}
			position := pkg.Fset.Position(obj.Pos())
			rel, local, err := relativeLocalPath(root, position.Filename)
			if err != nil || !local {
				continue
			}
			if _, ok := allowed[rel]; !ok {
				warnings[fmt.Sprintf(
					"go symbol %s.%s resolves outside snapshot universe: %s",
					pkg.PkgPath,
					name,
					rel,
				)] = struct{}{}
				continue
			}

			symbolID := symbolNodeID(pkg.PkgPath, name)
			nodes[symbolID] = graph.Node{
				ID:          symbolID,
				Kind:        symbolKind(obj),
				Name:        name,
				Path:        rel,
				Language:    "go",
				PackagePath: pkg.PkgPath,
				StartLine:   position.Line,
				EndLine:     position.Line,
			}
			addEdge(edges, graph.Edge{
				From:       fileNodeID(rel),
				To:         symbolID,
				Relation:   "DECLARES",
				Evidence:   "OBSERVED",
				Resolution: "semantic",
				Extractor:  extractorName,
				SourcePath: rel,
				StartLine:  position.Line,
			})
		}

		importPaths := make([]string, 0, len(pkg.Imports))
		for importPath := range pkg.Imports {
			importPaths = append(importPaths, importPath)
		}
		sort.Strings(importPaths)
		for _, importPath := range importPaths {
			imported := pkg.Imports[importPath]
			if imported == nil || imported.PkgPath == "" {
				continue
			}
			importID := packageNodeID(imported.PkgPath)
			if _, exists := nodes[importID]; !exists {
				nodes[importID] = graph.Node{
					ID:          importID,
					Kind:        "PACKAGE",
					Name:        imported.Name,
					Language:    "go",
					PackagePath: imported.PkgPath,
					External:    !packageHasAllowedFile(root, imported, allowed),
				}
			}
			addEdge(edges, graph.Edge{
				From:       packageID,
				To:         importID,
				Relation:   "IMPORTS",
				Evidence:   "OBSERVED",
				Resolution: "semantic",
				Extractor:  extractorName,
			})
		}
	}

	result.Nodes = sortedNodes(nodes)
	result.Edges = sortedEdges(edges)
	result.Warnings = sortedWarnings(warnings)
	return result
}

func allowedGoSources(sources []snapshot.Source) map[string]struct{} {
	allowed := make(map[string]struct{})
	for _, source := range sources {
		if source.Kind == snapshot.SourceRegular && strings.HasSuffix(strings.ToLower(source.Path), ".go") {
			allowed[filepath.ToSlash(source.Path)] = struct{}{}
		}
	}
	return allowed
}

func packageFiles(
	root string,
	goFiles []string,
	compiledGoFiles []string,
	allowed map[string]struct{},
) ([]string, string, error) {
	seen := make(map[string]struct{})
	var localFiles []string

	for _, filename := range append(append([]string{}, goFiles...), compiledGoFiles...) {
		rel, local, err := relativeLocalPath(root, filename)
		if err != nil {
			return nil, "", err
		}
		if !local {
			continue
		}
		if _, ok := allowed[rel]; !ok {
			return nil, rel, nil
		}
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		localFiles = append(localFiles, rel)
	}

	sort.Strings(localFiles)
	return localFiles, "", nil
}

func packageHasAllowedFile(root string, pkg *packages.Package, allowed map[string]struct{}) bool {
	if pkg == nil {
		return false
	}
	for _, filename := range pkg.GoFiles {
		rel, local, err := relativeLocalPath(root, filename)
		if err == nil && local {
			if _, ok := allowed[rel]; ok {
				return true
			}
		}
	}
	return false
}

func relativeLocalPath(root, filename string) (string, bool, error) {
	if filename == "" {
		return "", false, nil
	}
	rel, err := filepath.Rel(root, filename)
	if err != nil {
		return "", false, err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false, nil
	}
	return filepath.ToSlash(filepath.Clean(rel)), true, nil
}

func safeEnvironment() []string {
	blocked := map[string]struct{}{
		"GOPROXY":          {},
		"GOSUMDB":          {},
		"GOTOOLCHAIN":      {},
		"CGO_ENABLED":      {},
		"GOPACKAGESDRIVER": {},
	}
	env := make([]string, 0, len(os.Environ())+4)
	for _, item := range os.Environ() {
		key, _, found := strings.Cut(item, "=")
		if found {
			if _, skip := blocked[key]; skip {
				continue
			}
		}
		env = append(env, item)
	}
	return append(
		env,
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOTOOLCHAIN=local",
		"CGO_ENABLED=0",
	)
}

func packageNodeID(path string) string {
	return "go:package:" + path
}

func fileNodeID(path string) string {
	return "file:" + filepath.ToSlash(path)
}

func symbolNodeID(packagePath, name string) string {
	return "go:symbol:" + packagePath + ":" + name
}

func symbolKind(obj types.Object) string {
	switch obj.(type) {
	case *types.TypeName:
		return "TYPE"
	case *types.Func:
		return "FUNCTION"
	case *types.Const:
		return "CONST"
	case *types.Var:
		return "VAR"
	default:
		return "SYMBOL"
	}
}

func addEdge(edges map[string]graph.Edge, edge graph.Edge) {
	key := strings.Join([]string{
		edge.From,
		edge.To,
		edge.Relation,
		edge.Evidence,
		edge.Resolution,
		edge.Extractor,
		edge.SourcePath,
		fmt.Sprintf("%d", edge.StartLine),
	}, "\x00")
	edges[key] = edge
}

func sortedNodes(nodes map[string]graph.Node) []graph.Node {
	keys := make([]string, 0, len(nodes))
	for key := range nodes {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]graph.Node, 0, len(keys))
	for _, key := range keys {
		out = append(out, nodes[key])
	}
	return out
}

func sortedEdges(edges map[string]graph.Edge) []graph.Edge {
	keys := make([]string, 0, len(edges))
	for key := range edges {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]graph.Edge, 0, len(keys))
	for _, key := range keys {
		out = append(out, edges[key])
	}
	return out
}

func sortedWarnings(warnings map[string]struct{}) []string {
	out := make([]string, 0, len(warnings))
	for warning := range warnings {
		out = append(out, warning)
	}
	sort.Strings(out)
	return out
}
