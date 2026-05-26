package index_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestNodesKindIsNotNullAndHasCheckConstraint(test *testing.T) {
	test.Parallel()

	dbPath := filepath.Join(test.TempDir(), "tusk.db")
	store, _ := index.Open(dbPath)
	defer store.Close()

	var sqlText string
	scanErr := store.DB().QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='nodes'`).Scan(&sqlText)
	if scanErr != nil {
		test.Fatalf("read nodes DDL: %v", scanErr)
	}

	if !strings.Contains(sqlText, "kind") || !strings.Contains(sqlText, "NOT NULL") {
		test.Errorf("nodes DDL missing NOT NULL kind:\n%s", sqlText)
	}
	if !strings.Contains(sqlText, "CHECK") {
		test.Errorf("nodes DDL missing CHECK constraint:\n%s", sqlText)
	}
}

func TestNodesCheckRejectsBadKindShape(test *testing.T) {
	test.Parallel()

	dbPath := filepath.Join(test.TempDir(), "tusk.db")
	store, _ := index.Open(dbPath)
	defer store.Close()

	// file rows with source != NULL must be rejected
	_, execErr := store.DB().Exec(`
		INSERT INTO nodes (id, type, path, properties_json,
		                   last_mtime, last_size, last_checksum,
		                   kind, source)
		VALUES ('bad-file', 'note', 'bad.md', '{}', 0, 0, '',
		        'file', 'markdown')
	`)
	if execErr == nil {
		test.Error("CHECK should reject file row with non-NULL source")
	}

	// subunit rows with NULL source must be rejected
	_, execErr = store.DB().Exec(`
		INSERT INTO nodes (id, type, path, properties_json,
		                   last_mtime, last_size, last_checksum,
		                   parent_id, kind, source)
		VALUES ('bad-sub', 'section', 'bad.md', '{}', 0, 0, '',
		        'some-parent', 'subunit', NULL)
	`)
	if execErr == nil {
		test.Error("CHECK should reject subunit row with NULL source")
	}
}

func TestNodesPartialUniqueIsKindFile(test *testing.T) {
	test.Parallel()

	dbPath := filepath.Join(test.TempDir(), "tusk.db")
	store, _ := index.Open(dbPath)
	defer store.Close()

	var sqlText string
	scanErr := store.DB().QueryRow(`
		SELECT sql FROM sqlite_master
		WHERE type='index' AND tbl_name='nodes' AND sql LIKE '%path%'
	`).Scan(&sqlText)
	if scanErr != nil {
		test.Fatalf("read path index DDL: %v", scanErr)
	}

	if !strings.Contains(sqlText, "kind = 'file'") && !strings.Contains(sqlText, "kind='file'") {
		test.Errorf("partial UNIQUE index does not predicate on kind='file':\n%s", sqlText)
	}
}

func TestNodesHasKindTypeCompositeIndex(test *testing.T) {
	test.Parallel()

	dbPath := filepath.Join(test.TempDir(), "tusk.db")
	store, _ := index.Open(dbPath)
	defer store.Close()

	var name string
	scanErr := store.DB().QueryRow(`
		SELECT name FROM sqlite_master
		WHERE type='index' AND tbl_name='nodes' AND name='nodes_kind_type_idx'
	`).Scan(&name)
	if scanErr != nil {
		test.Fatalf("nodes_kind_type_idx missing: %v", scanErr)
	}
}
