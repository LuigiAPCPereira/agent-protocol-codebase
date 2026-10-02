package graph

import (
	"fmt"
	"sort"
)

const ImpactSemanticsStructuralDependents = "structural-dependents"

type ImpactEntry struct {
	Node  Node
	Depth int
}

type Impact struct {
	Target    Node
	Affected  []ImpactEntry
	Edges     []Edge
	Truncated bool
}

type impactArc struct {
	Next string
	Edge Edge
}

func StructuralDependents(
	nodes []Node,
	edges []Edge,
	target string,
	maxDepth int,
	limit int,
) (Impact, error) {
	nodeByID := make(map[string]Node, len(nodes))
	for _, node := range nodes {
		nodeByID[node.ID] = node
	}
	targetNode, ok := nodeByID[target]
	if !ok {
		return Impact{}, fmt.Errorf("%w: %s", ErrNodeNotFound, target)
	}

	impact := Impact{Target: targetNode}
	if maxDepth < 1 || limit < 1 {
		return impact, nil
	}

	adj := make(map[string][]impactArc)
	for _, edge := range edges {
		if !impactRelation(edge.Relation) {
			continue
		}
		// Stored graph relations point from owner/dependent toward the
		// declaration, contained unit, or imported dependency. Structural
		// impact walks the opposite direction: dependency -> dependent.
		adj[edge.To] = append(adj[edge.To], impactArc{
			Next: edge.From,
			Edge: edge,
		})
	}
	for id := range adj {
		sort.Slice(adj[id], func(i, j int) bool {
			if adj[id][i].Next != adj[id][j].Next {
				return adj[id][i].Next < adj[id][j].Next
			}
			if adj[id][i].Edge.Relation != adj[id][j].Edge.Relation {
				return adj[id][i].Edge.Relation < adj[id][j].Edge.Relation
			}
			if adj[id][i].Edge.From != adj[id][j].Edge.From {
				return adj[id][i].Edge.From < adj[id][j].Edge.From
			}
			return adj[id][i].Edge.To < adj[id][j].Edge.To
		})
	}

	type queued struct {
		ID    string
		Depth int
	}
	queue := []queued{{ID: target}}
	visited := map[string]bool{target: true}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.Depth >= maxDepth {
			continue
		}

		for _, arc := range adj[current.ID] {
			if visited[arc.Next] {
				continue
			}
			if len(impact.Affected) >= limit {
				impact.Truncated = true
				return impact, nil
			}

			visited[arc.Next] = true
			depth := current.Depth + 1
			impact.Affected = append(impact.Affected, ImpactEntry{
				Node:  nodeByID[arc.Next],
				Depth: depth,
			})
			impact.Edges = append(impact.Edges, arc.Edge)
			queue = append(queue, queued{ID: arc.Next, Depth: depth})
		}
	}

	return impact, nil
}

func impactRelation(relation string) bool {
	switch relation {
	case "DECLARES", "CONTAINS", "IMPORTS":
		return true
	default:
		return false
	}
}
