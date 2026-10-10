package graph

import (
	"errors"
	"testing"
)

func TestShortestPathAnyDirectionUsesFewestEdges(t *testing.T) {
	nodes := []Node{
		{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"},
	}
	edges := []Edge{
		{From: "a", To: "b", Relation: "R"},
		{From: "b", To: "d", Relation: "R"},
		{From: "a", To: "c", Relation: "R"},
		{From: "c", To: "d", Relation: "R"},
	}

	got, err := ShortestPath(nodes, edges, "a", "d", DirectionAny, 4)
	if err != nil {
		t.Fatalf("shortest path: %v", err)
	}
	if !got.Found || len(got.Edges) != 2 {
		t.Fatalf("path = %+v, want depth 2", got)
	}
	if got.Nodes[1].ID != "b" {
		t.Fatalf("tie-break chose %q, want lexicographic b", got.Nodes[1].ID)
	}
}

func TestShortestPathRespectsDirection(t *testing.T) {
	nodes := []Node{{ID: "package"}, {ID: "file"}, {ID: "symbol"}}
	edges := []Edge{
		{From: "package", To: "file", Relation: "CONTAINS"},
		{From: "file", To: "symbol", Relation: "DECLARES"},
	}

	outbound, err := ShortestPath(nodes, edges, "package", "symbol", DirectionOutbound, 3)
	if err != nil || !outbound.Found {
		t.Fatalf("outbound path = %+v err=%v", outbound, err)
	}

	reverse, err := ShortestPath(nodes, edges, "symbol", "package", DirectionOutbound, 3)
	if err != nil {
		t.Fatalf("reverse outbound: %v", err)
	}
	if reverse.Found {
		t.Fatalf("reverse outbound unexpectedly found path: %+v", reverse)
	}

	inbound, err := ShortestPath(nodes, edges, "symbol", "package", DirectionInbound, 3)
	if err != nil || !inbound.Found {
		t.Fatalf("inbound path = %+v err=%v", inbound, err)
	}
}

func TestShortestPathHonorsMaxDepth(t *testing.T) {
	nodes := []Node{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	edges := []Edge{
		{From: "a", To: "b"},
		{From: "b", To: "c"},
	}

	got, err := ShortestPath(nodes, edges, "a", "c", DirectionAny, 1)
	if err != nil {
		t.Fatalf("shortest path: %v", err)
	}
	if got.Found {
		t.Fatalf("path exceeded max depth: %+v", got)
	}
}

func TestShortestPathRejectsUnknownNode(t *testing.T) {
	_, err := ShortestPath([]Node{{ID: "a"}}, nil, "a", "missing", DirectionAny, 2)
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("error = %v, want ErrNodeNotFound", err)
	}
}
