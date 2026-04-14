package sqlite_test

import (
	"testing"

	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
)

func TestMigration004_SeedsDefaultProject(t *testing.T) {
	store, err := sqlite.New(t.TempDir()+"/test.db", migrations.FS)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	defer store.Close()

	var name, workflowID string
	err = store.DB().QueryRow(
		`SELECT name, workflow_id FROM projects WHERE id = '00000000-0000-0000-0000-000000000000'`,
	).Scan(&name, &workflowID)
	if err != nil {
		t.Fatalf("querying seed row: %v", err)
	}
	if name != "default" {
		t.Errorf("got name %q, want %q", name, "default")
	}
	if workflowID != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("got workflow_id %q, want kanban UUID", workflowID)
	}
}
