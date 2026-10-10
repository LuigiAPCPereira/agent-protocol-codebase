package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	apiv1 "github.com/LuigiAPCPereira/agent-protocol-codebase/api/v1"
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/repository"
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/store"
)

func executeQuery(ctx context.Context, req apiv1.Request, result apiv1.Result) apiv1.Result {
	args, err := parseQueryArguments(req.Arguments)
	if err != nil {
		result.Error = &apiv1.APIError{
			Code:    "INVALID_QUERY",
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
			Message: "query requires a persisted codebase index; run scan first",
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
			Message: "the current index contains no queryable graph facts",
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

	queried, err := store.Query(ctx, indexPath, store.QueryFilter{
		Name:    args.Name,
		Path:    args.Path,
		Kind:    args.Kind,
		Package: args.Package,
		Limit:   args.Limit,
	})
	if err != nil {
		result.Error = &apiv1.APIError{
			Code:    "QUERY_FAILED",
			Message: err.Error(),
		}
		return result
	}

	data := apiv1.QueryData{
		Matches:   append([]string(nil), queried.Matches...),
		Nodes:     make([]apiv1.GraphNode, 0, len(queried.Nodes)),
		Edges:     make([]apiv1.GraphEdge, 0, len(queried.Edges)),
		Truncated: queried.Truncated,
	}
	for _, node := range queried.Nodes {
		data.Nodes = append(data.Nodes, apiv1.GraphNode{
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
	for _, edge := range queried.Edges {
		data.Edges = append(data.Edges, apiv1.GraphEdge{
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

	encoded, err := json.Marshal(data)
	if err != nil {
		return internalError(result, err)
	}
	result.Data = encoded
	result.Capabilities = append(result.Capabilities, apiv1.Capability{
		Name:       "graph.query",
		Available:  true,
		Resolution: "direct-neighborhood",
	})
	return result
}

func parseQueryArguments(raw map[string]any) (apiv1.QueryArguments, error) {
	allowed := map[string]struct{}{
		"name":    {},
		"path":    {},
		"kind":    {},
		"package": {},
		"limit":   {},
	}
	for key := range raw {
		if _, ok := allowed[key]; !ok {
			return apiv1.QueryArguments{}, fmt.Errorf("unsupported query argument %q", key)
		}
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		return apiv1.QueryArguments{}, fmt.Errorf("encode query arguments: %w", err)
	}

	var args apiv1.QueryArguments
	if err := json.Unmarshal(encoded, &args); err != nil {
		return apiv1.QueryArguments{}, fmt.Errorf("decode query arguments: %w", err)
	}

	args.Name = strings.TrimSpace(args.Name)
	args.Path = strings.TrimSpace(args.Path)
	args.Kind = strings.TrimSpace(args.Kind)
	args.Package = strings.TrimSpace(args.Package)

	if args.Name == "" && args.Path == "" && args.Kind == "" && args.Package == "" {
		return apiv1.QueryArguments{}, fmt.Errorf("at least one of name, path, kind, or package is required")
	}
	if args.Limit == 0 {
		args.Limit = 20
	}
	if args.Limit < 1 || args.Limit > 50 {
		return apiv1.QueryArguments{}, fmt.Errorf("limit must be between 1 and 50")
	}
	return args, nil
}

func capabilityAvailableByName(capabilities []apiv1.Capability, name string) bool {
	for _, capability := range capabilities {
		if capability.Name == name {
			return capability.Available
		}
	}
	return false
}
