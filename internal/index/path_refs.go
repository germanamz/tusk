package index

import (
	"cmp"
	"database/sql"
	"fmt"
	"slices"
)

// PathRefRow is one path_refs row: a page (SourceID, a file node id) naming a
// workspace path (Target) under a paths edge type (Type) on a file line (Line,
// 0 when the format keeps no positions).
type PathRefRow struct {
	SourceID string
	Type     string
	Target   string
	Line     int
}

// The path ref methods hang off EdgeRepo because a path ref is an edge whose
// target is a path: every pipeline that writes a page's edges (reindex, node
// create/modify/move, edge add/remove) already holds this handle and rewrites
// the page's refs at the same point.

const pathRefColumns = "source_id, type, target, line"

// ReplacePathRefs replaces every path ref sourceID records with refs, in one
// transaction. Duplicate rows in refs collapse onto the primary key.
func (repo *EdgeRepo) ReplacePathRefs(sourceID string, refs []PathRefRow) error {
	tx, beginErr := repo.db.Begin()

	if beginErr != nil {
		return fmt.Errorf("edgeRepo: path refs begin: %w", beginErr)
	}

	if _, deleteErr := tx.Exec(`DELETE FROM path_refs WHERE source_id = ?`, sourceID); deleteErr != nil {
		_ = tx.Rollback()

		return fmt.Errorf("edgeRepo: path refs delete %s: %w", sourceID, deleteErr)
	}

	for _, ref := range refs {
		if _, insertErr := tx.Exec(
			`INSERT OR IGNORE INTO path_refs (`+pathRefColumns+`) VALUES (?, ?, ?, ?)`,
			sourceID, ref.Type, ref.Target, ref.Line,
		); insertErr != nil {
			_ = tx.Rollback()

			return fmt.Errorf("edgeRepo: path refs insert %s→%s: %w", sourceID, ref.Target, insertErr)
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("edgeRepo: path refs commit: %w", commitErr)
	}

	return nil
}

// PathRefsFrom returns the path refs of the given pages, ordered by page, then
// line, then target.
func (repo *EdgeRepo) PathRefsFrom(sourceIDs []string) ([]PathRefRow, error) {
	return repo.pathRefsWhereIn("source_id", sourceIDs)
}

// PathRefsTo returns the path refs whose target is one of targets, ordered by
// page, then line, then target.
func (repo *EdgeRepo) PathRefsTo(targets []string) ([]PathRefRow, error) {
	return repo.pathRefsWhereIn("target", targets)
}

// AllPathRefs returns every path ref, ordered by page, then line, then target.
func (repo *EdgeRepo) AllPathRefs() ([]PathRefRow, error) {
	rows, queryErr := repo.db.Query(`SELECT ` + pathRefColumns + ` FROM path_refs ORDER BY source_id, line, target, type`)

	if queryErr != nil {
		return nil, fmt.Errorf("edgeRepo: list path refs: %w", queryErr)
	}

	return scanPathRefs(rows)
}

func (repo *EdgeRepo) pathRefsWhereIn(column string, values []string) ([]PathRefRow, error) {
	var refs []PathRefRow

	for _, chunk := range chunkStrings(values, maxInVariables) {
		if len(chunk) == 0 {
			continue
		}

		args := make([]any, len(chunk))

		for position, value := range chunk {
			args[position] = value
		}

		rows, queryErr := repo.db.Query(
			`SELECT `+pathRefColumns+` FROM path_refs WHERE `+column+` IN (`+inPlaceholders(len(chunk))+`)`,
			args...,
		)

		if queryErr != nil {
			return nil, fmt.Errorf("edgeRepo: list path refs by %s: %w", column, queryErr)
		}

		scanned, scanErr := scanPathRefs(rows)

		if scanErr != nil {
			return nil, scanErr
		}

		refs = append(refs, scanned...)
	}

	sortPathRefs(refs)

	return refs, nil
}

// sortPathRefs orders refs by page, then line, then target, then type: the
// order AllPathRefs reads them in, kept across chunked queries.
func sortPathRefs(refs []PathRefRow) {
	slices.SortFunc(refs, func(left, right PathRefRow) int {
		return cmp.Or(
			cmp.Compare(left.SourceID, right.SourceID),
			cmp.Compare(left.Line, right.Line),
			cmp.Compare(left.Target, right.Target),
			cmp.Compare(left.Type, right.Type),
		)
	})
}

func scanPathRefs(rows *sql.Rows) ([]PathRefRow, error) {
	defer rows.Close()

	var refs []PathRefRow

	for rows.Next() {
		var ref PathRefRow

		if scanErr := rows.Scan(&ref.SourceID, &ref.Type, &ref.Target, &ref.Line); scanErr != nil {
			return nil, fmt.Errorf("edgeRepo: scan path ref: %w", scanErr)
		}

		refs = append(refs, ref)
	}

	return refs, rows.Err()
}
