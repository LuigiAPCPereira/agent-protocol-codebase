package repository

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectCleanAndDirtyWorkspace(t *testing.T) {
	repo := newGitRepository(t)
	ctx := context.Background()

	clean, err := Inspect(ctx, repo)
	if err != nil {
		t.Fatalf("inspect clean repository: %v", err)
	}
	if clean.Commit == "" {
		t.Fatal("expected HEAD commit")
	}
	if clean.Dirty {
		t.Fatal("clean repository reported dirty")
	}
	if clean.WorkspaceFingerprint != "" {
		t.Fatalf("clean fingerprint = %q, want empty", clean.WorkspaceFingerprint)
	}

	tracked := filepath.Join(repo, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("modify tracked file: %v", err)
	}

	dirty, err := Inspect(ctx, repo)
	if err != nil {
		t.Fatalf("inspect dirty repository: %v", err)
	}
	if !dirty.Dirty {
		t.Fatal("dirty repository reported clean")
	}
	if !strings.HasPrefix(dirty.WorkspaceFingerprint, "sha256:") {
		t.Fatalf("fingerprint = %q, want sha256 prefix", dirty.WorkspaceFingerprint)
	}

	again, err := Inspect(ctx, repo)
	if err != nil {
		t.Fatalf("inspect dirty repository again: %v", err)
	}
	if again.WorkspaceFingerprint != dirty.WorkspaceFingerprint {
		t.Fatalf("fingerprint is not deterministic: %q != %q", again.WorkspaceFingerprint, dirty.WorkspaceFingerprint)
	}
}

func TestInspectFingerprintIncludesUntrackedContent(t *testing.T) {
	repo := newGitRepository(t)
	ctx := context.Background()
	path := filepath.Join(repo, "untracked.txt")

	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}
	first, err := Inspect(ctx, repo)
	if err != nil {
		t.Fatalf("inspect first untracked state: %v", err)
	}

	if err := os.WriteFile(path, []byte("two\n"), 0o644); err != nil {
		t.Fatalf("rewrite untracked file: %v", err)
	}
	second, err := Inspect(ctx, repo)
	if err != nil {
		t.Fatalf("inspect second untracked state: %v", err)
	}

	if first.WorkspaceFingerprint == second.WorkspaceFingerprint {
		t.Fatal("untracked content change did not change workspace fingerprint")
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
