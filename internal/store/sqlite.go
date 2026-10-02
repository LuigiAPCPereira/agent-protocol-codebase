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

const schemaVersion = 2

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

	for _, table := range []string{"edges", "nodes", "sources", "current_index"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}

	const insertIndex = "INSERT INTO current_index " +
		"(id, snapshot_id, base_commit, workspace_fingerprint, state, partial_reason, analysis_version) " +
		"VALUES (1, ?, ?, ?, ?, ?, ?)"
	if _, err := tx.ExecContext(
		ctx,
		insertIndex,
		index.SnapshotID,
		index.BaseCommit,
		index.WorkspaceFingerprint,
		index.State,
		index.PartialReason,
		index.AnalysisVersion,
	); err != nil {
		return fmt.Errorf("write current index: %w", err)
	}

	if err := insertSources(ctx, tx, index.Sources); err != nil {
		return err
	}
	if err := insertNodes(ctx, tx, index.Nodes); err != nil {
		return err
	}
	if err := insertEdges(ctx, tx, index.Edges); err != nil {
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
	const selectIndex = "SELECT snapshot_id, base_commit, workspace_fingerprint, state, partial_reason, analysis_version " +
		"FROM current_index WHERE id = 1"
	err = db.QueryRowContext(ctx, selectIndex).Scan(
		&index.SnapshotID,
		&index.BaseCommit,
		&index.WorkspaceFingerprint,
		&index.State,
		&index.PartialReason,
		&index.AnalysisVersion,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Index{}, ErrNotFound
	}
	if err != nil {
		return Index{}, fmt.Errorf("read current index: %w", err)
	}

	if index.Sources, err = loadSources(ctx, db); err != nil {
		return Index{}, err
	}
	if index.Nodes, err = loadNodes(ctx, db); err != nil {
		return Index{}, err
	}
	if index.Edges, err = loadEdges(ctx, db); err != nil {
		return Index{}, err
	}
	return index, nil
}

func insertSources(ctx context.Context, tx *sql.Tx, sources []Source) error {
	const query = "INSERT INTO sources (path, kind, executable, content_hash, size) VALUES (?, ?, ?, ?, ?)"
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("prepare source insert: %w", err)
	}
	defer stmt.Close()

	for _, source := range sources {
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
	return nil
}

func insertNodes(ctx context.Context, tx *sql.Tx, nodes []Node) error {
	const query = "INSERT INTO nodes " +
		"(id, kind, name, path, language, package_path, start_line, end_line, external) " +
		"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)"
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("prepare node insert: %w", err)
	}
	defer stmt.Close()

	for _, node := range nodes {
		if _, err := stmt.ExecContext(
			ctx,
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

func insertEdges(ctx context.Context, tx *sql.Tx, edges []Edge) error {
	const query = "INSERT INTO edges " +
		"(from_id, to_id, relation, evidence, resolution, extractor, source_path, start_line) " +
		"VALUES (?, ?, ?, ?, ?, ?, ?, ?)"
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("prepare edge insert: %w", err)
	}
	defer stmt.Close()

	for _, edge := range edges {
		if _, err := stmt.ExecContext(
			ctx,
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

func loadSources(ctx context.Context, db *sql.DB) ([]Source, error) {
	rows, err := db.QueryContext(ctx, "SELECT path, kind, executable, content_hash, size FROM sources ORDER BY path")
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

func loadNodes(ctx context.Context, db *sql.DB) ([]Node, error) {
	const query = "SELECT id, kind, name, path, language, package_path, start_line, end_line, external " +
		"FROM nodes ORDER BY id"
	rows, err := db.QueryContext(ctx, query)
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

func loadEdges(ctx context.Context, db *sql.DB) ([]Edge, error) {
	const query = "SELECT from_id, to_id, relation, evidence, resolution, extractor, source_path, start_line " +
		"FROM edges ORDER BY from_id, relation, to_id, source_path, start_line"
	rows, err := db.QueryContext(ctx, query)
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
		"CREATE TABLE current_index (" +
			"id INTEGER PRIMARY KEY CHECK (id = 1)," +
			"snapshot_id TEXT NOT NULL," +
			"base_commit TEXT NOT NULL," +
			"workspace_fingerprint TEXT NOT NULL," +
			"state TEXT NOT NULL," +
			"partial_reason TEXT NOT NULL," +
			"analysis_version INTEGER NOT NULL" +
			")",
		"CREATE TABLE sources (" +
			"path TEXT PRIMARY KEY," +
			"kind TEXT NOT NULL," +
			"executable INTEGER NOT NULL," +
			"content_hash TEXT NOT NULL," +
			"size INTEGER NOT NULL CHECK (size >= 0)" +
			")",
		"CREATE TABLE nodes (" +
			"id TEXT PRIMARY KEY," +
			"kind TEXT NOT NULL," +
			"name TEXT NOT NULL," +
			"path TEXT NOT NULL," +
			"language TEXT NOT NULL," +
			"package_path TEXT NOT NULL," +
			"start_line INTEGER NOT NULL CHECK (start_line >= 0)," +
			"end_line INTEGER NOT NULL CHECK (end_line >= 0)," +
			"external INTEGER NOT NULL" +
			")",
		"CREATE TABLE edges (" +
			"from_id TEXT NOT NULL," +
			"to_id TEXT NOT NULL," +
			"relation TEXT NOT NULL," +
			"evidence TEXT NOT NULL," +
			"resolution TEXT NOT NULL," +
			"extractor TEXT NOT NULL," +
			"source_path TEXT NOT NULL," +
			"start_line INTEGER NOT NULL CHECK (start_line >= 0)," +
			"PRIMARY KEY (from_id, to_id, relation, evidence, resolution, extractor, source_path, start_line)" +
			")",
		"PRAGMA user_version = 2",
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
