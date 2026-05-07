package index_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestPropertyDriftRepo_AppendThenList(test *testing.T) {
	store, closer := newTestIndexForPropertyDrift(test)
	defer closer()

	repo := index.NewPropertyDriftRepo(store)

	row := index.PropertyDriftRow{
		NodeID:     "tickets/foo",
		NodeType:   "ticket",
		Kind:       "undeclared-property",
		Property:   "assignee",
		Details:    "not declared on type \"ticket\"",
		ObservedAt: 1700_000_000,
	}

	if appendErr := repo.Append(row); appendErr != nil {
		test.Fatalf("Append: %v", appendErr)
	}

	rows, listErr := repo.ListAll()

	if listErr != nil {
		test.Fatalf("ListAll: %v", listErr)
	}

	if len(rows) != 1 || rows[0].NodeID != "tickets/foo" || rows[0].Property != "assignee" {
		test.Errorf("ListAll = %+v", rows)
	}
}

func TestPropertyDriftRepo_AppendIdempotentOnPK(test *testing.T) {
	store, closer := newTestIndexForPropertyDrift(test)
	defer closer()

	repo := index.NewPropertyDriftRepo(store)

	row := index.PropertyDriftRow{
		NodeID:     "tickets/foo",
		NodeType:   "ticket",
		Kind:       "type-mismatch",
		Property:   "priority",
		Details:    "value \"high\"",
		ObservedAt: 100,
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

func TestPropertyDriftRepo_ClearForNode(test *testing.T) {
	store, closer := newTestIndexForPropertyDrift(test)
	defer closer()

	repo := index.NewPropertyDriftRepo(store)

	rows := []index.PropertyDriftRow{
		{NodeID: "tickets/foo", NodeType: "ticket", Kind: "undeclared-property", Property: "x", ObservedAt: 1},
		{NodeID: "tickets/foo", NodeType: "ticket", Kind: "type-mismatch", Property: "y", ObservedAt: 2},
		{NodeID: "tickets/bar", NodeType: "ticket", Kind: "undeclared-property", Property: "z", ObservedAt: 3},
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

func TestPropertyDriftRepo_CountAll(test *testing.T) {
	store, closer := newTestIndexForPropertyDrift(test)
	defer closer()

	repo := index.NewPropertyDriftRepo(store)

	rows := []index.PropertyDriftRow{
		{NodeID: "a", NodeType: "ticket", Kind: "type-mismatch", Property: "x", ObservedAt: 1},
		{NodeID: "b", NodeType: "ticket", Kind: "type-mismatch", Property: "y", ObservedAt: 2},
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

func newTestIndexForPropertyDrift(test *testing.T) (*index.Index, func()) {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return store, func() { store.Close() }
}
