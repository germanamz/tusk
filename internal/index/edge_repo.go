package index

import (
	"database/sql"
	"fmt"
)

// EdgeRow is the index representation of a single edge.
type EdgeRow struct {
	Type       string
	SourceID   string
	TargetID   string
	Ordinal    int
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
			INSERT INTO edges (type, source_id, target_id, ordinal, source_path)
			VALUES (?, ?, ?, ?, ?)
		`, edge.Type, edge.SourceID, edge.TargetID, edge.Ordinal, edge.SourcePath); insertErr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("edgeRepo: insert %s→%s: %w", edge.SourceID, edge.TargetID, insertErr)
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("edgeRepo: commit: %w", commitErr)
	}

	return nil
}

// ListBySource returns all edges where source_id = sourceID, ordered by type, ordinal.
func (repo *EdgeRepo) ListBySource(sourceID string) ([]EdgeRow, error) {
	return repo.queryEdges(`SELECT type, source_id, target_id, ordinal, source_path FROM edges WHERE source_id = ? ORDER BY type, ordinal`, sourceID)
}

// ListByTarget returns all edges where target_id = targetID, ordered by type.
func (repo *EdgeRepo) ListByTarget(targetID string) ([]EdgeRow, error) {
	return repo.queryEdges(`SELECT type, source_id, target_id, ordinal, source_path FROM edges WHERE target_id = ? ORDER BY type, source_id`, targetID)
}

// ListByType returns all edges where type = edgeType, ordered by source_id, ordinal.
func (repo *EdgeRepo) ListByType(edgeType string) ([]EdgeRow, error) {
	return repo.queryEdges(`SELECT type, source_id, target_id, ordinal, source_path FROM edges WHERE type = ? ORDER BY source_id, ordinal`, edgeType)
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

		if scanErr := rows.Scan(&row.Type, &row.SourceID, &row.TargetID, &row.Ordinal, &row.SourcePath); scanErr != nil {
			return nil, fmt.Errorf("edgeRepo: scan: %w", scanErr)
		}

		results = append(results, row)
	}

	return results, rows.Err()
}
