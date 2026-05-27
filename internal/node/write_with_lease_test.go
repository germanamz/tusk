package node_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
)

func setupLeaseHarness(test *testing.T) (string, *index.Index, *index.FileStateRepo) {
	test.Helper()

	root := test.TempDir()
	store := openTempIndex(test, root)

	test.Cleanup(func() { store.Close() })

	return root, store, index.NewFileStateRepo(store)
}

func writeFile(test *testing.T, path string, content []byte) {
	test.Helper()

	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
		test.Fatalf("mkdir %s: %v", filepath.Dir(path), mkErr)
	}

	if writeErr := os.WriteFile(path, content, 0o644); writeErr != nil {
		test.Fatalf("write %s: %v", path, writeErr)
	}
}

func mustReadFile(test *testing.T, path string) []byte {
	test.Helper()

	content, readErr := os.ReadFile(path)

	if readErr != nil {
		test.Fatalf("read %s: %v", path, readErr)
	}

	return content
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)

	return hex.EncodeToString(sum[:])
}

func TestWriteWithLease_ReplaceHappyPath(test *testing.T) {
	root, _, repo := setupLeaseHarness(test)

	relPath := "notes/a.md"
	original := []byte("hello\n")
	updated := []byte("hello world\n")

	writeFile(test, filepath.Join(root, relPath), original)

	writeErr := node.WriteWithLease(
		context.Background(), root, repo, "worker-A", time.Minute, relPath,
		func(current []byte) (node.Mutation, error) {
			if string(current) != string(original) {
				test.Errorf("mutator current = %q, want %q", current, original)
			}

			return node.WriteReplace(updated), nil
		},
	)

	if writeErr != nil {
		test.Fatalf("WriteWithLease: %v", writeErr)
	}

	if got := mustReadFile(test, filepath.Join(root, relPath)); string(got) != string(updated) {
		test.Errorf("on-disk content = %q, want %q", got, updated)
	}

	row, getErr := repo.Get(relPath)

	if getErr != nil {
		test.Fatalf("Get after write: %v", getErr)
	}

	if row.ContentHash != sha256Hex(updated) {
		test.Errorf("content_hash = %q, want %q", row.ContentHash, sha256Hex(updated))
	}

	if row.Size != int64(len(updated)) {
		test.Errorf("size = %d, want %d", row.Size, len(updated))
	}

	if row.LeasedBy.Valid || row.LeasedUntilNs.Valid {
		test.Errorf("lease columns not cleared: %+v / %+v", row.LeasedBy, row.LeasedUntilNs)
	}

	if row.PendingTempPath.Valid || row.PendingHash.Valid {
		test.Errorf("pending columns not cleared: %+v / %+v", row.PendingTempPath, row.PendingHash)
	}

	if row.State != index.FileStateLive {
		test.Errorf("state = %q, want live", row.State)
	}

	stagingDir := filepath.Join(root, ".tusk", "staging")
	entries, readErr := os.ReadDir(stagingDir)

	if readErr != nil {
		test.Fatalf("read staging dir: %v", readErr)
	}

	if len(entries) != 0 {
		test.Errorf("staging dir not empty after successful write: %d entries", len(entries))
	}
}

func TestWriteWithLease_Tombstone(test *testing.T) {
	root, _, repo := setupLeaseHarness(test)

	relPath := "notes/doomed.md"
	original := []byte("bye\n")

	writeFile(test, filepath.Join(root, relPath), original)

	writeErr := node.WriteWithLease(
		context.Background(), root, repo, "worker-A", time.Minute, relPath,
		func(current []byte) (node.Mutation, error) {
			return node.WriteTombstone(), nil
		},
	)

	if writeErr != nil {
		test.Fatalf("WriteWithLease: %v", writeErr)
	}

	if _, statErr := os.Stat(filepath.Join(root, relPath)); !errors.Is(statErr, os.ErrNotExist) {
		test.Errorf("file should be removed, stat err = %v", statErr)
	}

	row, getErr := repo.Get(relPath)

	if getErr != nil {
		test.Fatalf("Get after tombstone: %v", getErr)
	}

	if row.State != index.FileStateTombstone {
		test.Errorf("state = %q, want tombstone", row.State)
	}

	if row.LeasedBy.Valid || row.LeasedUntilNs.Valid {
		test.Errorf("lease columns not cleared: %+v / %+v", row.LeasedBy, row.LeasedUntilNs)
	}
}

