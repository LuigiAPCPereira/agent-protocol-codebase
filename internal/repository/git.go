package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type State struct {
	Root                 string
	Commit               string
	Dirty                bool
	WorkspaceFingerprint string
}

type Entry struct {
	Path    string
	Tracked bool
	GitMode string
}

func Inspect(ctx context.Context, dir string) (State, error) {
	if dir == "" {
		dir = "."
	}

	rootOut, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return State{}, fmt.Errorf("discover git root: %w", err)
	}
	root := strings.TrimSpace(string(rootOut))

	commitOut, err := git(ctx, root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return State{}, fmt.Errorf("resolve HEAD: %w", err)
	}
	commit := strings.TrimSpace(string(commitOut))

	status, err := git(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return State{}, fmt.Errorf("inspect worktree: %w", err)
	}
	if len(status) == 0 {
		return State{
			Root:   root,
			Commit: commit,
			Dirty:  false,
		}, nil
	}

	fingerprint, err := fingerprintWorkspace(ctx, root, commit, status)
	if err != nil {
		return State{}, err
	}

	return State{
		Root:                 root,
		Commit:               commit,
		Dirty:                true,
		WorkspaceFingerprint: fingerprint,
	}, nil
}

func IndexPath(ctx context.Context, root string) (string, error) {
	out, err := git(ctx, root, "rev-parse", "--git-path", "agent-protocol/codebase.sqlite3")
	if err != nil {
		return "", fmt.Errorf("resolve codebase index path: %w", err)
	}

	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("resolve codebase index path: empty path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, filepath.FromSlash(path))
	}
	return filepath.Clean(path), nil
}

func ListVisibleEntries(ctx context.Context, root string) ([]Entry, error) {
	trackedOut, err := git(ctx, root, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, fmt.Errorf("list tracked repository files: %w", err)
	}
	untrackedOut, err := git(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("list untracked repository files: %w", err)
	}

	byPath := make(map[string]Entry)
	for _, record := range bytes.Split(trackedOut, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		tab := bytes.IndexByte(record, '\t')
		if tab < 0 {
			return nil, fmt.Errorf("parse git index entry %q: missing path separator", string(record))
		}
		fields := strings.Fields(string(record[:tab]))
		if len(fields) < 3 {
			return nil, fmt.Errorf("parse git index entry %q: malformed metadata", string(record[:tab]))
		}
		path := string(record[tab+1:])
		byPath[path] = Entry{
			Path:    path,
			Tracked: true,
			GitMode: fields[0],
		}
	}

	for _, path := range splitNUL(untrackedOut) {
		if _, exists := byPath[path]; exists {
			continue
		}
		byPath[path] = Entry{Path: path}
	}

	entries := make([]Entry, 0, len(byPath))
	for _, entry := range byPath {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	return entries, nil
}

func SameIdentity(a, b State) bool {
	return a.Commit == b.Commit &&
		a.Dirty == b.Dirty &&
		a.WorkspaceFingerprint == b.WorkspaceFingerprint
}

func fingerprintWorkspace(ctx context.Context, root, commit string, status []byte) (string, error) {
	diff, err := git(ctx, root, "diff", "--binary", "--no-ext-diff", "--submodule=short", "HEAD", "--")
	if err != nil {
		return "", fmt.Errorf("read tracked workspace delta: %w", err)
	}

	untrackedOut, err := git(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", fmt.Errorf("list untracked files: %w", err)
	}

	h := sha256.New()
	writePart(h, "format", []byte("ap-codebase-workspace-v1"))
	writePart(h, "commit", []byte(commit))
	writePart(h, "status", status)
	writePart(h, "diff", diff)

	paths := splitNUL(untrackedOut)
	sort.Strings(paths)
	for _, path := range paths {
		if err := hashUntracked(h, root, path); err != nil {
			return "", err
		}
	}

	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func hashUntracked(h hash.Hash, root, path string) error {
	fullPath := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(fullPath)
	if err != nil {
		return fmt.Errorf("stat untracked path %q: %w", path, err)
	}

	writePart(h, "untracked-path", []byte(path))

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		writePart(h, "untracked-kind", []byte("symlink"))
		target, err := os.Readlink(fullPath)
		if err != nil {
			return fmt.Errorf("read untracked symlink %q: %w", path, err)
		}
		writePart(h, "untracked-symlink", []byte(target))
	case info.Mode().IsRegular():
		kind := "regular"
		if info.Mode()&0o111 != 0 {
			kind = "regular-executable"
		}
		writePart(h, "untracked-kind", []byte(kind))

		content, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("read untracked file %q: %w", path, err)
		}
		writePart(h, "untracked-content", content)
	default:
		writePart(h, "untracked-kind", []byte("special"))
	}

	return nil
}

func splitNUL(data []byte) []string {
	if len(data) == 0 {
		return nil
	}

	parts := bytes.Split(data, []byte{0})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) != 0 {
			out = append(out, string(part))
		}
	}
	return out
}

func writePart(h hash.Hash, label string, data []byte) {
	fmt.Fprintf(h, "%s:%d\n", label, len(data))
	_, _ = h.Write(data)
	_, _ = h.Write([]byte{0})
}

func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, message)
	}
	return out, nil
}
