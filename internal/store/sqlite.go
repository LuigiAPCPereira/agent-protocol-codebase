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

const (
	schemaVersion = 3
	historyLimit  = 8
)

var (
	ErrNotFound          = errors.New("codebase index not found")
	ErrSnapshotNotFound  = errors.New("codebase snapshot not found")
	ErrUnsupportedSchema = errors.New("unsupported codebase index schema")
)

type Index struct {
	SnapshotID           string
	BaseCommit           string
	WorkspaceFingerprint string
	State                string
	PartialReason        string
	AnalysisVersion      int
	Sources              []Source
	Nodes                []Node
	Edges                []Edge
}

type Source struct {
	Path        string
	Kind        string
	Executable  bool
	ContentHash string
	Size        int64
}

type Node struct {
	ID          string
	Kind        string
	Name        string
	Path        string
	Language    string
	PackagePath string
	StartLine   int
	EndLine     int
	External    bool
}

type Edge struct {
	From       string
	To         string
	Relation   string
	Evidence   string
	Resolution string
	Extractor  string
	SourcePath string
	StartLine  int
}

func Save(ctx context.Context, path string, index Index) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create index directory: %w", err)
	}
	if err := rebuildLegacyIndex(ctx, path); err != nil {
		return err
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

	if err := deleteSnapshotChildren(ctx, tx, index.SnapshotID); err != nil {
		return err
	}

	const upsertSnapshot = "INSERT INTO snapshots " +
		"(snapshot_id, base_commit, workspace_fingerprint, state, partial_reason, analysis_version) " +
		"VALUES (?, ?, ?, ?, ?, ?) " +
		"ON CONFLICT(snapshot_id) DO UPDATE SET " +
		"base_commit=excluded.base_commit, " +
		"workspace_fingerprint=excluded.workspace_fingerprint, " +
		"state=excluded.state, partial_reason=excluded.partial_reason, " +
		"analysis_version=excluded.analysis_version"
	if _, err := tx.ExecContext(
		ctx,
		upsertSnapshot,
		index.SnapshotID,
		index.BaseCommit,
		index.WorkspaceFingerprint,
		index.State,
		index.PartialReason,
		index.AnalysisVersion,
	); err != nil {
		return fmt.Errorf("write snapshot metadata: %w", err)
	}

	if err := insertSources(ctx, tx, index.SnapshotID, index.Sources); err != nil {
		return err
	}
	if err := insertNodes(ctx, tx, index.SnapshotID, index.Nodes); err != nil {
		return err
	}
	if err := insertEdges(ctx, tx, index.SnapshotID, index.Edges); err != nil {
		return err
	}

	const upsertCurrent = "INSERT INTO current_index (id, snapshot_id) VALUES (1, ?) " +
		"ON CONFLICT(id) DO UPDATE SET snapshot_id=excluded.snapshot_id"
	if _, err := tx.ExecContext(ctx, upsertCurrent, index.SnapshotID); err != nil {
		return fmt.Errorf("write current index pointer: %w", err)
	}

	if err := pruneHistory(ctx, tx, index.SnapshotID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit index transaction: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("restrict index permissions: %w", err)
	}
	return nil
}


func rebuildLegacyIndex(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat existing index: %w", err)
	}

	db, err := sql.Open("sqlite3", readOnlyDSN(path))
	if err != nil {
		return fmt.Errorf("open existing index: %w", err)
	}
	db.SetMaxOpenConns(1)

	var version int
	queryErr := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	closeErr := db.Close()
	if queryErr != nil {
		return fmt.Errorf("read existing index schema: %w", queryErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close existing index: %w", closeErr)
	}

	switch version {
	case 1, 2:
		for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
			if err := os.Remove(candidate); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove legacy index %q: %w", candidate, err)
			}
		}
	}
	return nil
}

func Load(ctx context.Context, path string) (Index, error) {
	db, err := openReadOnly(ctx, path)
	if err != nil {
		return Index{}, err
	}
	defer db.Close()

	var snapshotID string
	err = db.QueryRowContext(ctx, "SELECT snapshot_id FROM current_index WHERE id = 1").Scan(&snapshotID)
	if errors.Is(err, sql.ErrNoRows) {
		return Index{}, ErrNotFound
	}
	if err != nil {
		return Index{}, fmt.Errorf("read current index pointer: %w", err)
	}
	return loadSnapshotDB(ctx, db, snapshotID)
}

func LoadSnapshot(ctx context.Context, path, snapshotID string) (Index, error) {
	db, err := openReadOnly(ctx, path)
	if err != nil {
		return Index{}, err
	}
	defer db.Close()
	return loadSnapshotDB(ctx, db, snapshotID)
}

func openReadOnly(ctx context.Context, path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("stat index: %w", err)
	}

	db, err := sql.Open("sqlite3", readOnlyDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	db.SetMaxOpenConns(1)

	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		db.Close()
		return nil, fmt.Errorf("read index schema: %w", err)
	}
	if version != schemaVersion {
		db.Close()
		return nil, fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchema, version, schemaVersion)
	}
	return db, nil
}

