package graph

import "testing"

func TestStructuralDependentsWalksOwnershipAndImportsInReverse(t *testing.T) {
	nodes := []Node{
		{ID: "symbol"},
		{ID: "file"},
		{ID: "package"},
		{ID: "importer"},
	}
	edges := []Edge{
		{From: "file", To: "symbol", Relation: "DECLARES"},
		{From: "package", To: "file", Relation: "CONTAINS"},
		{From: "importer", To: "package", Relation: "IMPORTS"},
	}

	got, err := StructuralDependents(nodes, edges, "symbol", 4, 10)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if len(got.Affected) != 3 {
		t.Fatalf("affected = %+v, want 3 entries", got.Affected)
	}
	wantIDs := []string{"file", "package", "importer"}
	wantDepths := []int{1, 2, 3}
	for i, entry := range got.Affected {
		if entry.Node.ID != wantIDs[i] || entry.Depth != wantDepths[i] {
			t.Fatalf("affected[%d] = %+v, want id=%s depth=%d", i, entry, wantIDs[i], wantDepths[i])
		}
	}
	if got.Edges[0].From != "file" || got.Edges[0].To != "symbol" {
		t.Fatalf("stored edge direction was rewritten: %+v", got.Edges[0])
	}
}

func TestStructuralDependentsDoesNotTreatArbitraryRelationsAsImpact(t *testing.T) {
	nodes := []Node{{ID: "callee"}, {ID: "caller"}}
	edges := []Edge{{From: "caller", To: "callee", Relation: "CALLS"}}

	got, err := StructuralDependents(nodes, edges, "callee", 3, 10)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if len(got.Affected) != 0 || len(got.Edges) != 0 {
		t.Fatalf("CALLS entered impact policy implicitly: %+v", got)
	}
}

func TestStructuralDependentsHonorsDepthAndLimit(t *testing.T) {
	nodes := []Node{
		{ID: "dep"},
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
	}
	edges := []Edge{
		{From: "a", To: "dep", Relation: "IMPORTS"},
		{From: "b", To: "dep", Relation: "IMPORTS"},
		{From: "c", To: "a", Relation: "IMPORTS"},
	}

	shallow, err := StructuralDependents(nodes, edges, "dep", 1, 10)
	if err != nil {
		t.Fatalf("shallow impact: %v", err)
	}
	if len(shallow.Affected) != 2 {
		t.Fatalf("shallow affected = %+v, want only direct dependents", shallow.Affected)
	}

	limited, err := StructuralDependents(nodes, edges, "dep", 4, 1)
	if err != nil {
		t.Fatalf("limited impact: %v", err)
	}
	if len(limited.Affected) != 1 || !limited.Truncated {
		t.Fatalf("limited impact = %+v, want one entry and truncated", limited)
	}
}

func TestStructuralDependentsIsDeterministic(t *testing.T) {
	nodes := []Node{{ID: "dep"}, {ID: "z"}, {ID: "a"}}
	edges := []Edge{
		{From: "z", To: "dep", Relation: "IMPORTS"},
		{From: "a", To: "dep", Relation: "IMPORTS"},
	}

	got, err := StructuralDependents(nodes, edges, "dep", 2, 10)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if len(got.Affected) != 2 || got.Affected[0].Node.ID != "a" || got.Affected[1].Node.ID != "z" {
		t.Fatalf("affected order = %+v, want lexical a,z", got.Affected)
	}
}
