package index

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ReindexNodeIDPrefix is the literal prefix prepended to the file path when
// enqueueing a reindex job. The embed_queue PRIMARY KEY is node_id alone, so
// real embed node IDs (path minus extension) and reindex node IDs (path) would
// otherwise collide. Prefixing the reindex variant keeps the key namespaces
// disjoint without a schema change. T6.4's reindex worker strips this prefix
// with strings.TrimPrefix.
const ReindexNodeIDPrefix = "reindex:"

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

// enqueueEmbedSQL queues an embed job for a node. A row already pending is
// left alone, so a retrying row keeps its attempt count and any lease. A
// parked row is re-armed with a fresh retry budget: callers enqueue a node
// when it changed or when `reindex --force` re-queues it, and either is a
// reason to try a node that stopped retrying again (#845).
const enqueueEmbedSQL = `
	INSERT INTO embed_queue (node_id, enqueued_at, attempts)
	VALUES (?, ?, 0)
	ON CONFLICT(node_id) DO UPDATE SET
		attempts   = 0,
		last_error = NULL,
		parked_at  = NULL
	WHERE embed_queue.parked_at IS NOT NULL
`

// Enqueue inserts a row for nodeID. Idempotent: a pending row is left as it
// is, and a parked row is re-armed (see enqueueEmbedSQL). The kind column
// defaults to 'embed' via the table default; EnqueueReindex sets
// kind = 'reindex' explicitly.
func (repo *EmbedQueueRepo) Enqueue(nodeID string) error {
	_, execErr := repo.db.Exec(enqueueEmbedSQL, nodeID, time.Now().UnixNano())

	if execErr != nil {
		return fmt.Errorf("embedQueueRepo: enqueue %s: %w", nodeID, execErr)
	}

	return nil
}

