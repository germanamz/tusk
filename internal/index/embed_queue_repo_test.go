package index_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
)

const (
	testWorkerA = "worker-a"
	testWorkerB = "worker-b"
	testTTL     = 60 * time.Second
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

func TestEmbedQueueRepo_DrainEmptyReturnsEmpty(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	drained, drainErr := repo.DrainEmbed(testWorkerA, 10, testTTL)

	if drainErr != nil {
		test.Fatalf("Drain: %v", drainErr)
	}

	if len(drained) != 0 {
		test.Errorf("Drain on empty = %d rows, want 0", len(drained))
	}
}

func TestEmbedQueueRepo_DrainClaimsBatchAndSetsLease(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	for _, nodeID := range []string{"a", "b", "c"} {
		if enqErr := repo.Enqueue(nodeID); enqErr != nil {
			test.Fatalf("Enqueue %s: %v", nodeID, enqErr)
		}
	}

	drained, drainErr := repo.DrainEmbed(testWorkerA, 10, testTTL)

	if drainErr != nil {
		test.Fatalf("Drain: %v", drainErr)
	}

	if len(drained) != 3 {
		test.Fatalf("len = %d, want 3 (all rows claimed)", len(drained))
	}

	// Drain no longer removes rows on read; they stay in the table
	// holding leases.
	depth, _ := repo.Depth()

	if depth != 3 {
		test.Errorf("Depth after Drain = %d, want 3 (claim does not delete)", depth)
	}

	for _, row := range drained {
		if row.LeasedBy == nil || *row.LeasedBy != testWorkerA {
			test.Errorf("LeasedBy = %v, want %q", row.LeasedBy, testWorkerA)
		}

		if row.LeasedUntilNs == nil || *row.LeasedUntilNs == 0 {
			test.Errorf("LeasedUntilNs = %v, want non-nil non-zero", row.LeasedUntilNs)
		}

		if row.LeaseStartedAtNs == nil || *row.LeaseStartedAtNs == 0 {
			test.Errorf("LeaseStartedAtNs = %v, want non-nil non-zero", row.LeaseStartedAtNs)
		}

		if row.Kind != "embed" {
			test.Errorf("Kind = %q, want %q", row.Kind, "embed")
		}
	}
}

func TestEmbedQueueRepo_DrainHonorsBatchSize(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	for _, nodeID := range []string{"a", "b", "c", "d", "e"} {
		if enqErr := repo.Enqueue(nodeID); enqErr != nil {
			test.Fatalf("Enqueue %s: %v", nodeID, enqErr)
		}
	}

	drained, _ := repo.DrainEmbed(testWorkerA, 2, testTTL)

	if len(drained) != 2 {
		test.Errorf("len = %d, want 2", len(drained))
	}
}

func TestEmbedQueueRepo_DrainTwoWorkersGetDisjointBatches(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	for _, nodeID := range []string{"a", "b", "c", "d"} {
		if enqErr := repo.Enqueue(nodeID); enqErr != nil {
			test.Fatalf("Enqueue %s: %v", nodeID, enqErr)
		}
	}

	firstBatch, drainErr := repo.DrainEmbed(testWorkerA, 2, testTTL)

	if drainErr != nil {
		test.Fatalf("first Drain: %v", drainErr)
	}

	secondBatch, drainErr := repo.DrainEmbed(testWorkerB, 2, testTTL)

	if drainErr != nil {
		test.Fatalf("second Drain: %v", drainErr)
	}

	if len(firstBatch) != 2 || len(secondBatch) != 2 {
		test.Fatalf("batch sizes = %d, %d; want 2, 2", len(firstBatch), len(secondBatch))
	}

	seen := make(map[string]struct{}, 4)

	for _, row := range firstBatch {
		if row.LeasedBy == nil || *row.LeasedBy != testWorkerA {
			test.Errorf("first batch LeasedBy = %v, want %q", row.LeasedBy, testWorkerA)
		}

		seen[row.NodeID] = struct{}{}
	}

	for _, row := range secondBatch {
		if row.LeasedBy == nil || *row.LeasedBy != testWorkerB {
			test.Errorf("second batch LeasedBy = %v, want %q", row.LeasedBy, testWorkerB)
		}

		if _, dup := seen[row.NodeID]; dup {
			test.Errorf("node %s appeared in both batches", row.NodeID)
		}
	}
}

func TestEmbedQueueRepo_DrainSkipsRowsLeasedByAnotherWorker(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if enqErr := repo.Enqueue("only"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	firstBatch, _ := repo.DrainEmbed(testWorkerA, 10, testTTL)

	if len(firstBatch) != 1 {
		test.Fatalf("first batch = %d, want 1", len(firstBatch))
	}

	// Lease held by worker A; worker B's drain sees nothing claimable.
	secondBatch, drainErr := repo.DrainEmbed(testWorkerB, 10, testTTL)

	if drainErr != nil {
		test.Fatalf("second Drain: %v", drainErr)
	}

	if len(secondBatch) != 0 {
		test.Errorf("second batch = %d, want 0 (row leased by other worker)", len(secondBatch))
	}
}

func TestEmbedQueueRepo_DrainReclaimsExpiredLease(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if enqErr := repo.Enqueue("only"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	// Negative TTL forces leased_until_ns into the past.
	firstBatch, _ := repo.DrainEmbed(testWorkerA, 10, -1*time.Second)

	if len(firstBatch) != 1 {
		test.Fatalf("first batch = %d, want 1", len(firstBatch))
	}

	// Worker B now sees the row as expired-claimable.
	secondBatch, drainErr := repo.DrainEmbed(testWorkerB, 10, testTTL)

	if drainErr != nil {
		test.Fatalf("second Drain: %v", drainErr)
	}

	if len(secondBatch) != 1 {
		test.Fatalf("second batch = %d, want 1 (expired lease reclaimable)", len(secondBatch))
	}

	if secondBatch[0].LeasedBy == nil || *secondBatch[0].LeasedBy != testWorkerB {
		test.Errorf("LeasedBy = %v, want %q", secondBatch[0].LeasedBy, testWorkerB)
	}
}

func TestEmbedQueueRepo_AckDeletesWhenWorkerMatches(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if enqErr := repo.Enqueue("n1"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	if _, drainErr := repo.DrainEmbed(testWorkerA, 10, testTTL); drainErr != nil {
		test.Fatalf("Drain: %v", drainErr)
	}

	if ackErr := repo.Ack("n1", testWorkerA); ackErr != nil {
		test.Fatalf("Ack: %v", ackErr)
	}

	depth, _ := repo.Depth()

	if depth != 0 {
		test.Errorf("Depth after Ack = %d, want 0", depth)
	}
}

func TestEmbedQueueRepo_AckIsNoOpWhenLeaseMovedOn(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if enqErr := repo.Enqueue("n1"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	if _, drainErr := repo.DrainEmbed(testWorkerA, 10, testTTL); drainErr != nil {
		test.Fatalf("Drain: %v", drainErr)
	}

	// Stale worker tries to Ack — the lease is held by worker A. No-op.
	if ackErr := repo.Ack("n1", testWorkerB); ackErr != nil {
		test.Fatalf("Ack stale: %v", ackErr)
	}

	depth, _ := repo.Depth()

	if depth != 1 {
		test.Errorf("Depth after stale Ack = %d, want 1 (row preserved)", depth)
	}
}

func TestEmbedQueueRepo_NackBumpsAttemptsAndClearsLease(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if enqErr := repo.Enqueue("n1"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	firstBatch, _ := repo.DrainEmbed(testWorkerA, 10, testTTL)

	if len(firstBatch) != 1 || firstBatch[0].Attempts != 0 {
		test.Fatalf("first batch = %+v, want one row with Attempts=0", firstBatch)
	}

	if nackErr := repo.Nack("n1", testWorkerA, errBoom()); nackErr != nil {
		test.Fatalf("Nack: %v", nackErr)
	}

	secondBatch, _ := repo.DrainEmbed(testWorkerA, 10, testTTL)

	if len(secondBatch) != 1 {
		test.Fatalf("second batch = %d, want 1", len(secondBatch))
	}

	if secondBatch[0].Attempts != 1 {
		test.Errorf("Attempts after Nack = %d, want 1", secondBatch[0].Attempts)
	}

	if secondBatch[0].LastError != "boom" {
		test.Errorf("LastError = %q, want %q", secondBatch[0].LastError, "boom")
	}
}

func TestEmbedQueueRepo_NackIsNoOpWhenLeaseMovedOn(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewEmbedQueueRepo(store)

	if enqErr := repo.Enqueue("n1"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	if _, drainErr := repo.DrainEmbed(testWorkerA, 10, testTTL); drainErr != nil {
		test.Fatalf("Drain: %v", drainErr)
	}

	// Stale worker — its Nack must not touch the row.
	if nackErr := repo.Nack("n1", testWorkerB, errBoom()); nackErr != nil {
		test.Fatalf("Nack: %v", nackErr)
	}

	var (
		leasedBy  *string
		attempts  int
		lastError *string
	)

	if scanErr := store.DB().QueryRow(`
		SELECT leased_by, attempts, last_error FROM embed_queue WHERE node_id = ?
	`, "n1").Scan(&leasedBy, &attempts, &lastError); scanErr != nil {
		test.Fatalf("inspect row: %v", scanErr)
	}

	if leasedBy == nil || *leasedBy != testWorkerA {
		test.Errorf("LeasedBy after stale Nack = %v, want %q", leasedBy, testWorkerA)
	}

	if attempts != 0 {
		test.Errorf("Attempts after stale Nack = %d, want 0", attempts)
	}

	if lastError != nil && *lastError != "" {
		test.Errorf("LastError after stale Nack = %v, want nil/empty", lastError)
	}
}

func TestEmbedQueueRepo_DropRemovesRow(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if enqErr := repo.Enqueue("doomed"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	if _, drainErr := repo.DrainEmbed(testWorkerA, 10, testTTL); drainErr != nil {
		test.Fatalf("Drain: %v", drainErr)
	}

	if dropErr := repo.Drop("doomed", testWorkerA); dropErr != nil {
		test.Fatalf("Drop: %v", dropErr)
	}

	depth, _ := repo.Depth()

	if depth != 0 {
		test.Errorf("Depth after Drop = %d, want 0", depth)
	}
}

func TestEmbedQueueRepo_DropIsNoOpWhenLeaseMovedOn(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if enqErr := repo.Enqueue("n1"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	if _, drainErr := repo.DrainEmbed(testWorkerA, 10, testTTL); drainErr != nil {
		test.Fatalf("Drain: %v", drainErr)
	}

	if dropErr := repo.Drop("n1", testWorkerB); dropErr != nil {
		test.Fatalf("Drop: %v", dropErr)
	}

	depth, _ := repo.Depth()

	if depth != 1 {
		test.Errorf("Depth after stale Drop = %d, want 1 (row preserved)", depth)
	}
}

func TestEmbedQueueRepo_DrainFiltersOutReindexKind(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewEmbedQueueRepo(store)

	// Insert a reindex row directly via SQL — there's no public helper
	// yet; Phase 6 will add EnqueueReindex.
	if _, execErr := store.DB().Exec(`
		INSERT INTO embed_queue (node_id, enqueued_at, attempts, kind)
		VALUES (?, ?, 0, 'reindex')
	`, "reindex-only", time.Now().UnixNano()); execErr != nil {
		test.Fatalf("insert reindex row: %v", execErr)
	}

	if enqErr := repo.Enqueue("embed-only"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	drained, drainErr := repo.DrainEmbed(testWorkerA, 10, testTTL)

	if drainErr != nil {
		test.Fatalf("Drain: %v", drainErr)
	}

	if len(drained) != 1 {
		test.Fatalf("len = %d, want 1 (reindex row must be filtered)", len(drained))
	}

	if drained[0].NodeID != "embed-only" {
		test.Errorf("NodeID = %q, want %q", drained[0].NodeID, "embed-only")
	}

	// Reindex row remains untouched.
	var (
		kind     string
		leasedBy *string
	)

	if scanErr := store.DB().QueryRow(`SELECT kind, leased_by FROM embed_queue WHERE node_id = ?`, "reindex-only").Scan(&kind, &leasedBy); scanErr != nil {
		test.Fatalf("inspect reindex row: %v", scanErr)
	}

	if kind != "reindex" {
		test.Errorf("kind = %q, want %q", kind, "reindex")
	}

	if leasedBy != nil {
		test.Errorf("LeasedBy on reindex row = %v, want nil (untouched)", leasedBy)
	}
}

func TestEmbedQueueRepo_EnqueueDefaultsKindToEmbedAndLeavesLeaseFieldsNil(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewEmbedQueueRepo(store)

	if enqErr := repo.Enqueue("n1"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	// Inspect the row directly (rather than via Drain, which would
	// overwrite the lease fields) to verify the table-default kind and
	// the initial nil lease columns.
	var (
		kind             string
		leasedBy         *string
		leasedUntilNs    *int64
		leaseStartedAtNs *int64
	)

	if scanErr := store.DB().QueryRow(`
		SELECT kind, leased_by, leased_until_ns, lease_started_at_ns
		FROM embed_queue WHERE node_id = ?
	`, "n1").Scan(&kind, &leasedBy, &leasedUntilNs, &leaseStartedAtNs); scanErr != nil {
		test.Fatalf("inspect row: %v", scanErr)
	}

	if kind != "embed" {
		test.Errorf("Kind = %q, want %q", kind, "embed")
	}

	if leasedBy != nil {
		test.Errorf("LeasedBy = %v, want nil", leasedBy)
	}

	if leasedUntilNs != nil {
		test.Errorf("LeasedUntilNs = %v, want nil", leasedUntilNs)
	}

	if leaseStartedAtNs != nil {
		test.Errorf("LeaseStartedAtNs = %v, want nil", leaseStartedAtNs)
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

func TestEmbedQueueRepo_EnqueueReindexInsertsPrefixedRow(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewEmbedQueueRepo(store)

	if enqErr := repo.EnqueueReindex("notes/a.md"); enqErr != nil {
		test.Fatalf("EnqueueReindex: %v", enqErr)
	}

	var (
		nodeID string
		kind   string
	)

	if scanErr := store.DB().QueryRow(`SELECT node_id, kind FROM embed_queue`).Scan(&nodeID, &kind); scanErr != nil {
		test.Fatalf("scan row: %v", scanErr)
	}

	if nodeID != "reindex:notes/a.md" {
		test.Errorf("node_id = %q, want %q", nodeID, "reindex:notes/a.md")
	}

	if kind != "reindex" {
		test.Errorf("kind = %q, want %q", kind, "reindex")
	}
}

func TestEmbedQueueRepo_EnqueueReindexIsIdempotent(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	for idx := 0; idx < 3; idx++ {
		if enqErr := repo.EnqueueReindex("notes/dup.md"); enqErr != nil {
			test.Fatalf("EnqueueReindex %d: %v", idx, enqErr)
		}
	}

	depth, _ := repo.Depth()

	if depth != 1 {
		test.Errorf("Depth = %d, want 1 after idempotent EnqueueReindex", depth)
	}
}

func TestEmbedQueueRepo_EnqueueReindexRejectsPrefixedPath(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if enqErr := repo.EnqueueReindex("reindex:already/prefixed.md"); enqErr == nil {
		test.Fatalf("EnqueueReindex on prefixed path: want error, got nil")
	}

	depth, _ := repo.Depth()

	if depth != 0 {
		test.Errorf("Depth = %d, want 0 (no row written on reject)", depth)
	}
}

func TestEmbedQueueRepo_DepthByKindSeparatesEmbedAndReindex(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if enqErr := repo.Enqueue("a"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	if enqErr := repo.Enqueue("b"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	if enqErr := repo.EnqueueReindex("notes/x.md"); enqErr != nil {
		test.Fatalf("EnqueueReindex: %v", enqErr)
	}

	embedDepth, embedErr := repo.DepthByKind("embed")

	if embedErr != nil {
		test.Fatalf("DepthByKind embed: %v", embedErr)
	}

	if embedDepth != 2 {
		test.Errorf("DepthByKind(embed) = %d, want 2", embedDepth)
	}

	reindexDepth, reindexErr := repo.DepthByKind("reindex")

	if reindexErr != nil {
		test.Fatalf("DepthByKind reindex: %v", reindexErr)
	}

	if reindexDepth != 1 {
		test.Errorf("DepthByKind(reindex) = %d, want 1", reindexDepth)
	}
}

func TestEmbedQueueRepo_DrainEmbedIgnoresReindexRows(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if enqErr := repo.EnqueueReindex("notes/a.md"); enqErr != nil {
		test.Fatalf("EnqueueReindex: %v", enqErr)
	}

	if enqErr := repo.Enqueue("embed-node"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	drained, drainErr := repo.DrainEmbed(testWorkerA, 10, testTTL)

	if drainErr != nil {
		test.Fatalf("DrainEmbed: %v", drainErr)
	}

	if len(drained) != 1 {
		test.Fatalf("len = %d, want 1 (reindex row must not drain)", len(drained))
	}

	if drained[0].NodeID != "embed-node" {
		test.Errorf("NodeID = %q, want %q", drained[0].NodeID, "embed-node")
	}
}

func TestEmbedQueueRepo_DrainReindexReturnsOnlyReindexRows(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewEmbedQueueRepo(store)

	if enqErr := repo.EnqueueReindex("notes/a.md"); enqErr != nil {
		test.Fatalf("EnqueueReindex: %v", enqErr)
	}

	if enqErr := repo.EnqueueReindex("notes/b.md"); enqErr != nil {
		test.Fatalf("EnqueueReindex: %v", enqErr)
	}

	if enqErr := repo.Enqueue("embed-only"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	drained, drainErr := repo.DrainReindex(testWorkerA, 10, testTTL)

	if drainErr != nil {
		test.Fatalf("DrainReindex: %v", drainErr)
	}

	if len(drained) != 2 {
		test.Fatalf("len = %d, want 2 (only reindex rows)", len(drained))
	}

	for _, row := range drained {
		if row.Kind != "reindex" {
			test.Errorf("Kind = %q, want %q", row.Kind, "reindex")
		}

		if !strings.HasPrefix(row.NodeID, index.ReindexNodeIDPrefix) {
			test.Errorf("NodeID = %q, want prefix %q", row.NodeID, index.ReindexNodeIDPrefix)
		}

		if row.LeasedBy == nil || *row.LeasedBy != testWorkerA {
			test.Errorf("LeasedBy = %v, want %q", row.LeasedBy, testWorkerA)
		}
	}

	// The embed-only row stays unleased and undrained.
	var (
		kind     string
		leasedBy *string
	)

	if scanErr := store.DB().QueryRow(`SELECT kind, leased_by FROM embed_queue WHERE node_id = ?`, "embed-only").Scan(&kind, &leasedBy); scanErr != nil {
		test.Fatalf("inspect embed row: %v", scanErr)
	}

	if kind != "embed" {
		test.Errorf("kind = %q, want %q", kind, "embed")
	}

	if leasedBy != nil {
		test.Errorf("LeasedBy on embed row = %v, want nil (untouched)", leasedBy)
	}
}

func TestEmbedQueueRepo_DrainReindexLeasesAndReclaimsExpired(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if enqErr := repo.EnqueueReindex("notes/only.md"); enqErr != nil {
		test.Fatalf("EnqueueReindex: %v", enqErr)
	}

	// Negative TTL forces leased_until_ns into the past.
	firstBatch, drainErr := repo.DrainReindex(testWorkerA, 10, -1*time.Second)

	if drainErr != nil {
		test.Fatalf("first DrainReindex: %v", drainErr)
	}

	if len(firstBatch) != 1 {
		test.Fatalf("first batch = %d, want 1", len(firstBatch))
	}

	secondBatch, drainErr := repo.DrainReindex(testWorkerB, 10, testTTL)

	if drainErr != nil {
		test.Fatalf("second DrainReindex: %v", drainErr)
	}

	if len(secondBatch) != 1 {
		test.Fatalf("second batch = %d, want 1 (expired lease reclaimable)", len(secondBatch))
	}

	if secondBatch[0].LeasedBy == nil || *secondBatch[0].LeasedBy != testWorkerB {
		test.Errorf("LeasedBy = %v, want %q", secondBatch[0].LeasedBy, testWorkerB)
	}
}

type boomErr struct{}

func (boomErr) Error() string { return "boom" }

func errBoom() error { return boomErr{} }
