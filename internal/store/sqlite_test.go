package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "index.sqlite3")
	input := testIndex("sha256:snapshot", "sha256:a")

	if err := Save(context.Background(), path, input); err != nil {
		t.Fatalf("save index: %v", err)
	}

	got, err := Load(context.Background(), path)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	assertIndexIdentity(t, got, input)

	historical, err := LoadSnapshot(context.Background(), path, input.SnapshotID)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	assertIndexIdentity(t, historical, input)

	if len(got.Sources) != 1 || len(got.Nodes) != 2 || len(got.Edges) != 1 {
		t.Fatalf("unexpected persisted graph: %+v", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat index: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("index permissions = %o, want no group/other bits", info.Mode().Perm())
	}
}

func TestSaveRetainsHistoricalSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.sqlite3")
	first := testIndex("snap-1", "sha256:a")
	second := testIndex("snap-2", "sha256:b")

	if err := Save(context.Background(), path, first); err != nil {
		t.Fatalf("save first: %v", err)
	}
	if err := Save(context.Background(), path, second); err != nil {
		t.Fatalf("save second: %v", err)
	}

	current, err := Load(context.Background(), path)
	if err != nil {
		t.Fatalf("load current: %v", err)
	}
	if current.SnapshotID != second.SnapshotID {
		t.Fatalf("current snapshot = %q, want %q", current.SnapshotID, second.SnapshotID)
	}

	gotFirst, err := LoadSnapshot(context.Background(), path, first.SnapshotID)
	if err != nil {
		t.Fatalf("load retained first snapshot: %v", err)
	}
	if gotFirst.Sources[0].ContentHash != "sha256:a" {
		t.Fatalf("first snapshot content hash = %q", gotFirst.Sources[0].ContentHash)
	}
}

func TestSavePrunesHistoryToBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.sqlite3")
	ctx := context.Background()

	for i := 0; i < historyLimit+2; i++ {
		index := testIndex(fmt.Sprintf("snap-%02d", i), fmt.Sprintf("sha256:%02d", i))
		if err := Save(ctx, path, index); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	if _, err := LoadSnapshot(ctx, path, "snap-00"); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("oldest snapshot error = %v, want ErrSnapshotNotFound", err)
	}
	if _, err := LoadSnapshot(ctx, path, fmt.Sprintf("snap-%02d", historyLimit+1)); err != nil {
		t.Fatalf("latest snapshot missing: %v", err)
	}
}

func TestLoadMissingReturnsNotFound(t *testing.T) {
	_, err := Load(context.Background(), filepath.Join(t.TempDir(), "missing.sqlite3"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestLoadSnapshotMissingReturnsSnapshotNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.sqlite3")
	if err := Save(context.Background(), path, testIndex("present", "sha256:a")); err != nil {
		t.Fatalf("seed index: %v", err)
	}
	_, err := LoadSnapshot(context.Background(), path, "missing")
	if !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("error = %v, want ErrSnapshotNotFound", err)
	}
}


func TestSaveRebuildsKnownLegacySchemas(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "legacy.sqlite3")
			db, err := sql.Open("sqlite3", path)
			if err != nil {
				t.Fatalf("open legacy db: %v", err)
			}
			if _, err := db.Exec("CREATE TABLE legacy_marker (id INTEGER PRIMARY KEY)"); err != nil {
				db.Close()
				t.Fatalf("create legacy marker: %v", err)
			}
			if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
				db.Close()
				t.Fatalf("set legacy version: %v", err)
			}
			if err := db.Close(); err != nil {
				t.Fatalf("close legacy db: %v", err)
			}

			want := testIndex("rebuilt", "sha256:new")
			if err := Save(context.Background(), path, want); err != nil {
				t.Fatalf("save over legacy schema v%d: %v", version, err)
			}

			got, err := Load(context.Background(), path)
			if err != nil {
				t.Fatalf("load rebuilt index: %v", err)
			}
			assertIndexIdentity(t, got, want)

			db, err = sql.Open("sqlite3", readOnlyDSN(path))
			if err != nil {
				t.Fatalf("open rebuilt db: %v", err)
			}
			defer db.Close()

			var schema int
			if err := db.QueryRow("PRAGMA user_version").Scan(&schema); err != nil {
				t.Fatalf("read rebuilt schema: %v", err)
			}
			if schema != schemaVersion {
				t.Fatalf("rebuilt schema = %d, want %d", schema, schemaVersion)
			}

			var markerCount int
			err = db.QueryRow(
				"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'legacy_marker'",
			).Scan(&markerCount)
			if err != nil {
				t.Fatalf("inspect rebuilt schema: %v", err)
			}
			if markerCount != 0 {
				t.Fatal("legacy schema marker survived rebuild")
			}
		})
	}
}

func TestSaveRejectsUnknownSchemaWithoutReplacingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unknown.sqlite3")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open unknown db: %v", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 99"); err != nil {
		db.Close()
		t.Fatalf("set unknown schema version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close unknown db: %v", err)
	}

	err = Save(context.Background(), path, testIndex("new", "sha256:new"))
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("save error = %v, want ErrUnsupportedSchema", err)
	}

	db, err = sql.Open("sqlite3", readOnlyDSN(path))
	if err != nil {
		t.Fatalf("reopen unknown db: %v", err)
	}
	defer db.Close()

	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read preserved unknown schema: %v", err)
	}
	if version != 99 {
		t.Fatalf("unknown schema version = %d, want 99", version)
	}
}

func TestLoadRejectsUnsupportedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.sqlite3")
	if err := Save(context.Background(), path, testIndex("snap", "sha256:a")); err != nil {
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

func testIndex(snapshotID, contentHash string) Index {
	return Index{
		SnapshotID:      snapshotID,
		BaseCommit:      "abc123",
		State:           "EXACT",
		AnalysisVersion: 1,
		Sources: []Source{
			{
				Path:        "a.go",
				Kind:        "regular",
				ContentHash: contentHash,
				Size:        12,
			},
		},
		Nodes: []Node{
			{
				ID:          "go:package:example.com/a",
				Kind:        "PACKAGE",
				Name:        "a",
				Language:    "go",
				PackagePath: "example.com/a",
			},
			{
				ID:       "file:a.go",
				Kind:     "FILE",
				Name:     "a.go",
				Path:     "a.go",
				Language: "go",
			},
		},
		Edges: []Edge{
			{
				From:       "go:package:example.com/a",
				To:         "file:a.go",
				Relation:   "CONTAINS",
				Evidence:   "OBSERVED",
				Resolution: "semantic",
				Extractor:  "go/packages-v1",
				SourcePath: "a.go",
			},
		},
	}
}

func assertIndexIdentity(t *testing.T, got, want Index) {
	t.Helper()
	if got.SnapshotID != want.SnapshotID ||
		got.BaseCommit != want.BaseCommit ||
		got.WorkspaceFingerprint != want.WorkspaceFingerprint ||
		got.State != want.State ||
		got.AnalysisVersion != want.AnalysisVersion {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, want)
	}
}
