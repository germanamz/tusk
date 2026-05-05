package index_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func openTestIndex(test *testing.T) *index.Index {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	return store
}

func TestNodeRepo_UpsertAndGet(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	row := index.NodeRow{
		ID:             "notes/auth-rfc",
		Type:           "note",
		Path:           "notes/auth-rfc.md",
		Title:          "Auth RFC",
		PropertiesJSON: `{"title":"Auth RFC"}`,
		LastMtime:      100,
		LastSize:       42,
		LastChecksum:   "abc123",
	}

	if upsertErr := repo.Upsert(row); upsertErr != nil {
		test.Fatalf("Upsert: %v", upsertErr)
	}

	loaded, loadErr := repo.Get("notes/auth-rfc")

	if loadErr != nil {
		test.Fatalf("Get: %v", loadErr)
	}

	if loaded.Type != "note" || loaded.Title != "Auth RFC" {
		test.Errorf("got = %+v", loaded)
	}
}

func TestNodeRepo_UpsertReplacesExistingRow(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	first := index.NodeRow{
		ID: "x", Type: "ticket", Path: "x.md", Title: "first",
		PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "a",
	}

	second := index.NodeRow{
		ID: "x", Type: "ticket", Path: "x.md", Title: "second",
		PropertiesJSON: `{}`, LastMtime: 2, LastSize: 2, LastChecksum: "b",
	}

	if upsertErr := repo.Upsert(first); upsertErr != nil {
		test.Fatalf("first upsert: %v", upsertErr)
	}

	if upsertErr := repo.Upsert(second); upsertErr != nil {
		test.Fatalf("second upsert: %v", upsertErr)
	}

	loaded, loadErr := repo.Get("x")

	if loadErr != nil {
		test.Fatalf("Get: %v", loadErr)
	}

	if loaded.Title != "second" {
		test.Errorf("Title = %q, want %q", loaded.Title, "second")
	}
}

func TestNodeRepo_GetReturnsErrNotFoundWhenMissing(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	_, getErr := repo.Get("missing")

	if getErr != index.ErrNodeNotFound {
		test.Errorf("err = %v, want ErrNodeNotFound", getErr)
	}
}

func TestNodeRepo_ListAll(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	rows := []index.NodeRow{
		{ID: "a", Type: "ticket", Path: "a.md", Title: "A", PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "1"},
		{ID: "b", Type: "note", Path: "b.md", Title: "B", PropertiesJSON: `{}`, LastMtime: 2, LastSize: 2, LastChecksum: "2"},
	}

	for _, row := range rows {
		if upsertErr := repo.Upsert(row); upsertErr != nil {
			test.Fatalf("upsert: %v", upsertErr)
		}
	}

	listed, listErr := repo.List(index.ListFilter{})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(listed) != 2 {
		test.Fatalf("len = %d, want 2", len(listed))
	}
}

func TestNodeRepo_ListByType(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	repo.Upsert(index.NodeRow{ID: "a", Type: "ticket", Path: "a.md", Title: "A", PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "1"})
	repo.Upsert(index.NodeRow{ID: "b", Type: "note", Path: "b.md", Title: "B", PropertiesJSON: `{}`, LastMtime: 2, LastSize: 2, LastChecksum: "2"})

	listed, listErr := repo.List(index.ListFilter{Type: "ticket"})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(listed) != 1 || listed[0].ID != "a" {
		test.Errorf("listed = %+v", listed)
	}
}

func TestNodeRepo_DeleteByPath(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	repo.Upsert(index.NodeRow{ID: "x", Type: "note", Path: "x.md", Title: "X", PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "1"})

	if deleteErr := repo.DeleteByPath("x.md"); deleteErr != nil {
		test.Fatalf("DeleteByPath: %v", deleteErr)
	}

	_, getErr := repo.Get("x")

	if getErr != index.ErrNodeNotFound {
		test.Errorf("err after delete = %v, want ErrNodeNotFound", getErr)
	}
}
