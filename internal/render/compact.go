// Package render owns the compact wire format used by tusk's read verbs.
// The compact format is column-aligned text suitable for both human reading
// at a terminal and small-context-window agent consumption (spec §4.4).
package render

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/germanamz/tusk/internal/query"
)

// CompactRow is the shape the compact renderer consumes. The renderer is
// reusable from `tusk query`, `tusk node list`, `tusk edge list`, and any
// future verb that returns a row-shaped result.
type CompactRow struct {
	ID    string
	Type  string
	Title string

	Body       string
	Properties map[string]any
	Edges      []query.EdgeRef

	// Score, when non-zero, renders as a `score=…` token after the title
	// column. Used by semantic query results.
	Score float64
	// HasScore disambiguates "score is 0 by coincidence" (rare) from
	// "score wasn't set by the caller" so the renderer can decide whether
	// to emit the score token.
	HasScore bool

	// Explain-trace fields. When HasExplain is true the renderer appends a
	// space-separated `cosine=… graph=… final=… dist=N` tail to the row's
	// record line. The format is intentionally machine-friendly (key=value
	// tokens) so agents can parse it; the plan's `0.84 = 0.74×0.8 + …`
	// math notation was illustrative only.
	CosineScore float64
	GraphScore  float64
	FinalScore  float64
	Distance    int
	HasExplain  bool

	// MatchedUnits, when non-empty, triggers the hierarchical render
	// path: the file row is followed by indented `→ #<hash>` lines for
	// each matched sub-unit. See writeMatchedUnits.
	MatchedUnits []query.MatchedUnit
}

// defaultMatchedUnitsLimit caps how many matched units the compact renderer
// prints per file row before collapsing the tail to `(N more)`. JSON output
// is unaffected. The threshold is intentionally generous so a typical
// section-heavy file still shows in full.
const defaultMatchedUnitsLimit = 20

// CompactOpts narrows the renderer's behavior.
type CompactOpts struct {
	// Fields, when non-empty, projects each row to the named fields. The
	// renderer is permissive: unknown field names are ignored. The default
	// (empty Fields) renders id/type/title plus any populated expansions.
	Fields []string
}

// CompactNodeRows renders rows as compact records. Each record is one line of
// tab-aligned columns; expanded body/edges/properties follow as indented
// continuation lines. Output is deterministic — identical input slices always
// produce byte-identical output (the renderer test asserts this).
func CompactNodeRows(out io.Writer, rows []CompactRow, opts CompactOpts) error {
	if len(rows) == 0 {
		return nil
	}

	fieldSet := buildFieldSet(opts.Fields)

	// Column widths over the record lines (id, type, title). Computed up
	// front so every record uses the same widths.
	var idWidth, typeWidth, titleWidth int

	for _, row := range rows {
		if showField(fieldSet, "id") && len(row.ID) > idWidth {
			idWidth = len(row.ID)
		}

		if showField(fieldSet, "type") && len(row.Type) > typeWidth {
			typeWidth = len(row.Type)
		}

		if showField(fieldSet, "title") && len(row.Title) > titleWidth {
			titleWidth = len(row.Title)
		}
	}

	var builder strings.Builder

	for _, row := range rows {
		writeRecordLine(&builder, row, fieldSet, idWidth, typeWidth, titleWidth)
		writeBody(&builder, row, fieldSet)
		writeEdges(&builder, row, fieldSet)
		writeMatchedUnits(&builder, row, fieldSet)
	}

	_, writeErr := io.WriteString(out, builder.String())

	return writeErr
}

// CompactEdgeRows renders a flat slice of edge rows for `tusk edge list`.
// Compact form: TYPE  SOURCE  TARGET  SOURCE_PATH (tab-separated columns).
func CompactEdgeRows(out io.Writer, rows []EdgeListEntry) error {
	if len(rows) == 0 {
		return nil
	}

	var typeWidth, sourceWidth, targetWidth int

	for _, row := range rows {
		if len(row.Type) > typeWidth {
			typeWidth = len(row.Type)
		}

		if len(row.SourceID) > sourceWidth {
			sourceWidth = len(row.SourceID)
		}

		if len(row.TargetID) > targetWidth {
			targetWidth = len(row.TargetID)
		}
	}

	var builder strings.Builder

	for _, row := range rows {
		fmt.Fprintf(&builder, "%-*s  %-*s  %-*s  %s\n",
			typeWidth, row.Type,
			sourceWidth, row.SourceID,
			targetWidth, row.TargetID,
			row.SourcePath)
	}

	_, writeErr := io.WriteString(out, builder.String())

	return writeErr
}

