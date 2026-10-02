package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "index.sqlite3")
	input := Index{
		SnapshotID:           "sha256:snapshot",
		BaseCommit:           "abc123",
		WorkspaceFingerprint: "sha256:workspace",
		State:                "EXACT",
		Sources: []Source{
			{
				Path:        "a.go",
				Kind:        "regular",
				ContentHash: "sha256:a",
				Size:        12,
			},
			{
				Path:        "script.sh",
				Kind:        "regular",
				Executable:  true,
				ContentHash: "sha256:b",
				Size:        8,
			},
		},
	}

	if err := Save(context.Background(), path, input); err != nil {
		t.Fatalf("save index: %v", err)
	}

	got, err := Load(context.Background(), path)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	if got.SnapshotID != input.SnapshotID ||
		got.BaseCommit != input.BaseCommit ||
		got.WorkspaceFingerprint != input.WorkspaceFingerprint ||
		got.State != input.State {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, input)
	}
	if len(got.Sources) != 2 || !got.Sources[1].Executable {
		t.Fatalf("unexpected sources: %+v", got.Sources)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat index: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("index permissions = %o, want no group/other bits", info.Mode().Perm())
	}
}

func TestLoadMissingReturnsNotFound(t *testing.T) {
	_, err := Load(context.Background(), filepath.Join(t.TempDir(), "missing.sqlite3"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestLoadRejectsUnsupportedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.sqlite3")
	if err := Save(context.Background(), path, Index{
		SnapshotID: "snap",
		BaseCommit: "abc",
		State:      "EXACT",
	}); err != nil {
		t.Fatalf("seed index: %v", err)
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open writable test db: %v", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 99"); err != nil {
		db.Close()
		t.Fatalf("set schema version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close test db: %v", err)
	}

	_, err = Load(context.Background(), path)
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("error = %v, want ErrUnsupportedSchema", err)
	}
}
