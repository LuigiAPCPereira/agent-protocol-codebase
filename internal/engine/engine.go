package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	apiv1 "github.com/LuigiAPCPereira/agent-protocol-codebase/api/v1"
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/repository"
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/snapshot"
)

const Version = "0.0.0-dev"

func Execute(ctx context.Context, req apiv1.Request) apiv1.Result {
	result := apiv1.Result{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     req.RequestID,
		Engine: apiv1.EngineInfo{
			Name:    "ap-codebase",
			Version: Version,
		},
		Index: apiv1.IndexInfo{State: apiv1.IndexAbsent},
	}

	if err := apiv1.ValidateRequest(req); err != nil {
		result.Error = &apiv1.APIError{
			Code:    "INVALID_REQUEST",
			Message: err.Error(),
		}
		return result
	}

	switch req.Operation {
	case apiv1.OperationStatus:
		return executeStatus(ctx, req, result)
	case apiv1.OperationScan:
		return executeScan(ctx, req, result)
	default:
		result.Error = &apiv1.APIError{
			Code:    "NOT_IMPLEMENTED",
			Message: fmt.Sprintf("operation %q is reserved but not implemented", req.Operation),
		}
		return result
	}
}

func executeStatus(ctx context.Context, req apiv1.Request, result apiv1.Result) apiv1.Result {
	state, err := inspectRepository(ctx, req.Repository.Root)
	if err != nil {
		return repositoryInspectionError(result, err)
	}

	result.Repository = observedRepository(req.Repository.ID, state)
	result.Capabilities = []apiv1.Capability{
		{
			Name:       "repository.identity",
			Available:  true,
			Resolution: "exact",
		},
		{
			Name:      "index",
			Available: false,
			Reason:    "persistent structural index is not implemented yet",
		},
	}

	if err := verifyExpectation(req.Repository.Revision, result.Repository.Revision); err != nil {
		result.Error = err
		return result
	}

	data, err := json.Marshal(map[string]any{
		"dirty": state.Dirty,
	})
	if err != nil {
		return internalError(result, err)
	}
	result.Data = data
	return result
}

func executeScan(ctx context.Context, req apiv1.Request, result apiv1.Result) apiv1.Result {
	before, err := inspectRepository(ctx, req.Repository.Root)
	if err != nil {
		return repositoryInspectionError(result, err)
	}

	result.Repository = observedRepository(req.Repository.ID, before)
	if expectationErr := verifyExpectation(req.Repository.Revision, result.Repository.Revision); expectationErr != nil {
		result.Error = expectationErr
		return result
	}
	if before.Dirty && req.Repository.Revision.WorkspaceFingerprint == "" {
		result.Error = &apiv1.APIError{
			Code:    "WORKSPACE_IDENTITY_REQUIRED",
			Message: "dirty worktree requires the exact workspace_fingerprint returned by status",
		}
		return result
	}

	built, err := snapshot.Build(ctx, before)
	if err != nil {
		result.Error = &apiv1.APIError{
			Code:      "SCAN_FAILED",
			Message:   err.Error(),
			Retryable: true,
		}
		return result
	}

	after, err := inspectRepository(ctx, before.Root)
	if err != nil {
		return repositoryInspectionError(result, err)
	}
	if !repository.SameIdentity(before, after) {
		result.Repository = observedRepository(req.Repository.ID, after)
		result.Error = &apiv1.APIError{
			Code:      "WORKSPACE_CHANGED_DURING_SCAN",
			Message:   "repository identity changed while the snapshot was being built",
			Retryable: true,
		}
		return result
	}

	apiSources := make([]apiv1.Source, 0, len(built.Sources))
	for _, source := range built.Sources {
		apiSources = append(apiSources, apiv1.Source{
			Path:        source.Path,
			Kind:        apiv1.SourceKind(source.Kind),
			Executable:  source.Executable,
			ContentHash: source.ContentHash,
			Size:        source.Size,
		})
	}

	indexState := apiv1.IndexExact
	partialReason := ""
	if len(built.PartialReasons) > 0 {
		indexState = apiv1.IndexPartial
		partialReason = strings.Join(built.PartialReasons, "; ")
	}

	result.Index = apiv1.IndexInfo{
		State:         indexState,
		SnapshotID:    built.ID,
		BaseCommit:    before.Commit,
		Fingerprint:   before.WorkspaceFingerprint,
		PartialReason: partialReason,
	}
	result.Capabilities = []apiv1.Capability{
		{
			Name:       "repository.identity",
			Available:  true,
			Resolution: "exact",
		},
		{
			Name:       "source.inventory",
			Available:  true,
			Resolution: "exact",
		},
		{
			Name:       "snapshot",
			Available:  true,
			Resolution: strings.ToLower(string(indexState)),
		},
	}

	data, err := json.Marshal(apiv1.ScanData{
		Snapshot: apiv1.Snapshot{
			ID:       built.ID,
			Revision: result.Repository.Revision,
			Sources:  apiSources,
		},
		Warnings: built.PartialReasons,
	})
	if err != nil {
		return internalError(result, err)
	}
	result.Data = data
	return result
}

func inspectRepository(ctx context.Context, root string) (repository.State, error) {
	if root == "" {
		root = "."
	}
	return repository.Inspect(ctx, root)
}

func observedRepository(id string, state repository.State) apiv1.Repository {
	return apiv1.Repository{
		ID: id,
		Revision: apiv1.Revision{
			Commit:               state.Commit,
			Dirty:                state.Dirty,
			WorkspaceFingerprint: state.WorkspaceFingerprint,
		},
	}
}

func repositoryInspectionError(result apiv1.Result, err error) apiv1.Result {
	result.Error = &apiv1.APIError{
		Code:    "REPOSITORY_INSPECTION_FAILED",
		Message: err.Error(),
	}
	return result
}

func internalError(result apiv1.Result, err error) apiv1.Result {
	result.Error = &apiv1.APIError{
		Code:    "INTERNAL_ERROR",
		Message: err.Error(),
	}
	return result
}

func verifyExpectation(expected, observed apiv1.Revision) *apiv1.APIError {
	if expected.Commit != "" && expected.Commit != observed.Commit {
		return &apiv1.APIError{
			Code:    "REVISION_MISMATCH",
			Message: fmt.Sprintf("requested commit %q but observed %q", expected.Commit, observed.Commit),
		}
	}
	if expected.WorkspaceFingerprint != "" && expected.WorkspaceFingerprint != observed.WorkspaceFingerprint {
		return &apiv1.APIError{
			Code:    "WORKSPACE_MISMATCH",
			Message: "requested workspace fingerprint does not match the observed worktree",
		}
	}
	if expected.Dirty && !observed.Dirty {
		return &apiv1.APIError{
			Code:    "WORKSPACE_MISMATCH",
			Message: "requested a dirty workspace but observed a clean worktree",
		}
	}
	return nil
}
