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

func TestRename_ConcurrentDifferentFilesBothSucceed(test *testing.T) {
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
		fileState, "seed", time.Minute,
	)

	for _, relPath := range []string{"notes/a.md", "notes/b.md"} {
		if _, createErr := service.Create(node.CreateInput{RelPath: relPath, Type: "note"}); createErr != nil {
			test.Fatalf("create %s: %v", relPath, createErr)
		}
	}

	var (
		wg   sync.WaitGroup
		errs [2]error
	)

	wg.Add(2)

	go func() {
		defer wg.Done()
		_, errs[0] = node.Rename(
			root, nodeRepo, edgeRepo, fileState, "worker-1", time.Minute,
			manifest.EdgeTypes{}, nil, nil, "notes/a", "notes/c.md",
		)
	}()

	go func() {
		defer wg.Done()
		_, errs[1] = node.Rename(
			root, nodeRepo, edgeRepo, fileState, "worker-2", time.Minute,
			manifest.EdgeTypes{}, nil, nil, "notes/b", "notes/d.md",
		)
	}()

	wg.Wait()

	if errs[0] != nil {
		test.Fatalf("Rename a→c: %v", errs[0])
	}

	if errs[1] != nil {
		test.Fatalf("Rename b→d: %v", errs[1])
	}

	for _, want := range []string{"notes/c.md", "notes/d.md"} {
		if _, statErr := os.Stat(filepath.Join(root, want)); statErr != nil {
			test.Errorf("expected %s on disk: %v", want, statErr)
		}

		row, getErr := fileState.Get(want)

		if getErr != nil {
			test.Fatalf("file_state Get %s: %v", want, getErr)
		}

		if row.State != index.FileStateLive {
			test.Errorf("%s state = %q, want %q", want, row.State, index.FileStateLive)
		}

		if row.LeasedBy.Valid {
			test.Errorf("%s lease still held: %+v", want, row.LeasedBy)
		}

		if row.ContentHash == "" {
			test.Errorf("%s content_hash empty after rename commit", want)
		}
	}

	for _, gone := range []string{"notes/a.md", "notes/b.md"} {
		if _, statErr := os.Stat(filepath.Join(root, gone)); !errors.Is(statErr, os.ErrNotExist) {
			test.Errorf("expected %s gone, stat err = %v", gone, statErr)
		}

		row, getErr := fileState.Get(gone)

		if getErr != nil {
			test.Fatalf("file_state Get %s: %v", gone, getErr)
		}

		if row.State != index.FileStateTombstone {
			test.Errorf("%s state = %q, want %q", gone, row.State, index.FileStateTombstone)
		}

		if row.LeasedBy.Valid {
			test.Errorf("%s lease still held: %+v", gone, row.LeasedBy)
		}
	}
}

func TestRename_ConcurrentSwapNamesSerializeWithoutDeadlock(test *testing.T) {
	// A→B and B→A both want leases on {A, B}. Without lexicographic
	// ordering this is the canonical two-lock deadlock: A→B claims its
	// source A first, B→A claims its source B first, each then waits on
	// the lease the other holds. With lex ordering both claim min(A,B)
	// then max(A,B); the second contender bounces off with ErrBusy
	// instead of hanging.
	//
	// Both files exist at the start so the in-lease target-exists check
	// fires for whichever caller wins both leases; the precise per-call
	// outcome (ErrBusy vs target-exists vs success after the other
	// side's tombstone) varies with timing, but neither call hangs.
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
		fileState, "seed", time.Minute,
	)

	for _, relPath := range []string{"swap/a.md", "swap/b.md"} {
		if _, createErr := service.Create(node.CreateInput{RelPath: relPath, Type: "note"}); createErr != nil {
			test.Fatalf("create %s: %v", relPath, createErr)
		}
	}

	var (
		wg   sync.WaitGroup
		errs [2]error
	)

	wg.Add(2)

	go func() {
		defer wg.Done()
		_, errs[0] = node.Rename(
			root, nodeRepo, edgeRepo, fileState, "worker-1", time.Minute,
			manifest.EdgeTypes{}, nil, nil, "swap/a", "swap/b.md",
		)
	}()

	go func() {
		defer wg.Done()
		_, errs[1] = node.Rename(
			root, nodeRepo, edgeRepo, fileState, "worker-2", time.Minute,
			manifest.EdgeTypes{}, nil, nil, "swap/b", "swap/a.md",
		)
	}()

	done := make(chan struct{})

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		test.Fatalf("concurrent swap rename hung — likely deadlock (errs=%v)", errs)
	}

	for _, path := range []string{"swap/a.md", "swap/b.md"} {
		row, getErr := fileState.Get(path)

		if getErr != nil {
			test.Fatalf("file_state Get %s: %v", path, getErr)
		}

		if row.LeasedBy.Valid {
			test.Errorf("%s lease still held: %+v", path, row.LeasedBy)
		}

		if row.PendingTempPath.Valid {
			test.Errorf("%s pending_temp_path still set: %+v", path, row.PendingTempPath)
		}
	}
}

func TestRename_SourceLeaseBusyReturnsErrBusyWithoutTakingDestLease(test *testing.T) {
	// Pre-Claim the source path under a different worker. Rename must
	// bounce off the source Claim with ErrBusy. Destination row may
	// have been inserted by EnsurePlaceholder, but its leased_by must
	// remain null.
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
		fileState, "seed", time.Minute,
	)

	// "notes/a.md" lex-sorts before "notes/z.md" so the source is the
	// first lease Rename tries to claim — proving the bounce happens on
	// the source without touching the destination's lease at all.
	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/a.md", Type: "note"}); createErr != nil {
		test.Fatalf("create source: %v", createErr)
	}

	if ensureErr := fileState.EnsurePlaceholder("notes/a.md"); ensureErr != nil {
		test.Fatalf("ensure source placeholder: %v", ensureErr)
	}

	if _, claimErr := fileState.Claim("notes/a.md", "other-worker", time.Minute); claimErr != nil {
		test.Fatalf("pre-claim source: %v", claimErr)
	}

	defer func() {
		_ = fileState.Release(index.ReleaseContext{
			Path: "notes/a.md", WorkerID: "other-worker", Success: false,
		})
	}()

	_, renameErr := node.Rename(
		root, nodeRepo, edgeRepo, fileState, "worker-self", time.Minute,
		manifest.EdgeTypes{}, nil, nil, "notes/a", "notes/z.md",
	)

	if !errors.Is(renameErr, index.ErrBusy) {
		test.Fatalf("Rename err = %v, want index.ErrBusy", renameErr)
	}

	dstRow, getErr := fileState.Get("notes/z.md")

	if getErr != nil && !errors.Is(getErr, index.ErrFileStateNotFound) {
		test.Fatalf("file_state Get notes/z.md: %v", getErr)
	}

	if getErr == nil && dstRow.LeasedBy.Valid {
		test.Errorf("destination lease was taken: %+v", dstRow.LeasedBy)
	}
}
