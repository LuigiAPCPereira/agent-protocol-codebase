package delta

import (
	"sort"
	"strconv"
	"strings"

	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/graph"
)

type Source struct {
	Path        string
	Kind        string
	Executable  bool
	ContentHash string
	Size        int64
}

type Index struct {
	Sources []Source
	Nodes   []graph.Node
	Edges   []graph.Edge
}

type SourceChange struct {
	Before Source
	After  Source
}

type NodeChange struct {
	Before graph.Node
	After  graph.Node
}

type Result struct {
	SourceAdded   []Source
	SourceRemoved []Source
	SourceChanged []SourceChange
	NodeAdded     []graph.Node
	NodeRemoved   []graph.Node
	NodeChanged   []NodeChange
	EdgeAdded     []graph.Edge
	EdgeRemoved   []graph.Edge
	SourceCounts  Counts
	NodeCounts    Counts
	EdgeCounts    EdgeCounts
	Truncated     bool
}

type Counts struct {
	Added   int
	Removed int
	Changed int
}

type EdgeCounts struct {
	Added   int
	Removed int
}

func Compare(base, head Index, limit int) Result {
	if limit < 1 {
		limit = 1
	}

	var out Result
	out.SourceAdded, out.SourceRemoved, out.SourceChanged =
		diffSources(base.Sources, head.Sources)
	out.NodeAdded, out.NodeRemoved, out.NodeChanged =
		diffNodes(base.Nodes, head.Nodes)
	out.EdgeAdded, out.EdgeRemoved = diffEdges(base.Edges, head.Edges)

	out.SourceCounts = Counts{
		Added:   len(out.SourceAdded),
		Removed: len(out.SourceRemoved),
		Changed: len(out.SourceChanged),
	}
	out.NodeCounts = Counts{
		Added:   len(out.NodeAdded),
		Removed: len(out.NodeRemoved),
		Changed: len(out.NodeChanged),
	}
	out.EdgeCounts = EdgeCounts{
		Added:   len(out.EdgeAdded),
		Removed: len(out.EdgeRemoved),
	}

	out.SourceAdded, out.Truncated = capSlice(out.SourceAdded, limit, out.Truncated)
	out.SourceRemoved, out.Truncated = capSlice(out.SourceRemoved, limit, out.Truncated)
	out.SourceChanged, out.Truncated = capSlice(out.SourceChanged, limit, out.Truncated)
	out.NodeAdded, out.Truncated = capSlice(out.NodeAdded, limit, out.Truncated)
	out.NodeRemoved, out.Truncated = capSlice(out.NodeRemoved, limit, out.Truncated)
	out.NodeChanged, out.Truncated = capSlice(out.NodeChanged, limit, out.Truncated)
	out.EdgeAdded, out.Truncated = capSlice(out.EdgeAdded, limit, out.Truncated)
	out.EdgeRemoved, out.Truncated = capSlice(out.EdgeRemoved, limit, out.Truncated)
	return out
}

func diffSources(base, head []Source) ([]Source, []Source, []SourceChange) {
	baseByPath := make(map[string]Source, len(base))
	headByPath := make(map[string]Source, len(head))
	for _, source := range base {
		baseByPath[source.Path] = source
	}
	for _, source := range head {
		headByPath[source.Path] = source
	}

	var added []Source
	var removed []Source
	var changed []SourceChange

	for path, before := range baseByPath {
		after, ok := headByPath[path]
		if !ok {
			removed = append(removed, before)
			continue
		}
		if before != after {
			changed = append(changed, SourceChange{Before: before, After: after})
		}
	}
	for path, after := range headByPath {
		if _, ok := baseByPath[path]; !ok {
			added = append(added, after)
		}
	}

	sort.Slice(added, func(i, j int) bool { return added[i].Path < added[j].Path })
	sort.Slice(removed, func(i, j int) bool { return removed[i].Path < removed[j].Path })
	sort.Slice(changed, func(i, j int) bool {
		return changed[i].Before.Path < changed[j].Before.Path
	})
	return added, removed, changed
}

func diffNodes(base, head []graph.Node) ([]graph.Node, []graph.Node, []NodeChange) {
	baseByID := make(map[string]graph.Node, len(base))
	headByID := make(map[string]graph.Node, len(head))
	for _, node := range base {
		baseByID[node.ID] = node
	}
	for _, node := range head {
		headByID[node.ID] = node
	}

	var added []graph.Node
	var removed []graph.Node
	var changed []NodeChange

	for id, before := range baseByID {
		after, ok := headByID[id]
		if !ok {
			removed = append(removed, before)
			continue
		}
		if before != after {
			changed = append(changed, NodeChange{Before: before, After: after})
		}
	}
	for id, after := range headByID {
		if _, ok := baseByID[id]; !ok {
			added = append(added, after)
		}
	}

	sort.Slice(added, func(i, j int) bool { return added[i].ID < added[j].ID })
	sort.Slice(removed, func(i, j int) bool { return removed[i].ID < removed[j].ID })
	sort.Slice(changed, func(i, j int) bool {
		return changed[i].Before.ID < changed[j].Before.ID
	})
	return added, removed, changed
}

func diffEdges(base, head []graph.Edge) ([]graph.Edge, []graph.Edge) {
	baseByKey := make(map[string]graph.Edge, len(base))
	headByKey := make(map[string]graph.Edge, len(head))
	for _, edge := range base {
		baseByKey[edgeKey(edge)] = edge
	}
	for _, edge := range head {
		headByKey[edgeKey(edge)] = edge
	}

	var added []graph.Edge
	var removed []graph.Edge
	for key, edge := range baseByKey {
		if _, ok := headByKey[key]; !ok {
			removed = append(removed, edge)
		}
	}
	for key, edge := range headByKey {
		if _, ok := baseByKey[key]; !ok {
			added = append(added, edge)
		}
	}

	sort.Slice(added, func(i, j int) bool { return edgeKey(added[i]) < edgeKey(added[j]) })
	sort.Slice(removed, func(i, j int) bool { return edgeKey(removed[i]) < edgeKey(removed[j]) })
	return added, removed
}

func edgeKey(edge graph.Edge) string {
	return strings.Join([]string{
		edge.From,
		edge.To,
		edge.Relation,
		edge.Evidence,
		edge.Resolution,
		edge.Extractor,
		edge.SourcePath,
		strconv.Itoa(edge.StartLine),
	}, "\x00")
}

func capSlice[T any](in []T, limit int, truncated bool) ([]T, bool) {
	if len(in) > limit {
		return in[:limit], true
	}
	return in, truncated
}
