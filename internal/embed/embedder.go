// Package embed contains the embedding pipeline for tusk's semantic retrieval:
// the Embedder interface, ChunkingStrategy interface, payload builder, and
// cosine similarity helper. Concrete embedder implementations live in this
// package (ollama.go); the interface admits more without touching the rest
// of the system.
package embed

import "context"

// Embedder produces a single vector per payload.
type Embedder interface {
	Embed(ctx context.Context, payload []byte) ([]float32, error)
	Model() string
	Dim() int
}

// DefaultTimeoutSeconds is the user-facing default applied at construction
// sites when the manifest field is unset. Phase 2 of Spec B adds
// DefaultBatchSize alongside this for batched embed calls.
//
// The worker-pool size is resolved by internal/embedconfig.ResolveWorkers,
// which honors an explicit 0 as "opt out of the worker pool in this
// instance."
const DefaultTimeoutSeconds = 120

// ResolveTimeoutSeconds returns the configured value or DefaultTimeoutSeconds when zero.
func ResolveTimeoutSeconds(configured int) int {
	if configured <= 0 {
		return DefaultTimeoutSeconds
	}

	return configured
}
