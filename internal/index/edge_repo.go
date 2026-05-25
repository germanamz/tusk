package index

import (
	"database/sql"
	"fmt"
	"strings"
)

// EdgeRow is the index representation of a single edge.
type EdgeRow struct {
	Type       string
	SourceID   string
	TargetID   string
	SourcePath string
}

// EdgeRepo persists EdgeRow values in the SQLite index.
type EdgeRepo struct {
	db *sql.DB
}

// NewEdgeRepo constructs an EdgeRepo backed by idx.
func NewEdgeRepo(idx *Index) *EdgeRepo {
	return &EdgeRepo{db: idx.DB()}
}

// UpsertAll replaces every edge declared by sourcePath with the provided set.
// The replacement is transactional: existing edges where source_id = sourceID
// AND source_path = sourcePath are deleted, then the new edges are inserted.
func (repo *EdgeRepo) UpsertAll(sourceID, sourcePath string, edges []EdgeRow) error {
	tx, beginErr := repo.db.Begin()

	if beginErr != nil {
		return fmt.Errorf("edgeRepo: begin: %w", beginErr)
	}

	if _, deleteErr := tx.Exec(`DELETE FROM edges WHERE source_id = ? AND source_path = ?`, sourceID, sourcePath); deleteErr != nil {
		_ = tx.Rollback()
		return fmt.Errorf("edgeRepo: delete %s: %w", sourceID, deleteErr)
	}

	for _, edge := range edges {
		if _, insertErr := tx.Exec(`
			INSERT INTO edges (type, source_id, target_id, source_path)
			VALUES (?, ?, ?, ?)
		`, edge.Type, edge.SourceID, edge.TargetID, edge.SourcePath); insertErr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("edgeRepo: insert %s→%s: %w", edge.SourceID, edge.TargetID, insertErr)
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("edgeRepo: commit: %w", commitErr)
	}

	return nil
}

// ListBySource returns all edges where source_id = sourceID, ordered by type then target_id.
func (repo *EdgeRepo) ListBySource(sourceID string) ([]EdgeRow, error) {
	return repo.queryEdges(`SELECT type, source_id, target_id, source_path FROM edges WHERE source_id = ? ORDER BY type, target_id`, sourceID)
}

// ListByTarget returns all edges where target_id = targetID, ordered by type then source_id.
func (repo *EdgeRepo) ListByTarget(targetID string) ([]EdgeRow, error) {
	return repo.queryEdges(`SELECT type, source_id, target_id, source_path FROM edges WHERE target_id = ? ORDER BY type, source_id`, targetID)
}

// ListByType returns all edges where type = edgeType, ordered by source_id then target_id.
func (repo *EdgeRepo) ListByType(edgeType string) ([]EdgeRow, error) {
	return repo.queryEdges(`SELECT type, source_id, target_id, source_path FROM edges WHERE type = ? ORDER BY source_id, target_id`, edgeType)
}

// ListAll returns every edge in the index, ordered by source_id, type, target_id.
func (repo *EdgeRepo) ListAll() ([]EdgeRow, error) {
	rows, queryErr := repo.db.Query(`
		SELECT type, source_id, target_id, source_path
		FROM edges
		ORDER BY source_id, type, target_id
	`)

	if queryErr != nil {
		return nil, queryErr
	}

	defer rows.Close()

	var out []EdgeRow

	for rows.Next() {
		var row EdgeRow

		if scanErr := rows.Scan(&row.Type, &row.SourceID, &row.TargetID, &row.SourcePath); scanErr != nil {
			return nil, scanErr
		}

		out = append(out, row)
	}

	return out, rows.Err()
}

// DeleteBySourceAndType removes every edge whose (source_id, type) matches
// sourceID and edgeType. Used by the sub-unit sync pipeline to clear the
// file's `contains` edges before rewriting them, without touching the
// file's frontmatter-derived edges (which carry the same source_id but
// different type values).
func (repo *EdgeRepo) DeleteBySourceAndType(sourceID, edgeType string) error {
	_, execErr := repo.db.Exec(`DELETE FROM edges WHERE source_id = ? AND type = ?`, sourceID, edgeType)

	if execErr != nil {
		return fmt.Errorf("edgeRepo: delete source %s type %s: %w", sourceID, edgeType, execErr)
	}

	return nil
}

