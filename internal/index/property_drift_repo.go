package index

import (
	"database/sql"
	"fmt"
)

// PropertyDriftRow is one observed property-validation drift event. The
// primary key (node_id, kind, property, value) collapses repeated observations
// of the same drift into a single row with the most recent observed_at.
//
// Value is the offending property value. It distinguishes multiple broken
// values of one list-of(ref) property (each becomes its own row); per-property
// kinds — undeclared-property, type-mismatch, required-missing, enum-violation
// — leave it empty, so they still collapse to one row per (node, kind,
// property) exactly as before (#689).
type PropertyDriftRow struct {
	NodeID     string
	NodeType   string
	Kind       string // "undeclared-property" | "type-mismatch" | "required-missing" | "enum-violation"
	Property   string
	Value      string
	Details    string
	ObservedAt int64
}

// PropertyDriftRepo persists property-validation drift events for
// `tusk doctor` to surface.
type PropertyDriftRepo struct {
	db *sql.DB
}

// NewPropertyDriftRepo constructs a repo backed by idx.
func NewPropertyDriftRepo(idx *Index) *PropertyDriftRepo {
	return &PropertyDriftRepo{db: idx.DB()}
}

// Append upserts a drift row. Idempotent on the primary key; the latest
// observed_at wins alongside the updated node_type and details.
func (repo *PropertyDriftRepo) Append(row PropertyDriftRow) error {
	_, execErr := repo.db.Exec(`
		INSERT INTO property_drift (node_id, node_type, kind, property, value, details, observed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, kind, property, value) DO UPDATE SET
			node_type   = excluded.node_type,
			details     = excluded.details,
			observed_at = excluded.observed_at
	`, row.NodeID, row.NodeType, row.Kind, row.Property, row.Value, row.Details, row.ObservedAt)

	if execErr != nil {
		return fmt.Errorf("propertyDriftRepo: append: %w", execErr)
	}

	return nil
}

// ListAll returns every drift row, sorted by (node_id, kind, property)
// for stable doctor rendering.
func (repo *PropertyDriftRepo) ListAll() ([]PropertyDriftRow, error) {
	rows, queryErr := repo.db.Query(`
		SELECT node_id, node_type, kind, property, value, details, observed_at
		FROM property_drift
		ORDER BY node_id, kind, property
	`)

	if queryErr != nil {
		return nil, fmt.Errorf("propertyDriftRepo: list: %w", queryErr)
	}

	defer rows.Close()

	var results []PropertyDriftRow

	for rows.Next() {
		var row PropertyDriftRow

		if scanErr := rows.Scan(&row.NodeID, &row.NodeType, &row.Kind, &row.Property, &row.Value, &row.Details, &row.ObservedAt); scanErr != nil {
			return nil, fmt.Errorf("propertyDriftRepo: scan: %w", scanErr)
		}

		results = append(results, row)
	}

	return results, rows.Err()
}

// ClearForNode removes every drift row for nodeID. Called by Modify and
// reindex on a clean validation pass.
func (repo *PropertyDriftRepo) ClearForNode(nodeID string) error {
	_, execErr := repo.db.Exec(`DELETE FROM property_drift WHERE node_id = ?`, nodeID)

	if execErr != nil {
		return fmt.Errorf("propertyDriftRepo: clear %s: %w", nodeID, execErr)
	}

	return nil
}

// DeleteOrphans removes every property-drift row whose node_id no longer
// resolves to a node row. Drift is only ever written while validating a live
// node, so a row without a node is an orphan left behind by a delete or rename
// (neither clears drift). Without this sweep the row is re-reported by every
// `tusk doctor` run forever (#685). Returns the number of rows swept.
func (repo *PropertyDriftRepo) DeleteOrphans() (int, error) {
	result, execErr := repo.db.Exec(`
		DELETE FROM property_drift
		WHERE node_id NOT IN (SELECT id FROM nodes)
	`)

	if execErr != nil {
		return 0, fmt.Errorf("propertyDriftRepo: delete orphans: %w", execErr)
	}

	affected, affectedErr := result.RowsAffected()

	if affectedErr != nil {
		return 0, fmt.Errorf("propertyDriftRepo: delete orphans rows-affected: %w", affectedErr)
	}

	return int(affected), nil
}

// refDriftKinds are the drift kinds produced by ref resolution. They depend on
// the state of OTHER nodes (a target appearing, an ambiguous candidate
// vanishing), so unlike per-file validation drift they can become stale
// without the drifted file itself changing; the reindex heal pass re-resolves
// them after every sweep.
var refDriftKinds = []string{"ref_dangling", "ref_ambiguous", "ref_type_mismatch", "ref_cycle"}

// refKindsPlaceholders is the SQL "(?, ?, ...)" list matching refDriftKinds.
const refKindsPlaceholders = "(?, ?, ?, ?)"

func refKindsArgs(prefix ...any) []any {
	args := prefix

	for _, kind := range refDriftKinds {
		args = append(args, kind)
	}

	return args
}

// ListRefKinds returns every drift row whose kind came from ref resolution,
// sorted like ListAll. The reindex heal pass re-enqueues these rows' files.
func (repo *PropertyDriftRepo) ListRefKinds() ([]PropertyDriftRow, error) {
	rows, queryErr := repo.db.Query(`
		SELECT node_id, node_type, kind, property, value, details, observed_at
		FROM property_drift
		WHERE kind IN `+refKindsPlaceholders+`
		ORDER BY node_id, kind, property
	`, refKindsArgs()...)

	if queryErr != nil {
		return nil, fmt.Errorf("propertyDriftRepo: list ref kinds: %w", queryErr)
	}

	defer rows.Close()

	var results []PropertyDriftRow

	for rows.Next() {
		var row PropertyDriftRow

		if scanErr := rows.Scan(&row.NodeID, &row.NodeType, &row.Kind, &row.Property, &row.Value, &row.Details, &row.ObservedAt); scanErr != nil {
			return nil, fmt.Errorf("propertyDriftRepo: scan ref kinds: %w", scanErr)
		}

		results = append(results, row)
	}

	return results, rows.Err()
}

// ClearRefKindsForNode removes nodeID's ref-resolution drift rows, leaving
// other kinds (undeclared-property, type-mismatch, ...) untouched. Ref
// resolution clears-then-appends via this so a healed ref drops its row even
// while unrelated drift keeps the full ClearForNode from firing.
func (repo *PropertyDriftRepo) ClearRefKindsForNode(nodeID string) error {
	_, execErr := repo.db.Exec(
		`DELETE FROM property_drift WHERE node_id = ? AND kind IN `+refKindsPlaceholders,
		refKindsArgs(nodeID)...)

	if execErr != nil {
		return fmt.Errorf("propertyDriftRepo: clear ref kinds %s: %w", nodeID, execErr)
	}

	return nil
}

// CountAll returns the total number of drift rows. Used by reindex's
// summary line.
func (repo *PropertyDriftRepo) CountAll() (int, error) {
	var count int

	scanErr := repo.db.QueryRow(`SELECT COUNT(*) FROM property_drift`).Scan(&count)

	if scanErr != nil {
		return 0, fmt.Errorf("propertyDriftRepo: count: %w", scanErr)
	}

	return count, nil
}
