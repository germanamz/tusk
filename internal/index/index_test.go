package index_test

import (
	"path/filepath"
	"strings"
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

	requiredTables := []string{"nodes", "edges", "embeddings", "meta"}

	for _, required := range requiredTables {
		if !contains(tables, required) {
			test.Errorf("missing table %q in %v", required, tables)
		}
	}

	// manifest_snapshot and warnings were retired (never read); the Open
	// migration drops them, so a fresh DB must not carry them.
	for _, retired := range []string{"manifest_snapshot", "warnings"} {
		if contains(tables, retired) {
			test.Errorf("retired table %q still present in %v", retired, tables)
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

// TestOpen_SynchronousIsNormal asserts the index opens with
// synchronous=NORMAL (1). The index is a rebuildable cache, so NORMAL (which
// in WAL mode fsyncs only at checkpoint) is the right durability/throughput
// trade — FULL (2) fsyncs every one of the pipelines' many tiny commits.
func TestOpen_SynchronousIsNormal(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	var synchronous int

	if queryErr := store.DB().QueryRow("PRAGMA synchronous").Scan(&synchronous); queryErr != nil {
		test.Fatalf("PRAGMA synchronous: %v", queryErr)
	}

	if synchronous != 1 {
		test.Errorf("PRAGMA synchronous = %d, want 1 (NORMAL)", synchronous)
	}
}

// TestOpen_TypeIndexServesTypeFilter asserts a bare `type = ?` filter (the most
// common structural predicate) is served by nodes_type_idx rather than scanning
// TestOpen_DropsUnusedFileStateLease pins B3: the never-read
// idx_file_state_lease partial index (its sweeper was never built) is dropped
// by the migration on every Open. The still-used idx_file_state_seen must
// survive.
func TestOpen_DropsUnusedFileStateLease(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	var leaseCount int

	if scanErr := store.DB().QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_file_state_lease'`,
	).Scan(&leaseCount); scanErr != nil {
		test.Fatalf("query sqlite_master: %v", scanErr)
	}

	if leaseCount != 0 {
		test.Errorf("idx_file_state_lease should be dropped by migration; found %d", leaseCount)
	}

	var seenCount int

	if scanErr := store.DB().QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_file_state_seen'`,
	).Scan(&seenCount); scanErr != nil {
		test.Fatalf("query sqlite_master: %v", scanErr)
	}

	if seenCount != 1 {
		test.Errorf("idx_file_state_seen must survive (it is used by the orphan reaper); found %d", seenCount)
	}
}

// TestOpen_DropsExistingFileStateLeaseOnUpgrade simulates a database created
// before B3 (the index present) and confirms re-opening with the migration
// reclaims it — the real-world upgrade path, not just a fresh open.
func TestOpen_DropsExistingFileStateLeaseOnUpgrade(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	first, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	// Recreate the legacy index as a pre-B3 database would have it.
	if _, execErr := first.DB().Exec(
		`CREATE INDEX IF NOT EXISTS idx_file_state_lease ON file_state(leased_until_ns) WHERE leased_by IS NOT NULL`,
	); execErr != nil {
		test.Fatalf("recreate legacy index: %v", execErr)
	}

	first.Close()

	reopened, reopenErr := index.Open(dbPath)

	if reopenErr != nil {
		test.Fatalf("reopen: %v", reopenErr)
	}

	defer reopened.Close()

	var leaseCount int

	if scanErr := reopened.DB().QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_file_state_lease'`,
	).Scan(&leaseCount); scanErr != nil {
		test.Fatalf("query sqlite_master: %v", scanErr)
	}

	if leaseCount != 0 {
		test.Errorf("re-open must drop a pre-existing idx_file_state_lease; found %d", leaseCount)
	}
}

// the table. nodes_kind_type_idx leads with the 2-value kind column, so it
// can't serve a type-only predicate.
func TestOpen_TypeIndexServesTypeFilter(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	var indexName string

	if scanErr := store.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name='nodes_type_idx'`,
	).Scan(&indexName); scanErr != nil {
		test.Fatalf("nodes_type_idx missing: %v", scanErr)
	}

	rows, queryErr := store.DB().Query(`EXPLAIN QUERY PLAN SELECT id FROM nodes WHERE type = ?`, "note")

	if queryErr != nil {
		test.Fatalf("EXPLAIN: %v", queryErr)
	}

	defer rows.Close()

	var plan strings.Builder

	for rows.Next() {
		var selectID, order, from int

		var detail string

		if scanErr := rows.Scan(&selectID, &order, &from, &detail); scanErr != nil {
			test.Fatalf("scan plan: %v", scanErr)
		}

		plan.WriteString(detail)
		plan.WriteString("\n")
	}

	if !strings.Contains(plan.String(), "nodes_type_idx") {
		test.Errorf("type=? plan does not use nodes_type_idx:\n%s", plan.String())
	}
}
