package delta

import (
	"testing"

	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/graph"
)

func TestCompareSeparatesSourceNodeAndEdgeChanges(t *testing.T) {
	base := Index{
		Sources: []Source{
			{Path: "a.go", ContentHash: "a"},
			{Path: "gone.go", ContentHash: "g"},
		},
		Nodes: []graph.Node{
			{ID: "n1", Name: "Old"},
			{ID: "gone"},
		},
		Edges: []graph.Edge{
			{From: "n1", To: "gone", Relation: "R"},
		},
	}
	head := Index{
		Sources: []Source{
			{Path: "a.go", ContentHash: "b"},
			{Path: "new.go", ContentHash: "n"},
		},
		Nodes: []graph.Node{
			{ID: "n1", Name: "New"},
			{ID: "new"},
		},
		Edges: []graph.Edge{
			{From: "n1", To: "new", Relation: "R"},
		},
	}

	got := Compare(base, head, 10)
	if got.SourceCounts != (Counts{Added: 1, Removed: 1, Changed: 1}) {
		t.Fatalf("source counts = %+v", got.SourceCounts)
	}
	if got.NodeCounts != (Counts{Added: 1, Removed: 1, Changed: 1}) {
		t.Fatalf("node counts = %+v", got.NodeCounts)
	}
	if got.EdgeCounts != (EdgeCounts{Added: 1, Removed: 1}) {
		t.Fatalf("edge counts = %+v", got.EdgeCounts)
	}
	if got.Truncated {
		t.Fatal("unexpected truncation")
	}
}

func TestCompareTruncatesDetailsButPreservesCounts(t *testing.T) {
	head := Index{
		Sources: []Source{
			{Path: "a"},
			{Path: "b"},
			{Path: "c"},
		},
	}

	got := Compare(Index{}, head, 2)
	if got.SourceCounts.Added != 3 || len(got.SourceAdded) != 2 || !got.Truncated {
		t.Fatalf("got = %+v", got)
	}
}
