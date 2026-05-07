package index

import (
	"database/sql"
	"fmt"
)

// PropertyDriftRow is one observed property-validation drift event. The
// primary key (node_id, kind, property) collapses repeated observations of
// the same drift into a single row with the most recent observed_at.
type PropertyDriftRow struct {
	NodeID     string
	NodeType   string
	Kind       string // "undeclared-property" | "type-mismatch" | "required-missing" | "enum-violation"
	Property   string
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
		INSERT INTO property_drift (node_id, node_type, kind, property, details, observed_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, kind, property) DO UPDATE SET
			node_type   = excluded.node_type,
			details     = excluded.details,
			observed_at = excluded.observed_at
	`, row.NodeID, row.NodeType, row.Kind, row.Property, row.Details, row.ObservedAt)

	if execErr != nil {
		return fmt.Errorf("propertyDriftRepo: append: %w", execErr)
	}

	return nil
}

// ListAll returns every drift row, sorted by (node_id, kind, property)
// for stable doctor rendering.
func (repo *PropertyDriftRepo) ListAll() ([]PropertyDriftRow, error) {
	rows, queryErr := repo.db.Query(`
		SELECT node_id, node_type, kind, property, details, observed_at
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

		if scanErr := rows.Scan(&row.NodeID, &row.NodeType, &row.Kind, &row.Property, &row.Details, &row.ObservedAt); scanErr != nil {
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
