package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	apiv1 "github.com/LuigiAPCPereira/agent-protocol-codebase/api/v1"
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/delta"
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/graph"
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/repository"
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/store"
)

func executeDiff(ctx context.Context, req apiv1.Request, result apiv1.Result) apiv1.Result {
	args, err := parseDiffArguments(req.Arguments)
	if err != nil {
		result.Error = &apiv1.APIError{
			Code:    "INVALID_DIFF",
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

	indexPath, err := repository.IndexPath(ctx, state.Root)
	if err != nil {
		result.Error = &apiv1.APIError{
			Code:    "INDEX_PATH_FAILED",
			Message: err.Error(),
		}
		return result
	}

	base, err := store.LoadSnapshot(ctx, indexPath, args.BaseSnapshotID)
	if err != nil {
		return diffLoadError(result, args.BaseSnapshotID, err)
	}
	head, err := store.LoadSnapshot(ctx, indexPath, args.HeadSnapshotID)
	if err != nil {
		return diffLoadError(result, args.HeadSnapshotID, err)
	}

	if base.AnalysisVersion != head.AnalysisVersion {
		result.Error = &apiv1.APIError{
			Code: "ANALYSIS_VERSION_MISMATCH",
			Message: fmt.Sprintf(
				"base analysis version %d differs from head analysis version %d",
				base.AnalysisVersion,
				head.AnalysisVersion,
			),
		}
		return result
	}

	compared := delta.Compare(deltaIndex(base), deltaIndex(head), args.Limit)
	data := apiv1.DiffData{
		BaseSnapshotID:  base.SnapshotID,
		HeadSnapshotID:  head.SnapshotID,
		AnalysisVersion: base.AnalysisVersion,
		Sources: apiv1.SourceDiff{
			Counts: apiv1.ChangeCounts{
				Added:   compared.SourceCounts.Added,
				Removed: compared.SourceCounts.Removed,
				Changed: compared.SourceCounts.Changed,
			},
			Added:   apiSources(compared.SourceAdded),
			Removed: apiSources(compared.SourceRemoved),
			Changed: apiSourceChanges(compared.SourceChanged),
		},
		Nodes: apiv1.GraphNodeDiff{
			Counts: apiv1.ChangeCounts{
				Added:   compared.NodeCounts.Added,
				Removed: compared.NodeCounts.Removed,
				Changed: compared.NodeCounts.Changed,
			},
			Added:   apiNodes(compared.NodeAdded),
			Removed: apiNodes(compared.NodeRemoved),
			Changed: apiNodeChanges(compared.NodeChanged),
		},
		Edges: apiv1.GraphEdgeDiff{
			Counts: apiv1.EdgeChangeCounts{
				Added:   compared.EdgeCounts.Added,
				Removed: compared.EdgeCounts.Removed,
			},
			Added:   apiEdges(compared.EdgeAdded),
			Removed: apiEdges(compared.EdgeRemoved),
		},
		Truncated: compared.Truncated,
	}

	encoded, err := json.Marshal(data)
	if err != nil {
		return internalError(result, err)
	}
	result.Data = encoded
	result.Capabilities = append(result.Capabilities, apiv1.Capability{
		Name:       "graph.diff",
		Available:  true,
		Resolution: "snapshot-to-snapshot",
	})
	return result
}

func diffLoadError(result apiv1.Result, snapshotID string, err error) apiv1.Result {
	switch {
	case errors.Is(err, store.ErrNotFound):
		result.Error = &apiv1.APIError{
			Code:    "INDEX_REQUIRED",
			Message: "diff requires persisted snapshot history; run scan first",
		}
	case errors.Is(err, store.ErrSnapshotNotFound):
		result.Error = &apiv1.APIError{
			Code:    "SNAPSHOT_NOT_FOUND",
			Message: fmt.Sprintf("snapshot %q is not retained in the local index history", snapshotID),
		}
	default:
		result.Error = &apiv1.APIError{
			Code:    "DIFF_FAILED",
			Message: err.Error(),
		}
	}
	return result
}

func parseDiffArguments(raw map[string]any) (apiv1.DiffArguments, error) {
	allowed := map[string]struct{}{
		"base_snapshot_id": {},
		"head_snapshot_id": {},
		"limit":            {},
	}
	for key := range raw {
		if _, ok := allowed[key]; !ok {
			return apiv1.DiffArguments{}, fmt.Errorf("unsupported diff argument %q", key)
		}
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		return apiv1.DiffArguments{}, fmt.Errorf("encode diff arguments: %w", err)
	}

	var args apiv1.DiffArguments
	if err := json.Unmarshal(encoded, &args); err != nil {
		return apiv1.DiffArguments{}, fmt.Errorf("decode diff arguments: %w", err)
	}
	args.BaseSnapshotID = strings.TrimSpace(args.BaseSnapshotID)
	args.HeadSnapshotID = strings.TrimSpace(args.HeadSnapshotID)
	if args.BaseSnapshotID == "" || args.HeadSnapshotID == "" {
		return apiv1.DiffArguments{}, fmt.Errorf("base_snapshot_id and head_snapshot_id are required")
	}
	if args.Limit == 0 {
		args.Limit = 100
	}
	if args.Limit < 1 || args.Limit > 250 {
		return apiv1.DiffArguments{}, fmt.Errorf("limit must be between 1 and 250")
	}
	return args, nil
}

func deltaIndex(index store.Index) delta.Index {
	sources := make([]delta.Source, 0, len(index.Sources))
	for _, source := range index.Sources {
		sources = append(sources, delta.Source{
			Path:        source.Path,
			Kind:        source.Kind,
			Executable:  source.Executable,
			ContentHash: source.ContentHash,
			Size:        source.Size,
		})
	}
	return delta.Index{
		Sources: sources,
		Nodes:   graphNodes(index.Nodes),
		Edges:   graphEdges(index.Edges),
	}
}

func apiSource(source delta.Source) apiv1.Source {
	return apiv1.Source{
		Path:        source.Path,
		Kind:        apiv1.SourceKind(source.Kind),
		Executable:  source.Executable,
		ContentHash: source.ContentHash,
		Size:        source.Size,
	}
}

func apiSources(in []delta.Source) []apiv1.Source {
	out := make([]apiv1.Source, 0, len(in))
	for _, source := range in {
		out = append(out, apiSource(source))
	}
	return out
}

func apiSourceChanges(in []delta.SourceChange) []apiv1.SourceChange {
	out := make([]apiv1.SourceChange, 0, len(in))
	for _, change := range in {
		out = append(out, apiv1.SourceChange{
			Before: apiSource(change.Before),
			After:  apiSource(change.After),
		})
	}
	return out
}

func apiNodes(in []graph.Node) []apiv1.GraphNode {
	out := make([]apiv1.GraphNode, 0, len(in))
	for _, node := range in {
		out = append(out, apiGraphNode(node))
	}
	return out
}

func apiNodeChanges(in []delta.NodeChange) []apiv1.GraphNodeChange {
	out := make([]apiv1.GraphNodeChange, 0, len(in))
	for _, change := range in {
		out = append(out, apiv1.GraphNodeChange{
			Before: apiGraphNode(change.Before),
			After:  apiGraphNode(change.After),
		})
	}
	return out
}

func apiEdges(in []graph.Edge) []apiv1.GraphEdge {
	out := make([]apiv1.GraphEdge, 0, len(in))
	for _, edge := range in {
		out = append(out, apiGraphEdge(edge))
	}
	return out
}
