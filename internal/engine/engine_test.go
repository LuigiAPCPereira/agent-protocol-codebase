package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
		t.Fatal("expected observed commit")
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
