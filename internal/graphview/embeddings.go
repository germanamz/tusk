package graphview

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/germanamz/tusk/internal/index"
)

// EmbeddingsResponse is the GET /api/graph/embeddings payload: one mean-pooled,
// L2-normalized vector per file node that has an embedding.
type EmbeddingsResponse struct {
	Model     string               `json:"model"`     // e.g. "nomic-embed-text"; "" when empty
	Dim       int                  `json:"dim"`       // vector length; 0 when empty
	Signature string               `json:"signature"` // stable hash of (id+contentHashes); cache key for the client
	Vectors   map[string][]float32 `json:"vectors"`   // nodeID -> unit vector (len == Dim)
}

func (srv *Server) handleEmbeddings(writer http.ResponseWriter, _ *http.Request) {
	resp := EmbeddingsResponse{Vectors: map[string][]float32{}}

	if srv.deps.Embeddings == nil {
		writeJSON(writer, resp) // no source configured → empty, valid payload
		return
	}

	fileRows, err := srv.deps.Nodes.ListFileNodes()
	if err != nil {
		http.Error(writer, "index unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	ids := make([]string, len(fileRows))
	for i, row := range fileRows {
		ids[i] = row.ID
	}

	rows, err := srv.deps.Embeddings.ListByNodeIDs(ids)
	if err != nil {
		http.Error(writer, "embeddings unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	resp = buildEmbeddingsResponse(rows)
	writeJSON(writer, resp)
}

// buildEmbeddingsResponse aggregates raw EmbeddingRows into one mean-pooled,
// L2-normalized vector per node. Rows must arrive ordered by node_id, chunk_idx.
// Model and Dim are taken from the first row seen; nodes whose chunk vectors
// have inconsistent lengths are skipped defensively. Nodes whose mean vector
// has zero L2 norm are also omitted from Vectors.
func buildEmbeddingsResponse(rows []index.EmbeddingRow) EmbeddingsResponse {
	resp := EmbeddingsResponse{Vectors: map[string][]float32{}}

	if len(rows) > 0 {
		resp.Model = rows[0].Model
		resp.Dim = rows[0].Dim
	}

	// nodeEntry accumulates chunks for one node.
	type nodeEntry struct {
		chunks        [][]float32
		contentHashes []string
		dim           int
		skip          bool // true when a chunk has unexpected length
	}

	nodeOrder := make([]string, 0)
	nodeMap := make(map[string]*nodeEntry)

	for _, row := range rows {
		entry, exists := nodeMap[row.NodeID]
		if !exists {
			entry = &nodeEntry{dim: row.Dim}
			nodeMap[row.NodeID] = entry
			nodeOrder = append(nodeOrder, row.NodeID)
		}

		if len(row.Vector) != entry.dim {
			entry.skip = true
		}

		entry.chunks = append(entry.chunks, row.Vector)
		entry.contentHashes = append(entry.contentHashes, row.ContentHash)
	}

	for _, nodeID := range nodeOrder {
		entry := nodeMap[nodeID]

		if entry.skip {
			continue
		}

		dim := entry.dim
		acc := make([]float64, dim)

		for _, chunk := range entry.chunks {
			for j, v := range chunk {
				acc[j] += float64(v)
			}
		}

		n := float64(len(entry.chunks))

		for j := range acc {
			acc[j] /= n
		}

		var norm float64

		for _, v := range acc {
			norm += v * v
		}

		norm = math.Sqrt(norm)

		if norm == 0 {
			// Degenerate zero vector → omit this node.
			continue
		}

		vec := make([]float32, dim)

		for j, v := range acc {
			vec[j] = float32(v / norm)
		}

		resp.Vectors[nodeID] = vec
	}

	// Signature: sha256 over *emitted* nodes in sorted id order. Only nodes that
	// appear in resp.Vectors contribute, so changes to skipped or zero-norm nodes
	// do not needlessly invalidate the client's projection cache.
	sortedIDs := make([]string, 0, len(resp.Vectors))
	for id := range resp.Vectors {
		sortedIDs = append(sortedIDs, id)
	}
	sort.Strings(sortedIDs)

	hasher := sha256.New()

	for _, nodeID := range sortedIDs {
		entry := nodeMap[nodeID]
		s := nodeID + "\x1f" + strings.Join(entry.contentHashes, ",") + "\n"
		_, _ = hasher.Write([]byte(s))
	}

	resp.Signature = hex.EncodeToString(hasher.Sum(nil))

	return resp
}
