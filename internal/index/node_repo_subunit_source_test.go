package index_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestBulkUpsert_StampsSuppliedSourceOnSubUnitRows(test *testing.T) {
	test.Parallel()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))
	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}
	defer store.Close()

	repo := index.NewNodeRepo(store)

	file := index.NodeRow{
		ID: "notes/page.html", Type: "note", Path: "notes/page.html",
		Title: "Page", PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "h",
	}
	if upsertErr := repo.Upsert(file); upsertErr != nil {
		test.Fatalf("Upsert file: %v", upsertErr)
	}

	sub := index.NodeRow{
		ID: "notes/page.html#P0", Type: "paragraph", Path: "notes/page.html",
		Title: "p", PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "h",
		ParentID:     sql.NullString{String: "notes/page.html", Valid: true},
		Ordinal:      sql.NullInt64{Int64: 0, Valid: true},
		EmbedPayload: sql.NullString{String: "p", Valid: true},
		ContentHash:  sql.NullString{String: "abc", Valid: true},
	}

	if upsertErr := repo.BulkUpsert([]index.NodeRow{sub}, "html"); upsertErr != nil {
		test.Fatalf("BulkUpsert: %v", upsertErr)
	}

	var source string
	if scanErr := store.DB().QueryRow(`SELECT source FROM nodes WHERE id = ?`, sub.ID).Scan(&source); scanErr != nil {
		test.Fatalf("scan source: %v", scanErr)
	}

	if source != "html" {
		test.Errorf("nodes.source = %q, want %q", source, "html")
	}
}