// InsertIgnore inserts every row, skipping duplicates that violate the
// UNIQUE(type, source_id, target_id, source_path) constraint. Used by the
// sub-unit sync pipeline to additively write the file's `contains` edges
// without first wiping unrelated edges. Wraps the inserts in a single
// transaction so partial failure rolls back.
func (repo *EdgeRepo) InsertIgnore(edges []EdgeRow) error {
	if len(edges) == 0 {
		return nil
	}

	tx, beginErr := repo.db.Begin()

	if beginErr != nil {
		return fmt.Errorf("edgeRepo: insert-ignore begin: %w", beginErr)
	}

	stmt, prepErr := tx.Prepare(`INSERT OR IGNORE INTO edges (type, source_id, target_id, source_path) VALUES (?, ?, ?, ?)`)

	if prepErr != nil {
		_ = tx.Rollback()
		return fmt.Errorf("edgeRepo: insert-ignore prepare: %w", prepErr)
	}

	for _, edge := range edges {
		if _, execErr := stmt.Exec(edge.Type, edge.SourceID, edge.TargetID, edge.SourcePath); execErr != nil {
			stmt.Close()
			_ = tx.Rollback()
			return fmt.Errorf("edgeRepo: insert-ignore %s→%s: %w", edge.SourceID, edge.TargetID, execErr)
		}
	}

	stmt.Close()

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("edgeRepo: insert-ignore commit: %w", commitErr)
	}

	return nil
}

// NeighborsByEdgeTypes returns every edge whose type matches one of edgeTypes
// and whose source or target endpoint is in sourceIDs. Used by the graph-expand
// walker (internal/graphexpand) to fetch all hop-N neighbors in a single SQL
// call.
//
// Both inputs are deduplicated before binding to avoid pathological argument
// counts; with K seeds and T edge types the bound is K + T placeholders, well
// under the SQLite default limit of 32766. Returns an empty slice (no error)
// when either input is empty.
func (repo *EdgeRepo) NeighborsByEdgeTypes(sourceIDs, edgeTypes []string) ([]EdgeRow, error) {
	if len(sourceIDs) == 0 || len(edgeTypes) == 0 {
		return nil, nil
	}

	uniqueIDs := dedupStrings(sourceIDs)
	uniqueTypes := dedupStrings(edgeTypes)

	idPlaceholders := strings.TrimRight(strings.Repeat("?,", len(uniqueIDs)), ",")
	typePlaceholders := strings.TrimRight(strings.Repeat("?,", len(uniqueTypes)), ",")

	queryText := fmt.Sprintf(`
		SELECT type, source_id, target_id, source_path
		FROM edges
		WHERE type IN (%s)
		  AND (source_id IN (%s) OR target_id IN (%s))
		ORDER BY type, source_id, target_id
	`, typePlaceholders, idPlaceholders, idPlaceholders)

	args := make([]any, 0, len(uniqueTypes)+2*len(uniqueIDs))

	for _, edgeType := range uniqueTypes {
		args = append(args, edgeType)
	}

	for _, sourceID := range uniqueIDs {
		args = append(args, sourceID)
	}

	for _, sourceID := range uniqueIDs {
		args = append(args, sourceID)
	}

	rows, queryErr := repo.db.Query(queryText, args...)

	if queryErr != nil {
		return nil, fmt.Errorf("edgeRepo: neighbors-by-edge-types query: %w", queryErr)
	}

	defer rows.Close()

	var results []EdgeRow

	for rows.Next() {
		row := EdgeRow{}

		if scanErr := rows.Scan(&row.Type, &row.SourceID, &row.TargetID, &row.SourcePath); scanErr != nil {
			return nil, fmt.Errorf("edgeRepo: neighbors-by-edge-types scan: %w", scanErr)
		}

		results = append(results, row)
	}

	if iterErr := rows.Err(); iterErr != nil {
		return nil, fmt.Errorf("edgeRepo: neighbors-by-edge-types iterate: %w", iterErr)
	}

	return results, nil
}

func dedupStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))

	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}

		seen[value] = struct{}{}
		out = append(out, value)
	}

	return out
}

// DeleteBySource removes every edge where source_id = sourceID, regardless of source_path.
func (repo *EdgeRepo) DeleteBySource(sourceID string) error {
	_, execErr := repo.db.Exec(`DELETE FROM edges WHERE source_id = ?`, sourceID)

	if execErr != nil {
		return fmt.Errorf("edgeRepo: delete source %s: %w", sourceID, execErr)
	}

	return nil
}

func (repo *EdgeRepo) queryEdges(query, arg string) ([]EdgeRow, error) {
	rows, queryErr := repo.db.Query(query, arg)

	if queryErr != nil {
		return nil, fmt.Errorf("edgeRepo: query: %w", queryErr)
	}

	defer rows.Close()

	var results []EdgeRow

	for rows.Next() {
		row := EdgeRow{}

		if scanErr := rows.Scan(&row.Type, &row.SourceID, &row.TargetID, &row.SourcePath); scanErr != nil {
			return nil, fmt.Errorf("edgeRepo: scan: %w", scanErr)
		}

		results = append(results, row)
	}

	return results, rows.Err()
}
