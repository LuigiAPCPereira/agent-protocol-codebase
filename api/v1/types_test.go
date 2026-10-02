package v1

import (
	"encoding/json"
	"testing"
)

func TestRequestRoundTripAndValidation(t *testing.T) {
	input := []byte(`{
		"schema_version": 1,
		"request_id": "cb-test",
		"operation": "path",
		"repository": {
			"id": "example/repo",
			"revision": {
				"commit": "abc123"
			}
		},
		"arguments": {
			"from": "LoginHandler",
			"to": "SessionStore"
		},
		"requirements": {
			"semantic": "preferred"
		}
	}`)

	var req Request
	if err := json.Unmarshal(input, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if err := ValidateRequest(req); err != nil {
		t.Fatalf("validate request: %v", err)
	}

	encoded, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var roundTrip Request
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("unmarshal round-trip request: %v", err)
	}
	if roundTrip.Operation != OperationPath {
		t.Fatalf("operation = %q, want %q", roundTrip.Operation, OperationPath)
	}
	if roundTrip.Repository.Revision.Commit != "abc123" {
		t.Fatalf("commit = %q, want abc123", roundTrip.Repository.Revision.Commit)
	}
}

func TestValidateRequestRejectsUnknownOperation(t *testing.T) {
	req := Request{
		SchemaVersion: SchemaVersion,
		RequestID:     "cb-test",
		Operation:     Operation("shell"),
		Repository: Repository{
			Revision: Revision{Commit: "abc123"},
		},
	}

	if err := ValidateRequest(req); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateStatusAllowsDiscoveryWithoutExpectedRevision(t *testing.T) {
	req := Request{
		SchemaVersion: SchemaVersion,
		RequestID:     "cb-status",
		Operation:     OperationStatus,
		Repository:    Repository{Root: "."},
	}

	if err := ValidateRequest(req); err != nil {
		t.Fatalf("validate status request: %v", err)
	}
}

func TestValidateNonStatusRequiresRevisionIdentity(t *testing.T) {
	req := Request{
		SchemaVersion: SchemaVersion,
		RequestID:     "cb-query",
		Operation:     OperationQuery,
	}

	if err := ValidateRequest(req); err == nil {
		t.Fatal("expected missing revision identity to fail")
	}
}

func TestValidateDirtyRevisionRequiresFingerprint(t *testing.T) {
	req := Request{
		SchemaVersion: SchemaVersion,
		RequestID:     "cb-scan",
		Operation:     OperationScan,
		Repository: Repository{
			Revision: Revision{
				Commit: "abc123",
				Dirty:  true,
			},
		},
	}

	if err := ValidateRequest(req); err == nil {
		t.Fatal("expected dirty revision without fingerprint to fail")
	}
}

func TestValidateRequestAllowsWorkspaceOnlyIdentity(t *testing.T) {
	req := Request{
		SchemaVersion: SchemaVersion,
		RequestID:     "cb-dirty",
		Operation:     OperationQuery,
		Repository: Repository{
			Revision: Revision{
				Dirty:                true,
				WorkspaceFingerprint: "sha256:deadbeef",
			},
		},
	}

	if err := ValidateRequest(req); err != nil {
		t.Fatalf("validate workspace-only request: %v", err)
	}
}
