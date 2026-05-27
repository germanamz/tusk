package index

import (
	"database/sql"
	"fmt"
	"time"
)

// QueueRow represents a row in embed_queue.
type QueueRow struct {
	NodeID           string
	EnqueuedAt       int64
	Attempts         int
	LastError        string
	LeasedBy         *string
	LeasedUntilNs    *int64
	LeaseStartedAtNs *int64
	Kind             string
}

// EmbedQueueRepo persists pending embed jobs.
type EmbedQueueRepo struct {
	db *sql.DB
}

// NewEmbedQueueRepo constructs an EmbedQueueRepo backed by idx.
func NewEmbedQueueRepo(idx *Index) *EmbedQueueRepo {
	return &EmbedQueueRepo{db: idx.DB()}
}

// Enqueue inserts a row for nodeID. Idempotent — if the row exists, no-op.
// The kind column defaults to 'embed'; Phase 6 adds a separate
// EnqueueReindex helper that sets kind = 'reindex' explicitly.
func (repo *EmbedQueueRepo) Enqueue(nodeID string) error {
	_, execErr := repo.db.Exec(`
		INSERT INTO embed_queue (node_id, enqueued_at, attempts)
		VALUES (?, ?, 0)
		ON CONFLICT(node_id) DO NOTHING
	`, nodeID, time.Now().UnixNano())

	if execErr != nil {
		return fmt.Errorf("embedQueueRepo: enqueue %s: %w", nodeID, execErr)
	}

	return nil
}

// ReEnqueue reinserts a row for nodeID with the explicit attempts count and
// last_error, bumping enqueued_at to time.Now() so FIFO ordering reflects the
// most-recent attempt (anti-starvation). If a row already exists for nodeID
// (rare — Drain deletes before the embed loop runs — but possible if a caller
// re-enqueues out of band), its attempts, last_error, and enqueued_at are
// overwritten. The kind column defaults to 'embed' and is not touched here;
// reindex jobs use the dedicated Phase 6 helper.
func (repo *EmbedQueueRepo) ReEnqueue(nodeID string, attempts int, lastError string) error {
	_, execErr := repo.db.Exec(`
		INSERT INTO embed_queue (node_id, enqueued_at, attempts, last_error)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			enqueued_at = excluded.enqueued_at,
			attempts = excluded.attempts,
			last_error = excluded.last_error
	`, nodeID, time.Now().UnixNano(), attempts, lastError)

	if execErr != nil {
		return fmt.Errorf("embedQueueRepo: re-enqueue %s: %w", nodeID, execErr)
	}

	return nil
}

// Drain returns up to limit rows oldest-first AND removes them from the queue
// in one transaction.
func (repo *EmbedQueueRepo) Drain(limit int) ([]QueueRow, error) {
	tx, beginErr := repo.db.Begin()

	if beginErr != nil {
		return nil, fmt.Errorf("embedQueueRepo: begin: %w", beginErr)
	}

	rows, queryErr := tx.Query(`
		SELECT node_id, enqueued_at, attempts, COALESCE(last_error, ''),
		       leased_by, leased_until_ns, lease_started_at_ns, kind
		FROM embed_queue
		ORDER BY enqueued_at ASC
		LIMIT ?
	`, limit)

	if queryErr != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("embedQueueRepo: drain query: %w", queryErr)
	}

	var drained []QueueRow

	for rows.Next() {
		var row QueueRow

		if scanErr := rows.Scan(
			&row.NodeID, &row.EnqueuedAt, &row.Attempts, &row.LastError,
			&row.LeasedBy, &row.LeasedUntilNs, &row.LeaseStartedAtNs, &row.Kind,
		); scanErr != nil {
			rows.Close()
			_ = tx.Rollback()
			return nil, fmt.Errorf("embedQueueRepo: drain scan: %w", scanErr)
		}

		drained = append(drained, row)
	}

	rows.Close()

	for _, row := range drained {
		if _, deleteErr := tx.Exec(`DELETE FROM embed_queue WHERE node_id = ?`, row.NodeID); deleteErr != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("embedQueueRepo: drain delete %s: %w", row.NodeID, deleteErr)
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return nil, fmt.Errorf("embedQueueRepo: drain commit: %w", commitErr)
	}

	return drained, nil
}

// Depth returns the number of pending rows.
func (repo *EmbedQueueRepo) Depth() (int, error) {
	var depth int

	if scanErr := repo.db.QueryRow(`SELECT COUNT(*) FROM embed_queue`).Scan(&depth); scanErr != nil {
		return 0, fmt.Errorf("embedQueueRepo: depth: %w", scanErr)
	}

	return depth, nil
}

// ListNodeIDs returns every pending node_id, sorted ascending.
func (repo *EmbedQueueRepo) ListNodeIDs() ([]string, error) {
	rows, queryErr := repo.db.Query(`SELECT node_id FROM embed_queue ORDER BY node_id`)

	if queryErr != nil {
		return nil, fmt.Errorf("embedQueueRepo: list node ids: %w", queryErr)
	}

	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id string

		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, fmt.Errorf("embedQueueRepo: list node ids scan: %w", scanErr)
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}