func TestWriteWithLease_NoChange(test *testing.T) {
	root, _, repo := setupLeaseHarness(test)

	relPath := "notes/quiet.md"
	original := []byte("steady\n")

	writeFile(test, filepath.Join(root, relPath), original)

	// Pre-populate the file_state row with non-default observed-state so
	// we can assert NoChange leaves it alone.
	upsertErr := repo.Upsert(index.FileStateRow{
		Path:        relPath,
		ContentHash: "preexisting-hash",
		MtimeNs:     12345,
		Size:        99,
		State:       index.FileStateLive,
		LastSeenGen: 7,
	})

	if upsertErr != nil {
		test.Fatalf("seed Upsert: %v", upsertErr)
	}

	beforeStat, statErr := os.Stat(filepath.Join(root, relPath))

	if statErr != nil {
		test.Fatalf("stat before: %v", statErr)
	}

	writeErr := node.WriteWithLease(
		context.Background(), root, repo, "worker-A", time.Minute, relPath,
		func(current []byte) (node.Mutation, error) {
			return node.WriteNoChange(), nil
		},
	)

	if writeErr != nil {
		test.Fatalf("WriteWithLease: %v", writeErr)
	}

	// File untouched.
	afterStat, statErr := os.Stat(filepath.Join(root, relPath))

	if statErr != nil {
		test.Fatalf("stat after: %v", statErr)
	}

	if !afterStat.ModTime().Equal(beforeStat.ModTime()) {
		test.Errorf("file mtime changed on NoChange: before=%v after=%v", beforeStat.ModTime(), afterStat.ModTime())
	}

	if string(mustReadFile(test, filepath.Join(root, relPath))) != string(original) {
		test.Error("file content changed on NoChange")
	}

	row, getErr := repo.Get(relPath)

	if getErr != nil {
		test.Fatalf("Get after NoChange: %v", getErr)
	}

	if row.ContentHash != "preexisting-hash" || row.MtimeNs != 12345 || row.Size != 99 {
		test.Errorf("observed state mutated on NoChange: %+v", row)
	}

	if row.LeasedBy.Valid || row.LeasedUntilNs.Valid {
		test.Errorf("lease not released on NoChange: %+v / %+v", row.LeasedBy, row.LeasedUntilNs)
	}

	// No staging files left behind.
	stagingDir := filepath.Join(root, ".tusk", "staging")

	if _, statErr := os.Stat(stagingDir); statErr == nil {
		entries, _ := os.ReadDir(stagingDir)

		if len(entries) != 0 {
			test.Errorf("staging dir not empty after NoChange: %d entries", len(entries))
		}
	}
}

func TestWriteWithLease_MutatorErrorAbandonsLease(test *testing.T) {
	root, _, repo := setupLeaseHarness(test)

	relPath := "notes/err.md"
	original := []byte("untouched\n")

	writeFile(test, filepath.Join(root, relPath), original)

	beforeStat, statErr := os.Stat(filepath.Join(root, relPath))

	if statErr != nil {
		test.Fatalf("stat before: %v", statErr)
	}

	sentinel := errors.New("mutator boom")

	writeErr := node.WriteWithLease(
		context.Background(), root, repo, "worker-A", time.Minute, relPath,
		func(current []byte) (node.Mutation, error) {
			return node.Mutation{}, sentinel
		},
	)

	if !errors.Is(writeErr, sentinel) {
		test.Fatalf("WriteWithLease error = %v, want %v", writeErr, sentinel)
	}

	if string(mustReadFile(test, filepath.Join(root, relPath))) != string(original) {
		test.Error("file content changed despite mutator error")
	}

	afterStat, statErr := os.Stat(filepath.Join(root, relPath))

	if statErr != nil {
		test.Fatalf("stat after: %v", statErr)
	}

	if !afterStat.ModTime().Equal(beforeStat.ModTime()) {
		test.Errorf("file mtime changed despite mutator error: before=%v after=%v", beforeStat.ModTime(), afterStat.ModTime())
	}

	row, getErr := repo.Get(relPath)

	if getErr != nil {
		test.Fatalf("Get after mutator error: %v", getErr)
	}

	if row.LeasedBy.Valid || row.LeasedUntilNs.Valid {
		test.Errorf("lease not released on mutator error: %+v / %+v", row.LeasedBy, row.LeasedUntilNs)
	}

	if row.PendingTempPath.Valid || row.PendingHash.Valid {
		test.Errorf("pending columns not cleared on mutator error: %+v / %+v", row.PendingTempPath, row.PendingHash)
	}

	// No staging files left behind.
	stagingDir := filepath.Join(root, ".tusk", "staging")

	if _, statErr := os.Stat(stagingDir); statErr == nil {
		entries, _ := os.ReadDir(stagingDir)

		if len(entries) != 0 {
			test.Errorf("staging dir not empty after mutator error: %d entries", len(entries))
		}
	}
}

