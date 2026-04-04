package sqlite

import (
	"testing"
	"time"

	"github.com/germanamz/tusk/migrations"
)

// testStore creates a fresh in-memory SQLite database with all migrations
// applied. It is the foundation for every test in the sqlite package.
func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// mustTimeNow returns the current UTC time truncated to millisecond precision
// to match SQLite's timestamp resolution.
func mustTimeNow() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond)
}

func TestNew(t *testing.T) {
	s := testStore(t)
	if s.DB() == nil {
		t.Fatal("expected non-nil *sql.DB")
	}
}

func TestPragmas(t *testing.T) {
	s := testStore(t)

	var journalMode string
	if err := s.DB().QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	// In-memory databases may return "memory" instead of "wal" since there
	// is no file to write a WAL to. Both are acceptable.
	if journalMode != "wal" && journalMode != "memory" {
		t.Fatalf("expected wal or memory, got %s", journalMode)
	}

	var fk int
	if err := s.DB().QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", fk)
	}

	var busyTimeout int
	if err := s.DB().QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("expected busy_timeout=5000, got %d", busyTimeout)
	}
}

func TestMigrations(t *testing.T) {
	s := testStore(t)

	var count int
	err := s.DB().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3 migrations applied, got %d", count)
	}

	var name string
	err = s.DB().QueryRow("SELECT name FROM projects WHERE id = '00000000-0000-0000-0000-000000000000'").Scan(&name)
	if err != nil {
		t.Fatal(err)
	}
	if name != "_default" {
		t.Fatalf("expected _default project, got %s", name)
	}

	tables := []string{"projects", "tasks", "annotations", "relations", "tags", "tag_assignments", "workflows", "workflow_transitions"}
	for _, table := range tables {
		var n string
		err := s.DB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n)
		if err != nil {
			t.Fatalf("table %s not found: %v", table, err)
		}
	}
}

func TestMigrationsIdempotent(t *testing.T) {
	s := testStore(t)

	err := s.migrate(migrations.FS)
	if err != nil {
		t.Fatalf("second migrate call failed: %v", err)
	}

	var count int
	err = s.DB().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3 migrations after idempotent call, got %d", count)
	}
}
