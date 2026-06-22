package index

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// EmbeddingRow is the index representation of one node's embedding (one chunk).
type EmbeddingRow struct {
	NodeID      string
	ChunkIdx    int
	Model       string
	ContentHash string
	Vector      []float32
	Dim         int
	Body        string
}

// EmbeddingRepo persists EmbeddingRow values in the SQLite index.
type EmbeddingRepo struct {
	db *sql.DB
}

// NewEmbeddingRepo constructs an EmbeddingRepo backed by idx.
func NewEmbeddingRepo(idx *Index) *EmbeddingRepo {
	return &EmbeddingRepo{db: idx.DB()}
}

// sqlExecer is satisfied by both *sql.DB and *sql.Tx, letting the mapping
// writer run inside or outside a transaction.
type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// embeddingJoinSelect resolves node-chunk mappings to their shared vectors. It
// produces the seven columns scanEmbeddings expects, in order.
const embeddingJoinSelect = `
	SELECT ne.node_id, ne.chunk_idx, e.model, e.content_hash, e.vector, e.dim, e.body
	FROM node_embeddings ne
	JOIN embeddings e ON e.content_hash = ne.content_hash AND e.model = ne.model`

// Upsert stores the shared vector (once per content_hash, model) and the
// node→content mapping for this chunk. Two nodes with identical content write
// the same embeddings row (transparent de-duplication) but distinct
// node_embeddings rows.
func (repo *EmbeddingRepo) Upsert(row EmbeddingRow) error {
	encoded, encodeErr := encodeVector(row.Vector)

	if encodeErr != nil {
		return encodeErr
	}

	tx, beginErr := repo.db.Begin()

	if beginErr != nil {
		return fmt.Errorf("embeddingRepo: upsert begin %s: %w", row.NodeID, beginErr)
	}

	defer func() { _ = tx.Rollback() }()

	if _, execErr := tx.Exec(`
		INSERT INTO embeddings (content_hash, model, vector, dim, body)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(content_hash, model) DO UPDATE SET
			vector = excluded.vector,
			dim    = excluded.dim,
			body   = excluded.body
	`, row.ContentHash, row.Model, encoded, row.Dim, row.Body); execErr != nil {
		return fmt.Errorf("embeddingRepo: upsert vector %s: %w", row.ContentHash, execErr)
	}

	if mapErr := upsertMapping(tx, row.NodeID, row.ChunkIdx, row.ContentHash, row.Model); mapErr != nil {
		return mapErr
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("embeddingRepo: upsert commit %s: %w", row.NodeID, commitErr)
	}

	return nil
}

// ExistsByContentHash reports whether a shared vector already exists for this
// content under the given model. The drain uses it to attach a node to an
// already-embedded content_hash instead of calling the model again.
func (repo *EmbeddingRepo) ExistsByContentHash(contentHash, model string) (bool, error) {
	var one int

	queryErr := repo.db.QueryRow(
		`SELECT 1 FROM embeddings WHERE content_hash = ? AND model = ? LIMIT 1`,
		contentHash, model,
	).Scan(&one)

	if errors.Is(queryErr, sql.ErrNoRows) {
		return false, nil
	}

	if queryErr != nil {
		return false, fmt.Errorf("embeddingRepo: exists %s: %w", contentHash, queryErr)
	}

	return true, nil
}

// MapNodeChunk records (or replaces) the node→content mapping for one chunk
// without touching the shared vector. Used to attach a node to an
// already-embedded content_hash (reuse without a model call).
func (repo *EmbeddingRepo) MapNodeChunk(nodeID string, chunkIdx int, contentHash, model string) error {
	return upsertMapping(repo.db, nodeID, chunkIdx, contentHash, model)
}

