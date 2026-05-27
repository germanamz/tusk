package index

import (
	"sync"

	"github.com/google/uuid"
)

var (
	workerIDOnce  sync.Once
	workerIDValue string
)

// WorkerID returns the stable per-process worker identity used as the
// `leased_by` token for file_state and embed_queue lease claims. The
// identity is generated once on first call via crypto/rand (UUIDv4)
// and cached for the lifetime of the process; subsequent calls return
// the same string.
//
// The identity is intentionally process-scoped, not workspace-scoped:
// two MCP servers running against the same workspace get different
// IDs, so a stale lease left by a crashed predecessor can never be
// mistaken for one held by the current process.
func WorkerID() string {
	workerIDOnce.Do(func() {
		workerIDValue = uuid.NewString()
	})

	return workerIDValue
}
