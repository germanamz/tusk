// Package embed contains the embedding pipeline for tusk's semantic retrieval:
// the Embedder interface, ChunkingStrategy interface, payload builder, and
// cosine similarity helper. Concrete embedder implementations live in this
// package (ollama.go); the interface admits more without touching the rest
// of the system.
package embed

import (
	"context"
	"errors"
)

// Embedder produces a single vector per payload.
type Embedder interface {
	Embed(ctx context.Context, payload []byte) ([]float32, error)
	Model() string
	Dim() int
}

// TransportError marks an embed failure as transient infrastructure trouble —
// the embedding backend is unreachable, timed out, dropped the connection, or
// returned a 5xx — as opposed to a per-node/content fault (a 4xx, a dimension
// mismatch, a parse error). DrainQueue aborts the whole drain pass on a
// TransportError and leaves the claimed rows leased for the next tick, rather
// than burning each node's retry budget and dropping it from semantic results
// over a brief blip.
type TransportError struct {
	Err error
}

func (transportErr *TransportError) Error() string {
	return transportErr.Err.Error()
}

func (transportErr *TransportError) Unwrap() error {
	return transportErr.Err
}

// IsTransportError reports whether err, or any error it wraps, is a
// TransportError.
func IsTransportError(err error) bool {
	var transportErr *TransportError

	return errors.As(err, &transportErr)
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
