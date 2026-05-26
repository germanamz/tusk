package index_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestEdgesKindIsNotNullAndHasCheck(test *testing.T) {
	test.Parallel()

	store, _ := index.Open(filepath.Join(test.TempDir(), "tusk.db"))
	defer store.Close()

	var sqlText string
	if scanErr := store.DB().QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='edges'`).Scan(&sqlText); scanErr != nil {
		test.Fatalf("read DDL: %v", scanErr)
	}

	if !strings.Contains(sqlText, "CHECK") {
		test.Errorf("edges DDL missing CHECK constraint:\n%s", sqlText)
	}
	if !strings.Contains(sqlText, "kind") || !strings.Contains(sqlText, "NOT NULL") {
		test.Errorf("edges DDL missing NOT NULL kind:\n%s", sqlText)
	}
}

func TestEdgesCheckRejectsBadShape(test *testing.T) {
	test.Parallel()

	store, _ := index.Open(filepath.Join(test.TempDir(), "tusk.db"))
	defer store.Close()

	_, execErr := store.DB().Exec(`
		INSERT INTO edges (type, source_id, target_id, source_path, kind, source)
		VALUES ('mentions', 'a', 'b', 'a', 'direct', 'markdown')
	`)
	if execErr == nil {
		test.Error("CHECK should reject direct edge with non-NULL source")
	}

	_, execErr = store.DB().Exec(`
		INSERT INTO edges (type, source_id, target_id, source_path, kind, source)
		VALUES ('contains', 'a', 'b', 'a', 'structural', NULL)
	`)
	if execErr == nil {
		test.Error("CHECK should reject structural edge with NULL source")
	}
}

func TestEdgesUniqueIncludesSource(test *testing.T) {
	test.Parallel()

	store, _ := index.Open(filepath.Join(test.TempDir(), "tusk.db"))
	defer store.Close()

	var sqlText string
	if scanErr := store.DB().QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='edges'`).Scan(&sqlText); scanErr != nil {
		test.Fatalf("read DDL: %v", scanErr)
	}

	if !strings.Contains(sqlText, "UNIQUE(source") {
		test.Errorf("edges UNIQUE constraint must include source as the first column:\n%s", sqlText)
	}
}

func TestEdgesHasSourceTypeAndKindIndexes(test *testing.T) {
	test.Parallel()

	store, _ := index.Open(filepath.Join(test.TempDir(), "tusk.db"))
	defer store.Close()

	for _, idxName := range []string{"edges_source_type_idx", "edges_kind_idx"} {
		var found string
		if scanErr := store.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name = ?`, idxName).Scan(&found); scanErr != nil {
			test.Errorf("missing index %q: %v", idxName, scanErr)
		}
	}
}
