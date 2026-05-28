package reindex_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/reindex"
)

// TestDrainReindexQueue_ProcessesWalkerEnqueuedJobs runs the walker in async
// mode so the reindex queue stays populated, then drains it manually and
// asserts that the worker reproduces the node + file_state rows the legacy
// in-walker pass produced.
func TestDrainReindexQueue_ProcessesWalkerEnqueuedJobs(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "notes/a.md", "type: note\ntitle: A\n", "Body.\n")
	writeNode(test, root, "notes/b.md", "type: note\ntitle: B\n", "Body.\n")
	writeNode(test, root, "tickets/c.md", "type: ticket\ntitle: C\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	fileStates := index.NewFileStateRepo(store)
	meta := index.NewMetaRepo(store)

	report, runErr := reindex.Run(reindex.Config{
		Root:       root,
		Repo:       repo,
		EmbedQueue: queueRepo,
		Meta:       meta,
		FileStates: fileStates,
		Async:      true,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.Generation != 1 {
		test.Errorf("Generation = %d, want 1", report.Generation)
	}

	// Async mode: walker enqueues only; no node rows yet.
	if loaded, _ := repo.List(index.ListFilter{}); len(loaded) != 0 {
		test.Fatalf("nodes after async walk = %d, want 0", len(loaded))
	}

	depth, _ := queueRepo.DepthByKind("reindex")

	if depth != 3 {
		test.Fatalf("reindex depth = %d, want 3", depth)
	}

	drainReport, drainErr := reindex.DrainReindexQueue(context.Background(), reindex.WorkerConfig{
		Root:       root,
		Repo:       repo,
		EmbedQueue: queueRepo,
		FileStates: fileStates,
		Workers:    2,
		TTL:        time.Minute,
		Generation: report.Generation,
	})

	if drainErr != nil {
		test.Fatalf("DrainReindexQueue: %v", drainErr)
	}

	if drainReport.Indexed != 3 {
		test.Errorf("DrainReport.Indexed = %d, want 3", drainReport.Indexed)
	}

	loaded, _ := repo.List(index.ListFilter{})

	if len(loaded) != 3 {
		test.Errorf("node rows after drain = %d, want 3", len(loaded))
	}

	for _, path := range []string{"notes/a.md", "notes/b.md", "tickets/c.md"} {
		row, getErr := fileStates.Get(path)

		if getErr != nil {
			test.Fatalf("FileStates.Get(%q): %v", path, getErr)
		}

		if row.LastSeenGen != report.Generation {
			test.Errorf("%s LastSeenGen = %d, want %d", path, row.LastSeenGen, report.Generation)
		}
	}

	remaining, _ := queueRepo.DepthByKind("reindex")

	if remaining != 0 {
		test.Errorf("reindex queue depth after drain = %d, want 0", remaining)
	}

	// Each processed file enqueues an embed job for the file-level row.
	embedDepth, _ := queueRepo.DepthByKind("embed")

	if embedDepth != 3 {
		test.Errorf("embed queue depth after drain = %d, want 3", embedDepth)
	}
}

// TestDrainReindexQueue_WorkersZeroReturnsEmptyReportWithoutClaiming asserts
// the T7.1 opt-out semantic: Workers=0 means the worker pool never starts and
// the queue is left intact for some other instance to drain.
func TestDrainReindexQueue_WorkersZeroReturnsEmptyReportWithoutClaiming(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "notes/a.md", "type: note\ntitle: A\n", "Body.\n")
	writeNode(test, root, "notes/b.md", "type: note\ntitle: B\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	fileStates := index.NewFileStateRepo(store)
	meta := index.NewMetaRepo(store)

	report, runErr := reindex.Run(reindex.Config{
		Root:       root,
		Repo:       repo,
		EmbedQueue: queueRepo,
		Meta:       meta,
		FileStates: fileStates,
		Async:      true,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	depth, _ := queueRepo.DepthByKind("reindex")

	if depth != 2 {
		test.Fatalf("reindex depth after walk = %d, want 2", depth)
	}

	drainReport, drainErr := reindex.DrainReindexQueue(context.Background(), reindex.WorkerConfig{
		Root:       root,
		Repo:       repo,
		EmbedQueue: queueRepo,
		FileStates: fileStates,
		Workers:    0,
		TTL:        time.Minute,
		Generation: report.Generation,
	})

	if drainErr != nil {
		test.Fatalf("DrainReindexQueue: %v", drainErr)
	}

	if drainReport.Indexed != 0 {
		test.Errorf("Indexed = %d, want 0 (workers=0 must not claim)", drainReport.Indexed)
	}

	remaining, _ := queueRepo.DepthByKind("reindex")

	if remaining != 2 {
		test.Errorf("reindex depth after opt-out drain = %d, want 2 (queue must be retained)", remaining)
	}

	if loaded, _ := repo.List(index.ListFilter{}); len(loaded) != 0 {
		test.Errorf("node rows after opt-out drain = %d, want 0", len(loaded))
	}
}

// TestRun_WorkersZeroSyncSkipsDrain asserts that reindex.Run with sync mode
// (Async=false) and Workers=0 walks and enqueues but does not drain, leaving
// the caller responsible for orchestrating drainage.
func TestRun_WorkersZeroSyncSkipsDrain(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "notes/a.md", "type: note\ntitle: A\n", "Body.\n")
	writeNode(test, root, "notes/b.md", "type: note\ntitle: B\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	fileStates := index.NewFileStateRepo(store)
	meta := index.NewMetaRepo(store)

	report, runErr := reindex.Run(reindex.Config{
		Root:       root,
		Repo:       repo,
		EmbedQueue: queueRepo,
		Meta:       meta,
		FileStates: fileStates,
		Workers:    0,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.Indexed != 0 {
		test.Errorf("Indexed = %d, want 0 (sync drain must be skipped)", report.Indexed)
	}

	depth, _ := queueRepo.DepthByKind("reindex")

	if depth != 2 {
		test.Errorf("reindex depth after Run = %d, want 2 (queue retained for drainage elsewhere)", depth)
	}

	if loaded, _ := repo.List(index.ListFilter{}); len(loaded) != 0 {
		test.Errorf("node rows after Run = %d, want 0", len(loaded))
	}
}

// TestDrainReindexQueue_ContextCancelLeavesRowLeased asserts that cancelling
// ctx mid-drain returns promptly without acking the in-flight row; the row
// stays leased until ttl expires, at which point another worker can reclaim.
func TestDrainReindexQueue_ContextCancelLeavesRowLeased(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "notes/a.md", "type: note\ntitle: A\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	queueRepo := index.NewEmbedQueueRepo(store)

	if enqErr := queueRepo.EnqueueReindex("notes/a.md"); enqErr != nil {
		test.Fatalf("EnqueueReindex: %v", enqErr)
	}

	// Cancel context immediately so the worker returns without processing.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, drainErr := reindex.DrainReindexQueue(ctx, reindex.WorkerConfig{
		Root:       root,
		Repo:       index.NewNodeRepo(store),
		EmbedQueue: queueRepo,
		FileStates: index.NewFileStateRepo(store),
		Workers:    1,
		TTL:        -1 * time.Second, // force lease into the past so the next drain reclaims
		Generation: 1,
	})

	if drainErr != nil {
		test.Fatalf("DrainReindexQueue: %v", drainErr)
	}

	// Row may or may not have been leased depending on which select fired first.
	// What we care about is that a second drain can still see/reclaim it.
	depth, _ := queueRepo.DepthByKind("reindex")

	if depth != 1 {
		test.Fatalf("reindex depth after cancelled drain = %d, want 1 (row preserved)", depth)
	}

	drainReport, drainErr := reindex.DrainReindexQueue(context.Background(), reindex.WorkerConfig{
		Root:       root,
		Repo:       index.NewNodeRepo(store),
		EmbedQueue: queueRepo,
		FileStates: index.NewFileStateRepo(store),
		Workers:    1,
		TTL:        time.Minute,
		Generation: 1,
	})

	if drainErr != nil {
		test.Fatalf("second DrainReindexQueue: %v", drainErr)
	}

	if drainReport.Indexed != 1 {
		test.Errorf("Indexed = %d, want 1 (row reclaimed after cancel)", drainReport.Indexed)
	}
}

// TestDrainReindexQueue_TwoRepoInstancesDrainSameQueueWithoutDuplication
// simulates two MCP processes pointing at the same DB. Each builds its own
// EmbedQueueRepo and races to drain the populated reindex queue; total node
// row count must equal the file count.
func TestDrainReindexQueue_TwoRepoInstancesDrainSameQueueWithoutDuplication(test *testing.T) {
	root := test.TempDir()

	const fileCount = 20

	for i := 0; i < fileCount; i++ {
		writeNode(test, root, filepath.Join("notes", "f"+itoa(i)+".md"), "type: note\ntitle: x\n", "body\n")
	}

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	primary := index.NewEmbedQueueRepo(store)
	meta := index.NewMetaRepo(store)
	fileStates := index.NewFileStateRepo(store)

	report, runErr := reindex.Run(reindex.Config{
		Root:       root,
		Repo:       repo,
		EmbedQueue: primary,
		Meta:       meta,
		FileStates: fileStates,
		Async:      true,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	// Two repo instances, same DB.
	secondary := index.NewEmbedQueueRepo(store)

	var wg sync.WaitGroup

	wg.Add(2)

	results := make([]int, 2)

	for idx, queue := range []*index.EmbedQueueRepo{primary, secondary} {
		go func() {
			defer wg.Done()

			drainReport, drainErr := reindex.DrainReindexQueue(context.Background(), reindex.WorkerConfig{
				Root:       root,
				Repo:       repo,
				EmbedQueue: queue,
				FileStates: fileStates,
				Workers:    2,
				TTL:        time.Minute,
				Generation: report.Generation,
			})

			if drainErr != nil {
				test.Errorf("DrainReindexQueue: %v", drainErr)

				return
			}

			results[idx] = drainReport.Indexed
		}()
	}

	wg.Wait()

	totalIndexed := results[0] + results[1]

	if totalIndexed != fileCount {
		test.Errorf("total Indexed across two instances = %d, want %d", totalIndexed, fileCount)
	}

	loaded, _ := repo.List(index.ListFilter{})

	if len(loaded) != fileCount {
		test.Errorf("node rows = %d, want %d", len(loaded), fileCount)
	}

	remaining, _ := primary.DepthByKind("reindex")

	if remaining != 0 {
		test.Errorf("reindex depth after parallel drain = %d, want 0", remaining)
	}
}

// itoa is a tiny strconv.Itoa wrapper to keep imports trim in this test file.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}

	var digits []byte

	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}

	return string(digits)
}
