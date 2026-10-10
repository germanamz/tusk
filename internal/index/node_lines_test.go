package index_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestNodeRow_LinesRoundTrip(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	seedNodes(test, store, "notes/f")

	rows := []index.NodeRow{
		{
			ID:             "notes/f#S1",
			Type:           "section",
			Path:           "notes/f.md",
			PropertiesJSON: "{}",
			LastChecksum:   "x",
			ParentID:       sql.NullString{String: "notes/f", Valid: true},
			Ordinal:        sql.NullInt64{Int64: 0, Valid: true},
			StartLine:      sql.NullInt64{Int64: 5, Valid: true},
			EndLine:        sql.NullInt64{Int64: 9, Valid: true},
		},
		{
			ID:             "notes/f#S1P1",
			Type:           "paragraph",
			Path:           "notes/f.md",
			PropertiesJSON: "{}",
			LastChecksum:   "x",
			ParentID:       sql.NullString{String: "notes/f#S1", Valid: true},
			Ordinal:        sql.NullInt64{Int64: 1, Valid: true},
		},
	}

	if upsertErr := repo.BulkUpsert(rows, "markdown"); upsertErr != nil {
		test.Fatalf("BulkUpsert: %v", upsertErr)
	}

	section, getErr := repo.Get("notes/f#S1")

	if getErr != nil {
		test.Fatalf("Get section: %v", getErr)
	}

	if section.StartLine != rows[0].StartLine || section.EndLine != rows[0].EndLine {
		test.Errorf("section lines = %v..%v, want 5..9", section.StartLine, section.EndLine)
	}

	paragraph, paragraphErr := repo.Get("notes/f#S1P1")

	if paragraphErr != nil {
		test.Fatalf("Get paragraph: %v", paragraphErr)
	}

	if paragraph.StartLine.Valid || paragraph.EndLine.Valid {
		test.Errorf("paragraph lines = %v..%v, want NULL", paragraph.StartLine, paragraph.EndLine)
	}
}

// TestOpen_AddsLineColumnsToLegacyNodes: an index written before sub-unit
// line ranges existed gains the two nullable columns in place on open, with
// its rows intact, so upgrading costs no rebuild and no re-embed.
func TestOpen_AddsLineColumnsToLegacyNodes(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	first, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	seedNodes(test, first, "notes/legacy")

	if _, execErr := first.DB().Exec(`
		ALTER TABLE nodes DROP COLUMN start_line;
		ALTER TABLE nodes DROP COLUMN end_line;
	`); execErr != nil {
		test.Fatalf("drop line columns: %v", execErr)
	}

	first.Close()

	for attempt := range 2 {
		reopened, reopenErr := index.Open(dbPath)

		if reopenErr != nil {
			test.Fatalf("reopen %d: %v", attempt, reopenErr)
		}

		var columns int

		if scanErr := reopened.DB().QueryRow(
			`SELECT count(*) FROM pragma_table_info('nodes') WHERE name IN ('start_line', 'end_line')`,
		).Scan(&columns); scanErr != nil {
			test.Fatalf("inspect nodes: %v", scanErr)
		}

		if columns != 2 {
			test.Errorf("reopen %d: found %d line columns, want 2", attempt, columns)
		}

		if _, getErr := index.NewNodeRepo(reopened).Get("notes/legacy"); getErr != nil {
			test.Errorf("reopen %d: legacy row lost: %v", attempt, getErr)
		}

		reopened.Close()
	}
}
