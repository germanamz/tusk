package index_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	_ "modernc.org/sqlite"
)

func TestOpen_CreatesSchemaOnFirstOpen(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	tables, queryErr := store.ListTables()

	if queryErr != nil {
		test.Fatalf("ListTables: %v", queryErr)
	}

	requiredTables := []string{"nodes", "manifest_snapshot", "warnings"}

	for _, required := range requiredTables {
		if !contains(tables, required) {
			test.Errorf("missing table %q in %v", required, tables)
		}
	}
}

func TestOpen_IsIdempotent(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	first, firstErr := index.Open(dbPath)

	if firstErr != nil {
		test.Fatalf("first Open: %v", firstErr)
	}

	first.Close()

	second, secondErr := index.Open(dbPath)

	if secondErr != nil {
		test.Fatalf("second Open: %v", secondErr)
	}

	second.Close()
}

func TestOpen_CreatesEdgesTable(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	tables, queryErr := store.ListTables()

	if queryErr != nil {
		test.Fatalf("ListTables: %v", queryErr)
	}

	if !contains(tables, "edges") {
		test.Errorf("missing edges table in %v", tables)
	}
}

func TestOpen_CreatesEmbeddingsAndQueueTables(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	tables, queryErr := store.ListTables()

	if queryErr != nil {
		test.Fatalf("ListTables: %v", queryErr)
	}

	for _, required := range []string{"embeddings", "embed_queue"} {
		if !contains(tables, required) {
			test.Errorf("missing table %q in %v", required, tables)
		}
	}
}

func TestOpen_CreatesMetaTable(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	tables, queryErr := store.ListTables()

	if queryErr != nil {
		test.Fatalf("ListTables: %v", queryErr)
	}

	if !contains(tables, "meta") {
		test.Errorf("missing table %q in %v", "meta", tables)
	}
}

func TestOpen_CreatesWorkflowDriftTable(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	tables, listErr := store.ListTables()

	if listErr != nil {
		test.Fatalf("ListTables: %v", listErr)
	}

	if !contains(tables, "workflow_drift") {
		test.Errorf("missing table %q in %v", "workflow_drift", tables)
	}
}

func TestOpen_CreatesPropertyDriftTable(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	tables, listErr := store.ListTables()

	if listErr != nil {
		test.Fatalf("ListTables: %v", listErr)
	}

	if !contains(tables, "property_drift") {
		test.Errorf("missing table %q in %v", "property_drift", tables)
	}
}

func TestOpen_DropsLegacyOrdinalColumn(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.db")

	rawDB, openErr := sql.Open("sqlite", path)

	if openErr != nil {
		test.Fatalf("seed open: %v", openErr)
	}

	legacySchema := `
CREATE TABLE edges (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	type        TEXT NOT NULL,
	source_id   TEXT NOT NULL,
	target_id   TEXT NOT NULL,
	ordinal     INTEGER NOT NULL DEFAULT 0,
	source_path TEXT NOT NULL,
	UNIQUE(type, source_id, target_id, ordinal)
);
INSERT INTO edges (type, source_id, target_id, ordinal, source_path)
VALUES ('blocks', 'a', 'b', 0, 'a.md'),
       ('blocks', 'a', 'b', 1, 'a.md');
`

	if _, execErr := rawDB.Exec(legacySchema); execErr != nil {
		test.Fatalf("seed schema: %v", execErr)
	}

	rawDB.Close()

	idx, reopenErr := index.Open(path)

	if reopenErr != nil {
		test.Fatalf("open: %v", reopenErr)
	}

	defer idx.Close()

	var count int

	if scanErr := idx.DB().QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&count); scanErr != nil {
		test.Fatalf("count: %v", scanErr)
	}

	if count != 1 {
		test.Errorf("expected 1 deduped row after migration, got %d", count)
	}

	cols, colsErr := idx.DB().Query(`PRAGMA table_info(edges)`)

	if colsErr != nil {
		test.Fatalf("table_info: %v", colsErr)
	}

	defer cols.Close()

	for cols.Next() {
		var cid int
		var name, columnType string
		var notNull, pk int
		var dfltValue sql.NullString

		if scanErr := cols.Scan(&cid, &name, &columnType, &notNull, &dfltValue, &pk); scanErr != nil {
			test.Fatalf("scan column: %v", scanErr)
		}

		if name == "ordinal" {
			test.Errorf("ordinal column should have been dropped")
		}
	}

	indexRows, indexQueryErr := idx.DB().Query(
		`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='edges'`,
	)

	if indexQueryErr != nil {
		test.Fatalf("query indexes: %v", indexQueryErr)
	}

	defer indexRows.Close()

	var indexNames []string

	for indexRows.Next() {
		var indexName string

		if scanErr := indexRows.Scan(&indexName); scanErr != nil {
			test.Fatalf("scan index name: %v", scanErr)
		}

		indexNames = append(indexNames, indexName)
	}

	if iterErr := indexRows.Err(); iterErr != nil {
		test.Fatalf("indexes iteration: %v", iterErr)
	}

	for _, required := range []string{
		"edges_source_idx", "edges_target_idx",
		"edges_type_idx", "edges_source_path_idx",
	} {
		if !contains(indexNames, required) {
			test.Errorf("missing index %q after migration; found %v", required, indexNames)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}

	return false
}
