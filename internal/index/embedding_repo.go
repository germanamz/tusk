package index

import (
	"bytes"
	"database/sql"
	"encoding/binary"
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

// Upsert inserts or replaces the embedding for (node_id, chunk_idx).
func (repo *EmbeddingRepo) Upsert(row EmbeddingRow) error {
	encoded, encodeErr := encodeVector(row.Vector)

	if encodeErr != nil {
		return encodeErr
	}

	// P2 made node_id the uniqueness key; chunk_idx is retained for
	// back-compat reads but is always 0 on new writes when the sub-units
	// pack is producing leaf-unit embeddings. Updates land on the
	// existing row regardless of chunk_idx, preserving the legacy
	// "latest chunk wins" semantic. For multi-chunk nodes, the last
	// chunk processed wins; the body column reflects that last chunk.
	_, execErr := repo.db.Exec(`
		INSERT INTO embeddings (node_id, chunk_idx, model, content_hash, vector, dim, body)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			chunk_idx    = excluded.chunk_idx,
			model        = excluded.model,
			content_hash = excluded.content_hash,
			vector       = excluded.vector,
			dim          = excluded.dim,
			body         = excluded.body
	`, row.NodeID, row.ChunkIdx, row.Model, row.ContentHash, encoded, row.Dim, row.Body)

	if execErr != nil {
		return fmt.Errorf("embeddingRepo: upsert %s: %w", row.NodeID, execErr)
	}

	return nil
}

// GetByNodeID returns all embeddings (chunks) for nodeID, ordered by chunk_idx.
func (repo *EmbeddingRepo) GetByNodeID(nodeID string) ([]EmbeddingRow, error) {
	rows, queryErr := repo.db.Query(`
		SELECT node_id, chunk_idx, model, content_hash, vector, dim, body
		FROM embeddings
		WHERE node_id = ?
		ORDER BY chunk_idx
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
func (repo *EmbeddingRepo) ListSubUnitsForFiles(fileIDs []string) ([]EmbeddingRow, error) {
	if len(fileIDs) == 0 {
		return nil, nil
	}

	conditions := make([]string, 0, len(fileIDs))
	args := make([]any, 0, len(fileIDs))

	for _, fileID := range fileIDs {
		conditions = append(conditions, "node_id GLOB ?")
		args = append(args, fileID+"#*")
	}

	query := fmt.Sprintf(
		`SELECT node_id, chunk_idx, model, content_hash, vector, dim, body FROM embeddings WHERE %s ORDER BY node_id, chunk_idx`,
		strings.Join(conditions, " OR "),
	)

	rows, queryErr := repo.db.Query(query, args...)

	if queryErr != nil {
		return nil, fmt.Errorf("embeddingRepo: list sub-units: %w", queryErr)
	}

	defer rows.Close()

	return scanEmbeddings(rows)
}

// ListByNodeIDs returns all embeddings whose node_id is in nodeIDs.
func (repo *EmbeddingRepo) ListByNodeIDs(nodeIDs []string) ([]EmbeddingRow, error) {
	if len(nodeIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]byte, 0, len(nodeIDs)*2-1)
	args := make([]any, 0, len(nodeIDs))

	for idx, nodeID := range nodeIDs {
		if idx > 0 {
			placeholders = append(placeholders, ',')
		}

		placeholders = append(placeholders, '?')
		args = append(args, nodeID)
	}

	query := fmt.Sprintf(`SELECT node_id, chunk_idx, model, content_hash, vector, dim, body FROM embeddings WHERE node_id IN (%s) ORDER BY node_id, chunk_idx`, string(placeholders))

	rows, queryErr := repo.db.Query(query, args...)

	if queryErr != nil {
		return nil, fmt.Errorf("embeddingRepo: list: %w", queryErr)
	}

	defer rows.Close()

	return scanEmbeddings(rows)
}

// DeleteByNodeID removes every embedding for nodeID.
func (repo *EmbeddingRepo) DeleteByNodeID(nodeID string) error {
	_, execErr := repo.db.Exec(`DELETE FROM embeddings WHERE node_id = ?`, nodeID)

	if execErr != nil {
		return fmt.Errorf("embeddingRepo: delete %s: %w", nodeID, execErr)
	}

	return nil
}

// ListNodeIDs returns every distinct node_id present in the embeddings
// table, sorted ascending.
func (repo *EmbeddingRepo) ListNodeIDs() ([]string, error) {
	rows, queryErr := repo.db.Query(`SELECT DISTINCT node_id FROM embeddings ORDER BY node_id`)

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

	// Per-node counts.
	rows, queryErr := repo.db.Query(`
		SELECT node_id, COUNT(*) AS chunk_count
		FROM embeddings
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

	// Large chunks (length(body) >= threshold).
	largeRows, largeErr := repo.db.Query(`
		SELECT node_id, chunk_idx, length(body) AS body_len
		FROM embeddings
		WHERE length(body) >= ?
		ORDER BY node_id, chunk_idx
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
