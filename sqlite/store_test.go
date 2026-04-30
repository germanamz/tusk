package sqlite

import (
	"testing"

	"github.com/germanamz/tusk/migrations"
)

// testStore creates a fresh in-memory SQLite database with all migrations
// applied. It is the foundation for every test in the sqlite package.
func testStore(test *testing.T) *Store {
	test.Helper()
	store, err := New(":memory:", migrations.FS)

	if err != nil {
		test.Fatalf("opening test store: %v", err)
	}

	test.Cleanup(func() { store.Close() })
	return store
}

func TestNew(test *testing.T) {
	store := testStore(test)
	if store.DB() == nil {
		test.Fatal("expected non-nil *sql.DB")
	}
}

func TestPragmas(test *testing.T) {
	store := testStore(test)

	var journalMode string
	if err := store.DB().QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		test.Fatal(err)
	}
	// In-memory databases may return "memory" instead of "wal" since there
	// is no file to write a WAL to. Both are acceptable.
	if journalMode != "wal" && journalMode != "memory" {
		test.Fatalf("expected wal or memory, got %s", journalMode)
	}

	var fk int
	if err := store.DB().QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		test.Fatal(err)
	}
	if fk != 1 {
		test.Fatalf("expected foreign_keys=1, got %d", fk)
	}

	var busyTimeout int
	if err := store.DB().QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		test.Fatal(err)
	}
	if busyTimeout != 5000 {
		test.Fatalf("expected busy_timeout=5000, got %d", busyTimeout)
	}
}

func TestMigrations(test *testing.T) {
	store := testStore(test)

	var count int
	err := store.DB().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)

	if err != nil {
		test.Fatal(err)
	}

	if count != 13 {
		test.Fatalf("expected 13 migrations applied, got %d", count)
	}

	tables := []string{"tasks", "annotations", "relations", "tags", "tag_assignments", "players", "workflows", "projects", "notes"}
	for _, table := range tables {
		var tableName string
		err := store.DB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&tableName)

		if err != nil {
			test.Fatalf("table %s not found: %v", table, err)
		}
	}
}

func TestMigrationsIdempotent(test *testing.T) {
	store := testStore(test)

	err := store.migrate(migrations.FS)

	if err != nil {
		test.Fatalf("second migrate call failed: %v", err)
	}

	var count int
	err = store.DB().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)

	if err != nil {
		test.Fatal(err)
	}

	if count != 13 {
		test.Fatalf("expected 13 migrations after idempotent call, got %d", count)
	}
}
