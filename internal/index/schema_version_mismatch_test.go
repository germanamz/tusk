package index_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestOpenReturnsIncompatibleWhenVersionDiffers(test *testing.T) {
	test.Parallel()

	dbPath := filepath.Join(test.TempDir(), "tusk.db")

	store, openErr := index.Open(dbPath)
	if openErr != nil {
		test.Fatalf("initial Open: %v", openErr)
	}

	meta := index.NewMetaRepo(store)
	if setErr := meta.Set(index.MetaSchemaVersionKey, "from-some-other-binary"); setErr != nil {
		test.Fatalf("seed mismatched version: %v", setErr)
	}

	if closeErr := store.Close(); closeErr != nil {
		test.Fatalf("close: %v", closeErr)
	}

	_, reopenErr := index.Open(dbPath)

	var versionErr *index.SchemaVersionError
	if !errors.As(reopenErr, &versionErr) {
		test.Fatalf("reopen returned %v, want *SchemaVersionError", reopenErr)
	}

	if !errors.Is(reopenErr, index.ErrSchemaIncompatible) {
		test.Fatal("reopen error must satisfy errors.Is(err, index.ErrSchemaIncompatible)")
	}

	if versionErr.Observed != "from-some-other-binary" {
		test.Errorf("Observed = %q, want %q", versionErr.Observed, "from-some-other-binary")
	}

	if versionErr.Expected != index.SchemaVersion {
		test.Errorf("Expected = %q, want %q", versionErr.Expected, index.SchemaVersion)
	}
}

func TestOpenAcceptsMatchingVersion(test *testing.T) {
	test.Parallel()

	dbPath := filepath.Join(test.TempDir(), "tusk.db")

	store, openErr := index.Open(dbPath)
	if openErr != nil {
		test.Fatalf("initial Open: %v", openErr)
	}
	if closeErr := store.Close(); closeErr != nil {
		test.Fatalf("close: %v", closeErr)
	}

	reopen, reopenErr := index.Open(dbPath)
	if reopenErr != nil {
		test.Fatalf("reopen with matching version: %v", reopenErr)
	}
	defer reopen.Close()
}
