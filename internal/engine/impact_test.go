package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	apiv1 "github.com/LuigiAPCPereira/agent-protocol-codebase/api/v1"
)

func TestExecuteImpactReturnsStructuralDependents(t *testing.T) {
	repo := newGoMultiPackageRepository(t)
	ctx := context.Background()

	discovered := status(t, ctx, repo)
	scan(t, ctx, repo, discovered.Repository.Revision)

	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "impact-service",
		Operation:     apiv1.OperationImpact,
		Repository: apiv1.Repository{
			Root:     repo,
			Revision: discovered.Repository.Revision,
		},
		Arguments: map[string]any{
			"target": "go:symbol:example.com/fixture/lib:Service",
		},
	})
	if result.Error != nil {
		t.Fatalf("impact error: %+v", result.Error)
	}

	var data apiv1.ImpactData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode impact result: %v", err)
	}
	if data.Semantics != "structural-dependents" {
		t.Fatalf("semantics = %q", data.Semantics)
	}

	want := []struct {
		id    string
		depth int
	}{
		{"file:lib/lib.go", 1},
		{"go:package:example.com/fixture/lib", 2},
		{"go:package:example.com/fixture/app", 3},
	}
	if len(data.Affected) != len(want) {
		t.Fatalf("affected = %+v, want %d entries", data.Affected, len(want))
	}
	for i, expected := range want {
		if data.Affected[i].Node.ID != expected.id || data.Affected[i].Depth != expected.depth {
			t.Fatalf("affected[%d] = %+v, want %s depth=%d", i, data.Affected[i], expected.id, expected.depth)
		}
	}
	if len(data.Edges) != 3 {
		t.Fatalf("edges = %+v, want 3 explanation edges", data.Edges)
	}
	if !capabilityAvailable(result.Capabilities, "graph.impact") {
		t.Fatalf("impact capability missing: %+v", result.Capabilities)
	}
}

func TestExecuteImpactDoesNotClaimBreakageOrTraverseContainmentForward(t *testing.T) {
	repo := newGoMultiPackageRepository(t)
	ctx := context.Background()

	discovered := status(t, ctx, repo)
	scan(t, ctx, repo, discovered.Repository.Revision)

	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "impact-package",
		Operation:     apiv1.OperationImpact,
		Repository: apiv1.Repository{
			Root:     repo,
			Revision: discovered.Repository.Revision,
		},
		Arguments: map[string]any{
			"target": "go:package:example.com/fixture/app",
		},
	})
	if result.Error != nil {
		t.Fatalf("impact error: %+v", result.Error)
	}

	var data apiv1.ImpactData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode impact result: %v", err)
	}
	if len(data.Affected) != 0 {
		t.Fatalf("top-level importing package should have no structural dependents here: %+v", data.Affected)
	}
}

func TestExecuteImpactRejectsStaleIndex(t *testing.T) {
	repo := newGoMultiPackageRepository(t)
	ctx := context.Background()
	initial := status(t, ctx, repo)
	scan(t, ctx, repo, initial.Repository.Revision)

	path := filepath.Join(repo, "lib", "lib.go")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read lib.go: %v", err)
	}
	if err := os.WriteFile(path, append(content, []byte("\nfunc ChangedImpact() {}\n")...), 0o644); err != nil {
		t.Fatalf("modify lib.go: %v", err)
	}

	current := status(t, ctx, repo)
	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "impact-stale",
		Operation:     apiv1.OperationImpact,
		Repository: apiv1.Repository{
			Root:     repo,
			Revision: current.Repository.Revision,
		},
		Arguments: map[string]any{
			"target": "go:package:example.com/fixture/lib",
		},
	})
	if result.Error == nil || result.Error.Code != "INDEX_NOT_CURRENT" {
		t.Fatalf("error = %+v, want INDEX_NOT_CURRENT", result.Error)
	}
}

func TestParseImpactArgumentsRejectsInvalidInput(t *testing.T) {
	if _, err := parseImpactArguments(nil); err == nil {
		t.Fatal("expected missing target error")
	}
	if _, err := parseImpactArguments(map[string]any{
		"target": "x", "max_depth": 9,
	}); err == nil {
		t.Fatal("expected max depth error")
	}
	if _, err := parseImpactArguments(map[string]any{
		"target": "x", "limit": 251,
	}); err == nil {
		t.Fatal("expected limit error")
	}
	if _, err := parseImpactArguments(map[string]any{
		"target": "x", "unknown": true,
	}); err == nil {
		t.Fatal("expected unknown argument error")
	}
}
