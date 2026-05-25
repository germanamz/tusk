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

// legacyPreP2Schema is the schema as it existed on `main` before the P2
// migration. Used to seed a "legacy" DB so we can verify the new Open
// upgrades it idempotently without losing data.
//
// The shape is the union of the pre-P2 schema across all `internal/index`
// tables that the P2 migration touches (nodes, edges, embeddings). It is
// kept here (rather than reading from git) so the test is self-contained
// and survives future schema edits.
const legacyPreP2Schema = `
CREATE TABLE nodes (
    id              TEXT PRIMARY KEY,
    type            TEXT NOT NULL,
    path            TEXT NOT NULL UNIQUE,
    title           TEXT,
    properties_json TEXT NOT NULL DEFAULT '{}',
    last_mtime      INTEGER NOT NULL,
    last_size       INTEGER NOT NULL,
    last_checksum   TEXT NOT NULL
);
CREATE TABLE edges (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    type        TEXT NOT NULL,
    source_id   TEXT NOT NULL,
    target_id   TEXT NOT NULL,
    source_path TEXT NOT NULL,
    UNIQUE(type, source_id, target_id, source_path)
);
CREATE TABLE embeddings (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id      TEXT NOT NULL,
    chunk_idx    INTEGER NOT NULL DEFAULT 0,
    model        TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    vector       BLOB NOT NULL,
    dim          INTEGER NOT NULL,
    body         TEXT NOT NULL DEFAULT '',
    UNIQUE(node_id, chunk_idx)
);
`

