package graph

import (
	"errors"
	"fmt"
	"sort"
)

type Direction string

const (
	DirectionAny      Direction = "any"
	DirectionOutbound Direction = "outbound"
	DirectionInbound  Direction = "inbound"
)

var ErrNodeNotFound = errors.New("graph node not found")

type Path struct {
	Found bool
	Nodes []Node
	Edges []Edge
}

type arc struct {
	Next string
	Edge Edge
}

type previous struct {
	Node string
	Edge Edge
}

func ShortestPath(nodes []Node, edges []Edge, from, to string, direction Direction, maxDepth int) (Path, error) {
	nodeByID := make(map[string]Node, len(nodes))
	for _, node := range nodes {
		nodeByID[node.ID] = node
	}
	if _, ok := nodeByID[from]; !ok {
		return Path{}, fmt.Errorf("%w: %s", ErrNodeNotFound, from)
	}
	if _, ok := nodeByID[to]; !ok {
		return Path{}, fmt.Errorf("%w: %s", ErrNodeNotFound, to)
	}
	if from == to {
		return Path{Found: true, Nodes: []Node{nodeByID[from]}}, nil
	}
	if maxDepth < 1 {
		return Path{}, nil
	}

	adj := make(map[string][]arc)
	for _, edge := range edges {
		switch direction {
		case DirectionAny:
			adj[edge.From] = append(adj[edge.From], arc{Next: edge.To, Edge: edge})
			adj[edge.To] = append(adj[edge.To], arc{Next: edge.From, Edge: edge})
		case DirectionOutbound:
			adj[edge.From] = append(adj[edge.From], arc{Next: edge.To, Edge: edge})
		case DirectionInbound:
			adj[edge.To] = append(adj[edge.To], arc{Next: edge.From, Edge: edge})
		default:
			return Path{}, fmt.Errorf("unsupported direction %q", direction)
		}
	}

	for key := range adj {
		sort.Slice(adj[key], func(i, j int) bool {
			if adj[key][i].Next != adj[key][j].Next {
				return adj[key][i].Next < adj[key][j].Next
			}
			if adj[key][i].Edge.Relation != adj[key][j].Edge.Relation {
				return adj[key][i].Edge.Relation < adj[key][j].Edge.Relation
			}
			if adj[key][i].Edge.From != adj[key][j].Edge.From {
				return adj[key][i].Edge.From < adj[key][j].Edge.From
			}
			return adj[key][i].Edge.To < adj[key][j].Edge.To
		})
	}

	type queued struct {
		ID    string
		Depth int
	}
	queue := []queued{{ID: from}}
	visited := map[string]bool{from: true}
	prev := make(map[string]previous)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.Depth >= maxDepth {
			continue
		}

		for _, next := range adj[current.ID] {
			if visited[next.Next] {
				continue
			}
			visited[next.Next] = true
			prev[next.Next] = previous{Node: current.ID, Edge: next.Edge}
			if next.Next == to {
				return reconstructPath(nodeByID, prev, from, to), nil
			}
			queue = append(queue, queued{ID: next.Next, Depth: current.Depth + 1})
		}
	}

	return Path{}, nil
}

func reconstructPath(nodes map[string]Node, prev map[string]previous, from, to string) Path {
	nodeIDs := []string{to}
	var reverseEdges []Edge
	for current := to; current != from; {
		step := prev[current]
		reverseEdges = append(reverseEdges, step.Edge)
		current = step.Node
		nodeIDs = append(nodeIDs, current)
	}

	for i, j := 0, len(nodeIDs)-1; i < j; i, j = i+1, j-1 {
		nodeIDs[i], nodeIDs[j] = nodeIDs[j], nodeIDs[i]
	}
	for i, j := 0, len(reverseEdges)-1; i < j; i, j = i+1, j-1 {
		reverseEdges[i], reverseEdges[j] = reverseEdges[j], reverseEdges[i]
	}

	outNodes := make([]Node, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		outNodes = append(outNodes, nodes[id])
	}
	return Path{
		Found: true,
		Nodes: outNodes,
		Edges: reverseEdges,
	}
}
