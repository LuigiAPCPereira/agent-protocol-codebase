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
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/repository"
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

func TestExecuteScanPersistsExactIndex(t *testing.T) {
	repo := newGitRepository(t)
	ctx := context.Background()

	discovered := status(t, ctx, repo)
	result := scan(t, ctx, repo, discovered.Repository.Revision)

	if result.Index.State != apiv1.IndexExact {
		t.Fatalf("scan index state = %q, want %q", result.Index.State, apiv1.IndexExact)
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

	after := status(t, ctx, repo)
	if after.Index.State != apiv1.IndexExact {
		t.Fatalf("persisted index state = %q, want EXACT", after.Index.State)
	}
	if after.Index.SnapshotID != result.Index.SnapshotID {
		t.Fatalf("persisted snapshot = %q, want %q", after.Index.SnapshotID, result.Index.SnapshotID)
	}

	indexPath, err := repository.IndexPath(ctx, repo)
	if err != nil {
		t.Fatalf("resolve index path: %v", err)
	}
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("stat persisted index: %v", err)
	}
}

func TestExecuteStatusReportsStaleAfterWorkspaceChange(t *testing.T) {
	repo := newGitRepository(t)
	ctx := context.Background()

	discovered := status(t, ctx, repo)
	scanResult := scan(t, ctx, repo, discovered.Repository.Revision)

	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("modify tracked file: %v", err)
	}

	after := status(t, ctx, repo)
	if after.Index.State != apiv1.IndexStale {
		t.Fatalf("index state = %q, want STALE", after.Index.State)
	}
	if after.Index.SnapshotID != scanResult.Index.SnapshotID {
		t.Fatalf("stale status lost snapshot identity: got %q want %q", after.Index.SnapshotID, scanResult.Index.SnapshotID)
	}
}

func TestExecuteScanRequiresDirtyWorkspaceFingerprint(t *testing.T) {
	repo := newGitRepository(t)
	ctx := context.Background()

	cleanStatus := status(t, ctx, repo)

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

	discovered := status(t, ctx, repo)
	if !discovered.Repository.Revision.Dirty ||
		discovered.Repository.Revision.WorkspaceFingerprint == "" {
		t.Fatalf("expected exact dirty identity, got %+v", discovered.Repository.Revision)
	}

	result := scan(t, ctx, repo, discovered.Repository.Revision)
	if result.Index.State != apiv1.IndexExact {
		t.Fatalf("scan index state = %q, want EXACT", result.Index.State)
	}

	after := status(t, ctx, repo)
	if after.Index.State != apiv1.IndexExact {
		t.Fatalf("persisted dirty index state = %q, want EXACT", after.Index.State)
	}
}

func status(t *testing.T, ctx context.Context, repo string) apiv1.Result {
	t.Helper()
	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "status-helper",
		Operation:     apiv1.OperationStatus,
		Repository:    apiv1.Repository{Root: repo},
	})
	if result.Error != nil {
		t.Fatalf("status error: %+v", result.Error)
	}
	return result
}

func scan(t *testing.T, ctx context.Context, repo string, revision apiv1.Revision) apiv1.Result {
	t.Helper()
	result := Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "scan-helper",
		Operation:     apiv1.OperationScan,
		Repository: apiv1.Repository{
			Root:     repo,
			Revision: revision,
		},
	})
	if result.Error != nil {
		t.Fatalf("scan error: %+v", result.Error)
	}
	return result
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