// GCOrphanVectors deletes shared vectors that no node mapping references, and
// returns the number removed. Callers MUST serialize this with drain: run it
// only when the embed queue is drained (and under the workspace lock) so a
// vector isn't reclaimed out from under an in-flight mapping write.
func (repo *EmbeddingRepo) GCOrphanVectors() (int, error) {
	result, execErr := repo.db.Exec(`
		DELETE FROM embeddings
		WHERE NOT EXISTS (
			SELECT 1 FROM node_embeddings ne
			WHERE ne.content_hash = embeddings.content_hash AND ne.model = embeddings.model
		)
	`)

	if execErr != nil {
		return 0, fmt.Errorf("embeddingRepo: gc orphans: %w", execErr)
	}

	removed, _ := result.RowsAffected()

	return int(removed), nil
}

// upsertMapping writes (or replaces) the node→content mapping for one chunk.
func upsertMapping(exec sqlExecer, nodeID string, chunkIdx int, contentHash, model string) error {
	if _, execErr := exec.Exec(`
		INSERT INTO node_embeddings (node_id, chunk_idx, content_hash, model)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(node_id, chunk_idx) DO UPDATE SET
			content_hash = excluded.content_hash,
			model        = excluded.model
	`, nodeID, chunkIdx, contentHash, model); execErr != nil {
		return fmt.Errorf("embeddingRepo: upsert mapping %s: %w", nodeID, execErr)
	}

	return nil
}

// GetByNodeID returns all embeddings (chunks) for nodeID, resolved through the
// junction to their shared vectors, ordered by chunk_idx.
func (repo *EmbeddingRepo) GetByNodeID(nodeID string) ([]EmbeddingRow, error) {
	rows, queryErr := repo.db.Query(embeddingJoinSelect+`
		WHERE ne.node_id = ?
		ORDER BY ne.chunk_idx
	`, nodeID)

	if queryErr != nil {
		return nil, fmt.Errorf("embeddingRepo: get %s: %w", nodeID, queryErr)
	}

	defer rows.Close()

	return scanEmbeddings(rows)
}

// ListSubUnitsForFiles returns every embedding whose node_id matches the
// composite sub-unit pattern `<fileID>#*` for any fileID in the input. Used
// by the query layer's sub-unit-aware semantic path: the structural filter
// returns file ids, and the semantic ranker pulls every sub-unit embedding
// owned by those files in a single batched query.
//
// We use GLOB rather than LIKE because LIKE treats `_` as a wildcard, which
// would silently alias sibling files (e.g. `notes/foo_a` matching
// `notes/foo b`'s sub-units). GLOB's wildcards are `*` and `?`, neither of
// which can appear in a workspace file id.
//
// Returns an empty slice when no rows match. Caller is responsible for
// stitching the embedding rows back to their parent file via the
// `<fileID>#<hash>` id format.
//
// The ids are chunked into batches of maxGlobConditions so the OR-chain never
// exceeds SQLite's expression-depth ceiling (#564): a hybrid filter matching
// thousands of sub-units reaches this path with thousands of ids.
func (repo *EmbeddingRepo) ListSubUnitsForFiles(fileIDs []string) ([]EmbeddingRow, error) {
	if len(fileIDs) == 0 {
		return nil, nil
	}

	var results []EmbeddingRow

	for _, chunk := range chunkStrings(fileIDs, maxGlobConditions) {
		conditions := make([]string, 0, len(chunk))
		args := make([]any, 0, len(chunk))

		for _, fileID := range chunk {
			conditions = append(conditions, "ne.node_id GLOB ?")
			args = append(args, fileID+"#*")
		}

		query := embeddingJoinSelect + ` WHERE ` + strings.Join(conditions, " OR ") + ` ORDER BY ne.node_id, ne.chunk_idx`

		chunkRows, queryErr := repo.queryEmbeddings(query, args...)

		if queryErr != nil {
			return nil, fmt.Errorf("embeddingRepo: list sub-units: %w", queryErr)
		}

		results = append(results, chunkRows...)
	}

	// Chunking splits the id set across queries, so the per-chunk ORDER BY no
	// longer yields a globally ordered result. Re-sort to preserve the
	// documented (node_id, chunk_idx) contract callers rely on.
	sort.SliceStable(results, func(left, right int) bool {
		if results[left].NodeID != results[right].NodeID {
			return results[left].NodeID < results[right].NodeID
		}

		return results[left].ChunkIdx < results[right].ChunkIdx
	})

	return results, nil
}

