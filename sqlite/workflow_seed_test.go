package sqlite_test

import (
	"testing"

	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
)

func TestMigration003_SeedsKanbanWorkflow(t *testing.T) {
	store, err := sqlite.New(t.TempDir()+"/test.db", migrations.FS)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	defer store.Close()

	var name string
	err = store.DB().QueryRow(
		`SELECT name FROM workflows WHERE id = '00000000-0000-0000-0000-000000000000'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("querying seed row: %v", err)
	}
	if name != "kanban" {
		t.Errorf("got name %q, want %q", name, "kanban")
	}
}
