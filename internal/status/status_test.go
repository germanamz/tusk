package status_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
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

// TestSnapshot_SeparatesFailedEmbedsFromPending pins #845: an embed row that
// stopped retrying is not pending, and is counted on its own.
func TestSnapshot_SeparatesFailedEmbedsFromPending(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	queueRepo := index.NewEmbedQueueRepo(store)
	queueRepo.Enqueue("pending")
	queueRepo.Enqueue("stuck")

	if _, drainErr := queueRepo.DrainEmbed("worker", 10, time.Minute); drainErr != nil {
		test.Fatalf("DrainEmbed: %v", drainErr)
	}

	if parkErr := queueRepo.Park("stuck", "worker", errors.New("refused")); parkErr != nil {
		test.Fatalf("Park: %v", parkErr)
	}

	snap, snapErr := status.Snapshot(status.Config{
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EmbedQueue: queueRepo,
		Meta:       index.NewMetaRepo(store),
	})

	if snapErr != nil {
		test.Fatalf("Snapshot: %v", snapErr)
	}

	if snap.EmbedQueueDepth != 1 || snap.EmbedFailed != 1 {
		test.Errorf("EmbedQueueDepth = %d, EmbedFailed = %d; want 1 and 1", snap.EmbedQueueDepth, snap.EmbedFailed)
	}
}

// TestSnapshot_EmbedWaitingWithoutEmbeddings: with no [embeddings] section
// nothing drains the queue; its rows wait for one to be added, and the
// snapshot says so instead of passing them off as a backlog.
func TestSnapshot_EmbedWaitingWithoutEmbeddings(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	queueRepo := index.NewEmbedQueueRepo(store)
	queueRepo.Enqueue("a")

	for _, testCase := range []struct {
		name     string
		manifest *manifest.Manifest
		want     bool
	}{
		{name: "no embeddings section", manifest: &manifest.Manifest{}, want: true},
		{name: "embeddings configured", manifest: &manifest.Manifest{Embeddings: manifest.EmbeddingsSection{Provider: "ollama"}}, want: false},
		{name: "manifest unknown", manifest: nil, want: false},
	} {
		snap, snapErr := status.Snapshot(status.Config{
			Nodes:      index.NewNodeRepo(store),
			Edges:      index.NewEdgeRepo(store),
			EmbedQueue: queueRepo,
			Meta:       index.NewMetaRepo(store),
			Manifest:   testCase.manifest,
		})

		if snapErr != nil {
			test.Fatalf("%s: Snapshot: %v", testCase.name, snapErr)
		}

		if snap.EmbedWaiting != testCase.want {
			test.Errorf("%s: EmbedWaiting = %v, want %v", testCase.name, snap.EmbedWaiting, testCase.want)
		}
	}
}