// EdgeListEntry is the compact-renderer's view of an index edge row. It is a
// renderer-local type so internal/render does not import internal/index.
type EdgeListEntry struct {
	Type       string
	SourceID   string
	TargetID   string
	SourcePath string
}

// buildFieldSet returns nil for an empty fields list (meaning "render all
// applicable columns"), or a set of allowed field names.
func buildFieldSet(fields []string) map[string]struct{} {
	if len(fields) == 0 {
		return nil
	}

	set := make(map[string]struct{}, len(fields))

	for _, name := range fields {
		set[name] = struct{}{}
	}

	return set
}

// showField returns true when the field should be rendered: either no
// projection is in effect (set == nil) or the field is listed in the
// projection set.
func showField(set map[string]struct{}, name string) bool {
	if set == nil {
		return true
	}

	_, present := set[name]

	return present
}

// writeRecordLine emits the id/type/title/score/properties part of a record.
func writeRecordLine(builder *strings.Builder, row CompactRow, fieldSet map[string]struct{}, idWidth, typeWidth, titleWidth int) {
	var parts []string

	if showField(fieldSet, "id") {
		parts = append(parts, padRight(row.ID, idWidth))
	}

	if showField(fieldSet, "type") {
		parts = append(parts, padRight(row.Type, typeWidth))
	}

	if showField(fieldSet, "title") {
		parts = append(parts, padRight(row.Title, titleWidth))
	}

	line := strings.Join(parts, "  ")

	if showField(fieldSet, "score") && row.HasScore {
		line = appendToken(line, fmt.Sprintf("score=%.4f", row.Score))
	}

	if row.HasExplain {
		line = appendToken(line, fmt.Sprintf(
			"cosine=%.4f graph=%.4f final=%.4f dist=%d",
			row.CosineScore, row.GraphScore, row.FinalScore, row.Distance,
		))
	}

	if showField(fieldSet, "properties") && len(row.Properties) > 0 {
		line = appendToken(line, formatProperties(row.Properties))
	}

	// Strip trailing whitespace so the last column is not padded with
	// spaces when no tokens follow. Padding is only meaningful when the
	// next column starts on the same line.
	builder.WriteString(strings.TrimRight(line, " "))
	builder.WriteString("\n")
}

// writeBody emits each line of row.Body with a two-space indent. A trailing
// newline on the body is preserved (callers typically include one).
func writeBody(builder *strings.Builder, row CompactRow, fieldSet map[string]struct{}) {
	if !showField(fieldSet, "body") || row.Body == "" {
		return
	}

	body := strings.TrimRight(row.Body, "\n")

	for _, line := range strings.Split(body, "\n") {
		builder.WriteString("  ")
		builder.WriteString(line)
		builder.WriteString("\n")
	}
}

// writeMatchedUnits emits the hierarchical sub-unit lines for a file row
// when MatchedUnits is non-empty (semantic group-by-parent or structural
// include=units). Each unit renders as:
//
//	→ #<hash>   section H2   "snippet..."   0.86
//
// Sections are decorated as `section H<n>`; leaves show their plain type
// (paragraph, list-item, etc). When the unit has no Score (structural
// path), the trailing score column is omitted. Long lists collapse to
// `(N more)` past defaultMatchedUnitsLimit to keep agent output bounded.
func writeMatchedUnits(builder *strings.Builder, row CompactRow, fieldSet map[string]struct{}) {
	if !showField(fieldSet, "matched_units") && fieldSet != nil {
		return
	}

	if len(row.MatchedUnits) == 0 {
		return
	}

	units := row.MatchedUnits

	limit := defaultMatchedUnitsLimit
	truncated := 0

	if len(units) > limit {
		truncated = len(units) - limit
		units = units[:limit]
	}

	// Compute per-column widths over the units in this group so the
	// arrow / id / decorated-type / snippet columns align.
	var (
		idWidth      int
		typeWidth    int
		snippetWidth int
		showScore    bool
	)

	displays := make([]matchedUnitDisplay, len(units))

	for index, unit := range units {
		display := formatMatchedUnit(unit)
		displays[index] = display

		if len(display.idCol) > idWidth {
			idWidth = len(display.idCol)
		}

		if len(display.typeCol) > typeWidth {
			typeWidth = len(display.typeCol)
		}

		if len(display.snippetCol) > snippetWidth {
			snippetWidth = len(display.snippetCol)
		}

		if unit.HasScore {
			showScore = true
		}
	}

	for index, display := range displays {
		var line strings.Builder

		line.WriteString("  → ")
		line.WriteString(padRight(display.idCol, idWidth))
		line.WriteString("  ")
		line.WriteString(padRight(display.typeCol, typeWidth))

		if snippetWidth > 0 {
			line.WriteString("  ")
			line.WriteString(padRight(display.snippetCol, snippetWidth))
		}

		if showScore && units[index].HasScore {
			fmt.Fprintf(&line, "  %.4f", units[index].Score)
		}

		builder.WriteString(strings.TrimRight(line.String(), " "))
		builder.WriteString("\n")
	}

	if truncated > 0 {
		fmt.Fprintf(builder, "  (%d more)\n", truncated)
	}
}

