package node_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func TestService_ModifyNoOpDoesNotTouchMtimeOrQueue(test *testing.T) {
	root := test.TempDir()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	fileState := index.NewFileStateRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)

	service := node.NewServiceWithLease(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		queueRepo,
		fileState,
		"test-worker",
		time.Minute,
	)

	if _, createErr := service.Create(node.CreateInput{
		RelPath:    "tickets/noop.md",
		Type:       "ticket",
		Title:      "No-op",
		Properties: map[string]any{"status": "open"},
		Body:       []byte("body\n"),
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	absPath := filepath.Join(root, "tickets/noop.md")

	statBefore, statErr := os.Stat(absPath)

	if statErr != nil {
		test.Fatalf("stat before: %v", statErr)
	}

	rowBefore, getErr := fileState.Get("tickets/noop.md")

	if getErr != nil {
		test.Fatalf("file_state Get before: %v", getErr)
	}

	// Drain+Ack the embed_queue row Create enqueued so the no-op test
	// below starts from an empty queue.
	drained, drainErr := queueRepo.DrainEmbed("test-worker", 16, time.Minute)

	if drainErr != nil {
		test.Fatalf("drain seed: %v", drainErr)
	}

	for _, row := range drained {
		if ackErr := queueRepo.Ack(row.NodeID, "test-worker"); ackErr != nil {
			test.Fatalf("ack seed: %v", ackErr)
		}
	}

	if pending, _ := queueRepo.ListNodeIDs(); len(pending) != 0 {
		test.Fatalf("embed_queue not empty after drain+ack: %v", pending)
	}

	// Sleep briefly so a real mtime change would be observable.
	time.Sleep(10 * time.Millisecond)

	// Modify with the same value the property already has — pure no-op.
	if _, modifyErr := service.Modify(node.ModifyInput{
		ID:       "tickets/noop",
		SetProps: map[string]any{"status": "open"},
	}); modifyErr != nil {
		test.Fatalf("Modify (no-op): %v", modifyErr)
	}

	statAfter, statErr := os.Stat(absPath)

	if statErr != nil {
		test.Fatalf("stat after: %v", statErr)
	}

	if !statAfter.ModTime().Equal(statBefore.ModTime()) {
		test.Errorf("mtime advanced on no-op: before=%v after=%v", statBefore.ModTime(), statAfter.ModTime())
	}

	if pending, _ := queueRepo.ListNodeIDs(); len(pending) != 0 {
		test.Errorf("embed_queue grew on no-op Modify: %v", pending)
	}

	rowAfter, getErr := fileState.Get("tickets/noop.md")

	if getErr != nil {
		test.Fatalf("file_state Get after: %v", getErr)
	}

	if rowAfter.ContentHash != rowBefore.ContentHash {
		test.Errorf("content_hash changed on no-op: before=%q after=%q", rowBefore.ContentHash, rowAfter.ContentHash)
	}

	if rowAfter.MtimeNs != rowBefore.MtimeNs {
		test.Errorf("file_state mtime changed on no-op: before=%d after=%d", rowBefore.MtimeNs, rowAfter.MtimeNs)
	}

	if rowAfter.LeasedBy.Valid {
		test.Errorf("lease still held after no-op: %+v", rowAfter.LeasedBy)
	}
}

func TestService_ModifyConcurrentSameNodeBothPropertiesLand(test *testing.T) {
	// Two workers Modify the same node concurrently, each setting a
	// different property. With lease serialization both writes land: one
	// commits first, the second re-reads the post-first state inside its
	// own lease claim and merges its property on top.
	root := test.TempDir()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	fileState := index.NewFileStateRepo(store)
	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)

	makeSvc := func(workerID string) *node.Service {
		return node.NewServiceWithLease(
			root, nodeRepo, edgeRepo, manifest.EdgeTypes{}, queueRepo,
			fileState, workerID, time.Minute,
		)
	}

	seed := makeSvc("seed")

	if _, createErr := seed.Create(node.CreateInput{
		RelPath: "tickets/race.md",
		Type:    "ticket",
		Title:   "Race",
	}); createErr != nil {
		test.Fatalf("seed Create: %v", createErr)
	}

	svcA := makeSvc("worker-A")
	svcB := makeSvc("worker-B")

	const attempts = 8

	for attempt := 0; attempt < attempts; attempt++ {
		var wg sync.WaitGroup
		errs := make([]error, 2)

		wg.Add(2)

		go func() {
			defer wg.Done()
			_, errs[0] = svcA.Modify(node.ModifyInput{
				ID:       "tickets/race",
				SetProps: map[string]any{"prop_a": "a"},
			})
		}()

		go func() {
			defer wg.Done()
			_, errs[1] = svcB.Modify(node.ModifyInput{
				ID:       "tickets/race",
				SetProps: map[string]any{"prop_b": "b"},
			})
		}()

		wg.Wait()

		// If either worker lost the Claim (ErrBusy), retry — the lease TTL
		// is long enough that this should not happen under test load, but
		// we accept the retry rather than racing the test.
		if errs[0] != nil || errs[1] != nil {
			continue
		}

		loaded, getErr := svcA.Get("tickets/race")

		if getErr != nil {
			test.Fatalf("Get after race: %v", getErr)
		}

		gotA, _ := loaded.Properties["prop_a"].(string)
		gotB, _ := loaded.Properties["prop_b"].(string)

		if gotA != "a" || gotB != "b" {
			test.Fatalf("attempt %d: properties not both landed: prop_a=%q prop_b=%q (props=%v)",
				attempt, gotA, gotB, loaded.Properties)
		}

		row, fsGetErr := fileState.Get("tickets/race.md")

		if fsGetErr != nil {
			test.Fatalf("file_state Get after race: %v", fsGetErr)
		}

		if row.State != index.FileStateLive {
			test.Errorf("state after race = %q, want %q", row.State, index.FileStateLive)
		}

		if row.LeasedBy.Valid {
			test.Errorf("lease still held after race: %+v", row.LeasedBy)
		}

		return
	}

	test.Fatalf("concurrent Modify never completed without ErrBusy after %d attempts", attempts)
}

func TestService_ModifyVanishedFileErrors(test *testing.T) {
	root := test.TempDir()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	service := node.NewServiceWithLease(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		index.NewFileStateRepo(store),
		"test-worker",
		time.Minute,
	)

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "tickets/gone.md",
		Type:    "ticket",
		Title:   "Gone",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	// Pull the file out from under the service. The index row stays, so
	// Modify finds the row, claims the lease, then the Mutator sees a
	// nil current and must reject.
	if rmErr := os.Remove(filepath.Join(root, "tickets/gone.md")); rmErr != nil {
		test.Fatalf("remove: %v", rmErr)
	}

	_, modifyErr := service.Modify(node.ModifyInput{
		ID:       "tickets/gone",
		SetProps: map[string]any{"status": "open"},
	})

	if modifyErr == nil {
		test.Fatalf("Modify vanished file: expected error, got nil")
	}

	if !strings.Contains(modifyErr.Error(), "vanished") {
		test.Errorf("Modify vanished file: error = %v, want 'vanished' in message", modifyErr)
	}
}