func loadSnapshotDB(ctx context.Context, db *sql.DB, snapshotID string) (Index, error) {
	var index Index
	const selectSnapshot = "SELECT snapshot_id, base_commit, workspace_fingerprint, state, partial_reason, analysis_version " +
		"FROM snapshots WHERE snapshot_id = ?"
	err := db.QueryRowContext(ctx, selectSnapshot, snapshotID).Scan(
		&index.SnapshotID,
		&index.BaseCommit,
		&index.WorkspaceFingerprint,
		&index.State,
		&index.PartialReason,
		&index.AnalysisVersion,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Index{}, fmt.Errorf("%w: %s", ErrSnapshotNotFound, snapshotID)
	}
	if err != nil {
		return Index{}, fmt.Errorf("read snapshot metadata: %w", err)
	}

	if index.Sources, err = loadSources(ctx, db, snapshotID); err != nil {
		return Index{}, err
	}
	if index.Nodes, err = loadNodes(ctx, db, snapshotID); err != nil {
		return Index{}, err
	}
	if index.Edges, err = loadEdges(ctx, db, snapshotID); err != nil {
		return Index{}, err
	}
	return index, nil
}

func insertSources(ctx context.Context, tx *sql.Tx, snapshotID string, sources []Source) error {
	const query = "INSERT INTO sources " +
		"(snapshot_id, path, kind, executable, content_hash, size) VALUES (?, ?, ?, ?, ?, ?)"
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("prepare source insert: %w", err)
	}
	defer stmt.Close()

	for _, source := range sources {
		if _, err := stmt.ExecContext(
			ctx,
			snapshotID,
			source.Path,
			source.Kind,
			source.Executable,
			source.ContentHash,
			source.Size,
		); err != nil {
			return fmt.Errorf("write source %q: %w", source.Path, err)
		}
	}
	return nil
}

func insertNodes(ctx context.Context, tx *sql.Tx, snapshotID string, nodes []Node) error {
	const query = "INSERT INTO nodes " +
		"(snapshot_id, id, kind, name, path, language, package_path, start_line, end_line, external) " +
		"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("prepare node insert: %w", err)
	}
	defer stmt.Close()

	for _, node := range nodes {
		if _, err := stmt.ExecContext(
			ctx,
			snapshotID,
			node.ID,
			node.Kind,
			node.Name,
			node.Path,
			node.Language,
			node.PackagePath,
			node.StartLine,
			node.EndLine,
			node.External,
		); err != nil {
			return fmt.Errorf("write node %q: %w", node.ID, err)
		}
	}
	return nil
}

func insertEdges(ctx context.Context, tx *sql.Tx, snapshotID string, edges []Edge) error {
	const query = "INSERT INTO edges " +
		"(snapshot_id, from_id, to_id, relation, evidence, resolution, extractor, source_path, start_line) " +
		"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)"
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("prepare edge insert: %w", err)
	}
	defer stmt.Close()

	for _, edge := range edges {
		if _, err := stmt.ExecContext(
			ctx,
			snapshotID,
			edge.From,
			edge.To,
			edge.Relation,
			edge.Evidence,
			edge.Resolution,
			edge.Extractor,
			edge.SourcePath,
			edge.StartLine,
		); err != nil {
			return fmt.Errorf("write edge %q --%s--> %q: %w", edge.From, edge.Relation, edge.To, err)
		}
	}
	return nil
}

