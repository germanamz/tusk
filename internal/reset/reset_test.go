package reset

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/epoch"
	"github.com/germanamz/tusk/internal/index"
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
	if got, _ := epoch.Index.Read(root); got != 1 {
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
	if got, _ := epoch.Index.Read(root); got != 0 {
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

// gatingEmbedder blocks every Embed call until release is closed, then returns a
// successful vector. started signals (once) that a call has entered Embed, so a
// test can deterministically observe the in-flight, still-leased state without
// time.Sleep. Goroutine-safe.
type gatingEmbedder struct {
	dim     int
	model   string
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (stub *gatingEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	stub.once.Do(func() { close(stub.started) })

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-stub.release:
	}

	out := make([]float32, stub.dim)

	for idx := range out {
		out[idx] = 0.1
	}

	return out, nil
}

func (stub *gatingEmbedder) Model() string { return stub.model }
func (stub *gatingEmbedder) Dim() int      { return stub.dim }

// writeNodeFile writes a minimal markdown node file with frontmatter, creating
// parent directories as needed.
func writeNodeFile(test *testing.T, root, relPath, body string) {
	test.Helper()

	abs := filepath.Join(root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	content := "---\ntype: note\ntitle: x\n---\n\n" + body + "\n"

	if writeErr := os.WriteFile(abs, []byte(content), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}
}

// TestReset_WhileDraining_NoSplitBrain proves the reset's quiesce→delete→reopen
// ordering keeps an in-flight drain from leaking a stale vector into the fresh
// index. A drainer leases a row and blocks mid-embed (vector not yet written);
// the reset closes that handle, rebuilds the DB, and bumps the epoch. When the
// drainer resumes, its Upsert lands on the now-closed old handle and errors out
// instead of writing into the fresh DB. The fresh index then re-embeds the node
// to exactly one vector. The gate channel makes the ordering deterministic.
//
// Red→green: if the reopen reuses the SAME handle (Quiesce nil, Reopen returns
// the old store), the stalled Upsert succeeds and the "fresh DB has zero rows"
// assertion fails — confirming the close-before-reopen is the guard under test.
func TestReset_WhileDraining_NoSplitBrain(test *testing.T) {
	root := test.TempDir()
	indexPath := filepath.Join(root, ".tusk", "index.db")

	storeOld, openErr := index.Open(indexPath)
	if openErr != nil {
		test.Fatalf("open old: %v", openErr)
	}

	nodeRepoOld := index.NewNodeRepo(storeOld)
	queueRepoOld := index.NewEmbedQueueRepo(storeOld)
	embeddingRepoOld := index.NewEmbeddingRepo(storeOld)

	writeNodeFile(test, root, "notes/a.md", "tiny body")

	if upsertErr := nodeRepoOld.Upsert(index.NodeRow{ID: "notes/a", Type: "note", Path: "notes/a.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert node: %v", upsertErr)
	}

	if enqErr := queueRepoOld.Enqueue("notes/a"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	gate := &gatingEmbedder{
		dim:     3,
		model:   "stub",
		started: make(chan struct{}),
		release: make(chan struct{}),
	}

	type drainOutcome struct {
		drained int
		err     error
	}

	drainDone := make(chan drainOutcome, 1)

	go func() {
		drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
			Root:       root,
			Nodes:      nodeRepoOld,
			Queue:      queueRepoOld,
			Embeddings: embeddingRepoOld,
			Embedder:   gate,
			Chunker:    embed.WholeDocument{},
			TTL:        time.Minute,
		})
		drainDone <- drainOutcome{drained: drained, err: drainErr}
	}()

	// Wait until the drainer is blocked inside Embed: the row is leased and all
	// pre-embed DB reads completed while storeOld was still open.
	select {
	case <-gate.started:
	case <-time.After(2 * time.Second):
		close(gate.release)
		<-drainDone
		test.Fatal("embedder never started — DrainQueue did not lease the row")
	}

	// Reset under the (assumed-held) lock: close the old handle, delete
	// artifacts, reopen fresh, bump epoch.
	result, resetErr := PerformLocked(Config{
		Root:      root,
		IndexPath: indexPath,
		Quiesce:   storeOld.Close,
		Reopen:    func() (*index.Index, error) { return index.Open(indexPath) },
	})

	if resetErr != nil {
		close(gate.release)
		<-drainDone
		test.Fatalf("PerformLocked: %v", resetErr)
	}

	storeFresh := result.Store
	defer storeFresh.Close()

	// Release the stalled drainer: its Upsert now targets the CLOSED old handle.
	close(gate.release)

	outcome := <-drainDone

	// (1) The drainer surfaced an error (the closed-DB Upsert) and did not panic.
	if outcome.err == nil {
		test.Error("drainer returned nil error; want a closed-DB failure from the stale Upsert")
	}

	if outcome.drained != 0 {
		test.Errorf("drained = %d, want 0 (the in-flight pass must not succeed)", outcome.drained)
	}

	// (2) The fresh DB never received the stale in-flight vector.
	embeddingRepoFresh := index.NewEmbeddingRepo(storeFresh)

	staleRows, getErr := embeddingRepoFresh.GetByNodeID("notes/a")
	if getErr != nil {
		test.Fatalf("GetByNodeID on fresh store: %v", getErr)
	}

	if len(staleRows) != 0 {
		test.Errorf("fresh index has %d embeddings for notes/a, want 0 (no split-brain leak)", len(staleRows))
	}

	// (3) The epoch was bumped.
	if result.Epoch != 1 {
		test.Errorf("epoch = %d, want 1", result.Epoch)
	}

	if persisted, _ := epoch.Index.Read(root); persisted != result.Epoch {
		test.Errorf("persisted epoch = %d, want %d", persisted, result.Epoch)
	}

	// The fresh index re-embeds the node to exactly one vector.
	nodeRepoFresh := index.NewNodeRepo(storeFresh)
	queueRepoFresh := index.NewEmbedQueueRepo(storeFresh)

	if upsertErr := nodeRepoFresh.Upsert(index.NodeRow{ID: "notes/a", Type: "note", Path: "notes/a.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("re-upsert node: %v", upsertErr)
	}

	if enqErr := queueRepoFresh.Enqueue("notes/a"); enqErr != nil {
		test.Fatalf("re-enqueue: %v", enqErr)
	}

	// A gating embedder with release already closed never blocks — it returns a
	// vector immediately.
	working := &gatingEmbedder{
		dim:     3,
		model:   "stub",
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	close(working.release)

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepoFresh,
		Queue:      queueRepoFresh,
		Embeddings: embeddingRepoFresh,
		Embedder:   working,
		Chunker:    embed.WholeDocument{},
		TTL:        time.Minute,
	})

	if drainErr != nil {
		test.Fatalf("fresh DrainQueue: %v", drainErr)
	}

	if drained != 1 {
		test.Errorf("fresh drained = %d, want 1", drained)
	}

	freshRows, _ := embeddingRepoFresh.GetByNodeID("notes/a")

	if len(freshRows) != 1 {
		test.Errorf("fresh index embeddings for notes/a = %d, want 1", len(freshRows))
	}
}
