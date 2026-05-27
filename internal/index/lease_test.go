package index_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
)

func seedFileStateRow(test *testing.T, store *index.Index, path string) {
	test.Helper()

	repo := index.NewFileStateRepo(store)

	upsertErr := repo.Upsert(index.FileStateRow{
		Path:        path,
		ContentHash: "initial",
		MtimeNs:     1000,
		Size:        10,
		State:       index.FileStateLive,
		LastSeenGen: 1,
	})

	if upsertErr != nil {
		test.Fatalf("seed Upsert %s: %v", path, upsertErr)
	}
}

func setLease(test *testing.T, store *index.Index, path, worker string, leasedUntilNs int64) {
	test.Helper()

	if _, execErr := store.DB().Exec(`
		UPDATE file_state
		SET leased_by = ?, leased_until_ns = ?
		WHERE path = ?
	`, worker, leasedUntilNs, path); execErr != nil {
		test.Fatalf("setLease %s: %v", path, execErr)
	}
}

func setPendingTemp(test *testing.T, store *index.Index, path, tempPath, pendingHash string) {
	test.Helper()

	if _, execErr := store.DB().Exec(`
		UPDATE file_state
		SET pending_temp_path = ?, pending_hash = ?
		WHERE path = ?
	`, tempPath, pendingHash, path); execErr != nil {
		test.Fatalf("setPendingTemp %s: %v", path, execErr)
	}
}

func TestFileStateRepo_ClaimSucceedsOnFreshRow(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewFileStateRepo(store)

	seedFileStateRow(test, store, "notes/a.md")

	lease, claimErr := repo.Claim("notes/a.md", "worker-A", time.Minute)

	if claimErr != nil {
		test.Fatalf("Claim: %v", claimErr)
	}

	if lease == nil {
		test.Fatal("Claim returned nil lease")
	}

	if lease.Path != "notes/a.md" || lease.WorkerID != "worker-A" {
		test.Errorf("lease identity = %+v", lease)
	}

	if lease.ContentHash != "initial" {
		test.Errorf("lease.ContentHash = %q, want %q", lease.ContentHash, "initial")
	}

	if lease.LeasedUntilNs <= time.Now().UnixNano() {
		test.Errorf("LeasedUntilNs should be in the future, got %d (now %d)", lease.LeasedUntilNs, time.Now().UnixNano())
	}

	after, getErr := repo.Get("notes/a.md")

	if getErr != nil {
		test.Fatalf("Get after claim: %v", getErr)
	}

	if !after.LeasedBy.Valid || after.LeasedBy.String != "worker-A" {
		test.Errorf("row leased_by = %+v, want worker-A", after.LeasedBy)
	}
}

func TestFileStateRepo_ClaimReturnsBusyWhenRowMissing(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewFileStateRepo(store)

	lease, claimErr := repo.Claim("missing.md", "worker-A", time.Minute)

	if !errors.Is(claimErr, index.ErrBusy) {
		test.Fatalf("Claim missing row error = %v, want ErrBusy", claimErr)
	}

	if lease != nil {
		test.Errorf("lease = %+v, want nil", lease)
	}
}

func TestFileStateRepo_ClaimReturnsBusyWhenAnotherWorkerHoldsLease(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewFileStateRepo(store)

	seedFileStateRow(test, store, "notes/a.md")
	setLease(test, store, "notes/a.md", "worker-A", time.Now().Add(time.Hour).UnixNano())

	lease, claimErr := repo.Claim("notes/a.md", "worker-B", time.Minute)

	if !errors.Is(claimErr, index.ErrBusy) {
		test.Fatalf("Claim on held lease error = %v, want ErrBusy", claimErr)
	}

	if lease != nil {
		test.Errorf("lease = %+v, want nil", lease)
	}

	after, _ := repo.Get("notes/a.md")

	if !after.LeasedBy.Valid || after.LeasedBy.String != "worker-A" {
		test.Errorf("incumbent lease overwritten: %+v", after.LeasedBy)
	}
}

func TestFileStateRepo_ClaimReclaimsExpiredLease(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewFileStateRepo(store)

	seedFileStateRow(test, store, "notes/a.md")
	expiredAt := time.Now().Add(-time.Hour).UnixNano()
	setLease(test, store, "notes/a.md", "worker-A", expiredAt)

	lease, claimErr := repo.Claim("notes/a.md", "worker-B", time.Minute)

	if claimErr != nil {
		test.Fatalf("Claim should reclaim expired lease: %v", claimErr)
	}

	if lease.WorkerID != "worker-B" {
		test.Errorf("lease.WorkerID = %q, want worker-B", lease.WorkerID)
	}

	after, _ := repo.Get("notes/a.md")

	if after.LeasedBy.String != "worker-B" {
		test.Errorf("row leased_by = %q, want worker-B", after.LeasedBy.String)
	}
}

