package index_test

import (
	"errors"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func newTestFileStateRepo(test *testing.T) *index.FileStateRepo {
	test.Helper()

	store := openTestIndex(test)

	return index.NewFileStateRepo(store)
}

func makeRow(path string, gen int64) index.FileStateRow {
	return index.FileStateRow{
		Path:        path,
		ContentHash: "hash-" + path,
		MtimeNs:     1000,
		Size:        42,
		State:       index.FileStateLive,
		LastSeenGen: gen,
	}
}

func TestFileStateRepo_GetMissingReturnsNotFound(test *testing.T) {
	repo := newTestFileStateRepo(test)

	row, getErr := repo.Get("missing.md")

	if !errors.Is(getErr, index.ErrFileStateNotFound) {
		test.Fatalf("Get error = %v, want ErrFileStateNotFound", getErr)
	}

	if row != nil {
		test.Errorf("Get row = %+v, want nil", row)
	}
}

func TestFileStateRepo_UpsertInsertsThenUpdates(test *testing.T) {
	repo := newTestFileStateRepo(test)

	original := makeRow("notes/a.md", 1)

	if upsertErr := repo.Upsert(original); upsertErr != nil {
		test.Fatalf("first Upsert: %v", upsertErr)
	}

	after, getErr := repo.Get("notes/a.md")

	if getErr != nil {
		test.Fatalf("Get after insert: %v", getErr)
	}

	if after.ContentHash != "hash-notes/a.md" || after.LastSeenGen != 1 || after.State != index.FileStateLive {
		test.Errorf("after insert = %+v", after)
	}

	if after.UpdatedAtNs == 0 {
		test.Error("UpdatedAtNs should be set on insert")
	}

	if after.LeasedBy.Valid || after.LeasedUntilNs.Valid || after.PendingTempPath.Valid || after.PendingHash.Valid {
		test.Errorf("lease/pending columns should be NULL on fresh insert: %+v", after)
	}

	updated := makeRow("notes/a.md", 2)
	updated.ContentHash = "hash-v2"
	updated.MtimeNs = 2000
	updated.Size = 99
	updated.State = index.FileStateLive

	if upsertErr := repo.Upsert(updated); upsertErr != nil {
		test.Fatalf("second Upsert: %v", upsertErr)
	}

	second, secondErr := repo.Get("notes/a.md")

	if secondErr != nil {
		test.Fatalf("Get after update: %v", secondErr)
	}

	if second.ContentHash != "hash-v2" || second.MtimeNs != 2000 || second.Size != 99 || second.LastSeenGen != 2 {
		test.Errorf("after update = %+v", second)
	}

	if second.UpdatedAtNs < after.UpdatedAtNs {
		test.Errorf("UpdatedAtNs went backwards: was %d, now %d", after.UpdatedAtNs, second.UpdatedAtNs)
	}
}

func TestFileStateRepo_UpsertPreservesLeaseColumns(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewFileStateRepo(store)

	row := makeRow("locked.md", 1)

	if upsertErr := repo.Upsert(row); upsertErr != nil {
		test.Fatalf("Upsert: %v", upsertErr)
	}

	if _, execErr := store.DB().Exec(`
		UPDATE file_state
		SET leased_by = ?, leased_until_ns = ?,
		    pending_temp_path = ?, pending_hash = ?
		WHERE path = ?
	`, "worker-1", int64(5000), ".tusk/staging/abc", "pending-hash", "locked.md"); execErr != nil {
		test.Fatalf("seed lease: %v", execErr)
	}

	// Re-Upsert with a different observed state. Lease and pending columns
	// must survive.
	bumped := makeRow("locked.md", 2)
	bumped.ContentHash = "hash-after"

	if upsertErr := repo.Upsert(bumped); upsertErr != nil {
		test.Fatalf("second Upsert: %v", upsertErr)
	}

	after, getErr := repo.Get("locked.md")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if after.ContentHash != "hash-after" || after.LastSeenGen != 2 {
		test.Errorf("observed-state fields not refreshed: %+v", after)
	}

	if !after.LeasedBy.Valid || after.LeasedBy.String != "worker-1" {
		test.Errorf("leased_by lost: %+v", after.LeasedBy)
	}

	if !after.LeasedUntilNs.Valid || after.LeasedUntilNs.Int64 != 5000 {
		test.Errorf("leased_until_ns lost: %+v", after.LeasedUntilNs)
	}

	if !after.PendingTempPath.Valid || after.PendingTempPath.String != ".tusk/staging/abc" {
		test.Errorf("pending_temp_path lost: %+v", after.PendingTempPath)
	}

	if !after.PendingHash.Valid || after.PendingHash.String != "pending-hash" {
		test.Errorf("pending_hash lost: %+v", after.PendingHash)
	}
}

func TestFileStateRepo_TombstoneSetsStateAndExcludesFromGenList(test *testing.T) {
	repo := newTestFileStateRepo(test)

	if upsertErr := repo.Upsert(makeRow("gone.md", 1)); upsertErr != nil {
		test.Fatalf("seed Upsert: %v", upsertErr)
	}

	preTomb, _ := repo.Get("gone.md")

	if tombErr := repo.Tombstone("gone.md"); tombErr != nil {
		test.Fatalf("Tombstone: %v", tombErr)
	}

	after, getErr := repo.Get("gone.md")

	if getErr != nil {
		test.Fatalf("Get after Tombstone: %v", getErr)
	}

	if after.State != index.FileStateTombstone {
		test.Errorf("state = %q, want %q", after.State, index.FileStateTombstone)
	}

	if after.UpdatedAtNs < preTomb.UpdatedAtNs {
		test.Errorf("UpdatedAtNs not bumped: pre=%d post=%d", preTomb.UpdatedAtNs, after.UpdatedAtNs)
	}

	// Tombstones are excluded from reap candidates.
	candidates, listErr := repo.ListByGenLessThan(10)

	if listErr != nil {
		test.Fatalf("ListByGenLessThan: %v", listErr)
	}

	for _, candidate := range candidates {
		if candidate.Path == "gone.md" {
			test.Errorf("tombstone returned by ListByGenLessThan: %+v", candidate)
		}
	}
}

func TestFileStateRepo_TombstoneMissingReturnsNotFound(test *testing.T) {
	repo := newTestFileStateRepo(test)

	tombErr := repo.Tombstone("missing.md")

	if !errors.Is(tombErr, index.ErrFileStateNotFound) {
		test.Errorf("Tombstone missing = %v, want ErrFileStateNotFound", tombErr)
	}
}

func TestFileStateRepo_ListByGenLessThanFiltersAndOrders(test *testing.T) {
	repo := newTestFileStateRepo(test)

	for _, seed := range []index.FileStateRow{
		makeRow("c.md", 1),
		makeRow("a.md", 2),
		makeRow("b.md", 5),
		makeRow("d.md", 5),
	} {
		if upsertErr := repo.Upsert(seed); upsertErr != nil {
			test.Fatalf("seed %s: %v", seed.Path, upsertErr)
		}
	}

	results, listErr := repo.ListByGenLessThan(5)

	if listErr != nil {
		test.Fatalf("ListByGenLessThan: %v", listErr)
	}

	if len(results) != 2 {
		test.Fatalf("len = %d, want 2 (a.md gen=2, c.md gen=1)", len(results))
	}

	if results[0].Path != "a.md" || results[1].Path != "c.md" {
		test.Errorf("order = [%s, %s], want [a.md, c.md]", results[0].Path, results[1].Path)
	}
}

func TestFileStateRepo_ListByGenLessThan_Empty(test *testing.T) {
	repo := newTestFileStateRepo(test)

	results, listErr := repo.ListByGenLessThan(100)

	if listErr != nil {
		test.Fatalf("ListByGenLessThan: %v", listErr)
	}

	if len(results) != 0 {
		test.Errorf("len = %d, want 0", len(results))
	}
}
