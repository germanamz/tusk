package index

import (
	"database/sql"
	"fmt"
)

// WorkflowDriftRow is one observed off-schema status. The primary key
// (node_id, pack_instance, observed_status) collapses repeated
// observations of the same drift into a single row.
type WorkflowDriftRow struct {
	NodeID         string
	PackInstance   string
	PackKind       string
	ObservedStatus string
	Property       string
	// ErrorCode is the workflow rejection code (e.g. "unknown-target-state").
	// Empty for rows written before this column existed.
	ErrorCode string
	// Detail is the fully-rendered rejection message; doctor prefers it over
	// reconstructing a message. Empty for legacy rows.
	Detail     string
	ObservedAt int64
}

// WorkflowDriftRepo persists workflow-validation drift events for
// `tusk doctor` to surface.
type WorkflowDriftRepo struct {
	db *sql.DB
}

// NewWorkflowDriftRepo constructs a repo backed by idx.
func NewWorkflowDriftRepo(idx *Index) *WorkflowDriftRepo {
	return &WorkflowDriftRepo{db: idx.DB()}
}

// Append upserts a drift row. Idempotent on the primary key; the latest
// observed_at + property + pack_kind win.
func (repo *WorkflowDriftRepo) Append(row WorkflowDriftRow) error {
	_, execErr := repo.db.Exec(`
		INSERT INTO workflow_drift (node_id, pack_instance, pack_kind, observed_status, property, error_code, detail, observed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, pack_instance, observed_status) DO UPDATE SET
			pack_kind = excluded.pack_kind,
			property = excluded.property,
			error_code = excluded.error_code,
			detail = excluded.detail,
			observed_at = excluded.observed_at
	`, row.NodeID, row.PackInstance, row.PackKind, row.ObservedStatus, row.Property, row.ErrorCode, row.Detail, row.ObservedAt)

	if execErr != nil {
		return fmt.Errorf("workflowDriftRepo: append: %w", execErr)
	}

	return nil
}

// ListAll returns every drift row, sorted by (node_id, pack_instance,
// observed_status) for stable rendering.
func (repo *WorkflowDriftRepo) ListAll() ([]WorkflowDriftRow, error) {
	rows, queryErr := repo.db.Query(`
		SELECT node_id, pack_instance, pack_kind, observed_status, property, error_code, detail, observed_at
		FROM workflow_drift
		ORDER BY node_id, pack_instance, observed_status
	`)

	if queryErr != nil {
		return nil, fmt.Errorf("workflowDriftRepo: list: %w", queryErr)
	}

	defer rows.Close()

	var results []WorkflowDriftRow

	for rows.Next() {
		var row WorkflowDriftRow

		if scanErr := rows.Scan(&row.NodeID, &row.PackInstance, &row.PackKind, &row.ObservedStatus, &row.Property, &row.ErrorCode, &row.Detail, &row.ObservedAt); scanErr != nil {
			return nil, fmt.Errorf("workflowDriftRepo: scan: %w", scanErr)
		}

		results = append(results, row)
	}

	return results, rows.Err()
}

// ClearForNode removes every drift row for nodeID. Called by Modify and
// reindex on a clean validation pass.
func (repo *WorkflowDriftRepo) ClearForNode(nodeID string) error {
	_, execErr := repo.db.Exec(`DELETE FROM workflow_drift WHERE node_id = ?`, nodeID)

	if execErr != nil {
		return fmt.Errorf("workflowDriftRepo: clear %s: %w", nodeID, execErr)
	}

	return nil
}

// CountAll returns the total number of drift rows. Used by reindex's
// summary line.
func (repo *WorkflowDriftRepo) CountAll() (int, error) {
	var count int

	scanErr := repo.db.QueryRow(`SELECT COUNT(*) FROM workflow_drift`).Scan(&count)

	if scanErr != nil {
		return 0, fmt.Errorf("workflowDriftRepo: count: %w", scanErr)
	}

	return count, nil
}
