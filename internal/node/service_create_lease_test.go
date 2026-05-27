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

// newLeaseTestService returns a Service plus the underlying FileStateRepo
// and store so the test can inspect file_state rows after operations.
func newLeaseTestService(test *testing.T, workerID string) (*node.Service, *index.FileStateRepo, *index.Index, string) {
	test.Helper()

	root := test.TempDir()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	fileState := index.NewFileStateRepo(store)

	service := node.NewServiceWithLease(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		fileState,
		workerID,
		time.Minute,
	)

	return service, fileState, store, root
}

func TestService_CreatePopulatesFileStateRow(test *testing.T) {
	service, fileState, _, root := newLeaseTestService(test, "test-worker")

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "tickets/leased.md",
		Type:    "ticket",
		Title:   "Leased",
		Body:    []byte("hello\n"),
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	row, getErr := fileState.Get("tickets/leased.md")

	if getErr != nil {
		test.Fatalf("file_state Get: %v", getErr)
	}

	onDisk := mustReadDisk(test, filepath.Join(root, "tickets/leased.md"))
	wantHash := sha256Hex(onDisk)

	if row.ContentHash != wantHash {
		test.Errorf("content_hash = %q, want %q", row.ContentHash, wantHash)
	}

	if row.State != index.FileStateLive {
		test.Errorf("state = %q, want %q", row.State, index.FileStateLive)
	}

	if row.LeasedBy.Valid || row.LeasedUntilNs.Valid {
		test.Errorf("lease columns not cleared: leasedBy=%+v leasedUntil=%+v", row.LeasedBy, row.LeasedUntilNs)
	}

	if row.PendingTempPath.Valid || row.PendingHash.Valid {
		test.Errorf("pending columns not cleared: tempPath=%+v hash=%+v", row.PendingTempPath, row.PendingHash)
	}
}

func TestService_CreateConcurrentSamePath(test *testing.T) {
	// Two workers race to Create the same path. Acceptable outcomes
	// per the T4.2 acceptance criteria: exactly one Create succeeds;
	// the other returns ErrAlreadyExists (raced through the Mutator
	// after the first commit landed) or index.ErrBusy (lost the
	// Claim while the first was holding the lease).
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

	svcA := makeSvc("worker-A")
	svcB := makeSvc("worker-B")

	input := node.CreateInput{
		RelPath: "tickets/race.md",
		Type:    "ticket",
		Title:   "Race",
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)

	wg.Add(2)

	go func() {
		defer wg.Done()
		_, errs[0] = svcA.Create(input)
	}()

	go func() {
		defer wg.Done()
		_, errs[1] = svcB.Create(input)
	}()

	wg.Wait()

	var (
		successCount  int
		acceptedFails int
	)

	for _, err := range errs {
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, node.ErrAlreadyExists):
			acceptedFails++
		case errors.Is(err, index.ErrBusy):
			acceptedFails++
		default:
			test.Errorf("unexpected error from concurrent Create: %v", err)
		}
	}

	if successCount != 1 {
		test.Errorf("concurrent Create successes = %d, want exactly 1 (errs=%v)", successCount, errs)
	}

	if acceptedFails != 1 {
		test.Errorf("concurrent Create accepted failures = %d, want exactly 1 (errs=%v)", acceptedFails, errs)
	}

	// File on disk and file_state row should both reflect the winner.
	row, getErr := fileState.Get("tickets/race.md")

	if getErr != nil {
		test.Fatalf("file_state Get after race: %v", getErr)
	}

	if row.State != index.FileStateLive {
		test.Errorf("state after race = %q, want %q", row.State, index.FileStateLive)
	}

	if row.LeasedBy.Valid {
		test.Errorf("lease still held after race: %+v", row.LeasedBy)
	}
}

func TestService_CreateOverTombstonedRowTransitionsBackToLive(test *testing.T) {
	service, fileState, _, _ := newLeaseTestService(test, "test-worker")

	relPath := "tickets/revived.md"

	// Pre-seed a tombstoned file_state row (as if the file had been
	// previously created and then soft-deleted via Tombstone).
	if upsertErr := fileState.Upsert(index.FileStateRow{
		Path:        relPath,
		ContentHash: "old-hash",
		MtimeNs:     1,
		Size:        1,
		State:       index.FileStateTombstone,
		LastSeenGen: 1,
	}); upsertErr != nil {
		test.Fatalf("seed tombstone row: %v", upsertErr)
	}

	if _, createErr := service.Create(node.CreateInput{
		RelPath: relPath,
		Type:    "ticket",
		Title:   "Revived",
	}); createErr != nil {
		test.Fatalf("Create after tombstone: %v", createErr)
	}

	row, getErr := fileState.Get(relPath)

	if getErr != nil {
		test.Fatalf("file_state Get: %v", getErr)
	}

	if row.State != index.FileStateLive {
		test.Errorf("state = %q, want %q (tombstone should transition back)", row.State, index.FileStateLive)
	}

	if row.ContentHash == "old-hash" {
		test.Errorf("content_hash still old: %q (should be updated to new file hash)", row.ContentHash)
	}
}

func mustReadDisk(test *testing.T, path string) []byte {
	test.Helper()

	content, readErr := os.ReadFile(path)

	if readErr != nil {
		test.Fatalf("read %s: %v", path, readErr)
	}

	return content
}
