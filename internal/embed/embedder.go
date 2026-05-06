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