func TestFileStateRepo_ClaimUnlinksStaleTemp(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewFileStateRepo(store)

	staging := test.TempDir()
	stalePath := filepath.Join(staging, "stale-temp")

	if writeErr := os.WriteFile(stalePath, []byte("garbage"), 0o600); writeErr != nil {
		test.Fatalf("write stale temp: %v", writeErr)
	}

	seedFileStateRow(test, store, "notes/a.md")
	setPendingTemp(test, store, "notes/a.md", stalePath, "stale-hash")

	lease, claimErr := repo.Claim("notes/a.md", "worker-A", time.Minute)

	if claimErr != nil {
		test.Fatalf("Claim: %v", claimErr)
	}

	if lease == nil {
		test.Fatal("Claim returned nil lease")
	}

	if _, statErr := os.Stat(stalePath); !errors.Is(statErr, os.ErrNotExist) {
		test.Errorf("stale temp not removed: stat err = %v", statErr)
	}

	after, _ := repo.Get("notes/a.md")

	if after.PendingTempPath.Valid {
		test.Errorf("pending_temp_path should be NULL after claim, got %+v", after.PendingTempPath)
	}

	if after.PendingHash.Valid {
		test.Errorf("pending_hash should be NULL after claim, got %+v", after.PendingHash)
	}
}

func TestFileStateRepo_ClaimMissingStaleTempIsNotFatal(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewFileStateRepo(store)

	seedFileStateRow(test, store, "notes/a.md")
	setPendingTemp(test, store, "notes/a.md", "/definitely/does/not/exist/abc", "stale-hash")

	lease, claimErr := repo.Claim("notes/a.md", "worker-A", time.Minute)

	if claimErr != nil {
		test.Fatalf("Claim should treat ENOENT as non-fatal: %v", claimErr)
	}

	if lease == nil {
		test.Fatal("lease nil after claim")
	}

	after, _ := repo.Get("notes/a.md")

	if after.PendingTempPath.Valid {
		test.Errorf("pending_temp_path should be cleared even when temp missing, got %+v", after.PendingTempPath)
	}
}

func TestFileStateRepo_ReleaseSuccessCommitsObservedState(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewFileStateRepo(store)

	seedFileStateRow(test, store, "notes/a.md")

	lease, claimErr := repo.Claim("notes/a.md", "worker-A", time.Minute)

	if claimErr != nil {
		test.Fatalf("Claim: %v", claimErr)
	}

	// Mark a pending temp on the row to verify Release clears it even on success.
	setPendingTemp(test, store, "notes/a.md", "/tmp/abc", "pending-hash")

	releaseErr := repo.Release(index.ReleaseContext{
		Path:        lease.Path,
		WorkerID:    lease.WorkerID,
		Success:     true,
		ContentHash: "after-commit",
		MtimeNs:     2000,
		Size:        77,
	})

	if releaseErr != nil {
		test.Fatalf("Release: %v", releaseErr)
	}

	after, _ := repo.Get("notes/a.md")

	if after.ContentHash != "after-commit" {
		test.Errorf("ContentHash = %q, want after-commit", after.ContentHash)
	}

	if after.MtimeNs != 2000 || after.Size != 77 {
		test.Errorf("Mtime=%d size=%d, want 2000/77", after.MtimeNs, after.Size)
	}

	if after.LeasedBy.Valid || after.LeasedUntilNs.Valid {
		test.Errorf("lease columns not cleared: %+v / %+v", after.LeasedBy, after.LeasedUntilNs)
	}

	if after.PendingTempPath.Valid || after.PendingHash.Valid {
		test.Errorf("pending columns not cleared: %+v / %+v", after.PendingTempPath, after.PendingHash)
	}

	if after.State != index.FileStateLive {
		test.Errorf("state changed unexpectedly: %q", after.State)
	}
}

