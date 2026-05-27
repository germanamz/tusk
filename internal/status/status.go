// Package status renders a quick health summary of the workspace.
package status

import (
	"github.com/germanamz/tusk/internal/index"
)

// SnapshotData describes the workspace at a moment in time.
type SnapshotData struct {
	NodesByType       map[string]int
	EdgeCount         int
	EmbedQueueDepth   int // pending rows with kind='embed'
	ReindexQueueDepth int // pending rows with kind='reindex'
	LastReindexAt     string
}

// Config configures Snapshot.
type Config struct {
	Nodes      *index.NodeRepo
	Edges      *index.EdgeRepo
	EmbedQueue *index.EmbedQueueRepo
	Meta       *index.MetaRepo
}

// Snapshot reads index aggregates and returns the rolled-up SnapshotData.
func Snapshot(config Config) (*SnapshotData, error) {
	snap := &SnapshotData{NodesByType: map[string]int{}}

	nodes, listErr := config.Nodes.List(index.ListFilter{})

	if listErr != nil {
		return nil, listErr
	}

	for _, row := range nodes {
		snap.NodesByType[row.Type]++
	}

	edges, edgeErr := config.Edges.ListAll()

	if edgeErr != nil {
		return nil, edgeErr
	}

	snap.EdgeCount = len(edges)

	embedDepth, embedDepthErr := config.EmbedQueue.DepthByKind("embed")

	if embedDepthErr != nil {
		return nil, embedDepthErr
	}

	snap.EmbedQueueDepth = embedDepth

	reindexDepth, reindexDepthErr := config.EmbedQueue.DepthByKind("reindex")

	if reindexDepthErr != nil {
		return nil, reindexDepthErr
	}

	snap.ReindexQueueDepth = reindexDepth

	last, lastErr := config.Meta.Get("last_reindex_at")

	if lastErr != nil {
		return nil, lastErr
	}

	snap.LastReindexAt = last

	return snap, nil
}
