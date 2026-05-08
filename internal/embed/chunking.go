package embed

// ChunkingStrategy splits a payload into one or more chunks; each chunk is
// embedded independently. Plan 5 ships only WholeDocument; future strategies
// (fixed-token, sentence-aware, etc.) plug in here.
type ChunkingStrategy interface {
	Chunk(payload []byte) [][]byte
}

// WholeDocument is the default Plan 5 strategy: one chunk per node, the entire
// payload.
type WholeDocument struct{}

// Chunk implements ChunkingStrategy.
func (strategy WholeDocument) Chunk(payload []byte) [][]byte {
	return [][]byte{payload}
}
