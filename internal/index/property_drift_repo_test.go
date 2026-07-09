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

func TestPropertyDriftRepo_ListRefKinds(test *testing.T) {
	store, closer := newTestIndexForPropertyDrift(test)
	defer closer()

	repo := index.NewPropertyDriftRepo(store)

	rows := []index.PropertyDriftRow{
		{NodeID: "tickets/foo", NodeType: "ticket", Kind: "ref_dangling", Property: "assignee", ObservedAt: 1},
		{NodeID: "tickets/foo", NodeType: "ticket", Kind: "undeclared-property", Property: "x", ObservedAt: 2},
		{NodeID: "tickets/bar", NodeType: "ticket", Kind: "ref_ambiguous", Property: "epic", ObservedAt: 3},
		{NodeID: "tickets/baz", NodeType: "ticket", Kind: "ref_type_mismatch", Property: "epic", ObservedAt: 4},
		{NodeID: "tickets/qux", NodeType: "ticket", Kind: "ref_cycle", Property: "parent", ObservedAt: 5},
		{NodeID: "tickets/quo", NodeType: "ticket", Kind: "type-mismatch", Property: "y", ObservedAt: 6},
	}

	for _, row := range rows {
		if appendErr := repo.Append(row); appendErr != nil {
			test.Fatalf("Append: %v", appendErr)
		}
	}

	refRows, listErr := repo.ListRefKinds()

	if listErr != nil {
		test.Fatalf("ListRefKinds: %v", listErr)
	}

	if len(refRows) != 4 {
		test.Fatalf("ListRefKinds returned %d rows, want the 4 ref kinds: %+v", len(refRows), refRows)
	}

	for _, row := range refRows {
		if row.Kind == "undeclared-property" || row.Kind == "type-mismatch" {
			test.Errorf("non-ref kind leaked into ListRefKinds: %+v", row)
		}
	}
}

func TestPropertyDriftRepo_ClearRefKindsForNode(test *testing.T) {
	store, closer := newTestIndexForPropertyDrift(test)
	defer closer()

	repo := index.NewPropertyDriftRepo(store)

	rows := []index.PropertyDriftRow{
		{NodeID: "tickets/foo", NodeType: "ticket", Kind: "ref_dangling", Property: "assignee", ObservedAt: 1},
		{NodeID: "tickets/foo", NodeType: "ticket", Kind: "ref_cycle", Property: "parent", ObservedAt: 2},
		{NodeID: "tickets/foo", NodeType: "ticket", Kind: "undeclared-property", Property: "x", ObservedAt: 3},
		{NodeID: "tickets/bar", NodeType: "ticket", Kind: "ref_dangling", Property: "assignee", ObservedAt: 4},
	}

	for _, row := range rows {
		if appendErr := repo.Append(row); appendErr != nil {
			test.Fatalf("Append: %v", appendErr)
		}
	}

	if clearErr := repo.ClearRefKindsForNode("tickets/foo"); clearErr != nil {
		test.Fatalf("ClearRefKindsForNode: %v", clearErr)
	}

	remaining, _ := repo.ListAll()

	if len(remaining) != 2 {
		test.Fatalf("remaining = %+v, want foo's undeclared-property and bar's ref_dangling", remaining)
	}

	for _, row := range remaining {
		if row.NodeID == "tickets/foo" && row.Kind != "undeclared-property" {
			test.Errorf("foo kept a ref kind: %+v", row)
		}

		if row.NodeID == "tickets/bar" && row.Kind != "ref_dangling" {
			test.Errorf("bar's ref row was wrongly cleared: %+v", row)
		}
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
