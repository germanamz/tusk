package sqlite_test

import (
	"testing"

	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
)

func TestMigration004_SeedsDefaultProject(test *testing.T) {
	store, err := sqlite.New(test.TempDir()+"/test.db", migrations.FS)

	if err != nil {
		test.Fatalf("opening test db: %v", err)
	}

	defer store.Close()

	var name, workflowID string
	err = store.DB().QueryRow(
		`SELECT name, workflow_id FROM projects WHERE id = '00000000-0000-0000-0000-000000000000'`,
	).Scan(&name, &workflowID)

	if err != nil {
		test.Fatalf("querying seed row: %v", err)
	}

	if name != "default" {
		test.Errorf("got name %q, want %q", name, "default")
	}
	if workflowID != "00000000-0000-0000-0000-000000000000" {
		test.Errorf("got workflow_id %q, want kanban UUID", workflowID)
	}
}
