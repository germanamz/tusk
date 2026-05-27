package index_test

import (
	"reflect"
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

func TestEmbedQueueRepo_ReEnqueuePreservesAttempts(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if enqErr := repo.Enqueue("n1"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	firstDrain, firstDrainErr := repo.Drain(10)

	if firstDrainErr != nil {
		test.Fatalf("first Drain: %v", firstDrainErr)
	}

	if len(firstDrain) != 1 || firstDrain[0].Attempts != 0 {
		test.Fatalf("first drain = %+v, want one row with Attempts=0", firstDrain)
	}

	if reErr := repo.ReEnqueue("n1", 1, "first failure"); reErr != nil {
		test.Fatalf("ReEnqueue 1: %v", reErr)
	}

	secondDrain, secondDrainErr := repo.Drain(10)

	if secondDrainErr != nil {
		test.Fatalf("second Drain: %v", secondDrainErr)
	}

	if len(secondDrain) != 1 {
		test.Fatalf("second drain len = %d, want 1", len(secondDrain))
	}

	if secondDrain[0].Attempts != 1 {
		test.Errorf("Attempts after first ReEnqueue = %d, want 1", secondDrain[0].Attempts)
	}

	if secondDrain[0].LastError != "first failure" {
		test.Errorf("LastError after first ReEnqueue = %q, want %q", secondDrain[0].LastError, "first failure")
	}

	if reErr := repo.ReEnqueue("n1", 2, "second failure"); reErr != nil {
		test.Fatalf("ReEnqueue 2: %v", reErr)
	}

	thirdDrain, thirdDrainErr := repo.Drain(10)

	if thirdDrainErr != nil {
		test.Fatalf("third Drain: %v", thirdDrainErr)
	}

	if len(thirdDrain) != 1 {
		test.Fatalf("third drain len = %d, want 1", len(thirdDrain))
	}

	if thirdDrain[0].Attempts != 2 {
		test.Errorf("Attempts after second ReEnqueue = %d, want 2", thirdDrain[0].Attempts)
	}

	if thirdDrain[0].LastError != "second failure" {
		test.Errorf("LastError after second ReEnqueue = %q, want %q", thirdDrain[0].LastError, "second failure")
	}
}

// TODO(retry-cap): assert enqueued_at-bump prevents starvation (skipped — flaky on coarse clocks).

func TestEmbedQueueRepo_EnqueueDefaultsKindToEmbedAndLeavesLeaseFieldsNil(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if enqErr := repo.Enqueue("n1"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	drained, drainErr := repo.Drain(10)

	if drainErr != nil {
		test.Fatalf("Drain: %v", drainErr)
	}

	if len(drained) != 1 {
		test.Fatalf("len = %d, want 1", len(drained))
	}

	row := drained[0]

	if row.Kind != "embed" {
		test.Errorf("Kind = %q, want %q", row.Kind, "embed")
	}

	if row.LeasedBy != nil {
		test.Errorf("LeasedBy = %v, want nil", row.LeasedBy)
	}

	if row.LeasedUntilNs != nil {
		test.Errorf("LeasedUntilNs = %v, want nil", row.LeasedUntilNs)
	}

	if row.LeaseStartedAtNs != nil {
		test.Errorf("LeaseStartedAtNs = %v, want nil", row.LeaseStartedAtNs)
	}
}

func TestEmbedQueueRepo_ReEnqueuePreservesKindDefault(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if reErr := repo.ReEnqueue("n1", 3, "boom"); reErr != nil {
		test.Fatalf("ReEnqueue: %v", reErr)
	}

	drained, drainErr := repo.Drain(10)

	if drainErr != nil {
		test.Fatalf("Drain: %v", drainErr)
	}

	if len(drained) != 1 {
		test.Fatalf("len = %d, want 1", len(drained))
	}

	if drained[0].Kind != "embed" {
		test.Errorf("Kind = %q, want %q", drained[0].Kind, "embed")
	}
}

func TestEmbedQueueRepo_ListNodeIDs(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewEmbedQueueRepo(store)

	for _, nodeID := range []string{"b/two", "a/one", "c/three"} {
		if enqErr := repo.Enqueue(nodeID); enqErr != nil {
			test.Fatalf("Enqueue %s: %v", nodeID, enqErr)
		}
	}

	ids, listErr := repo.ListNodeIDs()

	if listErr != nil {
		test.Fatalf("ListNodeIDs: %v", listErr)
	}

	want := []string{"a/one", "b/two", "c/three"}

	if !reflect.DeepEqual(ids, want) {
		test.Errorf("ListNodeIDs = %v, want %v", ids, want)
	}
}

func TestEmbedQueueRepo_ListNodeIDs_Empty(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewEmbedQueueRepo(store)

	ids, listErr := repo.ListNodeIDs()

	if listErr != nil {
		test.Fatalf("ListNodeIDs: %v", listErr)
	}

	if len(ids) != 0 {
		test.Errorf("ListNodeIDs = %v, want empty", ids)
	}
}
