package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	apiv1 "github.com/LuigiAPCPereira/agent-protocol-codebase/api/v1"
)

func TestExecuteDiffComparesRetainedSnapshots(t *testing.T) {
	repo := newGoRepository(t)
	ctx := context.Background()

	firstStatus := status(t, ctx, repo)
	first := scan(t, ctx, repo, firstStatus.Repository.Revision)

	path := filepath.Join(repo, "main.go")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if err := os.WriteFile(
		path,
		append(content, []byte("\ntype Added struct{}\n")...),
		0o644,
	); err != nil {
		t.Fatalf("modify main.go: %v", err)
	}

	secondStatus := status(t, ctx, repo)
	second := scan(t, ctx, repo, secondStatus.Repository.Revision)

	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "diff-two",
		Operation:     apiv1.OperationDiff,
		Repository: apiv1.Repository{
			Root:     repo,
			Revision: secondStatus.Repository.Revision,
		},
		Arguments: map[string]any{
			"base_snapshot_id": first.Index.SnapshotID,
			"head_snapshot_id": second.Index.SnapshotID,
		},
	})
	if result.Error != nil {
		t.Fatalf("diff error: %+v", result.Error)
	}

	var data apiv1.DiffData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode diff result: %v", err)
	}
	if data.Sources.Counts.Changed != 1 {
		t.Fatalf("source counts = %+v", data.Sources.Counts)
	}
	if data.Nodes.Counts.Added < 1 {
		t.Fatalf("node counts = %+v", data.Nodes.Counts)
	}
	if data.Edges.Counts.Added < 1 {
		t.Fatalf("edge counts = %+v", data.Edges.Counts)
	}
	if data.BaseSnapshotID != first.Index.SnapshotID ||
		data.HeadSnapshotID != second.Index.SnapshotID {
		t.Fatalf("wrong snapshot IDs: %+v", data)
	}
}

func TestExecuteDiffRequiresRetainedSnapshot(t *testing.T) {
	repo := newGoRepository(t)
	ctx := context.Background()

	current := status(t, ctx, repo)
	scanned := scan(t, ctx, repo, current.Repository.Revision)

	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "diff-missing",
		Operation:     apiv1.OperationDiff,
		Repository: apiv1.Repository{
			Root:     repo,
			Revision: current.Repository.Revision,
		},
		Arguments: map[string]any{
			"base_snapshot_id": "missing",
			"head_snapshot_id": scanned.Index.SnapshotID,
		},
	})
	if result.Error == nil || result.Error.Code != "SNAPSHOT_NOT_FOUND" {
		t.Fatalf("error = %+v, want SNAPSHOT_NOT_FOUND", result.Error)
	}
}

func TestParseDiffArgumentsRejectsInvalidInput(t *testing.T) {
	if _, err := parseDiffArguments(nil); err == nil {
		t.Fatal("expected missing IDs error")
	}
	if _, err := parseDiffArguments(map[string]any{
		"base_snapshot_id": "a",
		"head_snapshot_id": "b",
		"limit":            251,
	}); err == nil {
		t.Fatal("expected limit error")
	}
	if _, err := parseDiffArguments(map[string]any{
		"base_snapshot_id": "a",
		"head_snapshot_id": "b",
		"unknown":          true,
	}); err == nil {
		t.Fatal("expected unknown argument error")
	}
}
