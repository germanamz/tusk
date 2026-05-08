package index_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func newTestEmbedQueueRepo(test *testing.T) *index.EmbedQueueRepo {
	test.Helper()

	store := openTestIndex(test)

	return index.NewEmbedQueueRepo(store)
}

func TestEmbedQueueRepo_EnqueueAndDepth(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	for _, nodeID := range []string{"a", "b", "c"} {
		if enqueueErr := repo.Enqueue(nodeID); enqueueErr != nil {
			test.Fatalf("Enqueue %s: %v", nodeID, enqueueErr)
		}
	}

	depth, depthErr := repo.Depth()

	if depthErr != nil {
		test.Fatalf("Depth: %v", depthErr)
	}

	if depth != 3 {
		test.Errorf("Depth = %d, want 3", depth)
	}
}

func TestEmbedQueueRepo_EnqueueIsIdempotent(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	for idx := 0; idx < 3; idx++ {
		if enqueueErr := repo.Enqueue("same"); enqueueErr != nil {
			test.Fatalf("Enqueue %d: %v", idx, enqueueErr)
		}
	}

	depth, _ := repo.Depth()

	if depth != 1 {
		test.Errorf("Depth = %d, want 1 after idempotent enqueue", depth)
	}
}

func TestEmbedQueueRepo_DrainReturnsEnqueuedNodesAndRemovesThem(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	for _, nodeID := range []string{"a", "b", "c"} {
		repo.Enqueue(nodeID)
	}

	drained, drainErr := repo.Drain(10)

	if drainErr != nil {
		test.Fatalf("Drain: %v", drainErr)
	}

	if len(drained) != 3 {
		test.Errorf("len = %d, want 3", len(drained))
	}

	depth, _ := repo.Depth()

	if depth != 0 {
		test.Errorf("Depth after drain = %d, want 0", depth)
	}
}

func TestEmbedQueueRepo_DrainHonorsLimit(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	for _, nodeID := range []string{"a", "b", "c", "d", "e"} {
		repo.Enqueue(nodeID)
	}

	drained, _ := repo.Drain(2)

	if len(drained) != 2 {
		test.Errorf("len = %d, want 2", len(drained))
	}

	depth, _ := repo.Depth()

	if depth != 3 {
		test.Errorf("Depth after partial drain = %d, want 3", depth)
	}
}

func TestEmbedQueueRepo_MarkFailedKeepsInQueue(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	repo.Enqueue("flaky")

	if markErr := repo.MarkFailed("flaky", "ollama unreachable"); markErr != nil {
		test.Fatalf("MarkFailed: %v", markErr)
	}

	depth, _ := repo.Depth()

	if depth != 1 {
		test.Errorf("Depth after MarkFailed = %d, want 1 (still queued)", depth)
	}
}
