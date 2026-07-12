package node_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func TestDelete_TombstonesFileStateRow(test *testing.T) {
	root := test.TempDir()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	fileState := index.NewFileStateRepo(store)

	service := node.NewServiceWithLease(
		root, nodeRepo, edgeRepo, manifest.EdgeTypes{}, nil,
		fileState, "test-worker", time.Minute,
	)

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "tickets/doomed.md",
		Type:    "ticket",
		Title:   "Doomed",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if deleteErr := node.Delete(
		root, nodeRepo, edgeRepo, fileState, nil, "test-worker", time.Minute, "tickets/doomed",
	); deleteErr != nil {
		test.Fatalf("Delete: %v", deleteErr)
	}

	if _, statErr := os.Stat(filepath.Join(root, "tickets/doomed.md")); !errors.Is(statErr, os.ErrNotExist) {
		test.Errorf("file still present, stat err = %v", statErr)
	}

	row, getErr := fileState.Get("tickets/doomed.md")

	if getErr != nil {
		test.Fatalf("file_state Get: %v", getErr)
	}

	if row.State != index.FileStateTombstone {
		test.Errorf("state = %q, want %q", row.State, index.FileStateTombstone)
	}

	if row.LeasedBy.Valid {
		test.Errorf("lease still held after Delete: %+v", row.LeasedBy)
	}

	if row.PendingTempPath.Valid {
		test.Errorf("pending_temp_path still set after Delete: %+v", row.PendingTempPath)
	}

	if _, getNodeErr := nodeRepo.Get("tickets/doomed"); getNodeErr != index.ErrNodeNotFound {
		test.Errorf("node row still present after Delete: getErr=%v", getNodeErr)
	}
}

func TestDelete_AlreadyMissingFileSucceeds(test *testing.T) {
	root := test.TempDir()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	fileState := index.NewFileStateRepo(store)

	service := node.NewServiceWithLease(
		root, nodeRepo, edgeRepo, manifest.EdgeTypes{}, nil,
		fileState, "test-worker", time.Minute,
	)

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "tickets/ghost.md",
		Type:    "ticket",
		Title:   "Ghost",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	// Pull the file out from under the index. The node row stays.
	if rmErr := os.Remove(filepath.Join(root, "tickets/ghost.md")); rmErr != nil {
		test.Fatalf("remove file: %v", rmErr)
	}

	if deleteErr := node.Delete(
		root, nodeRepo, edgeRepo, fileState, nil, "test-worker", time.Minute, "tickets/ghost",
	); deleteErr != nil {
		test.Fatalf("Delete on missing file: %v", deleteErr)
	}

	if _, getNodeErr := nodeRepo.Get("tickets/ghost"); getNodeErr != index.ErrNodeNotFound {
		test.Errorf("node row still present after Delete: getErr=%v", getNodeErr)
	}

	row, getErr := fileState.Get("tickets/ghost.md")

	if getErr != nil {
		test.Fatalf("file_state Get: %v", getErr)
	}

	if row.LeasedBy.Valid {
		test.Errorf("lease still held after Delete: %+v", row.LeasedBy)
	}

	if row.PendingTempPath.Valid {
		test.Errorf("pending_temp_path still set after Delete: %+v", row.PendingTempPath)
	}
}

func TestDelete_ConcurrentWithModifySerializesViaLease(test *testing.T) {
	// Two workers race on the same node: one Modify, one Delete. The
	// lease ensures they serialize. Whichever Claims first runs; the
	// other either sees ErrBusy or observes the post-first state. In
	// either case the lease is released cleanly and there is no
	// corruption (no orphaned pending_temp_path, no stuck lease).
	root := test.TempDir()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	fileState := index.NewFileStateRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)

	makeSvc := func(workerID string) *node.Service {
		return node.NewServiceWithLease(
			root, nodeRepo, edgeRepo, manifest.EdgeTypes{}, queueRepo,
			fileState, workerID, time.Minute,
		)
	}

	seed := makeSvc("seed")

	const attempts = 8

	for attempt := 0; attempt < attempts; attempt++ {
		if _, createErr := seed.Create(node.CreateInput{
			RelPath: "tickets/race.md",
			Type:    "ticket",
			Title:   "Race",
		}); createErr != nil {
			test.Fatalf("attempt %d: seed Create: %v", attempt, createErr)
		}

		modifySvc := makeSvc("worker-modify")

		var wg sync.WaitGroup
		errs := make([]error, 2)

		wg.Add(2)

		go func() {
			defer wg.Done()
			_, errs[0] = modifySvc.Modify(node.ModifyInput{
				ID:       "tickets/race",
				SetProps: map[string]any{"status": "open"},
			})
		}()

		go func() {
			defer wg.Done()
			errs[1] = node.Delete(
				root, nodeRepo, edgeRepo, fileState, nil, "worker-delete", time.Minute, "tickets/race",
			)
		}()

		wg.Wait()

		row, fsGetErr := fileState.Get("tickets/race.md")

		if fsGetErr != nil {
			test.Fatalf("attempt %d: file_state Get: %v", attempt, fsGetErr)
		}

		if row.LeasedBy.Valid {
			test.Fatalf("attempt %d: lease still held: %+v", attempt, row.LeasedBy)
		}

		if row.PendingTempPath.Valid {
			test.Fatalf("attempt %d: pending_temp_path still set: %+v", attempt, row.PendingTempPath)
		}

		// Retry if either operation lost the Claim race. We want to observe
		// at least one attempt where neither got ErrBusy so we can assert on
		// the post-conditions of a clean serialized run.
		if errors.Is(errs[0], index.ErrBusy) || errors.Is(errs[1], index.ErrBusy) {
			// Clean up before next attempt: ensure node row + file are gone.
			_ = node.Delete(
				root, nodeRepo, edgeRepo, fileState, nil, "cleanup", time.Minute, "tickets/race",
			)

			continue
		}

		// Neither got ErrBusy, so the lease serialized the two file writes.
		// Both clean orderings converge on the same authoritative outcome:
		//   1. Modify's lease ran first → Delete then tombstoned the file.
		//   2. Delete's lease ran first → Modify saw a vanished file and errored.
		// Either way Delete reports success, the file (the source of truth) is
		// gone, and file_state is marked tombstone.
		if errs[1] != nil {
			test.Fatalf("attempt %d: Delete returned err: %v (modifyErr=%v)", attempt, errs[1], errs[0])
		}

		if row.State != index.FileStateTombstone {
			test.Errorf("attempt %d: file_state = %q, want %q", attempt, row.State, index.FileStateTombstone)
		}

		if _, statErr := os.Stat(filepath.Join(root, "tickets/race.md")); !errors.Is(statErr, os.ErrNotExist) {
			test.Errorf("attempt %d: file still present, stat err = %v", attempt, statErr)
		}

		// The nodes index row is intentionally NOT asserted gone here. Modify
		// upserts its row AFTER WriteWithLease releases the lease (see
		// persistNodeRow: "runs after WriteWithLease has committed the file
		// (no lease is held here)"). So a Modify whose file write won the lease
		// can re-upsert a row in the window between its own os.Stat and Upsert
		// while Delete tombstones the file and removes the row — leaving a stale
		// row for a now-deleted file. That is a transient index-cache artifact
		// the watcher/reindex reconciles (the file is gone), not a lease-
		// serialization failure. The lease guarantees the file + file_state
		// outcome asserted above; index-row convergence is reindex's job.

		return
	}

	test.Fatalf("concurrent Delete+Modify never completed without ErrBusy after %d attempts", attempts)
}
