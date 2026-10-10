package golang

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/graph"
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/snapshot"
)

func TestExtractPackagesSymbolsAndImports(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/fixture\n\ngo 1.26.0\n")
	writeFile(t, root, "a/a.go", `package a

import "example.com/fixture/b"

type Service struct{}

func Use() string { return b.Value }
`)
	writeFile(t, root, "b/b.go", `package b

const Value = "ok"
`)

	result := Extract(context.Background(), root, []snapshot.Source{
		{Path: "a/a.go", Kind: snapshot.SourceRegular},
		{Path: "b/b.go", Kind: snapshot.SourceRegular},
	})

	if len(result.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", result.Warnings)
	}
	assertNode(t, result, "go:package:example.com/fixture/a", "PACKAGE")
	assertNode(t, result, "file:a/a.go", "FILE")
	assertNode(t, result, "go:symbol:example.com/fixture/a:Service", "TYPE")
	assertNode(t, result, "go:symbol:example.com/fixture/a:Use", "FUNCTION")
	assertNode(t, result, "go:symbol:example.com/fixture/b:Value", "CONST")
	assertEdge(
		t,
		result,
		"go:package:example.com/fixture/a",
		"go:package:example.com/fixture/b",
		"IMPORTS",
	)
}

func TestExtractRejectsPackageDependingOnFileOutsideSnapshotUniverse(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/fixture\n\ngo 1.26.0\n")
	writeFile(t, root, "main.go", "package fixture\n\nfunc Visible() {}\n")
	writeFile(t, root, "generated.go", "package fixture\n\nfunc Generated() {}\n")

	result := Extract(context.Background(), root, []snapshot.Source{
		{Path: "main.go", Kind: snapshot.SourceRegular},
	})

	if len(result.Nodes) != 0 || len(result.Edges) != 0 {
		t.Fatalf("package outside snapshot universe must be skipped: nodes=%v edges=%v", result.Nodes, result.Edges)
	}
	found := false
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "outside snapshot universe") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected snapshot-universe warning, got %v", result.Warnings)
	}
}

func writeFile(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertNode(t *testing.T, result graph.Result, id, kind string) {
	t.Helper()
	for _, node := range result.Nodes {
		if node.ID == id {
			if node.Kind != kind {
				t.Fatalf("node %s kind = %q, want %q", id, node.Kind, kind)
			}
			return
		}
	}
	t.Fatalf("missing node %s in %+v", id, result.Nodes)
}

func assertEdge(t *testing.T, result graph.Result, from, to, relation string) {
	t.Helper()
	for _, edge := range result.Edges {
		if edge.From == from && edge.To == to && edge.Relation == relation {
			if edge.Evidence != "OBSERVED" || edge.Resolution != "semantic" {
				t.Fatalf("edge provenance = %+v", edge)
			}
			return
		}
	}
	t.Fatalf("missing edge %s --%s--> %s in %+v", from, relation, to, result.Edges)
}
