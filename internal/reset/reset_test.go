package reset

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/indexepoch"
	"github.com/germanamz/tusk/internal/lock"
)

func TestPerform_DeletesReopensAndBumps(test *testing.T) {
	root := test.TempDir()
	indexPath := filepath.Join(root, ".tusk", "index.db")

	// Seed a real index plus a stale WAL sidecar.
	seed, openErr := index.Open(indexPath)
	if openErr != nil {
		test.Fatalf("seed open: %v", openErr)
	}
	seed.Close()
	if writeErr := os.WriteFile(indexPath+"-wal", []byte("stale"), 0o644); writeErr != nil {
		test.Fatalf("seed wal: %v", writeErr)
	}

	quiesced := false

	result, err := Perform(context.Background(), Config{
		Root:      root,
		IndexPath: indexPath,
		LockTTL:   time.Second,
		Quiesce:   func() error { quiesced = true; return nil },
		Reopen:    func() (*index.Index, error) { return index.Open(indexPath) },
	})
	if err != nil {
		test.Fatalf("Perform: %v", err)
	}
	defer result.Store.Close()

	if !quiesced {
		test.Error("Quiesce was not invoked")
	}
	if result.Epoch != 1 {
		test.Errorf("expected epoch 1, got %d", result.Epoch)
	}
	if got, _ := indexepoch.Read(root); got != 1 {
		test.Errorf("expected persisted epoch 1, got %d", got)
	}
	if len(result.DeletedArtifacts) == 0 {
		test.Error("expected at least the main db reported deleted")
	}
	// Fresh handle must be usable.
	if _, listErr := result.Store.ListTables(); listErr != nil {
		test.Errorf("fresh store unusable: %v", listErr)
	}
}

func TestPerform_ReapsStaging(test *testing.T) {
	root := test.TempDir()
	indexPath := filepath.Join(root, ".tusk", "index.db")
	stagingDir := filepath.Join(root, ".tusk", "staging")

	if mkErr := os.MkdirAll(stagingDir, 0o755); mkErr != nil {
		test.Fatalf("mkdir staging: %v", mkErr)
	}
	if writeErr := os.WriteFile(filepath.Join(stagingDir, "foo.tmp"), []byte("x"), 0o644); writeErr != nil {
		test.Fatalf("seed staging: %v", writeErr)
	}

	result, err := Perform(context.Background(), Config{
		Root:      root,
		IndexPath: indexPath,
		LockTTL:   time.Second,
		Reopen:    func() (*index.Index, error) { return index.Open(indexPath) },
	})
	if err != nil {
		test.Fatalf("Perform: %v", err)
	}
	defer result.Store.Close()

	if _, statErr := os.Stat(stagingDir); !os.IsNotExist(statErr) {
		test.Errorf("staging dir survived reset (stat err: %v)", statErr)
	}
}

func TestPerform_ReopenFailureLeavesEpochUnbumped(test *testing.T) {
	root := test.TempDir()
	indexPath := filepath.Join(root, ".tusk", "index.db")

	seed, openErr := index.Open(indexPath)
	if openErr != nil {
		test.Fatalf("seed open: %v", openErr)
	}
	seed.Close()

	_, err := Perform(context.Background(), Config{
		Root:      root,
		IndexPath: indexPath,
		LockTTL:   time.Second,
		Reopen:    func() (*index.Index, error) { return nil, errors.New("boom") },
	})
	if err == nil {
		test.Fatal("expected Perform to fail when Reopen fails")
	}

	// Epoch must NOT have advanced.
	if got, _ := indexepoch.Read(root); got != 0 {
		test.Errorf("epoch advanced to %d on reopen failure; want 0", got)
	}

	// Lock must have been released — a fresh acquire must succeed quickly.
	handle, _ := lock.NewWorkspaceLock(root)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if acquireErr := handle.Acquire(ctx); acquireErr != nil {
		test.Errorf("lock not released after reopen failure: %v", acquireErr)
	}
	handle.Release()
}

func TestPerform_LockBusyDeletesNothing(test *testing.T) {
	root := test.TempDir()
	indexPath := filepath.Join(root, ".tusk", "index.db")

	seed, openErr := index.Open(indexPath)
	if openErr != nil {
		test.Fatalf("seed open: %v", openErr)
	}
	seed.Close()

	// Hold the workspace lock from a separate handle to simulate a busy reset.
	holder, _ := lock.NewWorkspaceLock(root)
	holdCtx, holdCancel := context.WithTimeout(context.Background(), time.Second)
	defer holdCancel()
	if acquireErr := holder.Acquire(holdCtx); acquireErr != nil {
		test.Fatalf("holder acquire: %v", acquireErr)
	}
	defer holder.Release()

	reopenCalled := false
	_, err := Perform(context.Background(), Config{
		Root:      root,
		IndexPath: indexPath,
		LockTTL:   200 * time.Millisecond,
		Reopen:    func() (*index.Index, error) { reopenCalled = true; return index.Open(indexPath) },
	})

	if !errors.Is(err, lock.ErrBusy) {
		test.Fatalf("expected lock.ErrBusy, got %v", err)
	}
	if reopenCalled {
		test.Error("Reopen ran despite lock contention")
	}
	// The seeded index must still be on disk — nothing was deleted.
	if _, statErr := os.Stat(indexPath); statErr != nil {
		test.Errorf("seed index was deleted under contention: %v", statErr)
	}
}
