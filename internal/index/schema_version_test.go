package index_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestSchemaVersionConstantNonEmpty(test *testing.T) {
	test.Parallel()

	if index.SchemaVersion == "" {
		test.Fatal("index.SchemaVersion must be a non-empty identifier")
	}
}

func TestMetaSchemaVersionKey(test *testing.T) {
	test.Parallel()

	if index.MetaSchemaVersionKey != "schema_version" {
		test.Fatalf("MetaSchemaVersionKey = %q, want %q", index.MetaSchemaVersionKey, "schema_version")
	}
}

func TestOpenWritesSchemaVersion(test *testing.T) {
	test.Parallel()

	dir := test.TempDir()
	store, openErr := index.Open(filepath.Join(dir, "tusk.db"))
	if openErr != nil {
		test.Fatalf("index.Open: %v", openErr)
	}
	defer store.Close()

	meta := index.NewMetaRepo(store)

	got, getErr := meta.Get(index.MetaSchemaVersionKey)
	if getErr != nil {
		test.Fatalf("meta.Get: %v", getErr)
	}

	if got != index.SchemaVersion {
		test.Fatalf("meta[%s] = %q, want %q", index.MetaSchemaVersionKey, got, index.SchemaVersion)
	}
}
