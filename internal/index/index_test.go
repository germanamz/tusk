package index_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
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