// TestOpen_P2MigrationFromLegacyDB seeds a database with the pre-P2
// schema, inserts representative rows, opens it through the new Open,
// and asserts the migration ran without dropping data. Then opens the
// same DB a second time and asserts the second open is a no-op
// (idempotency).
func TestOpen_P2MigrationFromLegacyDB(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.db")

	rawDB, openErr := sql.Open("sqlite", path)

	if openErr != nil {
		test.Fatalf("seed open: %v", openErr)
	}

	if _, execErr := rawDB.Exec(legacyPreP2Schema); execErr != nil {
		test.Fatalf("seed schema: %v", execErr)
	}

	seed := `
INSERT INTO nodes (id, type, path, title, properties_json, last_mtime, last_size, last_checksum)
VALUES ('notes/a', 'note', 'notes/a.md', 'A', '{}', 0, 0, 'h'),
       ('notes/b', 'note', 'notes/b.md', 'B', '{}', 0, 0, 'h');
INSERT INTO edges (type, source_id, target_id, source_path)
VALUES ('links', 'notes/a', 'notes/b', 'notes/a.md');
INSERT INTO embeddings (node_id, chunk_idx, model, content_hash, vector, dim, body)
VALUES ('notes/a', 0, 'm', 'h0', x'00000000', 1, 'first'),
       ('notes/a', 1, 'm', 'h1', x'00000000', 1, 'second');
`

	if _, execErr := rawDB.Exec(seed); execErr != nil {
		test.Fatalf("seed rows: %v", execErr)
	}

	rawDB.Close()

	idx, openErr := index.Open(path)

	if openErr != nil {
		test.Fatalf("first Open: %v", openErr)
	}

	// Verify nodes gained the P2 columns.
	cols, colsErr := idx.DB().Query(`PRAGMA table_info(nodes)`)

	if colsErr != nil {
		test.Fatalf("table_info nodes: %v", colsErr)
	}

	have := map[string]bool{}

	for cols.Next() {
		var cid int
		var name, columnType string
		var notNull, pk int
		var dfltValue sql.NullString

		if scanErr := cols.Scan(&cid, &name, &columnType, &notNull, &dfltValue, &pk); scanErr != nil {
			test.Fatalf("scan: %v", scanErr)
		}

		have[name] = true
	}

	cols.Close()

	for _, required := range []string{"parent_id", "ordinal", "embed_payload"} {
		if !have[required] {
			test.Errorf("missing column %q on nodes after migration", required)
		}
	}

	// Verify the composite index exists.
	var indexName string

	idxErr := idx.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name='nodes_parent_id_ordinal'`,
	).Scan(&indexName)

	if idxErr != nil {
		test.Errorf("nodes_parent_id_ordinal index missing: %v", idxErr)
	}

	// Verify the embeddings FK to nodes exists and only one row per
	// node survived (the lowest chunk_idx).
	var embeddingCount int

	if scanErr := idx.DB().QueryRow(`SELECT COUNT(*) FROM embeddings WHERE node_id = 'notes/a'`).Scan(&embeddingCount); scanErr != nil {
		test.Fatalf("count embeddings: %v", scanErr)
	}

	if embeddingCount != 1 {
		test.Errorf("embeddings rows for notes/a = %d, want 1 after collapse", embeddingCount)
	}

	// Verify the edges FK to nodes exists.
	fkRows, fkErr := idx.DB().Query(`PRAGMA foreign_key_list(edges)`)

	if fkErr != nil {
		test.Fatalf("edges fk_list: %v", fkErr)
	}

	hasSourceFK := false

	for fkRows.Next() {
		var id, seq int
		var table, from, to, onUpdate, onDelete, match string

		if scanErr := fkRows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); scanErr != nil {
			test.Fatalf("scan fk: %v", scanErr)
		}

		if table == "nodes" && from == "source_id" {
			hasSourceFK = true
		}
	}

	fkRows.Close()

	if !hasSourceFK {
		test.Errorf("edges.source_id FK to nodes(id) missing after migration")
	}

	// Pre-existing edge row should still be present (notes/a → notes/b
	// has both endpoints in nodes).
	var edgeCount int

	if scanErr := idx.DB().QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&edgeCount); scanErr != nil {
		test.Fatalf("count edges: %v", scanErr)
	}

	if edgeCount != 1 {
		test.Errorf("edges count = %d, want 1", edgeCount)
	}

	idx.Close()

	// Re-open: second open should be a no-op.
	idx2, openErr2 := index.Open(path)

	if openErr2 != nil {
		test.Fatalf("second Open: %v", openErr2)
	}

	defer idx2.Close()

	var secondCount int

	if scanErr := idx2.DB().QueryRow(`SELECT COUNT(*) FROM embeddings`).Scan(&secondCount); scanErr != nil {
		test.Fatalf("count embeddings second: %v", scanErr)
	}

	if secondCount != 1 {
		test.Errorf("second open changed embeddings count: %d", secondCount)
	}
}

// TestOpen_CascadeDeleteEdgesAndEmbeddings asserts the foreign keys
// added in the P2 migration actually cascade when a node row is
// deleted. The PRAGMA foreign_keys = ON setting in Open is load-bearing
// here — if it slips off, the FKs silently no-op.
func TestOpen_CascadeDeleteEdgesAndEmbeddings(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	idx, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer idx.Close()

	if _, execErr := idx.DB().Exec(`
		INSERT INTO nodes (id, type, path, title, properties_json, last_mtime, last_size, last_checksum)
		VALUES ('src', 'note', 'src.md', 'src', '{}', 0, 0, 'h'),
		       ('tgt', 'note', 'tgt.md', 'tgt', '{}', 0, 0, 'h');
	`); execErr != nil {
		test.Fatalf("seed nodes: %v", execErr)
	}

	if _, execErr := idx.DB().Exec(`
		INSERT INTO edges (type, source_id, target_id, source_path)
		VALUES ('links', 'src', 'tgt', 'src.md');
	`); execErr != nil {
		test.Fatalf("seed edge: %v", execErr)
	}

	if _, execErr := idx.DB().Exec(`
		INSERT INTO embeddings (node_id, chunk_idx, model, content_hash, vector, dim, body)
		VALUES ('src', 0, 'm', 'h', x'00000000', 1, 'b');
	`); execErr != nil {
		test.Fatalf("seed embedding: %v", execErr)
	}

	if _, execErr := idx.DB().Exec(`DELETE FROM nodes WHERE id = 'src'`); execErr != nil {
		test.Fatalf("delete node: %v", execErr)
	}

	var edgeCount int

	if scanErr := idx.DB().QueryRow(`SELECT COUNT(*) FROM edges WHERE source_id = 'src'`).Scan(&edgeCount); scanErr != nil {
		test.Fatalf("count edges: %v", scanErr)
	}

	if edgeCount != 0 {
		test.Errorf("expected cascade-delete to remove edges; remaining = %d", edgeCount)
	}

	var embedCount int

	if scanErr := idx.DB().QueryRow(`SELECT COUNT(*) FROM embeddings WHERE node_id = 'src'`).Scan(&embedCount); scanErr != nil {
		test.Fatalf("count embeddings: %v", scanErr)
	}

	if embedCount != 0 {
		test.Errorf("expected cascade-delete to remove embeddings; remaining = %d", embedCount)
	}
}