func TestWriteWithLease_StaleTempRecovery(test *testing.T) {
	root, store, repo := setupLeaseHarness(test)

	relPath := "notes/recovered.md"
	original := []byte("alive\n")

	writeFile(test, filepath.Join(root, relPath), original)

	// Seed file_state row with a pending temp left behind by a crashed
	// predecessor. The Claim path inside WriteWithLease should unlink it.
	stagingDir := filepath.Join(root, ".tusk", "staging")

	if mkErr := os.MkdirAll(stagingDir, 0o755); mkErr != nil {
		test.Fatalf("mkdir staging: %v", mkErr)
	}

	stalePath := filepath.Join(stagingDir, "stale-temp-uuid")

	if writeErr := os.WriteFile(stalePath, []byte("garbage from crashed writer"), 0o600); writeErr != nil {
		test.Fatalf("write stale: %v", writeErr)
	}

	upsertErr := repo.Upsert(index.FileStateRow{
		Path:        relPath,
		ContentHash: sha256Hex(original),
		MtimeNs:     1,
		Size:        int64(len(original)),
		State:       index.FileStateLive,
		LastSeenGen: 1,
	})

	if upsertErr != nil {
		test.Fatalf("Upsert seed: %v", upsertErr)
	}

	// Drop pending_* values directly via DB (FileStateRepo has no public
	// setter outside the in-flight write path; the lease test suite uses
	// the same pattern).
	if _, execErr := store.DB().Exec(`
		UPDATE file_state
		SET pending_temp_path = ?, pending_hash = ?
		WHERE path = ?
	`, stalePath, "stale-hash", relPath); execErr != nil {
		test.Fatalf("inject stale pending: %v", execErr)
	}

	updated := []byte("alive and well\n")

	writeErr := node.WriteWithLease(
		context.Background(), root, repo, "worker-A", time.Minute, relPath,
		func(current []byte) (node.Mutation, error) {
			return node.WriteReplace(updated), nil
		},
	)

	if writeErr != nil {
		test.Fatalf("WriteWithLease: %v", writeErr)
	}

	if _, statErr := os.Stat(stalePath); !errors.Is(statErr, os.ErrNotExist) {
		test.Errorf("stale temp not unlinked by Claim: stat err = %v", statErr)
	}

	if string(mustReadFile(test, filepath.Join(root, relPath))) != string(updated) {
		test.Error("file content not updated after stale-temp recovery")
	}
}

