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
// The kind column defaults to 'embed' via the table default; Phase 6 adds
// a separate EnqueueReindex helper that sets kind = 'reindex' explicitly.
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
// most-recent attempt (anti-starvation). If a row already exists for nodeID,
// its attempts, last_error, and enqueued_at are overwritten. The kind column
// defaults to 'embed' via the table default and is not touched here; reindex
// jobs use the dedicated Phase 6 helper. This method is retained for the
// rebuild/reindex flow in Phase 6; the embed-drain path uses Nack instead.
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

// Drain atomically claims up to batchSize unleased (or expired-lease)
// embed rows for workerID, setting leased_by / leased_until_ns /
// lease_started_at_ns to now and now+ttl. Returns the claimed rows
// oldest-first. The kind filter is hardcoded to 'embed'; reindex jobs
// are drained by a separate Phase 6 worker path.
//
// On success the caller embeds each row and calls Ack to delete it; on
// failure the caller calls Nack to release the lease and bump attempts,
// or Drop to remove the row when the attempts cap is hit. On crash the
// lease expires after ttl and another worker's Drain reclaims it.
func (repo *EmbedQueueRepo) Drain(workerID string, batchSize int, ttl time.Duration) ([]QueueRow, error) {
	now := time.Now().UnixNano()
	leasedUntil := now + ttl.Nanoseconds()

	rows, queryErr := repo.db.Query(`
		UPDATE embed_queue
		SET    leased_by           = ?,
		       leased_until_ns     = ?,
		       lease_started_at_ns = ?
		WHERE  node_id IN (
		         SELECT node_id FROM embed_queue
		         WHERE  kind = 'embed'
		           AND  (leased_by IS NULL OR leased_until_ns < ?)
		         ORDER BY enqueued_at ASC
		         LIMIT ?
		       )
		RETURNING node_id, enqueued_at, attempts, COALESCE(last_error, ''), kind
	`, workerID, leasedUntil, now, now, batchSize)

	if queryErr != nil {
		return nil, fmt.Errorf("embedQueueRepo: drain claim: %w", queryErr)
	}

	defer rows.Close()

	var drained []QueueRow

	for rows.Next() {
		var row QueueRow

		if scanErr := rows.Scan(&row.NodeID, &row.EnqueuedAt, &row.Attempts, &row.LastError, &row.Kind); scanErr != nil {
			return nil, fmt.Errorf("embedQueueRepo: drain scan: %w", scanErr)
		}

		leasedBy := workerID
		leasedUntilNs := leasedUntil
		leaseStartedAtNs := now
		row.LeasedBy = &leasedBy
		row.LeasedUntilNs = &leasedUntilNs
		row.LeaseStartedAtNs = &leaseStartedAtNs

		drained = append(drained, row)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("embedQueueRepo: drain rows: %w", rowsErr)
	}

	return drained, nil
}

// Ack removes a claimed row after a successful embed. The leased_by
// predicate guards against acking a row whose lease has expired and
// been re-claimed by another worker — that case is a silent no-op
// (zero rows affected, nil return) rather than an error.
func (repo *EmbedQueueRepo) Ack(nodeID, workerID string) error {
	if _, execErr := repo.db.Exec(
		`DELETE FROM embed_queue WHERE node_id = ? AND leased_by = ?`,
		nodeID, workerID,
	); execErr != nil {
		return fmt.Errorf("embedQueueRepo: ack %s: %w", nodeID, execErr)
	}

	return nil
}

// Nack releases the lease on a row after a failed embed, incrementing
// attempts and recording last_error. The row returns to the unleased
// pool for the next Drain call (the same worker or another). The
// leased_by predicate is the same lease-guard as Ack: if the lease has
// already expired and been reclaimed, this is a silent no-op.
func (repo *EmbedQueueRepo) Nack(nodeID, workerID string, embedErr error) error {
	var lastErr string

	if embedErr != nil {
		lastErr = embedErr.Error()
	}

	if _, execErr := repo.db.Exec(`
		UPDATE embed_queue
		SET    leased_by           = NULL,
		       leased_until_ns     = NULL,
		       lease_started_at_ns = NULL,
		       attempts            = attempts + 1,
		       last_error          = ?
		WHERE  node_id = ? AND leased_by = ?
	`, lastErr, nodeID, workerID); execErr != nil {
		return fmt.Errorf("embedQueueRepo: nack %s: %w", nodeID, execErr)
	}

	return nil
}

// Drop removes a row permanently — used when the attempts cap is hit
// and the row should not be retried. Same lease-guard semantics as
// Ack / Nack: if the lease has moved on, this is a silent no-op.
func (repo *EmbedQueueRepo) Drop(nodeID, workerID string) error {
	if _, execErr := repo.db.Exec(
		`DELETE FROM embed_queue WHERE node_id = ? AND leased_by = ?`,
		nodeID, workerID,
	); execErr != nil {
		return fmt.Errorf("embedQueueRepo: drop %s: %w", nodeID, execErr)
	}

	return nil
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
