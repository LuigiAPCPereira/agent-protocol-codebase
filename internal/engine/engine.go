package engine

import (
	"context"
	"encoding/json"
	"fmt"

	apiv1 "github.com/LuigiAPCPereira/agent-protocol-codebase/api/v1"
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/repository"
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
	default:
		result.Error = &apiv1.APIError{
			Code:    "NOT_IMPLEMENTED",
			Message: fmt.Sprintf("operation %q is reserved but not implemented", req.Operation),
		}
		return result
	}
}

func executeStatus(ctx context.Context, req apiv1.Request, result apiv1.Result) apiv1.Result {
	dir := req.Repository.Root
	if dir == "" {
		dir = "."
	}

	state, err := repository.Inspect(ctx, dir)
	if err != nil {
		result.Error = &apiv1.APIError{
			Code:    "REPOSITORY_INSPECTION_FAILED",
			Message: err.Error(),
		}
		return result
	}

	observed := apiv1.Repository{
		ID: req.Repository.ID,
		Revision: apiv1.Revision{
			Commit:               state.Commit,
			Dirty:                state.Dirty,
			WorkspaceFingerprint: state.WorkspaceFingerprint,
		},
	}
	result.Repository = observed
	result.Capabilities = []apiv1.Capability{
		{
			Name:       "repository.identity",
			Available:  true,
			Resolution: "exact",
		},
		{
			Name:      "index",
			Available: false,
			Reason:    "structural index is not implemented yet",
		},
	}

	if err := verifyExpectation(req.Repository.Revision, observed.Revision); err != nil {
		result.Error = err
		return result
	}

	data, _ := json.Marshal(map[string]any{
		"dirty": state.Dirty,
	})
	result.Data = data
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
