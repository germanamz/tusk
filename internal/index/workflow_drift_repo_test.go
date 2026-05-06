package index_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestWorkflowDriftRepo_AppendThenList(test *testing.T) {
	store, closer := newTestIndex(test)
	defer closer()

	repo := index.NewWorkflowDriftRepo(store)

	row := index.WorkflowDriftRow{
		NodeID:         "tickets/foo",
		PackInstance:   "tickets",
		PackKind:       "workflow",
		ObservedStatus: "blocked",
		Property:       "status",
		ObservedAt:     1700_000_000,
	}

	if appendErr := repo.Append(row); appendErr != nil {
		test.Fatalf("Append: %v", appendErr)
	}

	rows, listErr := repo.ListAll()

	if listErr != nil {
		test.Fatalf("ListAll: %v", listErr)
	}

	if len(rows) != 1 || rows[0].NodeID != "tickets/foo" || rows[0].ObservedStatus != "blocked" {
		test.Errorf("ListAll = %+v", rows)
	}
}

func TestWorkflowDriftRepo_AppendIdempotentOnPK(test *testing.T) {
	store, closer := newTestIndex(test)
	defer closer()

	repo := index.NewWorkflowDriftRepo(store)

	row := index.WorkflowDriftRow{
		NodeID: "tickets/foo", PackInstance: "tickets", PackKind: "workflow",
		ObservedStatus: "blocked", Property: "status", ObservedAt: 100,
	}

	for _, observedAt := range []int64{100, 200, 300} {
		row.ObservedAt = observedAt

		if appendErr := repo.Append(row); appendErr != nil {
			test.Fatalf("Append: %v", appendErr)
		}
	}

	rows, _ := repo.ListAll()

	if len(rows) != 1 {
		test.Errorf("ListAll: want 1 row (PK collapsed), got %d", len(rows))
	}

	if rows[0].ObservedAt != 300 {
		test.Errorf("ObservedAt = %d, want most recent (300)", rows[0].ObservedAt)
	}
}

func TestWorkflowDriftRepo_ClearForNode(test *testing.T) {
	store, closer := newTestIndex(test)
	defer closer()

	repo := index.NewWorkflowDriftRepo(store)

	rows := []index.WorkflowDriftRow{
		{NodeID: "tickets/foo", PackInstance: "tickets", PackKind: "workflow", ObservedStatus: "x", Property: "status", ObservedAt: 1},
		{NodeID: "tickets/foo", PackInstance: "tickets", PackKind: "workflow", ObservedStatus: "y", Property: "status", ObservedAt: 2},
		{NodeID: "tickets/bar", PackInstance: "tickets", PackKind: "workflow", ObservedStatus: "z", Property: "status", ObservedAt: 3},
	}

	for _, row := range rows {
		if appendErr := repo.Append(row); appendErr != nil {
			test.Fatalf("Append: %v", appendErr)
		}
	}

	if clearErr := repo.ClearForNode("tickets/foo"); clearErr != nil {
		test.Fatalf("ClearForNode: %v", clearErr)
	}

	remaining, _ := repo.ListAll()

	if len(remaining) != 1 || remaining[0].NodeID != "tickets/bar" {
		test.Errorf("after Clear: remaining = %+v, want only tickets/bar", remaining)
	}
}

func TestWorkflowDriftRepo_CountAll(test *testing.T) {
	store, closer := newTestIndex(test)
	defer closer()

	repo := index.NewWorkflowDriftRepo(store)

	rows := []index.WorkflowDriftRow{
		{NodeID: "a", PackInstance: "p", PackKind: "workflow", ObservedStatus: "x", Property: "status", ObservedAt: 1},
		{NodeID: "b", PackInstance: "p", PackKind: "workflow", ObservedStatus: "y", Property: "status", ObservedAt: 2},
	}

	for _, row := range rows {
		_ = repo.Append(row)
	}

	count, countErr := repo.CountAll()

	if countErr != nil {
		test.Fatalf("CountAll: %v", countErr)
	}

	if count != 2 {
		test.Errorf("CountAll = %d, want 2", count)
	}
}

func newTestIndex(test *testing.T) (*index.Index, func()) {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return store, func() { store.Close() }
}
