package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/ncruces/go-sqlite3/driver"
)

const schemaVersion = 1

var (
	ErrNotFound          = errors.New("codebase index not found")
	ErrUnsupportedSchema = errors.New("unsupported codebase index schema")
)

type Index struct {
	SnapshotID           string
	BaseCommit           string
	WorkspaceFingerprint string
	State                string
	PartialReason        string
	Sources              []Source
}

type Source struct {
	Path        string
	Kind        string
	Executable  bool
	ContentHash string
	Size        int64
}

func Save(ctx context.Context, path string, index Index) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create index directory: %w", err)
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return fmt.Errorf("open index: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin index transaction: %w", err)
	}
	defer tx.Rollback()

	if err := ensureSchema(ctx, tx); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM sources"); err != nil {
		return fmt.Errorf("clear sources: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM current_index"); err != nil {
		return fmt.Errorf("clear current index: %w", err)
	}

	const insertIndex = "INSERT INTO current_index " +
		"(id, snapshot_id, base_commit, workspace_fingerprint, state, partial_reason) " +
		"VALUES (1, ?, ?, ?, ?, ?)"
	if _, err := tx.ExecContext(
		ctx,
		insertIndex,
		index.SnapshotID,
		index.BaseCommit,
		index.WorkspaceFingerprint,
		index.State,
		index.PartialReason,
	); err != nil {
		return fmt.Errorf("write current index: %w", err)
	}

	const insertSource = "INSERT INTO sources " +
		"(path, kind, executable, content_hash, size) VALUES (?, ?, ?, ?, ?)"
	stmt, err := tx.PrepareContext(ctx, insertSource)
	if err != nil {
		return fmt.Errorf("prepare source insert: %w", err)
	}
	defer stmt.Close()

	for _, source := range index.Sources {
		if _, err := stmt.ExecContext(
			ctx,
			source.Path,
			source.Kind,
			source.Executable,
			source.ContentHash,
			source.Size,
		); err != nil {
			return fmt.Errorf("write source %q: %w", source.Path, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit index transaction: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("restrict index permissions: %w", err)
	}
	return nil
}

func Load(ctx context.Context, path string) (Index, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return Index{}, ErrNotFound
		}
		return Index{}, fmt.Errorf("stat index: %w", err)
	}

	db, err := sql.Open("sqlite3", readOnlyDSN(path))
	if err != nil {
		return Index{}, fmt.Errorf("open index: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return Index{}, fmt.Errorf("read index schema: %w", err)
	}
	if version != schemaVersion {
		return Index{}, fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchema, version, schemaVersion)
	}

	var index Index
	const selectIndex = "SELECT snapshot_id, base_commit, workspace_fingerprint, state, partial_reason " +
		"FROM current_index WHERE id = 1"
	err = db.QueryRowContext(ctx, selectIndex).Scan(
		&index.SnapshotID,
		&index.BaseCommit,
		&index.WorkspaceFingerprint,
		&index.State,
		&index.PartialReason,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Index{}, ErrNotFound
	}
	if err != nil {
		return Index{}, fmt.Errorf("read current index: %w", err)
	}

	const selectSources = "SELECT path, kind, executable, content_hash, size " +
		"FROM sources ORDER BY path"
	rows, err := db.QueryContext(ctx, selectSources)
	if err != nil {
		return Index{}, fmt.Errorf("read sources: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var source Source
		if err := rows.Scan(
			&source.Path,
			&source.Kind,
			&source.Executable,
			&source.ContentHash,
			&source.Size,
		); err != nil {
			return Index{}, fmt.Errorf("scan source: %w", err)
		}
		index.Sources = append(index.Sources, source)
	}
	if err := rows.Err(); err != nil {
		return Index{}, fmt.Errorf("iterate sources: %w", err)
	}

	return index, nil
}

func ensureSchema(ctx context.Context, tx *sql.Tx) error {
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read index schema: %w", err)
	}

	switch version {
	case 0:
		const createIndex = "CREATE TABLE current_index (" +
			"id INTEGER PRIMARY KEY CHECK (id = 1)," +
			"snapshot_id TEXT NOT NULL," +
			"base_commit TEXT NOT NULL," +
			"workspace_fingerprint TEXT NOT NULL," +
			"state TEXT NOT NULL," +
			"partial_reason TEXT NOT NULL" +
			")"
		if _, err := tx.ExecContext(ctx, createIndex); err != nil {
			return fmt.Errorf("create current index table: %w", err)
		}

		const createSources = "CREATE TABLE sources (" +
			"path TEXT PRIMARY KEY," +
			"kind TEXT NOT NULL," +
			"executable INTEGER NOT NULL," +
			"content_hash TEXT NOT NULL," +
			"size INTEGER NOT NULL CHECK (size >= 0)" +
			")"
		if _, err := tx.ExecContext(ctx, createSources); err != nil {
			return fmt.Errorf("create sources table: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "PRAGMA user_version = 1"); err != nil {
			return fmt.Errorf("set index schema version: %w", err)
		}
		return nil
	case schemaVersion:
		return nil
	default:
		return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchema, version, schemaVersion)
	}
}

func readOnlyDSN(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	query := u.Query()
	query.Set("mode", "ro")
	u.RawQuery = query.Encode()
	return u.String()
}
