package status

import (
	"github.com/germanamz/tusk/internal/index"
)

// Request configures a Run call. All fields are required: callers are expected
// to open the index and construct the repos before invoking the service.
type Request struct {
	Nodes      *index.NodeRepo
	Edges      *index.EdgeRepo
	EmbedQueue *index.EmbedQueueRepo
	Meta       *index.MetaRepo
}

// Result is the typed payload returned by Run. The shape mirrors SnapshotData
// so existing renderers and MCP handlers can consume it without translation.
type Result struct {
	NodesByType     map[string]int
	EdgeCount       int
	EmbedQueueDepth int
	LastReindexAt   string
}

// Run is the canonical entry point for the `status` / `tusk_status` verb.
// It wraps Snapshot so the CLI and MCP handlers share a single code path.
func Run(req Request) (*Result, error) {
	snap, snapErr := Snapshot(Config(req))

	if snapErr != nil {
		return nil, snapErr
	}

	return &Result{
		NodesByType:     snap.NodesByType,
		EdgeCount:       snap.EdgeCount,
		EmbedQueueDepth: snap.EmbedQueueDepth,
		LastReindexAt:   snap.LastReindexAt,
	}, nil
}
