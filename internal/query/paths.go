package query

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/pathref"
)

// PathRef is one workspace path a row's page names, under one
// paths edge type, with every place it is named. Exists is checked against the
// disk when the query runs (exact case), so a ref to a renamed or deleted file
// reads false.
type PathRef struct {
	Path     string        `json:"path"`
	EdgeType string        `json:"edge_type"`
	Exists   bool          `json:"exists"`
	Mentions []PathMention `json:"mentions,omitempty"`
}

// PathMention is one line a path is named on. Section and Heading name the
// innermost section whose line range holds it; all three are absent when
// unknown (HTML keeps no positions, and a page outside any section, or a
// workspace with sub-units off, has no section to name).
type PathMention struct {
	Line    int    `json:"line,omitempty"`
	Section string `json:"section,omitempty"`
	Heading string `json:"heading,omitempty"`
}

// LoadPaths returns the path refs of each file id, keyed by id, ready to attach
// to result rows. Only anchored refs are listed (pathref.Disk.Anchored): a span
// whose first segment names nothing at the workspace root was never a path.
// When narrow holds predicates, a ref is kept only if one of them matches it,
// so a names-path query lists the refs that made the row match. Refs keep the
// order of their first mention.
func LoadPaths(db *sql.DB, workspaceRoot string, fileIDs []string, narrow []*filter.NamesPathPredicate) (map[string][]PathRef, error) {
	if db == nil {
		return nil, fmt.Errorf("query: include=paths requires a database")
	}

	if workspaceRoot == "" {
		return nil, fmt.Errorf("query: include=paths requires the workspace root")
	}

	refs, loadErr := loadPathRefRows(db, fileIDs)

	if loadErr != nil {
		return nil, loadErr
	}

	disk := pathref.NewDisk(workspaceRoot)
	sections := map[string][]index.SectionSpan{}
	byFile := map[string][]PathRef{}
	positions := map[string]int{}

	for _, ref := range refs {
		if !disk.Anchored(ref.Target) || !matchesAny(narrow, ref.Type, ref.Target) {
			continue
		}

		key := ref.SourceID + "\x00" + ref.Type + "\x00" + ref.Target
		position, seen := positions[key]

		if !seen {
			position = len(byFile[ref.SourceID])
			positions[key] = position
			byFile[ref.SourceID] = append(byFile[ref.SourceID], PathRef{
				Path:     ref.Target,
				EdgeType: ref.Type,
				Exists:   disk.Resolves(ref.Target),
			})
		}

		if ref.Line <= 0 {
			continue
		}

		spans, cached := sections[ref.SourceID]

		if !cached {
			loaded, sectionsErr := index.SectionSpans(db, ref.SourceID)

			if sectionsErr != nil {
				return nil, sectionsErr
			}

			sections[ref.SourceID] = loaded
			spans = loaded
		}

		mention := PathMention{Line: ref.Line}

		if innermost, found := index.InnermostSection(spans, ref.Line); found {
			mention.Section = innermost.ID
			mention.Heading = innermost.Heading
		}

		entry := &byFile[ref.SourceID][position]
		entry.Mentions = append(entry.Mentions, mention)
	}

	return byFile, nil
}

// LoadPathsForNode returns the path refs of one node for `node get --include
// paths`, in the shape query rows carry. A sub-unit id has none: refs belong to
// the file row, as they do on query rows.
func LoadPathsForNode(db *sql.DB, workspaceRoot, id string) ([]PathRef, error) {
	byFile, loadErr := LoadPaths(db, workspaceRoot, []string{id}, nil)

	if loadErr != nil {
		return nil, loadErr
	}

	return byFile[id], nil
}

// attachPaths loads the path refs of ids (one per result row, in row order) and
// hands each row's refs to set. The outer names-path predicates of filterText
// narrow what is listed, as LoadPaths describes.
func attachPaths(db *sql.DB, workspaceRoot, filterText string, ids []string, set func(position int, refs []PathRef)) error {
	byFile, loadErr := LoadPaths(db, workspaceRoot, ids, namesPathNarrowing(filterText))

	if loadErr != nil {
		return loadErr
	}

	for position, id := range ids {
		if refs := byFile[id]; len(refs) > 0 {
			set(position, refs)
		}
	}

	return nil
}

