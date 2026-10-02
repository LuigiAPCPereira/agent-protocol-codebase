package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	apiv1 "github.com/LuigiAPCPereira/agent-protocol-codebase/api/v1"
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/graph"
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/repository"
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/store"
)

func executePath(ctx context.Context, req apiv1.Request, result apiv1.Result) apiv1.Result {
	args, direction, err := parsePathArguments(req.Arguments)
	if err != nil {
		result.Error = &apiv1.APIError{
			Code:    "INVALID_PATH",
			Message: err.Error(),
		}
		return result
	}

	state, err := inspectRepository(ctx, req.Repository.Root)
	if err != nil {
		return repositoryInspectionError(result, err)
	}
	result.Repository = observedRepository(req.Repository.ID, state)
	if expectationErr := verifyExpectation(req.Repository.Revision, result.Repository.Revision); expectationErr != nil {
		result.Error = expectationErr
		return result
	}

	result.Index, result.Capabilities = persistedIndexStatus(ctx, state)
	switch result.Index.State {
	case apiv1.IndexAbsent:
		result.Error = &apiv1.APIError{
			Code:    "INDEX_REQUIRED",
			Message: "path requires a persisted codebase index; run scan first",
		}
		return result
	case apiv1.IndexStale:
		result.Error = &apiv1.APIError{
			Code:    "INDEX_NOT_CURRENT",
			Message: "persisted codebase index does not match the observed repository; run scan again",
		}
		return result
	case apiv1.IndexInvalid:
		result.Error = &apiv1.APIError{
			Code:    "INDEX_INVALID",
			Message: "persisted codebase index cannot be used safely; rebuild it with scan",
		}
		return result
	case apiv1.IndexExact, apiv1.IndexPartial:
	default:
		result.Error = &apiv1.APIError{
			Code:    "INDEX_INVALID",
			Message: fmt.Sprintf("unsupported index state %q", result.Index.State),
		}
		return result
	}

	if !capabilityAvailableByName(result.Capabilities, "graph") {
		result.Error = &apiv1.APIError{
			Code:    "GRAPH_UNAVAILABLE",
			Message: "the current index contains no traversable graph facts",
		}
		return result
	}

	indexPath, err := repository.IndexPath(ctx, state.Root)
	if err != nil {
		result.Error = &apiv1.APIError{
			Code:    "INDEX_PATH_FAILED",
			Message: err.Error(),
		}
		return result
	}
	index, err := store.Load(ctx, indexPath)
	if err != nil {
		result.Error = &apiv1.APIError{
			Code:    "PATH_FAILED",
			Message: err.Error(),
		}
		return result
	}

	path, err := graph.ShortestPath(
		graphNodes(index.Nodes),
		graphEdges(index.Edges),
		args.From,
		args.To,
		direction,
		args.MaxDepth,
	)
	if errors.Is(err, graph.ErrNodeNotFound) {
		result.Error = &apiv1.APIError{
			Code:    "NODE_NOT_FOUND",
			Message: err.Error(),
		}
		return result
	}
	if err != nil {
		result.Error = &apiv1.APIError{
			Code:    "PATH_FAILED",
			Message: err.Error(),
		}
		return result
	}

	data := apiv1.PathData{
		Found:     path.Found,
		Direction: string(direction),
		Depth:     len(path.Edges),
		Nodes:     make([]apiv1.GraphNode, 0, len(path.Nodes)),
		Edges:     make([]apiv1.GraphEdge, 0, len(path.Edges)),
	}
	for _, node := range path.Nodes {
		data.Nodes = append(data.Nodes, apiGraphNode(node))
	}
	for _, edge := range path.Edges {
		data.Edges = append(data.Edges, apiGraphEdge(edge))
	}

	encoded, err := json.Marshal(data)
	if err != nil {
		return internalError(result, err)
	}
	result.Data = encoded
	result.Capabilities = append(result.Capabilities, apiv1.Capability{
		Name:       "graph.path",
		Available:  true,
		Resolution: "shortest-unweighted",
	})
	return result
}

func parsePathArguments(raw map[string]any) (apiv1.PathArguments, graph.Direction, error) {
	allowed := map[string]struct{}{
		"from":      {},
		"to":        {},
		"direction": {},
		"max_depth": {},
	}
	for key := range raw {
		if _, ok := allowed[key]; !ok {
			return apiv1.PathArguments{}, "", fmt.Errorf("unsupported path argument %q", key)
		}
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		return apiv1.PathArguments{}, "", fmt.Errorf("encode path arguments: %w", err)
	}

	var args apiv1.PathArguments
	if err := json.Unmarshal(encoded, &args); err != nil {
		return apiv1.PathArguments{}, "", fmt.Errorf("decode path arguments: %w", err)
	}
	args.From = strings.TrimSpace(args.From)
	args.To = strings.TrimSpace(args.To)
	args.Direction = strings.ToLower(strings.TrimSpace(args.Direction))

	if args.From == "" || args.To == "" {
		return apiv1.PathArguments{}, "", fmt.Errorf("from and to node IDs are required")
	}
	if args.Direction == "" {
		args.Direction = string(graph.DirectionAny)
	}

	direction := graph.Direction(args.Direction)
	switch direction {
	case graph.DirectionAny, graph.DirectionOutbound, graph.DirectionInbound:
	default:
		return apiv1.PathArguments{}, "", fmt.Errorf(
			"direction must be one of any, outbound, or inbound",
		)
	}

	if args.MaxDepth == 0 {
		args.MaxDepth = 6
	}
	if args.MaxDepth < 1 || args.MaxDepth > 12 {
		return apiv1.PathArguments{}, "", fmt.Errorf("max_depth must be between 1 and 12")
	}
	return args, direction, nil
}

func graphNodes(nodes []store.Node) []graph.Node {
	out := make([]graph.Node, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, graph.Node{
			ID:          node.ID,
			Kind:        node.Kind,
			Name:        node.Name,
			Path:        node.Path,
			Language:    node.Language,
			PackagePath: node.PackagePath,
			StartLine:   node.StartLine,
			EndLine:     node.EndLine,
			External:    node.External,
		})
	}
	return out
}

func graphEdges(edges []store.Edge) []graph.Edge {
	out := make([]graph.Edge, 0, len(edges))
	for _, edge := range edges {
		out = append(out, graph.Edge{
			From:       edge.From,
			To:         edge.To,
			Relation:   edge.Relation,
			Evidence:   edge.Evidence,
			Resolution: edge.Resolution,
			Extractor:  edge.Extractor,
			SourcePath: edge.SourcePath,
			StartLine:  edge.StartLine,
		})
	}
	return out
}

func apiGraphNode(node graph.Node) apiv1.GraphNode {
	return apiv1.GraphNode{
		ID:          node.ID,
		Kind:        node.Kind,
		Name:        node.Name,
		Path:        node.Path,
		Language:    node.Language,
		PackagePath: node.PackagePath,
		StartLine:   node.StartLine,
		EndLine:     node.EndLine,
		External:    node.External,
	}
}

func apiGraphEdge(edge graph.Edge) apiv1.GraphEdge {
	return apiv1.GraphEdge{
		From:       edge.From,
		To:         edge.To,
		Relation:   edge.Relation,
		Evidence:   edge.Evidence,
		Resolution: edge.Resolution,
		Extractor:  edge.Extractor,
		SourcePath: edge.SourcePath,
		StartLine:  edge.StartLine,
	}
}
