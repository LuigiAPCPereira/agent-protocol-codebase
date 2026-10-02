// Package v1 defines the external Codebase API schema version 1.
//
// The package is intentionally transport-agnostic. Local processes, CI systems,
// remote workspaces, and user-assisted execution should exchange the same
// request/result envelopes.
package v1

import (
	"encoding/json"
	"errors"
	"fmt"
)

const SchemaVersion = 1

type Operation string

const (
	OperationScan   Operation = "scan"
	OperationStatus Operation = "status"
	OperationQuery  Operation = "query"
	OperationPath   Operation = "path"
	OperationImpact Operation = "impact"
	OperationDiff   Operation = "diff"
)

func (o Operation) Valid() bool {
	switch o {
	case OperationScan, OperationStatus, OperationQuery, OperationPath, OperationImpact, OperationDiff:
		return true
	default:
		return false
	}
}

type SemanticRequirement string

const (
	SemanticOff       SemanticRequirement = "off"
	SemanticPreferred SemanticRequirement = "preferred"
	SemanticRequired  SemanticRequirement = "required"
)

type Revision struct {
	Commit               string `json:"commit,omitempty"`
	Dirty                bool   `json:"dirty,omitempty"`
	WorkspaceFingerprint string `json:"workspace_fingerprint,omitempty"`
}

type Repository struct {
	ID       string   `json:"id,omitempty"`
	Root     string   `json:"root,omitempty"`
	Revision Revision `json:"revision"`
}

type Requirements struct {
	Semantic SemanticRequirement `json:"semantic,omitempty"`
}

type Request struct {
	SchemaVersion int            `json:"schema_version"`
	RequestID     string         `json:"request_id"`
	Operation     Operation      `json:"operation"`
	Repository    Repository     `json:"repository"`
	Arguments     map[string]any `json:"arguments,omitempty"`
	Requirements  Requirements   `json:"requirements,omitempty"`
}

type IndexState string

const (
	IndexExact   IndexState = "EXACT"
	IndexPartial IndexState = "PARTIAL"
	IndexStale   IndexState = "STALE"
	IndexInvalid IndexState = "INVALID"
)

type EngineInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Capability struct {
	Name       string `json:"name"`
	Available  bool   `json:"available"`
	Resolution string `json:"resolution,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type IndexInfo struct {
	State        IndexState `json:"state"`
	BaseCommit   string     `json:"base_commit,omitempty"`
	Fingerprint  string     `json:"workspace_fingerprint,omitempty"`
	PartialReason string    `json:"partial_reason,omitempty"`
}

type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
}

type Result struct {
	SchemaVersion int             `json:"schema_version"`
	RequestID     string          `json:"request_id"`
	Engine        EngineInfo      `json:"engine"`
	Repository    Repository      `json:"repository"`
	Index         IndexInfo       `json:"index"`
	Capabilities []Capability    `json:"capabilities,omitempty"`
	Data          json.RawMessage `json:"result,omitempty"`
	Error         *APIError       `json:"error,omitempty"`
}

func ValidateRequest(req Request) error {
	if req.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d: expected %d", req.SchemaVersion, SchemaVersion)
	}
	if req.RequestID == "" {
		return errors.New("request_id is required")
	}
	if !req.Operation.Valid() {
		return fmt.Errorf("unsupported operation %q", req.Operation)
	}
	if req.Repository.Revision.Commit == "" && req.Repository.Revision.WorkspaceFingerprint == "" {
		return errors.New("repository revision requires commit or workspace_fingerprint")
	}

	switch req.Requirements.Semantic {
	case "", SemanticOff, SemanticPreferred, SemanticRequired:
		return nil
	default:
		return fmt.Errorf("unsupported semantic requirement %q", req.Requirements.Semantic)
	}
}