// namesPathNarrowing returns the outer names-path predicates of filterText. The
// filter already compiled once by the time rows exist, so a parse error here
// can't happen in practice; it narrows nothing if it does.
func namesPathNarrowing(filterText string) []*filter.NamesPathPredicate {
	expr, parseErrs := filter.NewParser(filterText).Parse()

	if len(parseErrs) > 0 {
		return nil
	}

	return filter.OuterNamesPaths(expr)
}

func matchesAny(narrow []*filter.NamesPathPredicate, edgeType, target string) bool {
	if len(narrow) == 0 {
		return true
	}

	for _, predicate := range narrow {
		if predicate.Matches(edgeType, target) {
			return true
		}
	}

	return false
}

// pathRefChunk bounds the ids bound into one IN (...) clause, well under
// SQLite's variable limit, for a CLI query that returns every row.
const pathRefChunk = 500

// loadPathRefRows reads the path refs of fileIDs ordered by page, then line,
// so mentions land in document order. Each page's refs come from one chunk, so
// the per-chunk order is all LoadPaths needs.
func loadPathRefRows(db *sql.DB, fileIDs []string) ([]index.PathRefRow, error) {
	var refs []index.PathRefRow

	for start := 0; start < len(fileIDs); start += pathRefChunk {
		chunk := fileIDs[start:min(start+pathRefChunk, len(fileIDs))]
		args := make([]any, len(chunk))

		for position, id := range chunk {
			args[position] = id
		}

		chunkRefs, chunkErr := queryPathRefRows(db,
			`SELECT source_id, type, target, line FROM path_refs WHERE source_id IN (`+
				strings.Repeat("?, ", len(chunk)-1)+`?) ORDER BY source_id, line, target, type`,
			args...,
		)

		if chunkErr != nil {
			return nil, chunkErr
		}

		refs = append(refs, chunkRefs...)
	}

	return refs, nil
}

func queryPathRefRows(db *sql.DB, statement string, args ...any) ([]index.PathRefRow, error) {
	rows, queryErr := db.Query(statement, args...)

	if queryErr != nil {
		return nil, fmt.Errorf("query: load path refs: %w", queryErr)
	}

	defer rows.Close()

	var refs []index.PathRefRow

	for rows.Next() {
		var ref index.PathRefRow

		if scanErr := rows.Scan(&ref.SourceID, &ref.Type, &ref.Target, &ref.Line); scanErr != nil {
			return nil, fmt.Errorf("query: scan path ref: %w", scanErr)
		}

		refs = append(refs, ref)
	}

	return refs, rows.Err()
}

// FormatMentions renders mentions compactly for people and agents: runs of
// consecutive lines under one heading collapse into a range, each run followed
// by its heading in parentheses, e.g. "lines 40-41 (Core service), 80
// (Wiring)". It returns "" when no mention carries a line (HTML).
func FormatMentions(mentions []PathMention) string {
	type run struct {
		first, last int
		heading     string
	}

	var runs []run

	for _, mention := range mentions {
		if mention.Line <= 0 {
			continue
		}

		if last := len(runs) - 1; last >= 0 && runs[last].heading == mention.Heading && mention.Line <= runs[last].last+1 {
			runs[last].last = max(runs[last].last, mention.Line)

			continue
		}

		runs = append(runs, run{first: mention.Line, last: mention.Line, heading: mention.Heading})
	}

	if len(runs) == 0 {
		return ""
	}

	parts := make([]string, 0, len(runs))
	plural := len(runs) > 1 || runs[0].first != runs[0].last

	for _, each := range runs {
		part := strconv.Itoa(each.first)

		if each.last != each.first {
			part += "-" + strconv.Itoa(each.last)
		}

		if each.heading != "" {
			part += " (" + each.heading + ")"
		}

		parts = append(parts, part)
	}

	if plural {
		return "lines " + strings.Join(parts, ", ")
	}

	return "line " + parts[0]
}
