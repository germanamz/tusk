package index

import (
	"database/sql"
	"fmt"
)

// SectionSpan is a section sub-unit's id, heading text and line range.
type SectionSpan struct {
	ID        string
	Heading   string
	StartLine int
	EndLine   int
}

// SectionSpans returns the numbered section rows of fileID: the sub-units of
// type section whose line range is known (markdown; HTML sections carry none).
// It takes the raw handle so read paths holding only a *sql.DB can use it.
func SectionSpans(db *sql.DB, fileID string) ([]SectionSpan, error) {
	lo, hi := subUnitIDRange(fileID)

	rows, queryErr := db.Query(
		`SELECT id, COALESCE(title, ''), start_line, end_line FROM nodes
		 WHERE id >= ? AND id < ? AND kind = 'subunit' AND type = 'section'
		   AND start_line IS NOT NULL AND end_line IS NOT NULL`,
		lo, hi,
	)

	if queryErr != nil {
		return nil, fmt.Errorf("index: sections of %s: %w", fileID, queryErr)
	}

	defer rows.Close()

	var spans []SectionSpan

	for rows.Next() {
		var span SectionSpan

		if scanErr := rows.Scan(&span.ID, &span.Heading, &span.StartLine, &span.EndLine); scanErr != nil {
			return nil, fmt.Errorf("index: scan section of %s: %w", fileID, scanErr)
		}

		spans = append(spans, span)
	}

	return spans, rows.Err()
}

// InnermostSection returns the section whose range holds line and starts
// last: sections nest, so the latest-starting one is the deepest.
func InnermostSection(spans []SectionSpan, line int) (SectionSpan, bool) {
	var (
		best  SectionSpan
		found bool
	)

	for _, span := range spans {
		if line < span.StartLine || line > span.EndLine {
			continue
		}

		if !found || span.StartLine > best.StartLine || (span.StartLine == best.StartLine && span.EndLine < best.EndLine) {
			best, found = span, true
		}
	}

	return best, found
}
