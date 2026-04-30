package sqlite_test

import (
	"testing"

	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
)

func TestMigration003_SeedsKanbanWorkflow(test *testing.T) {
	store, openErr := sqlite.New(test.TempDir()+"/test.db", migrations.FS)

	if openErr != nil {
		test.Fatalf("opening test db: %v", openErr)
	}

	defer store.Close()

	var name string
	queryErr := store.DB().QueryRow(
		`SELECT name FROM workflows WHERE id = '00000000-0000-0000-0000-000000000000'`,
	).Scan(&name)

	if queryErr != nil {
		test.Fatalf("querying seed row: %v", queryErr)
	}

	if name != "kanban" {
		test.Errorf("got name %q, want %q", name, "kanban")
	}
}
