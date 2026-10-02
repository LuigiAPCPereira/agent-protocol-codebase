package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidQuery = errors.New("invalid graph query")

type QueryFilter struct {
	Name    string
	Path    string
	Kind    string
	Package string
	Limit   int
}

type QueryResult struct {
	Matches   []string
	Nodes     []Node
	Edges     []Edge
	Truncated bool
}

func Query(ctx context.Context, path string, filter QueryFilter) (QueryResult, error) {
	if strings.TrimSpace(filter.Name) == "" &&
		strings.TrimSpace(filter.Path) == "" &&
		strings.TrimSpace(filter.Kind) == "" &&
		strings.TrimSpace(filter.Package) == "" {
		return QueryResult{}, fmt.Errorf("%w: at least one selector is required", ErrInvalidQuery)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	db, err := sql.Open("sqlite3", readOnlyDSN(path))
	if err != nil {
		return QueryResult{}, fmt.Errorf("open index: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return QueryResult{}, fmt.Errorf("read index schema: %w", err)
	}
	if version != schemaVersion {
		return QueryResult{}, fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchema, version, schemaVersion)
	}

	var snapshotID string
	if err := db.QueryRowContext(ctx, "SELECT snapshot_id FROM current_index WHERE id = 1").Scan(&snapshotID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return QueryResult{}, ErrNotFound
		}
		return QueryResult{}, fmt.Errorf("read current index pointer: %w", err)
	}

	where, args := nodeWhere(filter)
	args = append([]any{snapshotID}, args...)
	args = append(args, limit+1)

	const columns = "id, kind, name, path, language, package_path, start_line, end_line, external"
	query := "SELECT " + columns + " FROM nodes WHERE snapshot_id = ? AND (" + where + ")" +
		" ORDER BY external ASC, lower(name), id LIMIT ?"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return QueryResult{}, fmt.Errorf("query graph nodes: %w", err)
	}

	var matched []Node
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
			rows.Close()
			return QueryResult{}, fmt.Errorf("scan graph node: %w", err)
		}
		matched = append(matched, node)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return QueryResult{}, fmt.Errorf("iterate graph nodes: %w", err)
	}
	if err := rows.Close(); err != nil {
		return QueryResult{}, fmt.Errorf("close graph node rows: %w", err)
	}

	result := QueryResult{}
	if len(matched) > limit {
		result.Truncated = true
		matched = matched[:limit]
	}
	if len(matched) == 0 {
		return result, nil
	}

	nodeByID := make(map[string]Node, len(matched))
	matchIDs := make([]string, 0, len(matched))
	for _, node := range matched {
		nodeByID[node.ID] = node
		matchIDs = append(matchIDs, node.ID)
		result.Matches = append(result.Matches, node.ID)
	}

	edgeLimit := limit * 8
	if edgeLimit < 16 {
		edgeLimit = 16
	}
	if edgeLimit > 100 {
		edgeLimit = 100
	}

	edges, truncatedEdges, err := incidentEdges(ctx, db, snapshotID, matchIDs, edgeLimit)
	if err != nil {
		return QueryResult{}, err
	}
	if truncatedEdges {
		result.Truncated = true
	}
	result.Edges = edges

	endpointIDs := make(map[string]struct{})
	for _, edge := range edges {
		if _, ok := nodeByID[edge.From]; !ok {
			endpointIDs[edge.From] = struct{}{}
		}
		if _, ok := nodeByID[edge.To]; !ok {
			endpointIDs[edge.To] = struct{}{}
		}
	}

	if len(endpointIDs) > 0 {
		neighbors, err := nodesByID(ctx, db, snapshotID, endpointIDs)
		if err != nil {
			return QueryResult{}, err
		}
		for _, node := range neighbors {
			nodeByID[node.ID] = node
		}
	}

	result.Nodes = make([]Node, 0, len(nodeByID))
	for _, node := range nodeByID {
		result.Nodes = append(result.Nodes, node)
	}
	sortNodes(result.Nodes)
	return result, nil
}

func nodeWhere(filter QueryFilter) (string, []any) {
	var clauses []string
	var args []any

	if value := strings.TrimSpace(filter.Name); value != "" {
		clauses = append(clauses, "instr(lower(name), lower(?)) > 0")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Path); value != "" {
		clauses = append(clauses, "instr(lower(path), lower(?)) > 0")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Kind); value != "" {
		clauses = append(clauses, "upper(kind) = upper(?)")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Package); value != "" {
		clauses = append(clauses, "instr(lower(package_path), lower(?)) > 0")
		args = append(args, value)
	}

	return strings.Join(clauses, " AND "), args
}

func incidentEdges(
	ctx context.Context,
	db *sql.DB,
	snapshotID string,
	matchIDs []string,
	limit int,
) ([]Edge, bool, error) {
	placeholders := questionMarks(len(matchIDs))
	args := make([]any, 0, len(matchIDs)*2+2)
	args = append(args, snapshotID)
	for _, id := range matchIDs {
		args = append(args, id)
	}
	for _, id := range matchIDs {
		args = append(args, id)
	}
	args = append(args, limit+1)

	query := "SELECT from_id, to_id, relation, evidence, resolution, extractor, source_path, start_line " +
		"FROM edges WHERE snapshot_id = ? AND (from_id IN (" + placeholders + ") OR to_id IN (" + placeholders + ")) " +
		"ORDER BY from_id, relation, to_id, source_path, start_line LIMIT ?"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("query incident edges: %w", err)
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
			return nil, false, fmt.Errorf("scan incident edge: %w", err)
		}
		edges = append(edges, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate incident edges: %w", err)
	}

	truncated := len(edges) > limit
	if truncated {
		edges = edges[:limit]
	}
	return edges, truncated, nil
}

func nodesByID(ctx context.Context, db *sql.DB, snapshotID string, ids map[string]struct{}) ([]Node, error) {
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sortStrings(ordered)

	args := make([]any, 0, len(ordered)+1)
	args = append(args, snapshotID)
	for _, id := range ordered {
		args = append(args, id)
	}

	const columns = "id, kind, name, path, language, package_path, start_line, end_line, external"
	query := "SELECT " + columns + " FROM nodes WHERE snapshot_id = ? AND id IN (" + questionMarks(len(ordered)) + ") ORDER BY id"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query endpoint nodes: %w", err)
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
			return nil, fmt.Errorf("scan endpoint node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate endpoint nodes: %w", err)
	}
	return nodes, nil
}

func questionMarks(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

func sortNodes(nodes []Node) {
	for i := 1; i < len(nodes); i++ {
		for j := i; j > 0 && nodes[j].ID < nodes[j-1].ID; j-- {
			nodes[j], nodes[j-1] = nodes[j-1], nodes[j]
		}
	}
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
