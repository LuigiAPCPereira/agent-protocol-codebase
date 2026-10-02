package snapshot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/repository"
)

func TestBuildIsDeterministicAndRespectsGitIgnore(t *testing.T) {
	repo := newGitRepository(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("ignored.bin\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("visible\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "ignored.bin"), []byte("ignored\n"), 0o644); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}

	state, err := repository.Inspect(ctx, repo)
	if err != nil {
		t.Fatalf("inspect repository: %v", err)
	}

	first, err := Build(ctx, state)
	if err != nil {
		t.Fatalf("build first snapshot: %v", err)
	}
	second, err := Build(ctx, state)
	if err != nil {
		t.Fatalf("build second snapshot: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("snapshot is not deterministic: %q != %q", first.ID, second.ID)
	}

	paths := make([]string, 0, len(first.Sources))
	for _, source := range first.Sources {
		paths = append(paths, source.Path)
	}
	if !slices.Contains(paths, "tracked.txt") || !slices.Contains(paths, "untracked.txt") {
		t.Fatalf("expected tracked and untracked sources, got %v", paths)
	}
	if slices.Contains(paths, "ignored.bin") {
		t.Fatalf("ignored file was indexed: %v", paths)
	}
}

func TestBuildChangesWhenVisibleContentChanges(t *testing.T) {
	repo := newGitRepository(t)
	ctx := context.Background()

	state, err := repository.Inspect(ctx, repo)
	if err != nil {
		t.Fatalf("inspect initial repository: %v", err)
	}
	first, err := Build(ctx, state)
	if err != nil {
		t.Fatalf("build first snapshot: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("modify tracked file: %v", err)
	}
	state, err = repository.Inspect(ctx, repo)
	if err != nil {
		t.Fatalf("inspect changed repository: %v", err)
	}
	second, err := Build(ctx, state)
	if err != nil {
		t.Fatalf("build second snapshot: %v", err)
	}

	if first.ID == second.ID {
		t.Fatal("content change did not change snapshot ID")
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
