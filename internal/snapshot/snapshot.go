package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"

	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/repository"
)

type SourceKind string

const (
	SourceRegular SourceKind = "regular"
	SourceSymlink SourceKind = "symlink"
)

type Source struct {
	Path        string
	Kind        SourceKind
	Executable  bool
	ContentHash string
	Size        int64
}

type Snapshot struct {
	ID             string
	Sources        []Source
	PartialReasons []string
}

func Build(ctx context.Context, state repository.State) (Snapshot, error) {
	entries, err := repository.ListVisibleEntries(ctx, state.Root)
	if err != nil {
		return Snapshot{}, err
	}

	sources := make([]Source, 0, len(entries))
	var partial []string

	for _, entry := range entries {
		source, ok, reason, err := inspectSource(state.Root, entry)
		if err != nil {
			return Snapshot{}, err
		}
		if reason != "" {
			partial = append(partial, reason)
		}
		if ok {
			sources = append(sources, source)
		}
	}

	id := snapshotID(state, sources, partial)
	return Snapshot{
		ID:             id,
		Sources:        sources,
		PartialReasons: partial,
	}, nil
}

func inspectSource(root string, entry repository.Entry) (Source, bool, string, error) {
	fullPath := filepath.Join(root, filepath.FromSlash(entry.Path))
	info, err := os.Lstat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			// A tracked file deleted from the worktree is intentionally absent
			// from the current snapshot.
			return Source{}, false, "", nil
		}
		return Source{}, false, "", fmt.Errorf("stat source %q: %w", entry.Path, err)
	}

	if entry.Tracked {
		switch entry.GitMode {
		case "160000":
			return Source{}, false, fmt.Sprintf("unsupported gitlink: %s", entry.Path), nil
		case "120000":
			target, err := readSymlinkTarget(fullPath, info)
			if err != nil {
				return Source{}, false, "", fmt.Errorf("read symlink %q: %w", entry.Path, err)
			}
			return symlinkSource(entry.Path, target), true, "", nil
		case "100644", "100755":
			return regularSource(entry.Path, fullPath, entry.GitMode == "100755")
		default:
			return Source{}, false, fmt.Sprintf("unsupported git mode %s: %s", entry.GitMode, entry.Path), nil
		}
	}

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(fullPath)
		if err != nil {
			return Source{}, false, "", fmt.Errorf("read symlink %q: %w", entry.Path, err)
		}
		return symlinkSource(entry.Path, target), true, "", nil
	case info.Mode().IsRegular():
		return regularSource(entry.Path, fullPath, info.Mode()&0o111 != 0)
	default:
		return Source{}, false, fmt.Sprintf("unsupported source kind: %s", entry.Path), nil
	}
}

func readSymlinkTarget(path string, info os.FileInfo) (string, error) {
	if info.Mode()&os.ModeSymlink != 0 {
		return os.Readlink(path)
	}

	// Git may materialize a tracked symlink as a regular file on hosts where
	// native symlinks are unavailable. Its file content is the link target.
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func symlinkSource(path, target string) Source {
	sum := sha256.Sum256([]byte(target))
	return Source{
		Path:        path,
		Kind:        SourceSymlink,
		ContentHash: "sha256:" + hex.EncodeToString(sum[:]),
		Size:        int64(len(target)),
	}
}

func regularSource(path, fullPath string, executable bool) (Source, bool, string, error) {
	f, err := os.Open(fullPath)
	if err != nil {
		return Source{}, false, "", fmt.Errorf("open source %q: %w", path, err)
	}

	h := sha256.New()
	size, copyErr := io.Copy(h, f)
	closeErr := f.Close()
	if copyErr != nil {
		return Source{}, false, "", fmt.Errorf("hash source %q: %w", path, copyErr)
	}
	if closeErr != nil {
		return Source{}, false, "", fmt.Errorf("close source %q: %w", path, closeErr)
	}
	return Source{
		Path:        path,
		Kind:        SourceRegular,
		Executable:  executable,
		ContentHash: "sha256:" + hex.EncodeToString(h.Sum(nil)),
		Size:        size,
	}, true, "", nil
}

func snapshotID(state repository.State, sources []Source, partial []string) string {
	h := sha256.New()
	writePart(h, "format", []byte("ap-codebase-snapshot-v1"))
	writePart(h, "commit", []byte(state.Commit))
	writePart(h, "workspace", []byte(state.WorkspaceFingerprint))

	for _, source := range sources {
		writePart(h, "path", []byte(source.Path))
		writePart(h, "kind", []byte(source.Kind))
		writePart(h, "executable", []byte(fmt.Sprintf("%t", source.Executable)))
		writePart(h, "hash", []byte(source.ContentHash))
		writePart(h, "size", []byte(fmt.Sprintf("%d", source.Size)))
	}
	for _, reason := range partial {
		writePart(h, "partial", []byte(reason))
	}

	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func writePart(h hash.Hash, label string, data []byte) {
	fmt.Fprintf(h, "%s:%d\n", label, len(data))
	_, _ = h.Write(data)
	_, _ = h.Write([]byte{0})
}
