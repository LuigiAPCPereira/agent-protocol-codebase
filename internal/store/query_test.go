package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestQueryReturnsMatchesWithIncidentContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.sqlite3")
	index := Index{
		SnapshotID:      "snap",
		BaseCommit:      "abc",
		State:           "EXACT",
		AnalysisVersion: 1,
		Nodes: []Node{
			{ID: "file:main.go", Kind: "FILE", Name: "main.go", Path: "main.go", Language: "go"},
			{ID: "go:package:example.com/app", Kind: "PACKAGE", Name: "app", Language: "go", PackagePath: "example.com/app"},
			{ID: "go:symbol:example.com/app:Service", Kind: "TYPE", Name: "Service", Path: "main.go", Language: "go", PackagePath: "example.com/app", StartLine: 3, EndLine: 3},
		},
		Edges: []Edge{
			{From: "go:package:example.com/app", To: "file:main.go", Relation: "CONTAINS", Evidence: "OBSERVED", Resolution: "semantic", Extractor: "go/packages-v1", SourcePath: "main.go"},
			{From: "file:main.go", To: "go:symbol:example.com/app:Service", Relation: "DECLARES", Evidence: "OBSERVED", Resolution: "semantic", Extractor: "go/packages-v1", SourcePath: "main.go", StartLine: 3},
		},
	}
	if err := Save(context.Background(), path, index); err != nil {
		t.Fatalf("save index: %v", err)
	}

	got, err := Query(context.Background(), path, QueryFilter{Name: "service", Limit: 10})
	if err != nil {
		t.Fatalf("query index: %v", err)
	}
	if len(got.Matches) != 1 || got.Matches[0] != "go:symbol:example.com/app:Service" {
		t.Fatalf("matches = %v", got.Matches)
	}
	if len(got.Nodes) != 2 {
		t.Fatalf("nodes = %+v, want match plus direct endpoint", got.Nodes)
	}
	if len(got.Edges) != 1 || got.Edges[0].Relation != "DECLARES" {
		t.Fatalf("edges = %+v, want DECLARES", got.Edges)
	}
}

func TestQueryCombinesSelectors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.sqlite3")
	if err := Save(context.Background(), path, Index{
		SnapshotID:      "snap",
		BaseCommit:      "abc",
		State:           "EXACT",
		AnalysisVersion: 1,
		Nodes: []Node{
			{ID: "one", Kind: "FUNCTION", Name: "Open", Path: "internal/store/sqlite.go", PackagePath: "example.com/internal/store"},
			{ID: "two", Kind: "TYPE", Name: "OpenState", Path: "internal/store/state.go", PackagePath: "example.com/internal/store"},
		},
	}); err != nil {
		t.Fatalf("save index: %v", err)
	}

	got, err := Query(context.Background(), path, QueryFilter{
		Name:    "open",
		Kind:    "function",
		Package: "internal/store",
	})
	if err != nil {
		t.Fatalf("query index: %v", err)
	}
	if len(got.Matches) != 1 || got.Matches[0] != "one" {
		t.Fatalf("matches = %v, want [one]", got.Matches)
	}
}

func TestQueryRequiresSelector(t *testing.T) {
	_, err := Query(context.Background(), "unused.sqlite3", QueryFilter{})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("error = %v, want ErrInvalidQuery", err)
	}
}
