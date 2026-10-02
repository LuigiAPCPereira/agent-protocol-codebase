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

func executeImpact(ctx context.Context, req apiv1.Request, result apiv1.Result) apiv1.Result {
	args, err := parseImpactArguments(req.Arguments)
	if err != nil {
		result.Error = &apiv1.APIError{
			Code:    "INVALID_IMPACT",
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
			Message: "impact requires a persisted codebase index; run scan first",
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
			Message: "the current index contains no graph facts for structural impact analysis",
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
			Code:    "IMPACT_FAILED",
			Message: err.Error(),
		}
		return result
	}

	impact, err := graph.StructuralDependents(
		graphNodes(index.Nodes),
		graphEdges(index.Edges),
		args.Target,
		args.MaxDepth,
		args.Limit,
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
			Code:    "IMPACT_FAILED",
			Message: err.Error(),
		}
		return result
	}

	data := apiv1.ImpactData{
		Target:    apiGraphNode(impact.Target),
		Affected:  make([]apiv1.ImpactEntry, 0, len(impact.Affected)),
		Edges:     make([]apiv1.GraphEdge, 0, len(impact.Edges)),
		Semantics: graph.ImpactSemanticsStructuralDependents,
		Truncated: impact.Truncated,
	}
	for _, entry := range impact.Affected {
		data.Affected = append(data.Affected, apiv1.ImpactEntry{
			Node:  apiGraphNode(entry.Node),
			Depth: entry.Depth,
		})
	}
	for _, edge := range impact.Edges {
		data.Edges = append(data.Edges, apiGraphEdge(edge))
	}

	encoded, err := json.Marshal(data)
	if err != nil {
		return internalError(result, err)
	}
	result.Data = encoded
	result.Capabilities = append(result.Capabilities, apiv1.Capability{
		Name:       "graph.impact",
		Available:  true,
		Resolution: graph.ImpactSemanticsStructuralDependents,
	})
	return result
}

func parseImpactArguments(raw map[string]any) (apiv1.ImpactArguments, error) {
	allowed := map[string]struct{}{
		"target":    {},
		"max_depth": {},
		"limit":     {},
	}
	for key := range raw {
		if _, ok := allowed[key]; !ok {
			return apiv1.ImpactArguments{}, fmt.Errorf("unsupported impact argument %q", key)
		}
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		return apiv1.ImpactArguments{}, fmt.Errorf("encode impact arguments: %w", err)
	}

	var args apiv1.ImpactArguments
	if err := json.Unmarshal(encoded, &args); err != nil {
		return apiv1.ImpactArguments{}, fmt.Errorf("decode impact arguments: %w", err)
	}
	args.Target = strings.TrimSpace(args.Target)
	if args.Target == "" {
		return apiv1.ImpactArguments{}, fmt.Errorf("target node ID is required")
	}
	if args.MaxDepth == 0 {
		args.MaxDepth = 4
	}
	if args.MaxDepth < 1 || args.MaxDepth > 8 {
		return apiv1.ImpactArguments{}, fmt.Errorf("max_depth must be between 1 and 8")
	}
	if args.Limit == 0 {
		args.Limit = 100
	}
	if args.Limit < 1 || args.Limit > 250 {
		return apiv1.ImpactArguments{}, fmt.Errorf("limit must be between 1 and 250")
	}
	return args, nil
}
