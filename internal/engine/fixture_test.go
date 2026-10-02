package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func newGoMultiPackageRepository(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runFixtureGit(t, dir, "init")
	writeFixtureFile(t, dir, "go.mod", "module example.com/fixture\n\ngo 1.26.0\n")
	writeFixtureFile(t, dir, "lib/lib.go", `package lib

type Service struct{}
`)
	writeFixtureFile(t, dir, "app/app.go", `package app

import "example.com/fixture/lib"

func Use(_ lib.Service) {}
`)
	runFixtureGit(t, dir, "add", "go.mod", "lib/lib.go", "app/app.go")
	runFixtureGit(
		t,
		dir,
		"-c", "user.name=Agent Protocol Test",
		"-c", "user.email=test@example.invalid",
		"commit", "-m", "initial",
	)
	return dir
}

func writeFixtureFile(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runFixtureGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
