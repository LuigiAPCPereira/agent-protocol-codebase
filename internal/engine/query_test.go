package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	apiv1 "github.com/LuigiAPCPereira/agent-protocol-codebase/api/v1"
)

func TestExecuteQueryReturnsMinimalIncidentSubgraph(t *testing.T) {
	repo := newGoRepository(t)
	ctx := context.Background()

	discovered := status(t, ctx, repo)
	scan(t, ctx, repo, discovered.Repository.Revision)

	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "query-service",
		Operation:     apiv1.OperationQuery,
		Repository: apiv1.Repository{
			Root:     repo,
			Revision: discovered.Repository.Revision,
		},
		Arguments: map[string]any{
			"name": "service",
		},
	})
	if result.Error != nil {
		t.Fatalf("query error: %+v", result.Error)
	}

	var data apiv1.QueryData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode query result: %v", err)
	}
	if len(data.Matches) != 1 ||
		data.Matches[0] != "go:symbol:example.com/fixture:Service" {
		t.Fatalf("matches = %v", data.Matches)
	}

	foundService := false
	foundFile := false
	for _, node := range data.Nodes {
		switch node.ID {
		case "go:symbol:example.com/fixture:Service":
			foundService = true
		case "file:main.go":
			foundFile = true
		}
	}
	if !foundService || !foundFile {
		t.Fatalf("query did not return match and direct endpoint: %+v", data.Nodes)
	}

	foundDeclares := false
	for _, edge := range data.Edges {
		if edge.Relation == "DECLARES" &&
			edge.To == "go:symbol:example.com/fixture:Service" {
			foundDeclares = true
			break
		}
	}
	if !foundDeclares {
		t.Fatalf("query did not return direct DECLARES edge: %+v", data.Edges)
	}
	if !capabilityAvailable(result.Capabilities, "graph.query") {
		t.Fatalf("query capability missing: %+v", result.Capabilities)
	}
}

func TestExecuteQueryRequiresSelector(t *testing.T) {
	repo := newGoRepository(t)
	ctx := context.Background()
	discovered := status(t, ctx, repo)

	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "query-empty",
		Operation:     apiv1.OperationQuery,
		Repository: apiv1.Repository{
			Root:     repo,
			Revision: discovered.Repository.Revision,
		},
	})
	if result.Error == nil || result.Error.Code != "INVALID_QUERY" {
		t.Fatalf("error = %+v, want INVALID_QUERY", result.Error)
	}
}

func TestExecuteQueryRejectsStaleIndex(t *testing.T) {
	repo := newGoRepository(t)
	ctx := context.Background()

	initial := status(t, ctx, repo)
	scan(t, ctx, repo, initial.Repository.Revision)

	mainPath := filepath.Join(repo, "main.go")
	content, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if err := os.WriteFile(mainPath, append(content, []byte("\nfunc Changed() {}\n")...), 0o644); err != nil {
		t.Fatalf("modify main.go: %v", err)
	}

	current := status(t, ctx, repo)
	if current.Index.State != apiv1.IndexStale {
		t.Fatalf("precondition: index state = %q, want STALE", current.Index.State)
	}

	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "query-stale",
		Operation:     apiv1.OperationQuery,
		Repository: apiv1.Repository{
			Root:     repo,
			Revision: current.Repository.Revision,
		},
		Arguments: map[string]any{
			"name": "Service",
		},
	})
	if result.Error == nil || result.Error.Code != "INDEX_NOT_CURRENT" {
		t.Fatalf("error = %+v, want INDEX_NOT_CURRENT", result.Error)
	}
}

func TestParseQueryArgumentsRejectsUnknownAndExcessiveLimit(t *testing.T) {
	if _, err := parseQueryArguments(map[string]any{"unknown": "x"}); err == nil {
		t.Fatal("expected unknown argument error")
	}
	if _, err := parseQueryArguments(map[string]any{"name": "x", "limit": 51}); err == nil {
		t.Fatal("expected limit validation error")
	}
}
