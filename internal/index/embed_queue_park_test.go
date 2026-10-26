package index_test

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// parkHeldRow enqueues nodeID, claims it as testWorkerA, and parks it with
// errBoom, failing the test on any error.
func parkHeldRow(test *testing.T, repo *index.EmbedQueueRepo, nodeID string) {
	test.Helper()

	if enqErr := repo.Enqueue(nodeID); enqErr != nil {
		test.Fatalf("Enqueue %s: %v", nodeID, enqErr)
	}

	if _, drainErr := repo.DrainEmbed(testWorkerA, 10, testTTL); drainErr != nil {
		test.Fatalf("DrainEmbed: %v", drainErr)
	}

	if parkErr := repo.Park(nodeID, testWorkerA, errBoom()); parkErr != nil {
		test.Fatalf("Park %s: %v", nodeID, parkErr)
	}
}

// TestEmbedQueueRepo_ParkStopsRetries: a parked row is no longer claimed, no
// longer counts as pending, and is listed on its own with its attempt count
// and last error (#845).
func TestEmbedQueueRepo_ParkStopsRetries(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	parkHeldRow(test, repo, "stuck")

	if enqErr := repo.Enqueue("healthy"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	claimed, drainErr := repo.DrainEmbed(testWorkerB, 10, testTTL)

	if drainErr != nil {
		test.Fatalf("DrainEmbed: %v", drainErr)
	}

	if len(claimed) != 1 || claimed[0].NodeID != "healthy" {
		test.Errorf("claimed = %+v, want only the healthy row", claimed)
	}

	if depth, _ := repo.DepthByKind("embed"); depth != 1 {
		test.Errorf("DepthByKind(embed) = %d, want 1 (the parked row is not pending)", depth)
	}

	if depth, _ := repo.Depth(); depth != 1 {
		test.Errorf("Depth = %d, want 1 (the parked row is not pending)", depth)
	}

	if parked, _ := repo.CountParked(); parked != 1 {
		test.Errorf("CountParked = %d, want 1", parked)
	}

	rows, listErr := repo.ListParked()

	if listErr != nil {
		test.Fatalf("ListParked: %v", listErr)
	}

	if len(rows) != 1 || rows[0].NodeID != "stuck" || rows[0].Attempts != 1 || rows[0].LastError != "boom" {
		test.Errorf("ListParked = %+v, want stuck with attempts 1 and last error boom", rows)
	}

	if ids, _ := repo.ListNodeIDs(); !reflect.DeepEqual(ids, []string{"healthy"}) {
		test.Errorf("ListNodeIDs = %v, want only the pending row", ids)
	}

	if retrying, _ := repo.ListRetrying(); len(retrying) != 0 {
		test.Errorf("ListRetrying = %+v, want none (a parked row has stopped retrying)", retrying)
	}
}

// TestEmbedQueueRepo_ParkIsNoOpWhenLeaseMovedOn: like Nack, Park only touches
// a row the caller still holds.
func TestEmbedQueueRepo_ParkIsNoOpWhenLeaseMovedOn(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if enqErr := repo.Enqueue("n1"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	if _, drainErr := repo.DrainEmbed(testWorkerA, 10, testTTL); drainErr != nil {
		test.Fatalf("DrainEmbed: %v", drainErr)
	}

	if parkErr := repo.Park("n1", testWorkerB, errBoom()); parkErr != nil {
		test.Fatalf("Park: %v", parkErr)
	}

	if parked, _ := repo.CountParked(); parked != 0 {
		test.Errorf("CountParked after a stale Park = %d, want 0", parked)
	}
}

// TestEmbedQueueRepo_EnqueueRearmsParkedRow: re-enqueueing a parked node (it
// changed, or `reindex --force` re-queued it) gives it a fresh retry budget,
// while a row still retrying keeps its count.
func TestEmbedQueueRepo_EnqueueRearmsParkedRow(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	parkHeldRow(test, repo, "single")
	parkHeldRow(test, repo, "batch")

	if enqErr := repo.Enqueue("single"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	if enqErr := repo.EnqueueMany([]string{"batch"}); enqErr != nil {
		test.Fatalf("EnqueueMany: %v", enqErr)
	}

	if parked, _ := repo.CountParked(); parked != 0 {
		test.Errorf("CountParked after re-enqueue = %d, want 0", parked)
	}

	claimed, drainErr := repo.DrainEmbed(testWorkerB, 10, testTTL)

	if drainErr != nil {
		test.Fatalf("DrainEmbed: %v", drainErr)
	}

	if len(claimed) != 2 {
		test.Fatalf("claimed %d rows, want 2", len(claimed))
	}

	for _, row := range claimed {
		if row.Attempts != 0 || row.LastError != "" {
			test.Errorf("re-armed row %s = attempts %d, last error %q; want a fresh budget", row.NodeID, row.Attempts, row.LastError)
		}
	}

	if nackErr := repo.Nack("single", testWorkerB, errBoom()); nackErr != nil {
		test.Fatalf("Nack: %v", nackErr)
	}

	if enqErr := repo.Enqueue("single"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	retrying, _ := repo.ListRetrying()

	if len(retrying) != 1 || retrying[0].Attempts != 1 {
		test.Errorf("ListRetrying = %+v, want single still at attempts 1", retrying)
	}
}

// TestEmbedQueueRepo_EnqueueAllEmbeddableRearmsParked: an [embeddings]
// settings change re-arms parked rows along with everything else.
func TestEmbedQueueRepo_EnqueueAllEmbeddableRearmsParked(test *testing.T) {
	store := openTestIndex(test)
	nodes := index.NewNodeRepo(store)
	queue := index.NewEmbedQueueRepo(store)

	if upsertErr := nodes.Upsert(index.NodeRow{ID: "notes/a", Type: "note", Path: "notes/a.md", Title: "A", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert file: %v", upsertErr)
	}

	parkHeldRow(test, queue, "notes/a")

	if _, enqErr := queue.EnqueueAllEmbeddable(); enqErr != nil {
		test.Fatalf("EnqueueAllEmbeddable: %v", enqErr)
	}

	if parked, _ := queue.CountParked(); parked != 0 {
		test.Errorf("CountParked = %d, want 0", parked)
	}

	if depth, _ := queue.DepthByKind("embed"); depth != 1 {
		test.Errorf("DepthByKind(embed) = %d, want 1", depth)
	}
}

// TestNodeRepo_DeleteDropsEmbedJobs: deleting a node deletes its embed job,
// whichever path deletes it (#845 finding 12), and leaves reindex jobs alone.
func TestNodeRepo_DeleteDropsEmbedJobs(test *testing.T) {
	store := openTestIndex(test)
	nodes := index.NewNodeRepo(store)
	queue := index.NewEmbedQueueRepo(store)

	parent := sql.NullString{String: "notes/a", Valid: true}

	if upsertErr := nodes.Upsert(index.NodeRow{ID: "notes/a", Type: "note", Path: "notes/a.md", Title: "A", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert file: %v", upsertErr)
	}

	if upsertErr := nodes.Upsert(index.NodeRow{ID: "notes/b", Type: "note", Path: "notes/b.md", Title: "B", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert file: %v", upsertErr)
	}

	if bulkErr := nodes.BulkUpsert([]index.NodeRow{
		{ID: "notes/a#S1P1", Type: "paragraph", Path: "notes/a.md", PropertiesJSON: "{}", LastChecksum: "x", ParentID: parent, Ordinal: sql.NullInt64{Int64: 0, Valid: true}, EmbedPayload: sql.NullString{String: "one", Valid: true}},
		{ID: "notes/a#S1P2", Type: "paragraph", Path: "notes/a.md", PropertiesJSON: "{}", LastChecksum: "x", ParentID: parent, Ordinal: sql.NullInt64{Int64: 1, Valid: true}, EmbedPayload: sql.NullString{String: "two", Valid: true}},
	}, "markdown"); bulkErr != nil {
		test.Fatalf("upsert sub-units: %v", bulkErr)
	}

	if enqErr := queue.EnqueueMany([]string{"notes/a", "notes/a#S1P1", "notes/a#S1P2", "notes/b"}); enqErr != nil {
		test.Fatalf("EnqueueMany: %v", enqErr)
	}

	if enqErr := queue.EnqueueReindex("notes/a.md"); enqErr != nil {
		test.Fatalf("EnqueueReindex: %v", enqErr)
	}

	if deleteErr := nodes.BulkDelete([]string{"notes/a#S1P2"}); deleteErr != nil {
		test.Fatalf("BulkDelete: %v", deleteErr)
	}

	assertQueued(test, store, []string{"notes/a", "notes/a#S1P1", "notes/b", "reindex:notes/a.md"})

	if deleteErr := nodes.DeleteByPath("notes/a.md"); deleteErr != nil {
		test.Fatalf("DeleteByPath: %v", deleteErr)
	}

	assertQueued(test, store, []string{"notes/b", "reindex:notes/a.md"})
}

// TestEmbedQueueRepo_DeleteOrphans: rows left behind for nodes that no longer
// exist (leaked before deletes cleaned up after themselves) are swept; live
// embed jobs and reindex jobs stay.
func TestEmbedQueueRepo_DeleteOrphans(test *testing.T) {
	store := openTestIndex(test)
	nodes := index.NewNodeRepo(store)
	queue := index.NewEmbedQueueRepo(store)

	if upsertErr := nodes.Upsert(index.NodeRow{ID: "keep", Type: "note", Path: "keep.md", Title: "K", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	if enqErr := queue.EnqueueMany([]string{"keep", "gone", "gone#S1P1"}); enqErr != nil {
		test.Fatalf("EnqueueMany: %v", enqErr)
	}

	if enqErr := queue.EnqueueReindex("gone.md"); enqErr != nil {
		test.Fatalf("EnqueueReindex: %v", enqErr)
	}

	removed, sweepErr := queue.DeleteOrphans()

	if sweepErr != nil {
		test.Fatalf("DeleteOrphans: %v", sweepErr)
	}

	if removed != 2 {
		test.Errorf("DeleteOrphans removed %d rows, want 2", removed)
	}

	assertQueued(test, store, []string{"keep", "reindex:gone.md"})
}

// TestOpen_AddsEmbedQueueParkingToLegacyIndex: an index written before
// parking and the delete trigger existed gains both in place on open, with
// its queue rows intact, so upgrading costs no rebuild.
func TestOpen_AddsEmbedQueueParkingToLegacyIndex(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	first, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	seedNodes(test, first, "notes/legacy")

	if enqErr := index.NewEmbedQueueRepo(first).Enqueue("notes/legacy"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	if _, execErr := first.DB().Exec(`
		DROP TRIGGER nodes_drop_embed_job;
		ALTER TABLE embed_queue DROP COLUMN parked_at;
	`); execErr != nil {
		test.Fatalf("strip parking: %v", execErr)
	}

	first.Close()

	for attempt := range 2 {
		reopened, reopenErr := index.Open(dbPath)

		if reopenErr != nil {
			test.Fatalf("reopen %d: %v", attempt, reopenErr)
		}

		queue := index.NewEmbedQueueRepo(reopened)

		if depth, _ := queue.DepthByKind("embed"); depth != 1 {
			test.Errorf("reopen %d: embed depth = %d, want the legacy row", attempt, depth)
		}

		if parked, countErr := queue.CountParked(); countErr != nil || parked != 0 {
			test.Errorf("reopen %d: CountParked = %d, %v; want 0, nil", attempt, parked, countErr)
		}

		reopened.Close()
	}

	reopened, reopenErr := index.Open(dbPath)

	if reopenErr != nil {
		test.Fatalf("reopen: %v", reopenErr)
	}

	defer reopened.Close()

	if deleteErr := index.NewNodeRepo(reopened).DeleteByPath("notes/legacy.md"); deleteErr != nil {
		test.Fatalf("DeleteByPath: %v", deleteErr)
	}

	assertQueued(test, reopened, nil)
}

// assertQueued fails the test unless the embed_queue holds exactly want
// (node ids of every kind, in any order).
func assertQueued(test *testing.T, store *index.Index, want []string) {
	test.Helper()

	rows, queryErr := store.DB().Query(`SELECT node_id FROM embed_queue`)

	if queryErr != nil {
		test.Fatalf("list queue: %v", queryErr)
	}

	defer rows.Close()

	var got []string

	for rows.Next() {
		var id string

		if scanErr := rows.Scan(&id); scanErr != nil {
			test.Fatalf("scan queue: %v", scanErr)
		}

		got = append(got, id)
	}

	sort.Strings(got)
	sort.Strings(want)

	if len(got) == 0 && len(want) == 0 {
		return
	}

	if !reflect.DeepEqual(got, want) {
		test.Errorf("embed_queue = %v, want %v", got, want)
	}
}