func loadSources(ctx context.Context, db *sql.DB, snapshotID string) ([]Source, error) {
	const query = "SELECT path, kind, executable, content_hash, size " +
		"FROM sources WHERE snapshot_id = ? ORDER BY path"
	rows, err := db.QueryContext(ctx, query, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("read sources: %w", err)
	}
	defer rows.Close()

	var sources []Source
	for rows.Next() {
		var source Source
		if err := rows.Scan(
			&source.Path,
			&source.Kind,
			&source.Executable,
			&source.ContentHash,
			&source.Size,
		); err != nil {
			return nil, fmt.Errorf("scan source: %w", err)
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sources: %w", err)
	}
	return sources, nil
}

func loadNodes(ctx context.Context, db *sql.DB, snapshotID string) ([]Node, error) {
	const query = "SELECT id, kind, name, path, language, package_path, start_line, end_line, external " +
		"FROM nodes WHERE snapshot_id = ? ORDER BY id"
	rows, err := db.QueryContext(ctx, query, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("read nodes: %w", err)
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var node Node
		if err := rows.Scan(
			&node.ID,
			&node.Kind,
			&node.Name,
			&node.Path,
			&node.Language,
			&node.PackagePath,
			&node.StartLine,
			&node.EndLine,
			&node.External,
		); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes: %w", err)
	}
	return nodes, nil
}

func loadEdges(ctx context.Context, db *sql.DB, snapshotID string) ([]Edge, error) {
	const query = "SELECT from_id, to_id, relation, evidence, resolution, extractor, source_path, start_line " +
		"FROM edges WHERE snapshot_id = ? ORDER BY from_id, relation, to_id, source_path, start_line"
	rows, err := db.QueryContext(ctx, query, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("read edges: %w", err)
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var edge Edge
		if err := rows.Scan(
			&edge.From,
			&edge.To,
			&edge.Relation,
			&edge.Evidence,
			&edge.Resolution,
			&edge.Extractor,
			&edge.SourcePath,
			&edge.StartLine,
		); err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		edges = append(edges, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate edges: %w", err)
	}
	return edges, nil
}

func deleteSnapshotChildren(ctx context.Context, tx *sql.Tx, snapshotID string) error {
	for _, table := range []string{"edges", "nodes", "sources"} {
		if _, err := tx.ExecContext(
			ctx,
			"DELETE FROM "+table+" WHERE snapshot_id = ?",
			snapshotID,
		); err != nil {
			return fmt.Errorf("clear %s for snapshot %q: %w", table, snapshotID, err)
		}
	}
	return nil
}

func pruneHistory(ctx context.Context, tx *sql.Tx, currentSnapshotID string) error {
	const query = "SELECT snapshot_id FROM snapshots WHERE snapshot_id <> ? " +
		"ORDER BY seq DESC LIMIT -1 OFFSET ?"
	rows, err := tx.QueryContext(ctx, query, currentSnapshotID, historyLimit-1)
	if err != nil {
		return fmt.Errorf("select snapshots to prune: %w", err)
	}

	var pruned []string
	for rows.Next() {
		var snapshotID string
		if err := rows.Scan(&snapshotID); err != nil {
			rows.Close()
			return fmt.Errorf("scan snapshot to prune: %w", err)
		}
		pruned = append(pruned, snapshotID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate snapshots to prune: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close snapshot prune rows: %w", err)
	}

	for _, snapshotID := range pruned {
		if err := deleteSnapshotChildren(ctx, tx, snapshotID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(
			ctx,
			"DELETE FROM snapshots WHERE snapshot_id = ?",
			snapshotID,
		); err != nil {
			return fmt.Errorf("delete pruned snapshot %q: %w", snapshotID, err)
		}
	}
	return nil
}

func ensureSchema(ctx context.Context, tx *sql.Tx) error {
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read index schema: %w", err)
	}
	if version != 0 && version != schemaVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchema, version, schemaVersion)
	}
	if version == schemaVersion {
		return nil
	}

	statements := []string{
		"CREATE TABLE snapshots (" +
			"seq INTEGER PRIMARY KEY AUTOINCREMENT," +
			"snapshot_id TEXT NOT NULL UNIQUE," +
			"base_commit TEXT NOT NULL," +
			"workspace_fingerprint TEXT NOT NULL," +
			"state TEXT NOT NULL," +
			"partial_reason TEXT NOT NULL," +
			"analysis_version INTEGER NOT NULL" +
			")",
		"CREATE TABLE current_index (" +
			"id INTEGER PRIMARY KEY CHECK (id = 1)," +
			"snapshot_id TEXT NOT NULL" +
			")",
		"CREATE TABLE sources (" +
			"snapshot_id TEXT NOT NULL," +
			"path TEXT NOT NULL," +
			"kind TEXT NOT NULL," +
			"executable INTEGER NOT NULL," +
			"content_hash TEXT NOT NULL," +
			"size INTEGER NOT NULL CHECK (size >= 0)," +
			"PRIMARY KEY (snapshot_id, path)" +
			")",
		"CREATE TABLE nodes (" +
			"snapshot_id TEXT NOT NULL," +
			"id TEXT NOT NULL," +
			"kind TEXT NOT NULL," +
			"name TEXT NOT NULL," +
			"path TEXT NOT NULL," +
			"language TEXT NOT NULL," +
			"package_path TEXT NOT NULL," +
			"start_line INTEGER NOT NULL CHECK (start_line >= 0)," +
			"end_line INTEGER NOT NULL CHECK (end_line >= 0)," +
			"external INTEGER NOT NULL," +
			"PRIMARY KEY (snapshot_id, id)" +
			")",
		"CREATE TABLE edges (" +
			"snapshot_id TEXT NOT NULL," +
			"from_id TEXT NOT NULL," +
			"to_id TEXT NOT NULL," +
			"relation TEXT NOT NULL," +
			"evidence TEXT NOT NULL," +
			"resolution TEXT NOT NULL," +
			"extractor TEXT NOT NULL," +
			"source_path TEXT NOT NULL," +
			"start_line INTEGER NOT NULL CHECK (start_line >= 0)," +
			"PRIMARY KEY (snapshot_id, from_id, to_id, relation, evidence, resolution, extractor, source_path, start_line)" +
			")",
		"PRAGMA user_version = 3",
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create index schema: %w", err)
		}
	}
	return nil
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
