// Package status renders a quick health summary of the workspace.
package status

import (
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// SnapshotData describes the workspace at a moment in time.
type SnapshotData struct {
	NodesByType       map[string]int
	EdgeCount         int
	EmbedQueueDepth   int // pending rows with kind='embed'
	ReindexQueueDepth int // pending rows with kind='reindex'
	EmbedFailed       int // embed rows that stopped retrying (parked); not pending
	// EmbedWaiting reports that the pending embed rows wait for an
	// [embeddings] section: none is configured, so nothing drains them, and
	// they embed once one is added.
	EmbedWaiting  bool
	LastReindexAt string
}

// Config configures Snapshot.
type Config struct {
	Nodes      *index.NodeRepo
	Edges      *index.EdgeRepo
	EmbedQueue *index.EmbedQueueRepo
	Meta       *index.MetaRepo
	Manifest   *manifest.Manifest // optional; nil leaves EmbedWaiting false
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

	edgeCount, edgeErr := config.Edges.Count()

	if edgeErr != nil {
		return nil, edgeErr
	}

	snap.EdgeCount = edgeCount

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

	failed, failedErr := config.EmbedQueue.CountParked()

	if failedErr != nil {
		return nil, failedErr
	}

	snap.EmbedFailed = failed
	snap.EmbedWaiting = embedWaiting(config.Manifest, embedDepth)

	last, lastErr := config.Meta.Get(index.LastReindexAtKey)

	if lastErr != nil {
		return nil, lastErr
	}

	snap.LastReindexAt = last

	return snap, nil
}

// embedWaiting reports whether depth pending embed rows wait for an
// [embeddings] section that loaded doesn't have. False when loaded is nil:
// without the manifest there is no telling.
func embedWaiting(loaded *manifest.Manifest, depth int) bool {
	return loaded != nil && loaded.Embeddings.Provider == "" && depth > 0
}