// EnqueueMany enqueues every nodeID in a single transaction, sharing one
// enqueued_at timestamp. Like Enqueue it is idempotent, so already-queued ids
// and in-batch duplicates are skipped and parked ids are re-armed. Callers
// that enqueue a whole file's sub-units use this to collapse O(sub-units)
// commits into one. An empty slice is a no-op.
func (repo *EmbedQueueRepo) EnqueueMany(nodeIDs []string) error {
	if len(nodeIDs) == 0 {
		return nil
	}

	tx, beginErr := repo.db.Begin()

	if beginErr != nil {
		return fmt.Errorf("embedQueueRepo: enqueue-many begin: %w", beginErr)
	}

	now := time.Now().UnixNano()

	for _, nodeID := range nodeIDs {
		if _, execErr := tx.Exec(enqueueEmbedSQL, nodeID, now); execErr != nil {
			_ = tx.Rollback()

			return fmt.Errorf("embedQueueRepo: enqueue-many %s: %w", nodeID, execErr)
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("embedQueueRepo: enqueue-many commit: %w", commitErr)
	}

	return nil
}

// EnqueueAllEmbeddable queues an embed job for every node the drain embeds:
// each file row, and each sub-unit leaf with a non-empty payload (sections
// are never embedded, and an empty-payload leaf is dropped by the drain). A
// row already queued, parked or not, is reset to a fresh, unleased job:
// whichever drainer held
// it may be embedding under the settings that just changed, and clearing the
// lease turns that drainer's Ack into a no-op so the row stays queued for an
// up-to-date drainer. Returns how many rows it queued or reset.
//
// reindex calls it when the [embeddings] document settings change, so
// unchanged files and leaves re-embed without being re-read; the drain then
// skip-acks any node whose stored hash and vector key still match.
func (repo *EmbedQueueRepo) EnqueueAllEmbeddable() (int, error) {
	result, execErr := repo.db.Exec(`
		INSERT INTO embed_queue (node_id, enqueued_at, attempts)
		SELECT id, ?, 0 FROM nodes
		WHERE kind = 'file'
		   OR (kind = 'subunit' AND type != 'section' AND COALESCE(embed_payload, '') != '')
		ON CONFLICT(node_id) DO UPDATE SET
			leased_by           = NULL,
			leased_until_ns     = NULL,
			lease_started_at_ns = NULL,
			attempts            = 0,
			last_error          = NULL,
			parked_at           = NULL
	`, time.Now().UnixNano())

	if execErr != nil {
		return 0, fmt.Errorf("embedQueueRepo: enqueue all embeddable: %w", execErr)
	}

	queued, _ := result.RowsAffected()

	return int(queued), nil
}

// Release returns a row workerID holds to the queue unleased, without
// spending an attempt (unlike Nack). A drainer that finds the [embeddings]
// settings changed under it releases its claimed rows for an up-to-date
// drainer. A no-op when workerID no longer holds the row.
func (repo *EmbedQueueRepo) Release(nodeID, workerID string) error {
	if _, execErr := repo.db.Exec(`
		UPDATE embed_queue
		SET    leased_by           = NULL,
		       leased_until_ns     = NULL,
		       lease_started_at_ns = NULL
		WHERE  node_id = ? AND leased_by = ?
	`, nodeID, workerID); execErr != nil {
		return fmt.Errorf("embedQueueRepo: release %s: %w", nodeID, execErr)
	}

	return nil
}

// EnqueueReindex inserts a kind='reindex' row keyed by ReindexNodeIDPrefix+path.
// Idempotent: ON CONFLICT(node_id) DO NOTHING handles two concurrent walks of
// the same file. Returns an error when path itself begins with the reserved
// prefix, preserving the round-trip invariant that T6.4's worker can recover
// the file path via strings.TrimPrefix(nodeID, ReindexNodeIDPrefix).
//
// Real embed node IDs derive from file paths via TrimSuffix(".md"); none
// currently start with the literal "reindex:" prefix in any vault path. This
// guard catches a future renaming that would break that invariant.
func (repo *EmbedQueueRepo) EnqueueReindex(path string) error {
	if strings.HasPrefix(path, ReindexNodeIDPrefix) {
		return fmt.Errorf("embedQueueRepo: enqueue reindex %q: path must not start with %q", path, ReindexNodeIDPrefix)
	}

	nodeID := ReindexNodeIDPrefix + path

	_, execErr := repo.db.Exec(`
		INSERT INTO embed_queue (node_id, enqueued_at, attempts, kind)
		VALUES (?, ?, 0, 'reindex')
		ON CONFLICT(node_id) DO NOTHING
	`, nodeID, time.Now().UnixNano())

	if execErr != nil {
		return fmt.Errorf("embedQueueRepo: enqueue reindex %s: %w", nodeID, execErr)
	}

	return nil
}

// DrainEmbed atomically claims up to batchSize unleased (or expired-lease)
// kind='embed' rows that are not parked for workerID, setting leased_by / leased_until_ns /
// lease_started_at_ns to now and now+ttl. Returns the claimed rows
// oldest-first.
//
// On success the caller embeds each row and calls Ack to delete it; on
// failure the caller calls Nack to release the lease and bump attempts,
// or Park to stop retrying it when the attempts cap is hit. On crash the
// lease expires after ttl and another worker's DrainEmbed reclaims it.
func (repo *EmbedQueueRepo) DrainEmbed(workerID string, batchSize int, ttl time.Duration) ([]QueueRow, error) {
	return repo.drainByKind(workerID, batchSize, ttl, "embed")
}

// DrainReindex atomically claims up to batchSize unleased (or expired-lease)
// kind='reindex' rows. Same lease semantics as DrainEmbed. Phase 6 introduces
// the reindex-queue worker pool that consumes these claims.
func (repo *EmbedQueueRepo) DrainReindex(workerID string, batchSize int, ttl time.Duration) ([]QueueRow, error) {
	return repo.drainByKind(workerID, batchSize, ttl, "reindex")
}

// drainByKind is the kind-parameterized lease-claim primitive shared by
// DrainEmbed and DrainReindex. The lease SQL lives here once.
func (repo *EmbedQueueRepo) drainByKind(workerID string, batchSize int, ttl time.Duration, kind string) ([]QueueRow, error) {
	now := time.Now().UnixNano()
	leasedUntil := now + ttl.Nanoseconds()

	rows, queryErr := repo.db.Query(`
		UPDATE embed_queue
		SET    leased_by           = ?,
		       leased_until_ns     = ?,
		       lease_started_at_ns = ?
		WHERE  node_id IN (
		         SELECT node_id FROM embed_queue
		         WHERE  kind = ?
		           AND  parked_at IS NULL
		           AND  (leased_by IS NULL OR leased_until_ns < ?)
		         ORDER BY enqueued_at ASC
		         LIMIT ?
		       )
		RETURNING node_id, enqueued_at, attempts, COALESCE(last_error, ''), kind
	`, workerID, leasedUntil, now, kind, now, batchSize)

	if queryErr != nil {
		return nil, fmt.Errorf("embedQueueRepo: drain %s claim: %w", kind, queryErr)
	}

	defer rows.Close()

	var drained []QueueRow

	for rows.Next() {
		var row QueueRow

		if scanErr := rows.Scan(&row.NodeID, &row.EnqueuedAt, &row.Attempts, &row.LastError, &row.Kind); scanErr != nil {
			return nil, fmt.Errorf("embedQueueRepo: drain %s scan: %w", kind, scanErr)
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
		return nil, fmt.Errorf("embedQueueRepo: drain %s rows: %w", kind, rowsErr)
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

// Drop removes a row permanently, for a job with nothing left to do: its
// node is gone, or it has no text to embed. Same lease-guard semantics as
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

// Park stops retrying a row that has used up its attempts: like Nack it
// releases the lease, bumps attempts and records last_error, and it also
// stamps parked_at, which keeps DrainEmbed from claiming the row and the
// depth counts from calling it pending. The row stays so doctor can say what
// failed; Enqueue (the node changed, or `reindex --force`) and
// EnqueueAllEmbeddable (an [embeddings] change) re-arm it, and deleting the
// node deletes it. Same lease guard as Nack: a no-op when workerID no longer
// holds the row.
func (repo *EmbedQueueRepo) Park(nodeID, workerID string, embedErr error) error {
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
		       last_error          = ?,
		       parked_at           = ?
		WHERE  node_id = ? AND leased_by = ?
	`, lastErr, time.Now().UnixNano(), nodeID, workerID); execErr != nil {
		return fmt.Errorf("embedQueueRepo: park %s: %w", nodeID, execErr)
	}

	return nil
}

// CountParked returns the number of embed rows that stopped retrying (see
// Park).
func (repo *EmbedQueueRepo) CountParked() (int, error) {
	var parked int

	if scanErr := repo.db.QueryRow(
		`SELECT COUNT(*) FROM embed_queue WHERE kind = 'embed' AND parked_at IS NOT NULL`,
	).Scan(&parked); scanErr != nil {
		return 0, fmt.Errorf("embedQueueRepo: count parked: %w", scanErr)
	}

	return parked, nil
}

// ListParked returns every embed row that stopped retrying, with its attempt
// count and last_error, sorted by node_id. It feeds doctor's embed-failed
// check.
func (repo *EmbedQueueRepo) ListParked() ([]QueueRow, error) {
	rows, queryErr := repo.db.Query(`
		SELECT node_id, enqueued_at, attempts, COALESCE(last_error, ''), kind
		FROM embed_queue
		WHERE kind = 'embed' AND parked_at IS NOT NULL
		ORDER BY node_id ASC
	`)

	if queryErr != nil {
		return nil, fmt.Errorf("embedQueueRepo: list parked: %w", queryErr)
	}

	defer rows.Close()

	var parked []QueueRow

	for rows.Next() {
		var row QueueRow

		if scanErr := rows.Scan(&row.NodeID, &row.EnqueuedAt, &row.Attempts, &row.LastError, &row.Kind); scanErr != nil {
			return nil, fmt.Errorf("embedQueueRepo: list parked scan: %w", scanErr)
		}

		parked = append(parked, row)
	}

	return parked, rows.Err()
}

// DeleteOrphans removes every embed row whose node no longer exists and
// returns how many it removed. Deleting a node deletes its row (the
// nodes_drop_embed_job trigger), so this only finds rows an index written
// before that trigger leaked (#845). Reindex rows key on a path, not a node,
// and are left alone.
func (repo *EmbedQueueRepo) DeleteOrphans() (int, error) {
	result, execErr := repo.db.Exec(`
		DELETE FROM embed_queue
		WHERE kind = 'embed'
		  AND NOT EXISTS (SELECT 1 FROM nodes WHERE nodes.id = embed_queue.node_id)
	`)

	if execErr != nil {
		return 0, fmt.Errorf("embedQueueRepo: delete orphans: %w", execErr)
	}

	removed, _ := result.RowsAffected()

	return int(removed), nil
}

// Depth returns the number of pending rows across all kinds. Parked rows are
// not pending; CountParked counts them. Callers that need to distinguish
// embed vs reindex jobs should use DepthByKind.
func (repo *EmbedQueueRepo) Depth() (int, error) {
	var depth int

	if scanErr := repo.db.QueryRow(`SELECT COUNT(*) FROM embed_queue WHERE parked_at IS NULL`).Scan(&depth); scanErr != nil {
		return 0, fmt.Errorf("embedQueueRepo: depth: %w", scanErr)
	}

	return depth, nil
}

// DepthByKind returns the number of pending rows whose kind matches the
// argument, parked rows left out. Used by status reporters to separate embed
// vs reindex backlogs.
func (repo *EmbedQueueRepo) DepthByKind(kind string) (int, error) {
	var depth int

	if scanErr := repo.db.QueryRow(`SELECT COUNT(*) FROM embed_queue WHERE kind = ? AND parked_at IS NULL`, kind).Scan(&depth); scanErr != nil {
		return 0, fmt.Errorf("embedQueueRepo: depth by kind %q: %w", kind, scanErr)
	}

	return depth, nil
}

// ListRetrying returns every pending kind='embed' row that has failed at
// least once (attempts > 0) and has not been parked, carrying its attempt
// count and last_error. A row that is failing is otherwise indistinguishable
// from a healthy backlog until the attempts cap parks it. This feeds doctor's
// IssueEmbedRetry check. Rows are returned oldest-first
// (enqueued_at asc, then node_id) for stable output.
func (repo *EmbedQueueRepo) ListRetrying() ([]QueueRow, error) {
	rows, queryErr := repo.db.Query(`
		SELECT node_id, enqueued_at, attempts, COALESCE(last_error, ''), kind
		FROM embed_queue
		WHERE kind = 'embed' AND attempts > 0 AND parked_at IS NULL
		ORDER BY enqueued_at ASC, node_id ASC
	`)

	if queryErr != nil {
		return nil, fmt.Errorf("embedQueueRepo: list retrying: %w", queryErr)
	}

	defer rows.Close()

	var retrying []QueueRow

	for rows.Next() {
		var row QueueRow

		if scanErr := rows.Scan(&row.NodeID, &row.EnqueuedAt, &row.Attempts, &row.LastError, &row.Kind); scanErr != nil {
			return nil, fmt.Errorf("embedQueueRepo: list retrying scan: %w", scanErr)
		}

		retrying = append(retrying, row)
	}

	return retrying, rows.Err()
}

// ListNodeIDs returns every pending kind='embed' node_id, sorted ascending.
// Parked rows are excluded (ListParked lists them), and so are reindex rows —
// callers wanting reindex jobs should query directly or use a kind-filtered
// helper.
func (repo *EmbedQueueRepo) ListNodeIDs() ([]string, error) {
	rows, queryErr := repo.db.Query(`SELECT node_id FROM embed_queue WHERE kind = 'embed' AND parked_at IS NULL ORDER BY node_id`)

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