// matchedUnitDisplay is the formatted column set for one MatchedUnit row.
type matchedUnitDisplay struct {
	idCol      string
	typeCol    string
	snippetCol string
}

// formatMatchedUnit produces the per-column strings for a unit. The id
// column is `#<hash>` when the unit id is composite (`<fileID>#<hash>`),
// or the full id otherwise. The type column decorates sections with their
// heading level. The snippet column wraps non-empty snippets in quotes.
func formatMatchedUnit(unit query.MatchedUnit) matchedUnitDisplay {
	idCol := unit.ID

	if hashIdx := strings.IndexByte(unit.ID, '#'); hashIdx >= 0 {
		idCol = "#" + unit.ID[hashIdx+1:]
	}

	typeCol := unit.Type

	if unit.Type == "section" && unit.HeadingLevel > 0 {
		typeCol = fmt.Sprintf("section H%d", unit.HeadingLevel)
	}

	snippetCol := ""

	if unit.Snippet != "" {
		snippetCol = fmt.Sprintf("%q", unit.Snippet)
	}

	return matchedUnitDisplay{idCol: idCol, typeCol: typeCol, snippetCol: snippetCol}
}

// writeEdges emits one indented line per edge using the spec §4.4 arrow form.
func writeEdges(builder *strings.Builder, row CompactRow, fieldSet map[string]struct{}) {
	if !showField(fieldSet, "edges") || len(row.Edges) == 0 {
		return
	}

	for _, edge := range row.Edges {
		arrow := "→"

		if edge.Direction == "in" {
			arrow = "←"
		}

		title := edge.TargetTitle

		if title != "" {
			fmt.Fprintf(builder, "  %s %s %s %s\n", arrow, edge.Type, edge.TargetID, title)

			continue
		}

		fmt.Fprintf(builder, "  %s %s %s\n", arrow, edge.Type, edge.TargetID)
	}
}

// padRight pads value with spaces on the right to width characters. Strings
// that already meet or exceed the width are returned unchanged.
func padRight(value string, width int) string {
	if len(value) >= width {
		return value
	}

	return value + strings.Repeat(" ", width-len(value))
}

// appendToken concatenates token to line with a two-space separator. When
// line is empty the token stands alone.
func appendToken(line, token string) string {
	if line == "" {
		return token
	}

	return line + "  " + token
}

// formatProperties renders properties as `key=value` tokens space-separated.
// Keys are emitted in sorted order for byte-stable output. Values are passed
// through fmt's default verb; complex values (slices/maps) are rendered with
// Go's default formatting — adequate for compact-line display.
func formatProperties(properties map[string]any) string {
	keys := make([]string, 0, len(properties))

	for key := range properties {
		// Skip the reserved frontmatter keys that already appear as
		// id / type / title in the compact line.
		if key == "type" || key == "title" {
			continue
		}

		keys = append(keys, key)
	}

	sort.Strings(keys)

	tokens := make([]string, 0, len(keys))

	for _, key := range keys {
		tokens = append(tokens, fmt.Sprintf("%s=%v", key, properties[key]))
	}

	return strings.Join(tokens, " ")
}