func TestWriteWithLease_LazyCreatesRowAndUpdatesOnSecondCall(test *testing.T) {
	root, store, repo := setupLeaseHarness(test)

	relPath := "notes/lazy.md"
	original := []byte("first\n")

	writeFile(test, filepath.Join(root, relPath), original)

	// No Upsert on the file_state row: this is the pre-existing-node
	// scenario the lazy-create path is for.
	if _, getErr := repo.Get(relPath); !errors.Is(getErr, index.ErrFileStateNotFound) {
		test.Fatalf("precondition: row should not exist, got %v", getErr)
	}

	firstContent := []byte("first updated\n")

	writeErr := node.WriteWithLease(
		context.Background(), root, repo, "worker-A", time.Minute, relPath,
		func(current []byte) (node.Mutation, error) {
			return node.WriteReplace(firstContent), nil
		},
	)

	if writeErr != nil {
		test.Fatalf("first WriteWithLease: %v", writeErr)
	}

	row, getErr := repo.Get(relPath)

	if getErr != nil {
		test.Fatalf("Get after first call: %v", getErr)
	}

	if row.ContentHash != sha256Hex(firstContent) {
		test.Errorf("content_hash after first call = %q, want %q", row.ContentHash, sha256Hex(firstContent))
	}

	secondContent := []byte("second\n")

	writeErr = node.WriteWithLease(
		context.Background(), root, repo, "worker-A", time.Minute, relPath,
		func(current []byte) (node.Mutation, error) {
			return node.WriteReplace(secondContent), nil
		},
	)

	if writeErr != nil {
		test.Fatalf("second WriteWithLease: %v", writeErr)
	}

	row, getErr = repo.Get(relPath)

	if getErr != nil {
		test.Fatalf("Get after second call: %v", getErr)
	}

	if row.ContentHash != sha256Hex(secondContent) {
		test.Errorf("content_hash after second call = %q, want %q", row.ContentHash, sha256Hex(secondContent))
	}

	// Confirm there's only one row for the path (no duplicate insert).
	var count int

	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM file_state WHERE path = ?`, relPath).Scan(&count); scanErr != nil {
		test.Fatalf("count rows: %v", scanErr)
	}

	if count != 1 {
		test.Errorf("file_state rows for %s = %d, want 1", relPath, count)
	}
}

func TestWriteWithLease_ConcurrentCallsSerialize(test *testing.T) {
	root, _, repo := setupLeaseHarness(test)

	relPath := "notes/race.md"
	original := []byte("start\n")

	writeFile(test, filepath.Join(root, relPath), original)

	gate := make(chan struct{})
	winnerContent := []byte("winner wrote me\n")

	mutator := func(current []byte) (node.Mutation, error) {
		// Hold the lease briefly so the second caller observes it as
		// busy. Tight enough that the test is fast; long enough that
		// the second Claim is essentially guaranteed to land while we
		// still hold the lease.
		time.Sleep(50 * time.Millisecond)

		return node.WriteReplace(winnerContent), nil
	}

	var (
		waitGroup sync.WaitGroup
		errAlpha  error
		errBeta   error
	)

	waitGroup.Add(2)

	go func() {
		defer waitGroup.Done()

		<-gate
		errAlpha = node.WriteWithLease(context.Background(), root, repo, "worker-alpha", time.Minute, relPath, mutator)
	}()

	go func() {
		defer waitGroup.Done()

		<-gate
		errBeta = node.WriteWithLease(context.Background(), root, repo, "worker-beta", time.Minute, relPath, mutator)
	}()

	close(gate)
	waitGroup.Wait()

	// Exactly one caller should succeed; the other should return ErrBusy.
	successes := 0
	busies := 0

	for _, callErr := range []error{errAlpha, errBeta} {
		switch {
		case callErr == nil:
			successes++
		case errors.Is(callErr, index.ErrBusy):
			busies++
		default:
			test.Errorf("unexpected error: %v", callErr)
		}
	}

	if successes != 1 || busies != 1 {
		test.Fatalf("want exactly one success and one ErrBusy, got successes=%d busies=%d (alpha=%v beta=%v)",
			successes, busies, errAlpha, errBeta)
	}

	// The winning content is on disk; the file_state row reflects it.
	if string(mustReadFile(test, filepath.Join(root, relPath))) != string(winnerContent) {
		test.Error("on-disk content does not match winning mutator")
	}

	row, getErr := repo.Get(relPath)

	if getErr != nil {
		test.Fatalf("Get after race: %v", getErr)
	}

	if row.ContentHash != sha256Hex(winnerContent) {
		test.Errorf("content_hash = %q, want %q", row.ContentHash, sha256Hex(winnerContent))
	}

	if row.LeasedBy.Valid {
		test.Errorf("lease still held after race: %+v", row.LeasedBy)
	}
}