// queryEmbeddings runs query and scans the result rows into EmbeddingRow
// values, closing the rows before returning.
func (repo *EmbeddingRepo) queryEmbeddings(query string, args ...any) ([]EmbeddingRow, error) {
	rows, queryErr := repo.db.Query(query, args...)

	if queryErr != nil {
		return nil, queryErr
	}

	defer rows.Close()

	return scanEmbeddings(rows)
}

// ListByNodeIDs returns all embeddings whose node_id is in nodeIDs.
func (repo *EmbeddingRepo) ListByNodeIDs(nodeIDs []string) ([]EmbeddingRow, error) {
	if len(nodeIDs) == 0 {
		return nil, nil
	}

	args := make([]any, len(nodeIDs))

	for idx, nodeID := range nodeIDs {
		args[idx] = nodeID
	}

	query := embeddingJoinSelect + fmt.Sprintf(` WHERE ne.node_id IN (%s) ORDER BY ne.node_id, ne.chunk_idx`, inPlaceholders(len(nodeIDs)))

	rows, queryErr := repo.db.Query(query, args...)

	if queryErr != nil {
		return nil, fmt.Errorf("embeddingRepo: list: %w", queryErr)
	}

	defer rows.Close()

	return scanEmbeddings(rows)
}

// DeleteByNodeID removes every node→content mapping for nodeID. The shared
// vectors stay until GCOrphanVectors sweeps any that no mapping references.
func (repo *EmbeddingRepo) DeleteByNodeID(nodeID string) error {
	_, execErr := repo.db.Exec(`DELETE FROM node_embeddings WHERE node_id = ?`, nodeID)

	if execErr != nil {
		return fmt.Errorf("embeddingRepo: delete %s: %w", nodeID, execErr)
	}

	return nil
}

