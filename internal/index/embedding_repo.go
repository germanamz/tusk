package index

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"fmt"
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

	_, execErr := repo.db.Exec(`
		INSERT INTO embeddings (node_id, chunk_idx, model, content_hash, vector, dim, body)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, chunk_idx) DO UPDATE SET
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