func TestFileStateRepo_ReleaseSuccessUpdatesStateWhenProvided(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewFileStateRepo(store)

	seedFileStateRow(test, store, "notes/a.md")

	lease, claimErr := repo.Claim("notes/a.md", "worker-A", time.Minute)

	if claimErr != nil {
		test.Fatalf("Claim: %v", claimErr)
	}

	releaseErr := repo.Release(index.ReleaseContext{
		Path:        lease.Path,
		WorkerID:    lease.WorkerID,
		Success:     true,
		State:       index.FileStateTombstone,
		ContentHash: "tombstone-hash",
		MtimeNs:     3000,
		Size:        0,
	})

	if releaseErr != nil {
		test.Fatalf("Release: %v", releaseErr)
	}

	after, _ := repo.Get("notes/a.md")

	if after.State != index.FileStateTombstone {
		test.Errorf("state = %q, want tombstone", after.State)
	}
}

func TestFileStateRepo_ReleaseAbandonLeavesObservedStateAlone(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewFileStateRepo(store)

	seedFileStateRow(test, store, "notes/a.md")

	before, _ := repo.Get("notes/a.md")

	lease, claimErr := repo.Claim("notes/a.md", "worker-A", time.Minute)

	if claimErr != nil {
		test.Fatalf("Claim: %v", claimErr)
	}

	setPendingTemp(test, store, "notes/a.md", "/tmp/staging-xyz", "doomed-hash")

	releaseErr := repo.Release(index.ReleaseContext{
		Path:     lease.Path,
		WorkerID: lease.WorkerID,
		Success:  false,
	})

	if releaseErr != nil {
		test.Fatalf("Release abandon: %v", releaseErr)
	}

	after, _ := repo.Get("notes/a.md")

	if after.ContentHash != before.ContentHash {
		test.Errorf("ContentHash mutated on abandon: was %q, now %q", before.ContentHash, after.ContentHash)
	}

	if after.MtimeNs != before.MtimeNs || after.Size != before.Size {
		test.Errorf("mtime/size mutated on abandon: was (%d,%d), now (%d,%d)",
			before.MtimeNs, before.Size, after.MtimeNs, after.Size)
	}

	if after.LeasedBy.Valid || after.LeasedUntilNs.Valid {
		test.Errorf("lease columns not cleared on abandon: %+v / %+v", after.LeasedBy, after.LeasedUntilNs)
	}

	if after.PendingTempPath.Valid || after.PendingHash.Valid {
		test.Errorf("pending columns not cleared on abandon: %+v / %+v", after.PendingTempPath, after.PendingHash)
	}
}

func TestFileStateRepo_ReleaseSkipsRowReclaimedByAnotherWorker(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewFileStateRepo(store)

	seedFileStateRow(test, store, "notes/a.md")

	if _, claimErr := repo.Claim("notes/a.md", "worker-A", time.Minute); claimErr != nil {
		test.Fatalf("first Claim: %v", claimErr)
	}

	// Simulate lease expiry and reclaim by another worker.
	expiredAt := time.Now().Add(-time.Hour).UnixNano()
	setLease(test, store, "notes/a.md", "worker-A", expiredAt)

	if _, reclaimErr := repo.Claim("notes/a.md", "worker-B", time.Minute); reclaimErr != nil {
		test.Fatalf("reclaim: %v", reclaimErr)
	}

	// Original worker tries to release. Should be a silent no-op.
	releaseErr := repo.Release(index.ReleaseContext{
		Path:        "notes/a.md",
		WorkerID:    "worker-A",
		Success:     true,
		ContentHash: "stale-write",
		MtimeNs:     9999,
		Size:        9999,
	})

	if releaseErr != nil {
		test.Fatalf("late Release: %v", releaseErr)
	}

	after, _ := repo.Get("notes/a.md")

	if after.ContentHash == "stale-write" {
		test.Error("late Release overwrote content_hash that belongs to the new claimant")
	}

	if !after.LeasedBy.Valid || after.LeasedBy.String != "worker-B" {
		test.Errorf("worker-B's lease lost: %+v", after.LeasedBy)
	}
}

func TestWorkerID_CachesAcrossCalls(test *testing.T) {
	index.ResetWorkerIDForTest()

	first := index.WorkerID()
	second := index.WorkerID()

	if first == "" {
		test.Fatal("WorkerID returned empty string")
	}

	if first != second {
		test.Errorf("WorkerID not cached: first=%q second=%q", first, second)
	}
}

func TestWorkerID_RegeneratesAfterReset(test *testing.T) {
	index.ResetWorkerIDForTest()
	first := index.WorkerID()

	index.ResetWorkerIDForTest()
	second := index.WorkerID()

	if first == second {
		test.Errorf("WorkerID did not regenerate after reset: %q", first)
	}
}
