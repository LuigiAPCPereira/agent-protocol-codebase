package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	apiv1 "github.com/LuigiAPCPereira/agent-protocol-codebase/api/v1"
)

func TestExecutePathFindsShortestStructuralPath(t *testing.T) {
	repo := newGoRepository(t)
	ctx := context.Background()

	discovered := status(t, ctx, repo)
	scan(t, ctx, repo, discovered.Repository.Revision)

	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "path-service",
		Operation:     apiv1.OperationPath,
		Repository: apiv1.Repository{
			Root:     repo,
			Revision: discovered.Repository.Revision,
		},
		Arguments: map[string]any{
			"from":      "go:package:example.com/fixture",
			"to":        "go:symbol:example.com/fixture:Service",
			"direction": "outbound",
		},
	})
	if result.Error != nil {
		t.Fatalf("path error: %+v", result.Error)
	}

	var data apiv1.PathData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode path result: %v", err)
	}
	if !data.Found || data.Depth != 2 {
		t.Fatalf("path = %+v, want found depth 2", data)
	}
	if len(data.Nodes) != 3 ||
		data.Nodes[0].ID != "go:package:example.com/fixture" ||
		data.Nodes[1].ID != "file:main.go" ||
		data.Nodes[2].ID != "go:symbol:example.com/fixture:Service" {
		t.Fatalf("unexpected path nodes: %+v", data.Nodes)
	}
	if len(data.Edges) != 2 ||
		data.Edges[0].Relation != "CONTAINS" ||
		data.Edges[1].Relation != "DECLARES" {
		t.Fatalf("unexpected path edges: %+v", data.Edges)
	}
	if !capabilityAvailable(result.Capabilities, "graph.path") {
		t.Fatalf("path capability missing: %+v", result.Capabilities)
	}
}

func TestExecutePathAnyCanTraverseAgainstStoredDirection(t *testing.T) {
	repo := newGoRepository(t)
	ctx := context.Background()

	discovered := status(t, ctx, repo)
	scan(t, ctx, repo, discovered.Repository.Revision)

	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "path-reverse",
		Operation:     apiv1.OperationPath,
		Repository: apiv1.Repository{
			Root:     repo,
			Revision: discovered.Repository.Revision,
		},
		Arguments: map[string]any{
			"from": "go:symbol:example.com/fixture:Service",
			"to":   "go:package:example.com/fixture",
		},
	})
	if result.Error != nil {
		t.Fatalf("path error: %+v", result.Error)
	}

	var data apiv1.PathData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode path result: %v", err)
	}
	if !data.Found || data.Depth != 2 || data.Direction != "any" {
		t.Fatalf("reverse path = %+v", data)
	}
	if data.Edges[0].From != "file:main.go" ||
		data.Edges[0].To != "go:symbol:example.com/fixture:Service" {
		t.Fatalf("stored edge direction was rewritten: %+v", data.Edges[0])
	}
}

func TestExecutePathReturnsNotFoundWithinDepth(t *testing.T) {
	repo := newGoRepository(t)
	ctx := context.Background()
	discovered := status(t, ctx, repo)
	scan(t, ctx, repo, discovered.Repository.Revision)

	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "path-too-shallow",
		Operation:     apiv1.OperationPath,
		Repository: apiv1.Repository{
			Root:     repo,
			Revision: discovered.Repository.Revision,
		},
		Arguments: map[string]any{
			"from":      "go:package:example.com/fixture",
			"to":        "go:symbol:example.com/fixture:Service",
			"max_depth": 1,
		},
	})
	if result.Error != nil {
		t.Fatalf("path error: %+v", result.Error)
	}

	var data apiv1.PathData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode path result: %v", err)
	}
	if data.Found {
		t.Fatalf("path exceeded max depth: %+v", data)
	}
}

func TestExecutePathRejectsStaleIndex(t *testing.T) {
	repo := newGoRepository(t)
	ctx := context.Background()
	initial := status(t, ctx, repo)
	scan(t, ctx, repo, initial.Repository.Revision)

	mainPath := filepath.Join(repo, "main.go")
	content, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if err := os.WriteFile(mainPath, append(content, []byte("\nfunc ChangedPath() {}\n")...), 0o644); err != nil {
		t.Fatalf("modify main.go: %v", err)
	}

	current := status(t, ctx, repo)
	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "path-stale",
		Operation:     apiv1.OperationPath,
		Repository: apiv1.Repository{
			Root:     repo,
			Revision: current.Repository.Revision,
		},
		Arguments: map[string]any{
			"from": "go:package:example.com/fixture",
			"to":   "file:main.go",
		},
	})
	if result.Error == nil || result.Error.Code != "INDEX_NOT_CURRENT" {
		t.Fatalf("error = %+v, want INDEX_NOT_CURRENT", result.Error)
	}
}

func TestParsePathArgumentsRejectsInvalidInput(t *testing.T) {
	if _, _, err := parsePathArguments(map[string]any{"from": "a"}); err == nil {
		t.Fatal("expected missing to error")
	}
	if _, _, err := parsePathArguments(map[string]any{
		"from": "a", "to": "b", "direction": "sideways",
	}); err == nil {
		t.Fatal("expected invalid direction error")
	}
	if _, _, err := parsePathArguments(map[string]any{
		"from": "a", "to": "b", "max_depth": 13,
	}); err == nil {
		t.Fatal("expected max depth error")
	}
}
