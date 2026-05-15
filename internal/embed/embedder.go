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

// DefaultWorkers and DefaultTimeoutSeconds are the user-facing defaults applied
// at construction sites when the corresponding manifest field is unset. Phase 2
// of Spec B adds DefaultBatchSize alongside these for batched embed calls.
const (
	DefaultWorkers        = 4
	DefaultTimeoutSeconds = 120
)

// ResolveWorkers returns the configured workers value or DefaultWorkers when zero.
func ResolveWorkers(configured int) int {
	if configured <= 0 {
		return DefaultWorkers
	}

	return configured
}

// ResolveTimeoutSeconds returns the configured value or DefaultTimeoutSeconds when zero.
func ResolveTimeoutSeconds(configured int) int {
	if configured <= 0 {
		return DefaultTimeoutSeconds
	}

	return configured
}
