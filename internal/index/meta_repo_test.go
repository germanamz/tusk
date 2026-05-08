package index_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestMetaRepo_GetMissingReturnsEmpty(test *testing.T) {
	store := openTempIndex(test)
	defer store.Close()

	repo := index.NewMetaRepo(store)

	value, getErr := repo.Get("missing")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if value != "" {
		test.Errorf("expected empty value, got %q", value)
	}
}

func TestMetaRepo_SetThenGet(test *testing.T) {
	store := openTempIndex(test)
	defer store.Close()

	repo := index.NewMetaRepo(store)

	if setErr := repo.Set("last_reindex_at", "1747000000"); setErr != nil {
		test.Fatalf("Set: %v", setErr)
	}

	value, getErr := repo.Get("last_reindex_at")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if value != "1747000000" {
		test.Errorf("value = %q", value)
	}
}

func TestMetaRepo_SetUpserts(test *testing.T) {
	store := openTempIndex(test)
	defer store.Close()

	repo := index.NewMetaRepo(store)

	if setErr := repo.Set("k", "v1"); setErr != nil {
		test.Fatalf("Set v1: %v", setErr)
	}

	if setErr := repo.Set("k", "v2"); setErr != nil {
		test.Fatalf("Set v2: %v", setErr)
	}

	value, _ := repo.Get("k")

	if value != "v2" {
		test.Errorf("expected v2, got %q", value)
	}
}

func openTempIndex(test *testing.T) *index.Index {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return store
}
