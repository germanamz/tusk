package index_test

import (
	"database/sql"
	"path/filepath"
	"slices"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestPathRefs_ReplaceAndRead(test *testing.T) {
	store := openTestIndex(test)
	seedNodes(test, store, "technical/ledger", "technical/arch")

	edges := index.NewEdgeRepo(store)

	ledger := []index.PathRefRow{
		{Type: "describes", Target: "server/ledger/core/service.go", Line: 41},
		{Type: "describes", Target: "server/ledger/core/service.go", Line: 40},
		{Type: "describes", Target: "server/ledger/core/service.go", Line: 40},
		{Type: "describes", Target: "server/wire.go", Line: 12},
	}

	if replaceErr := edges.ReplacePathRefs("technical/ledger", ledger); replaceErr != nil {
		test.Fatalf("ReplacePathRefs: %v", replaceErr)
	}

	arch := []index.PathRefRow{{Type: "describes", Target: "server/ledger", Line: 3}}

	if replaceErr := edges.ReplacePathRefs("technical/arch", arch); replaceErr != nil {
		test.Fatalf("ReplacePathRefs: %v", replaceErr)
	}

	from, fromErr := edges.PathRefsFrom([]string{"technical/ledger"})

	if fromErr != nil {
		test.Fatalf("PathRefsFrom: %v", fromErr)
	}

	wantFrom := []index.PathRefRow{
		{SourceID: "technical/ledger", Type: "describes", Target: "server/wire.go", Line: 12},
		{SourceID: "technical/ledger", Type: "describes", Target: "server/ledger/core/service.go", Line: 40},
		{SourceID: "technical/ledger", Type: "describes", Target: "server/ledger/core/service.go", Line: 41},
	}

	if !slices.Equal(from, wantFrom) {
		test.Errorf("PathRefsFrom =\n%v\nwant\n%v", from, wantFrom)
	}

	to, toErr := edges.PathRefsTo([]string{"server/ledger/core/service.go", "server/ledger", "server"})

	if toErr != nil {
		test.Fatalf("PathRefsTo: %v", toErr)
	}

	if len(to) != 3 || to[0].SourceID != "technical/arch" {
		test.Errorf("PathRefsTo = %v, want arch's ref then ledger's two", to)
	}

	all, allErr := edges.AllPathRefs()

	if allErr != nil {
		test.Fatalf("AllPathRefs: %v", allErr)
	}

	if len(all) != 4 {
		test.Errorf("AllPathRefs has %d rows, want 4", len(all))
	}

	if replaceErr := edges.ReplacePathRefs("technical/ledger", nil); replaceErr != nil {
		test.Fatalf("ReplacePathRefs(nil): %v", replaceErr)
	}

	if after, _ := edges.PathRefsFrom([]string{"technical/ledger"}); len(after) != 0 {
		test.Errorf("refs after clearing = %v, want none", after)
	}
}

func TestPathRefs_CascadeWithTheirPage(test *testing.T) {
	store := openTestIndex(test)
	seedNodes(test, store, "technical/ledger")

	edges := index.NewEdgeRepo(store)

	if replaceErr := edges.ReplacePathRefs("technical/ledger", []index.PathRefRow{{Type: "describes", Target: "a/b.go", Line: 1}}); replaceErr != nil {
		test.Fatalf("ReplacePathRefs: %v", replaceErr)
	}

	// Re-upserting the page keeps its refs: the upsert updates in place.
	seedNodes(test, store, "technical/ledger")

	if kept, _ := edges.AllPathRefs(); len(kept) != 1 {
		test.Fatalf("refs after re-upsert = %v, want 1", kept)
	}

	if deleteErr := index.NewNodeRepo(store).DeleteByPath("technical/ledger.md"); deleteErr != nil {
		test.Fatalf("Delete: %v", deleteErr)
	}

	if left, _ := edges.AllPathRefs(); len(left) != 0 {
		test.Errorf("refs after deleting the page = %v, want none", left)
	}
}

func TestPathRefs_TableAppearsOnAnExistingIndex(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	// Simulate an index written before path refs existed.
	if _, dropErr := store.DB().Exec(`DROP TABLE path_refs`); dropErr != nil {
		test.Fatalf("drop: %v", dropErr)
	}

	store.Close()

	reopened, reopenErr := index.Open(dbPath)

	if reopenErr != nil {
		test.Fatalf("reopen: %v", reopenErr)
	}

	defer reopened.Close()

	var name string

	scanErr := reopened.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='path_refs'`).Scan(&name)

	if scanErr == sql.ErrNoRows {
		test.Fatalf("path_refs missing after reopening an older index")
	}

	if scanErr != nil {
		test.Fatalf("probe: %v", scanErr)
	}
}
