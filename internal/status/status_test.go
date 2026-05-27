package status_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/status"
)

func TestSnapshot_CountsByType(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	nodes := index.NewNodeRepo(store)

	nodes.Upsert(index.NodeRow{ID: "t/a", Type: "ticket", Path: "t/a.md", PropertiesJSON: "{}", LastChecksum: "x"})
	nodes.Upsert(index.NodeRow{ID: "t/b", Type: "ticket", Path: "t/b.md", PropertiesJSON: "{}", LastChecksum: "x"})
	nodes.Upsert(index.NodeRow{ID: "n/c", Type: "note", Path: "n/c.md", PropertiesJSON: "{}", LastChecksum: "x"})

	snap, snapErr := status.Snapshot(status.Config{
		Nodes:      nodes,
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Meta:       index.NewMetaRepo(store),
	})

	if snapErr != nil {
		test.Fatalf("Snapshot: %v", snapErr)
	}

	if snap.NodesByType["ticket"] != 2 {
		test.Errorf("ticket count = %d, want 2", snap.NodesByType["ticket"])
	}

	if snap.NodesByType["note"] != 1 {
		test.Errorf("note count = %d, want 1", snap.NodesByType["note"])
	}
}

func TestSnapshot_ReportsQueueDepth(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	queueRepo := index.NewEmbedQueueRepo(store)
	queueRepo.Enqueue("a")
	queueRepo.Enqueue("b")
	queueRepo.Enqueue("c")

	snap, _ := status.Snapshot(status.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: queueRepo,
		Meta:       index.NewMetaRepo(store),
	})

	if snap.EmbedQueueDepth != 3 {
		test.Errorf("EmbedQueueDepth = %d, want 3", snap.EmbedQueueDepth)
	}

	if snap.ReindexQueueDepth != 0 {
		test.Errorf("ReindexQueueDepth = %d, want 0", snap.ReindexQueueDepth)
	}
}

func TestSnapshot_SeparatesEmbedAndReindexDepth(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	queueRepo := index.NewEmbedQueueRepo(store)
	queueRepo.Enqueue("embed-1")
	queueRepo.Enqueue("embed-2")
	queueRepo.EnqueueReindex("notes/a.md")

	snap, snapErr := status.Snapshot(status.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: queueRepo,
		Meta:       index.NewMetaRepo(store),
	})

	if snapErr != nil {
		test.Fatalf("Snapshot: %v", snapErr)
	}

	if snap.EmbedQueueDepth != 2 {
		test.Errorf("EmbedQueueDepth = %d, want 2 (reindex row must not count)", snap.EmbedQueueDepth)
	}

	if snap.ReindexQueueDepth != 1 {
		test.Errorf("ReindexQueueDepth = %d, want 1", snap.ReindexQueueDepth)
	}
}

func TestSnapshot_LastReindexAt(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	metaRepo := index.NewMetaRepo(store)
	metaRepo.Set("last_reindex_at", "1747000000")

	snap, _ := status.Snapshot(status.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Meta:       metaRepo,
	})

	if snap.LastReindexAt != "1747000000" {
		test.Errorf("LastReindexAt = %q, want 1747000000", snap.LastReindexAt)
	}
}
