package engine

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	apiv1 "github.com/LuigiAPCPereira/agent-protocol-codebase/api/v1"
)

func TestExecuteStatusReturnsObservedRevision(t *testing.T) {
	repo := newGitRepository(t)

	result := Execute(context.Background(), apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "status-test",
		Operation:     apiv1.OperationStatus,
		Repository:    apiv1.Repository{Root: repo},
	})

	if result.Error != nil {
		t.Fatalf("status error: %+v", result.Error)
	}
	if result.Repository.Revision.Commit == "" {
		t.Fatal("expected HEAD commit")
	}
	if result.Repository.Revision.Dirty {
		t.Fatal("expected clean repository")
	}
	if result.Index.State != apiv1.IndexAbsent {
		t.Fatalf("index state = %q, want %q", result.Index.State, apiv1.IndexAbsent)
	}
}

func TestExecuteStatusDetectsRevisionMismatch(t *testing.T) {
	repo := newGitRepository(t)

	result := Execute(context.Background(), apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "status-test",
		Operation:     apiv1.OperationStatus,
		Repository: apiv1.Repository{
			Root: repo,
			Revision: apiv1.Revision{
				Commit: "0000000000000000000000000000000000000000",
			},
		},
	})

	if result.Error == nil || result.Error.Code != "REVISION_MISMATCH" {
		t.Fatalf("error = %+v, want REVISION_MISMATCH", result.Error)
	}
}

func TestExecuteScanReturnsExactSnapshot(t *testing.T) {
	repo := newGitRepository(t)
	ctx := context.Background()

	status := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "discover",
		Operation:     apiv1.OperationStatus,
		Repository:    apiv1.Repository{Root: repo},
	})
	if status.Error != nil {
		t.Fatalf("status error: %+v", status.Error)
	}

	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "scan-test",
		Operation:     apiv1.OperationScan,
		Repository: apiv1.Repository{
			Root:     repo,
			Revision: status.Repository.Revision,
		},
	})
	if result.Error != nil {
		t.Fatalf("scan error: %+v", result.Error)
	}
	if result.Index.State != apiv1.IndexExact {
		t.Fatalf("index state = %q, want %q", result.Index.State, apiv1.IndexExact)
	}
	if !strings.HasPrefix(result.Index.SnapshotID, "sha256:") {
		t.Fatalf("snapshot ID = %q, want sha256 prefix", result.Index.SnapshotID)
	}

	var data apiv1.ScanData
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("decode scan result: %v", err)
	}
	if data.Snapshot.ID != result.Index.SnapshotID {
		t.Fatalf("snapshot IDs differ: %q != %q", data.Snapshot.ID, result.Index.SnapshotID)
	}
	if len(data.Snapshot.Sources) != 1 || data.Snapshot.Sources[0].Path != "tracked.txt" {
		t.Fatalf("unexpected sources: %+v", data.Snapshot.Sources)
	}
	if !strings.HasPrefix(data.Snapshot.Sources[0].ContentHash, "sha256:") {
		t.Fatalf("content hash = %q, want sha256 prefix", data.Snapshot.Sources[0].ContentHash)
	}
}

func TestExecuteScanRequiresDirtyWorkspaceFingerprint(t *testing.T) {
	repo := newGitRepository(t)
	ctx := context.Background()

	cleanStatus := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "discover",
		Operation:     apiv1.OperationStatus,
		Repository:    apiv1.Repository{Root: repo},
	})
	if cleanStatus.Error != nil {
		t.Fatalf("status error: %+v", cleanStatus.Error)
	}

	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("modify tracked file: %v", err)
	}

	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "scan-dirty",
		Operation:     apiv1.OperationScan,
		Repository: apiv1.Repository{
			Root: repo,
			Revision: apiv1.Revision{
				Commit: cleanStatus.Repository.Revision.Commit,
			},
		},
	})
	if result.Error == nil || result.Error.Code != "WORKSPACE_IDENTITY_REQUIRED" {
		t.Fatalf("error = %+v, want WORKSPACE_IDENTITY_REQUIRED", result.Error)
	}
}

func TestExecuteScanAcceptsExactDirtyFingerprint(t *testing.T) {
	repo := newGitRepository(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("modify tracked file: %v", err)
	}

	status := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "discover-dirty",
		Operation:     apiv1.OperationStatus,
		Repository:    apiv1.Repository{Root: repo},
	})
	if status.Error != nil {
		t.Fatalf("status error: %+v", status.Error)
	}
	if !status.Repository.Revision.Dirty || status.Repository.Revision.WorkspaceFingerprint == "" {
		t.Fatalf("expected exact dirty identity, got %+v", status.Repository.Revision)
	}

	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "scan-dirty",
		Operation:     apiv1.OperationScan,
		Repository: apiv1.Repository{
			Root:     repo,
			Revision: status.Repository.Revision,
		},
	})
	if result.Error != nil {
		t.Fatalf("scan error: %+v", result.Error)
	}
	if result.Index.State != apiv1.IndexExact {
		t.Fatalf("index state = %q, want EXACT", result.Index.State)
	}
}

func newGitRepository(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runGit(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "-c", "user.name=Agent Protocol Test", "-c", "user.email=test@example.invalid", "commit", "-m", "initial")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
