package index_test

import (
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

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}

	return false
}

// TestOpen_CascadeDeleteEdgesAndEmbeddings asserts the foreign keys
// added in the P2 migration actually cascade when a node row is
// deleted. The PRAGMA foreign_keys = ON setting in Open is load-bearing
// here — if it slips off, the FKs silently no-op.
func TestOpen_CascadeDeleteEdgesAndEmbeddingMappings(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	idx, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer idx.Close()

	if _, execErr := idx.DB().Exec(`
		INSERT INTO nodes (id, type, path, title, properties_json,
		                   last_mtime, last_size, last_checksum,
		                   kind, source)
		VALUES ('src', 'note', 'src.md', 'src', '{}', 0, 0, 'h', 'file', NULL),
		       ('tgt', 'note', 'tgt.md', 'tgt', '{}', 0, 0, 'h', 'file', NULL);
	`); execErr != nil {
		test.Fatalf("seed nodes: %v", execErr)
	}

	if _, execErr := idx.DB().Exec(`
		INSERT INTO edges (type, source_id, target_id, source_path, kind)
		VALUES ('links', 'src', 'tgt', 'src.md', 'direct');
	`); execErr != nil {
		test.Fatalf("seed edge: %v", execErr)
	}

	if _, execErr := idx.DB().Exec(`
		INSERT INTO embeddings (content_hash, model, vector, dim, body)
		VALUES ('h', 'm', x'00000000', 1, 'b');
	`); execErr != nil {
		test.Fatalf("seed embedding: %v", execErr)
	}

	if _, execErr := idx.DB().Exec(`
		INSERT INTO node_embeddings (node_id, chunk_idx, content_hash, model)
		VALUES ('src', 0, 'h', 'm');
	`); execErr != nil {
		test.Fatalf("seed node_embedding mapping: %v", execErr)
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

	var mapCount int

	if scanErr := idx.DB().QueryRow(`SELECT COUNT(*) FROM node_embeddings WHERE node_id = 'src'`).Scan(&mapCount); scanErr != nil {
		test.Fatalf("count node_embeddings: %v", scanErr)
	}

	if mapCount != 0 {
		test.Errorf("expected cascade-delete to remove node_embeddings mappings; remaining = %d", mapCount)
	}

	// The shared vector is content-scoped: it survives node deletion and is
	// reclaimed by GCOrphanVectors, not by the FK cascade.
	var vecCount int

	if scanErr := idx.DB().QueryRow(`SELECT COUNT(*) FROM embeddings WHERE content_hash = 'h'`).Scan(&vecCount); scanErr != nil {
		test.Fatalf("count embeddings: %v", scanErr)
	}

	if vecCount != 1 {
		test.Errorf("expected shared vector to survive cascade; remaining = %d", vecCount)
	}
}
