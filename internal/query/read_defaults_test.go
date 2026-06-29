package query_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/query"
)

// seedNotes upserts count note file rows so the structural read paths have a
// pool larger than any default cap under test.
func seedNotes(test *testing.T, store *index.Index, count int) {
	test.Helper()

	nodes := index.NewNodeRepo(store)

	for offset := 0; offset < count; offset++ {
		id := fmt.Sprintf("notes/n%02d", offset)

		if upsertErr := nodes.Upsert(index.NodeRow{
			ID:             id,
			Type:           "note",
			Path:           id + ".md",
			Title:          id,
			PropertiesJSON: "{}",
			LastChecksum:   "x",
		}); upsertErr != nil {
			test.Fatalf("upsert %s: %v", id, upsertErr)
		}
	}
}

// TestQueryRun_StructuralDefaultTakeCaps pins E8: a non-zero StructuralDefaultTake
// caps the structural-only path when Take is unset, while 0 leaves it uncapped
// (the CLI's "return every row" contract).
func TestQueryRun_StructuralDefaultTakeCaps(test *testing.T) {
	store := openTestStore(test)
	seedNotes(test, store, 15)

	deps := query.Deps{
		Database: store.DB(),
		Manifest: loadManifestWithSubUnits(test),
	}

	capped, capErr := query.Run(context.Background(), deps, query.Request{
		Filter:                "type=note",
		StructuralDefaultTake: 10,
	})

	if capErr != nil {
		test.Fatalf("capped run: %v", capErr)
	}

	if len(capped.Rows) != 10 {
		test.Errorf("capped len = %d, want 10", len(capped.Rows))
	}

	uncapped, uncapErr := query.Run(context.Background(), deps, query.Request{
		Filter:                "type=note",
		StructuralDefaultTake: 0,
	})

	if uncapErr != nil {
		test.Fatalf("uncapped run: %v", uncapErr)
	}

	if len(uncapped.Rows) != 15 {
		test.Errorf("uncapped len = %d, want 15 (no cap)", len(uncapped.Rows))
	}
}

// TestQueryRun_SemanticSkipRequiresTake pins the unified contract: the semantic
// path now errors on skip-without-effective-take instead of silently dropping
// the skip (the CLI bug). With a default take the skip is honored, so the guard
// passes and the call proceeds to the (here unconfigured) embedder check.
func TestQueryRun_SemanticSkipRequiresTake(test *testing.T) {
	store := openTestStore(test)
	seedNotes(test, store, 5)

	deps := query.Deps{
		Database: store.DB(),
		Manifest: loadManifestWithSubUnits(test),
	}

	_, skipErr := query.Run(context.Background(), deps, query.Request{
		Filter:              "type=note",
		Semantic:            "anything",
		Skip:                2,
		SemanticDefaultTake: 0,
	})

	if !errors.Is(skipErr, filter.ErrSkipRequiresTake) {
		test.Errorf("skip-without-take err = %v, want ErrSkipRequiresTake", skipErr)
	}

	_, defaultErr := query.Run(context.Background(), deps, query.Request{
		Filter:              "type=note",
		Semantic:            "anything",
		Skip:                2,
		SemanticDefaultTake: 10,
	})

	if !errors.Is(defaultErr, query.ErrSemanticUnavailable) {
		test.Errorf("skip-with-default err = %v, want ErrSemanticUnavailable (guard passed, no embedder)", defaultErr)
	}
}

// TestListRun_StructuralDefaultTakeCaps mirrors the cap for the node-list path,
// and confirms skip-without-take still errors when uncapped (CLI contract).
func TestListRun_StructuralDefaultTakeCaps(test *testing.T) {
	store := openTestStore(test)
	seedNotes(test, store, 15)
	loadedManifest := loadManifestWithSubUnits(test)

	capped, capErr := query.ListRun(store.DB(), loadedManifest, query.ListRequest{
		Filter:                "type=note",
		StructuralDefaultTake: 10,
	})

	if capErr != nil {
		test.Fatalf("capped list: %v", capErr)
	}

	if len(capped.Rows) != 10 {
		test.Errorf("capped len = %d, want 10", len(capped.Rows))
	}

	uncapped, uncapErr := query.ListRun(store.DB(), loadedManifest, query.ListRequest{
		Filter:                "type=note",
		StructuralDefaultTake: 0,
	})

	if uncapErr != nil {
		test.Fatalf("uncapped list: %v", uncapErr)
	}

	if len(uncapped.Rows) != 15 {
		test.Errorf("uncapped len = %d, want 15 (no cap)", len(uncapped.Rows))
	}

	_, skipErr := query.ListRun(store.DB(), loadedManifest, query.ListRequest{
		Filter:                "type=note",
		Skip:                  2,
		StructuralDefaultTake: 0,
	})

	if !errors.Is(skipErr, filter.ErrSkipRequiresTake) {
		test.Errorf("uncapped skip-without-take err = %v, want ErrSkipRequiresTake", skipErr)
	}
}
