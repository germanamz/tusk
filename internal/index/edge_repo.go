package index

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/germanamz/tusk/internal/typeref"
)

// EdgeRow is the index representation of a single edge.
//
// Kind classifies the writer path that produced this edge:
//   - "direct"     — value written by the user in YAML frontmatter (or in a
//     wikilink) and matched to a user-declared edge type.
//   - "derived"    — synthesized from a node-type's ref-property declaration.
//   - "structural" — produced by the sub-unit pipeline (contains / contained-by).
//
// Source carries the namespace identifier when present (e.g. "markdown" for
// structural sub-unit edges). NULL for direct and derived edges.
type EdgeRow struct {
	Type       string
	SourceID   string
	TargetID   string
	SourcePath string
	Kind       string
	Source     sql.NullString
}

// EdgeRepo persists EdgeRow values in the SQLite index.
type EdgeRepo struct {
	db *sql.DB
}

// NewEdgeRepo constructs an EdgeRepo backed by idx.
func NewEdgeRepo(idx *Index) *EdgeRepo {
	return &EdgeRepo{db: idx.DB()}
}

// edgeColumns is the column list shared by every edges-table read and write,
// in the order EdgeRow's fields are scanned.
const edgeColumns = "type, source_id, target_id, source_path, kind, source"

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
			INSERT INTO edges (`+edgeColumns+`)
			VALUES (?, ?, ?, ?, ?, ?)
		`, edge.Type, edge.SourceID, edge.TargetID, edge.SourcePath, edge.Kind, edge.Source); insertErr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("edgeRepo: insert %s→%s: %w", edge.SourceID, edge.TargetID, insertErr)
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("edgeRepo: commit: %w", commitErr)
	}

	return nil
}

// EdgeBatch is one (source_id, source_path) replacement for UpsertAllMany.
type EdgeBatch struct {
	SourceID   string
	SourcePath string
	Edges      []EdgeRow
}

// UpsertAllMany applies several UpsertAll replacements in a SINGLE transaction.
// For each batch it deletes the edges where source_id = SourceID AND
// source_path = SourcePath, then inserts that batch's edges — the same
// delete-then-insert contract as UpsertAll, so stale edges are removed rather
// than orphaned (an additive insert-or-ignore would leave them behind). It
// collapses the O(batches) commits of calling UpsertAll in a loop into one,
// which is what the sub-unit sync uses to write a whole file's edges at once.
// An empty slice is a no-op.
func (repo *EdgeRepo) UpsertAllMany(batches []EdgeBatch) error {
	if len(batches) == 0 {
		return nil
	}

	tx, beginErr := repo.db.Begin()

	if beginErr != nil {
		return fmt.Errorf("edgeRepo: upsert-all-many begin: %w", beginErr)
	}

	for _, batch := range batches {
		if _, deleteErr := tx.Exec(`DELETE FROM edges WHERE source_id = ? AND source_path = ?`, batch.SourceID, batch.SourcePath); deleteErr != nil {
			_ = tx.Rollback()

			return fmt.Errorf("edgeRepo: upsert-all-many delete %s: %w", batch.SourceID, deleteErr)
		}

		for _, edge := range batch.Edges {
			if _, insertErr := tx.Exec(`
				INSERT INTO edges (`+edgeColumns+`)
				VALUES (?, ?, ?, ?, ?, ?)
			`, edge.Type, edge.SourceID, edge.TargetID, edge.SourcePath, edge.Kind, edge.Source); insertErr != nil {
				_ = tx.Rollback()

				return fmt.Errorf("edgeRepo: upsert-all-many insert %s→%s: %w", edge.SourceID, edge.TargetID, insertErr)
			}
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("edgeRepo: upsert-all-many commit: %w", commitErr)
	}

	return nil
}

// ListBySource returns all edges where source_id = sourceID, ordered by type then target_id.
func (repo *EdgeRepo) ListBySource(sourceID string) ([]EdgeRow, error) {
	return repo.queryEdges(`SELECT `+edgeColumns+` FROM edges WHERE source_id = ? ORDER BY type, target_id`, sourceID)
}

// ListByTarget returns all edges where target_id = targetID, ordered by type then source_id.
func (repo *EdgeRepo) ListByTarget(targetID string) ([]EdgeRow, error) {
	return repo.queryEdges(`SELECT `+edgeColumns+` FROM edges WHERE target_id = ? ORDER BY type, source_id`, targetID)
}

// ListByTargetOrSubUnits returns all edges targeting nodeID or any sub-unit
// id under it ("<nodeID>#..."), ordered by type then source_id. Rename uses
// it to find every referring file, including ones that deep-link a section
// or paragraph of the moved node.
//
// The prefix length is measured SQL-side with length(?): SQLite's substr and
// length count UTF-8 characters, not bytes, so binding Go's len() here would
// silently skip every id containing a multi-byte character.
func (repo *EdgeRepo) ListByTargetOrSubUnits(nodeID string) ([]EdgeRow, error) {
	prefix := nodeID + "#"

	return repo.queryEdges(
		`SELECT `+edgeColumns+` FROM edges WHERE target_id = ? OR substr(target_id, 1, length(?)) = ? ORDER BY type, source_id`,
		nodeID, prefix, prefix,
	)
}

// ListByType returns all edges where type = edgeType, ordered by source_id then target_id.
func (repo *EdgeRepo) ListByType(edgeType string) ([]EdgeRow, error) {
	return repo.queryEdges(`SELECT `+edgeColumns+` FROM edges WHERE type = ? ORDER BY source_id, target_id`, edgeType)
}

// RetargetEdges rewrites the target of every edge pointing at oldID — or at
// a sub-unit id under it ("<oldID>#...") — to the same address under newID,
// regardless of the edge's source. This is the rename fix-up for incoming
// rows the per-file re-derive cannot reach: edges sourced from other files'
// sub-units, and edges targeting the moved file's sub-unit ids.
//
// All offsets are measured SQL-side (length(?)) because SQLite's substr and
// length count UTF-8 characters while Go's len() counts bytes — binding byte
// lengths would skip or corrupt every id with a multi-byte character.
//
// OR REPLACE resolves collisions with an already-existing edge at the new
// target, but SQLite's UNIQUE treats NULL `source` values as distinct, so a
// direct/derived duplicate survives the UPDATE — the follow-up DELETE
// removes those, keeping the oldest row per identity.
func (repo *EdgeRepo) RetargetEdges(oldID, newID string) error {
	tx, beginErr := repo.db.Begin()

	if beginErr != nil {
		return fmt.Errorf("edgeRepo: retarget begin: %w", beginErr)
	}

	prefix := oldID + "#"

	if _, execErr := tx.Exec(`
		UPDATE OR REPLACE edges
		SET target_id = ?1 || substr(target_id, length(?2) + 1)
		WHERE target_id = ?2 OR substr(target_id, 1, length(?3)) = ?3`,
		newID, oldID, prefix,
	); execErr != nil {
		_ = tx.Rollback()

		return fmt.Errorf("edgeRepo: retarget %s -> %s: %w", oldID, newID, execErr)
	}

	if _, execErr := tx.Exec(`
		DELETE FROM edges WHERE (target_id = ?1 OR substr(target_id, 1, length(?2)) = ?2) AND id NOT IN (
			SELECT MIN(id) FROM edges
			WHERE target_id = ?1 OR substr(target_id, 1, length(?2)) = ?2
			GROUP BY type, source_id, target_id, source_path, kind, COALESCE(source, '')
		)`,
		newID, newID+"#",
	); execErr != nil {
		_ = tx.Rollback()

		return fmt.Errorf("edgeRepo: retarget dedupe %s: %w", newID, execErr)
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("edgeRepo: retarget commit: %w", commitErr)
	}

	return nil
}

// Count returns the total number of edges in the index.
func (repo *EdgeRepo) Count() (int, error) {
	var count int

	if scanErr := repo.db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&count); scanErr != nil {
		return 0, fmt.Errorf("edgeRepo: count: %w", scanErr)
	}

	return count, nil
}

// ListByEdgeRef returns every edge matching ref's scope semantics, ordered
// by source_id then target_id. Scope mapping mirrors NeighborsByEdgeRefs:
//
//	ScopeAny    → type = ?
//	ScopeUser   → source IS NULL AND type = ?
//	ScopeSource → source = ?       AND type = ?
func (repo *EdgeRepo) ListByEdgeRef(ref typeref.Ref) ([]EdgeRow, error) {
	const selectClause = "SELECT " + edgeColumns + " FROM edges WHERE "
	const orderClause = ` ORDER BY source_id, target_id`

	var (
		whereClause string
		args        []any
	)

	switch ref.Scope {
	case typeref.ScopeAny:
		whereClause = `type = ?`
		args = []any{ref.Type}
	case typeref.ScopeUser:
		whereClause = `source IS NULL AND type = ?`
		args = []any{ref.Type}
	case typeref.ScopeSource:
		whereClause = `source = ? AND type = ?`
		args = []any{ref.Source, ref.Type}
	default:
		return nil, fmt.Errorf("edgeRepo: unsupported ref scope %v", ref.Scope)
	}

	rows, queryErr := repo.db.Query(selectClause+whereClause+orderClause, args...)

	if queryErr != nil {
		return nil, fmt.Errorf("edgeRepo: list-by-edge-ref query: %w", queryErr)
	}

	return scanEdgeRows(rows)
}

// ListAll returns every edge in the index, ordered by source_id, type, target_id.
func (repo *EdgeRepo) ListAll() ([]EdgeRow, error) {
	rows, queryErr := repo.db.Query(`
		SELECT ` + edgeColumns + `
		FROM edges
		ORDER BY source_id, type, target_id
	`)

	if queryErr != nil {
		return nil, queryErr
	}

	return scanEdgeRows(rows)
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

	stmt, prepErr := tx.Prepare(`INSERT OR IGNORE INTO edges (` + edgeColumns + `) VALUES (?, ?, ?, ?, ?, ?)`)

	if prepErr != nil {
		_ = tx.Rollback()
		return fmt.Errorf("edgeRepo: insert-ignore prepare: %w", prepErr)
	}

	for _, edge := range edges {
		if _, execErr := stmt.Exec(edge.Type, edge.SourceID, edge.TargetID, edge.SourcePath, edge.Kind, edge.Source); execErr != nil {
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

// NeighborsByEdgeRefs accepts parsed EdgeRef values and builds a grouped
// OR predicate so each scope maps to its correct SQL form:
//
//	ScopeAny    → type = ?
//	ScopeUser   → source IS NULL AND type = ?
//	ScopeSource → source = ?       AND type = ?
//
// Multiple refs are combined with OR. Edges match when either source_id
// or target_id is in sourceIDs.
func (repo *EdgeRepo) NeighborsByEdgeRefs(refs []typeref.EdgeRef, sourceIDs []string) ([]EdgeRow, error) {
	if len(refs) == 0 || len(sourceIDs) == 0 {
		return nil, nil
	}

	uniqueIDs := dedupStrings(sourceIDs)

	clauses := make([]string, 0, len(refs))
	args := make([]any, 0, len(refs)*2+2*len(uniqueIDs))

	for _, ref := range refs {
		switch ref.Scope {
		case typeref.ScopeAny:
			clauses = append(clauses, "type = ?")
			args = append(args, ref.Type)
		case typeref.ScopeUser:
			clauses = append(clauses, "(source IS NULL AND type = ?)")
			args = append(args, ref.Type)
		case typeref.ScopeSource:
			clauses = append(clauses, "(source = ? AND type = ?)")
			args = append(args, ref.Source, ref.Type)
		default:
			return nil, fmt.Errorf("edgeRepo: unsupported ref scope %v", ref.Scope)
		}
	}

	idPlaceholders := inPlaceholders(len(uniqueIDs))
	whereRefs := "(" + strings.Join(clauses, " OR ") + ")"

	queryText := fmt.Sprintf(`
		SELECT `+edgeColumns+`
		FROM edges
		WHERE %s
		  AND (source_id IN (%s) OR target_id IN (%s))
		ORDER BY type, source_id, target_id
	`, whereRefs, idPlaceholders, idPlaceholders)

	for _, id := range uniqueIDs {
		args = append(args, id)
	}

	for _, id := range uniqueIDs {
		args = append(args, id)
	}

	rows, queryErr := repo.db.Query(queryText, args...)

	if queryErr != nil {
		return nil, fmt.Errorf("edgeRepo: neighbors-by-edge-refs query: %w", queryErr)
	}

	return scanEdgeRows(rows)
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

func (repo *EdgeRepo) queryEdges(query string, args ...any) ([]EdgeRow, error) {
	rows, queryErr := repo.db.Query(query, args...)

	if queryErr != nil {
		return nil, fmt.Errorf("edgeRepo: query: %w", queryErr)
	}

	return scanEdgeRows(rows)
}

// scanEdgeRows drains rows into EdgeRow values, scanning columns in the
// order declared by edgeColumns. It closes rows before returning and
// surfaces both scan and iteration errors.
func scanEdgeRows(rows *sql.Rows) ([]EdgeRow, error) {
	defer rows.Close()

	var results []EdgeRow

	for rows.Next() {
		row := EdgeRow{}

		if scanErr := rows.Scan(&row.Type, &row.SourceID, &row.TargetID, &row.SourcePath, &row.Kind, &row.Source); scanErr != nil {
			return nil, fmt.Errorf("edgeRepo: scan: %w", scanErr)
		}

		results = append(results, row)
	}

	return results, rows.Err()
}