// ListNodeIDs returns every distinct node_id that has at least one embedding
// mapping, sorted ascending.
func (repo *EmbeddingRepo) ListNodeIDs() ([]string, error) {
	rows, queryErr := repo.db.Query(`SELECT DISTINCT node_id FROM node_embeddings ORDER BY node_id`)

	if queryErr != nil {
		return nil, fmt.Errorf("embeddingRepo: list node ids: %w", queryErr)
	}

	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id string

		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, fmt.Errorf("embeddingRepo: list node ids scan: %w", scanErr)
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// EmbeddingStats aggregates the embeddings table for tusk doctor.
type EmbeddingStats struct {
	TotalNodes   int
	TotalChunks  int
	MeanChunks   float64
	MedianChunks int
	MaxChunks    int
	TopByChunks  []NodeChunkCount
	LargeChunks  []NodeChunkInfo
}

// NodeChunkCount pairs a node id with its chunk count.
type NodeChunkCount struct {
	NodeID string
	Chunks int
}

// NodeChunkInfo identifies one chunk and its body length.
type NodeChunkInfo struct {
	NodeID   string
	ChunkIdx int
	BodyLen  int
}

// Stats returns aggregate statistics over the embeddings table.
// largeChunkThreshold is the inclusive byte length at or above which a chunk
// is reported in LargeChunks.
func (repo *EmbeddingRepo) Stats(largeChunkThreshold int) (EmbeddingStats, error) {
	var stats EmbeddingStats

	// Per-node counts (from the junction — one mapping per node-chunk).
	rows, queryErr := repo.db.Query(`
		SELECT node_id, COUNT(*) AS chunk_count
		FROM node_embeddings
		GROUP BY node_id
		ORDER BY chunk_count DESC, node_id ASC
	`)

	if queryErr != nil {
		return stats, fmt.Errorf("embeddingRepo: stats counts: %w", queryErr)
	}

	defer rows.Close()

	var perNode []NodeChunkCount

	for rows.Next() {
		var entry NodeChunkCount

		if scanErr := rows.Scan(&entry.NodeID, &entry.Chunks); scanErr != nil {
			return stats, fmt.Errorf("embeddingRepo: stats scan: %w", scanErr)
		}

		perNode = append(perNode, entry)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return stats, rowsErr
	}

	stats.TotalNodes = len(perNode)

	for _, entry := range perNode {
		stats.TotalChunks += entry.Chunks

		if entry.Chunks > stats.MaxChunks {
			stats.MaxChunks = entry.Chunks
		}
	}

	if stats.TotalNodes > 0 {
		stats.MeanChunks = float64(stats.TotalChunks) / float64(stats.TotalNodes)
	}

	stats.MedianChunks = medianChunkCount(perNode)

	topN := 5

	if len(perNode) < topN {
		topN = len(perNode)
	}

	stats.TopByChunks = append(stats.TopByChunks, perNode[:topN]...)

	// Large chunks (length(body) >= threshold), resolved per node-chunk
	// through the junction to the shared vector's body.
	largeRows, largeErr := repo.db.Query(`
		SELECT ne.node_id, ne.chunk_idx, length(e.body) AS body_len
		FROM node_embeddings ne
		JOIN embeddings e ON e.content_hash = ne.content_hash AND e.model = ne.model
		WHERE length(e.body) >= ?
		ORDER BY ne.node_id, ne.chunk_idx
	`, largeChunkThreshold)

	if largeErr != nil {
		return stats, fmt.Errorf("embeddingRepo: stats large: %w", largeErr)
	}

	defer largeRows.Close()

	for largeRows.Next() {
		var info NodeChunkInfo

		if scanErr := largeRows.Scan(&info.NodeID, &info.ChunkIdx, &info.BodyLen); scanErr != nil {
			return stats, fmt.Errorf("embeddingRepo: stats large scan: %w", scanErr)
		}

		stats.LargeChunks = append(stats.LargeChunks, info)
	}

	return stats, largeRows.Err()
}

// medianChunkCount returns the integer median chunk count from a slice already
// sorted by chunk count DESC, node_id ASC. An empty input returns 0.
func medianChunkCount(perNode []NodeChunkCount) int {
	if len(perNode) == 0 {
		return 0
	}

	counts := make([]int, len(perNode))

	for idx, entry := range perNode {
		counts[idx] = entry.Chunks
	}

	sort.Ints(counts)

	mid := len(counts) / 2

	if len(counts)%2 == 1 {
		return counts[mid]
	}

	return (counts[mid-1] + counts[mid]) / 2
}

func scanEmbeddings(rows *sql.Rows) ([]EmbeddingRow, error) {
	var results []EmbeddingRow

	for rows.Next() {
		var (
			row     EmbeddingRow
			encoded []byte
		)

		if scanErr := rows.Scan(&row.NodeID, &row.ChunkIdx, &row.Model, &row.ContentHash, &encoded, &row.Dim, &row.Body); scanErr != nil {
			return nil, fmt.Errorf("embeddingRepo: scan: %w", scanErr)
		}

		decoded, decodeErr := decodeVector(encoded, row.Dim)

		if decodeErr != nil {
			return nil, decodeErr
		}

		row.Vector = decoded
		results = append(results, row)
	}

	return results, rows.Err()
}

func encodeVector(vector []float32) ([]byte, error) {
	buffer := &bytes.Buffer{}

	for _, value := range vector {
		if writeErr := binary.Write(buffer, binary.LittleEndian, value); writeErr != nil {
			return nil, fmt.Errorf("embeddingRepo: encode vector: %w", writeErr)
		}
	}

	return buffer.Bytes(), nil
}

func decodeVector(encoded []byte, dim int) ([]float32, error) {
	if len(encoded)%4 != 0 {
		return nil, fmt.Errorf("embeddingRepo: vector blob length %d is not a multiple of 4", len(encoded))
	}

	if len(encoded)/4 != dim {
		return nil, fmt.Errorf("embeddingRepo: vector blob has %d float32s, dim says %d", len(encoded)/4, dim)
	}

	result := make([]float32, dim)
	reader := bytes.NewReader(encoded)

	for idx := 0; idx < dim; idx++ {
		if readErr := binary.Read(reader, binary.LittleEndian, &result[idx]); readErr != nil {
			return nil, fmt.Errorf("embeddingRepo: decode vector at index %d: %w", idx, readErr)
		}
	}

	return result, nil
}
