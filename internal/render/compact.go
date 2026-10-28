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
	Paths      []query.PathRef

	// Score, when non-zero, renders as a `score=…` token after the title
	// column. Used by semantic query results.
	Score float64
	// HasScore disambiguates "score is 0 by coincidence" (rare) from
	// "score wasn't set by the caller" so the renderer can decide whether
	// to emit the score token.
	HasScore bool

	// Explain-trace fields. When Trace is set the renderer appends a
	// space-separated `cosine=… final=…` tail to the row's record line, with
	// the components Trace names in between (`lexical=…`, `title=…`,
	// `graph=…`, `weight=…`) and `dist=N` after the final score when graph
	// expansion ran, so a row never shows a zero its ranking didn't compute
	// (query.Trace). The format is intentionally machine-friendly (key=value
	// tokens) so agents can parse it.
	CosineScore  float64
	LexicalScore float64
	TitleScore   float64
	GraphScore   float64
	Weight       float64
	FinalScore   float64
	Distance     int
	Trace        *query.Trace

	// MatchedUnits, when non-empty, triggers the hierarchical render
	// path: the file row is followed by indented `→ #<hash>` lines for
	// each matched sub-unit. See writeMatchedUnits.
	MatchedUnits []query.MatchedUnit
	// UnitsTotal is the file's unit count before a max-units cap cut
	// MatchedUnits, so the `(N more)` tail counts those too. 0 means
	// MatchedUnits is the whole set.
	UnitsTotal int
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
// maxWidth returns the longest extract(item) byte length across items — the
// shared column-width pass behind the compact renderers.
func maxWidth[T any](items []T, extract func(T) string) int {
	width := 0

	for _, item := range items {
		if length := len(extract(item)); length > width {
			width = length
		}
	}

	return width
}

func CompactNodeRows(out io.Writer, rows []CompactRow, opts CompactOpts) error {
	if len(rows) == 0 {
		return nil
	}

	fieldSet := buildFieldSet(opts.Fields)

	// Column widths over the record lines (id, type, title). Computed up
	// front so every record uses the same widths.
	var idWidth, typeWidth, titleWidth int

	if showField(fieldSet, "id") {
		idWidth = maxWidth(rows, func(row CompactRow) string { return row.ID })
	}

	if showField(fieldSet, "type") {
		typeWidth = maxWidth(rows, func(row CompactRow) string { return row.Type })
	}

	if showField(fieldSet, "title") {
		titleWidth = maxWidth(rows, func(row CompactRow) string { return row.Title })
	}

	var builder strings.Builder

	for _, row := range rows {
		writeRecordLine(&builder, row, fieldSet, idWidth, typeWidth, titleWidth)
		writeBody(&builder, row, fieldSet)
		writeEdges(&builder, row, fieldSet)
		writePaths(&builder, row, fieldSet)
		writeMatchedUnits(&builder, row, fieldSet)
	}

	_, writeErr := io.WriteString(out, builder.String())

	return writeErr
}

// CompactEdgeRows renders a flat slice of edge rows for `tusk edge list`.
// Compact form: TYPE  SOURCE  TARGET  SOURCE_PATH (space-aligned columns),
// then units=<id>,<id> when the row folds sub-unit copies of the edge.
func CompactEdgeRows(out io.Writer, rows []EdgeListEntry) error {
	if len(rows) == 0 {
		return nil
	}

	typeWidth := maxWidth(rows, func(row EdgeListEntry) string { return row.Type })
	sourceWidth := maxWidth(rows, func(row EdgeListEntry) string { return row.SourceID })
	targetWidth := maxWidth(rows, func(row EdgeListEntry) string { return row.TargetID })

	var builder strings.Builder

	for _, row := range rows {
		fmt.Fprintf(&builder, "%-*s  %-*s  %-*s  %s%s\n",
			typeWidth, row.Type,
			sourceWidth, row.SourceID,
			targetWidth, row.TargetID,
			row.SourcePath, unitsToken(row.Units))
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
	Units      []string
}

// EdgeListEntries converts the rows of a `tusk edge list` result to the
// entries CompactEdgeRows renders.
func EdgeListEntries(rows []query.FoldedEdge) []EdgeListEntry {
	entries := make([]EdgeListEntry, 0, len(rows))

	for _, row := range rows {
		entries = append(entries, EdgeListEntry{
			Type:       row.Type,
			SourceID:   row.SourceID,
			TargetID:   row.TargetID,
			SourcePath: row.SourcePath,
			Units:      row.Units,
		})
	}

	return entries
}

// unitsToken renders the sub-units an edge is written in as a trailing
// "  units=<id>,<id>" token, or "" when there are none.
func unitsToken(units []string) string {
	if len(units) == 0 {
		return ""
	}

	return "  units=" + strings.Join(units, ",")
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

	if row.Trace != nil {
		line = appendToken(line, explainTokens(row))
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

// explainTokens renders a row's score trace: the cosine, each component
// row.Trace names, the final score, and the hop distance when graph expansion
// ran.
func explainTokens(row CompactRow) string {
	tokens := []string{fmt.Sprintf("cosine=%.4f", row.CosineScore)}

	if row.Trace.Lexical {
		tokens = append(tokens, fmt.Sprintf("lexical=%.4f", row.LexicalScore))
	}

	if row.Trace.Title {
		tokens = append(tokens, fmt.Sprintf("title=%.4f", row.TitleScore))
	}

	if row.Trace.Graph {
		tokens = append(tokens, fmt.Sprintf("graph=%.4f", row.GraphScore))
	}

	if row.Trace.Weight {
		tokens = append(tokens, fmt.Sprintf("weight=%.4f", row.Weight))
	}

	tokens = append(tokens, fmt.Sprintf("final=%.4f", row.FinalScore))

	if row.Trace.Graph {
		tokens = append(tokens, fmt.Sprintf("dist=%d", row.Distance))
	}

	return strings.Join(tokens, " ")
}

// writeBody emits each line of row.Body with a two-space indent. A trailing
// newline on the body is preserved (callers typically include one).
func writeBody(builder *strings.Builder, row CompactRow, fieldSet map[string]struct{}) {
	if !showField(fieldSet, "body") || row.Body == "" {
		return
	}

	body := strings.TrimRight(row.Body, "\n")

	for line := range strings.SplitSeq(body, "\n") {
		builder.WriteString("  ")
		builder.WriteString(line)
		builder.WriteString("\n")
	}
}

// writeMatchedUnits emits the hierarchical sub-unit lines for a file row
// when MatchedUnits is non-empty (semantic group-by-parent or structural
// include=units). Each unit renders as:
//
//	→ #S1.2   section H2 "Balances"   L11-13   "snippet..."   0.86
//
// Sections are decorated as `section H<n>` plus their quoted heading; leaves
// show their plain type (paragraph, list-item, etc). The line column is
// omitted for units without positions (HTML). When the unit has no Score
// (structural path), the trailing score column is omitted. Long lists
// collapse to `(N more)` past defaultMatchedUnitsLimit to keep agent output
// bounded; the count includes units a max-units cap already removed.
func writeMatchedUnits(builder *strings.Builder, row CompactRow, fieldSet map[string]struct{}) {
	if !showField(fieldSet, "matched_units") && fieldSet != nil {
		return
	}

	if len(row.MatchedUnits) == 0 {
		return
	}

	units := row.MatchedUnits
	total := max(row.UnitsTotal, len(units))

	if len(units) > defaultMatchedUnitsLimit {
		units = units[:defaultMatchedUnitsLimit]
	}

	truncated := total - len(units)

	// Compute per-column widths over the units in this group so the
	// arrow / id / decorated-type / snippet columns align.
	displays := make([]matchedUnitDisplay, len(units))
	showScore := false

	for index, unit := range units {
		displays[index] = formatMatchedUnit(unit)

		if unit.HasScore {
			showScore = true
		}
	}

	idWidth := maxWidth(displays, func(display matchedUnitDisplay) string { return display.idCol })
	typeWidth := maxWidth(displays, func(display matchedUnitDisplay) string { return display.typeCol })
	linesWidth := maxWidth(displays, func(display matchedUnitDisplay) string { return display.linesCol })
	snippetWidth := maxWidth(displays, func(display matchedUnitDisplay) string { return display.snippetCol })

	for index, display := range displays {
		var line strings.Builder

		line.WriteString("  → ")
		line.WriteString(padRight(display.idCol, idWidth))
		line.WriteString("  ")
		line.WriteString(padRight(display.typeCol, typeWidth))

		if linesWidth > 0 {
			line.WriteString("  ")
			line.WriteString(padRight(display.linesCol, linesWidth))
		}

		// A unit whose text follows its line has no snippet, so its score
		// comes straight after the lines rather than padded past the
		// snippet column.
		if snippetWidth > 0 && units[index].Text == "" {
			line.WriteString("  ")
			line.WriteString(padRight(display.snippetCol, snippetWidth))
		}

		if showScore && units[index].HasScore {
			fmt.Fprintf(&line, "  %.4f", units[index].Score)
		}

		builder.WriteString(strings.TrimRight(line.String(), " "))
		builder.WriteString("\n")

		writeUnitText(builder, units[index])
	}

	if truncated > 0 {
		fmt.Fprintf(builder, "  (%d more)\n", truncated)
	}
}

// writeUnitText emits a unit's passage text (#814) under its line, headed by
// the lines it covers and indented four spaces. Blank lines stay empty.
func writeUnitText(builder *strings.Builder, unit query.MatchedUnit) {
	if unit.Text == "" {
		return
	}

	fmt.Fprintf(builder, "    text L%d-%d:\n", unit.TextStartLine, unit.TextEndLine)

	for line := range strings.SplitSeq(unit.Text, "\n") {
		if line != "" {
			builder.WriteString("    ")
			builder.WriteString(line)
		}

		builder.WriteString("\n")
	}
}

// matchedUnitDisplay is the formatted column set for one MatchedUnit row.
type matchedUnitDisplay struct {
	idCol      string
	typeCol    string
	linesCol   string
	snippetCol string
}

// formatMatchedUnit produces the per-column strings for a unit. The id
// column is `#<hash>` when the unit id is composite (`<fileID>#<hash>`),
// or the full id otherwise. The type column decorates sections with their
// heading level and quoted heading. The lines column is `L<start>-<end>`, or
// `L<n>` for a one-line unit, and empty when the unit has no position. The
// snippet column wraps non-empty snippets in quotes, and is empty for a unit
// whose text follows its line.
func formatMatchedUnit(unit query.MatchedUnit) matchedUnitDisplay {
	idCol := unit.ID

	if hashIdx := strings.IndexByte(unit.ID, '#'); hashIdx >= 0 {
		idCol = "#" + unit.ID[hashIdx+1:]
	}

	typeCol := unit.Type

	if unit.Type == "section" && unit.HeadingLevel > 0 {
		typeCol = fmt.Sprintf("section H%d", unit.HeadingLevel)
	}

	if unit.Heading != "" {
		typeCol += fmt.Sprintf(" %q", unit.Heading)
	}

	linesCol := ""

	switch {
	case unit.StartLine <= 0:
	case unit.StartLine == unit.EndLine:
		linesCol = fmt.Sprintf("L%d", unit.StartLine)
	default:
		linesCol = fmt.Sprintf("L%d-%d", unit.StartLine, unit.EndLine)
	}

	snippetCol := ""

	// A unit with text prints the text below its line; the snippet is a
	// window of the same passage.
	if unit.Snippet != "" && unit.Text == "" {
		snippetCol = fmt.Sprintf("%q", unit.Snippet)
	}

	return matchedUnitDisplay{idCol: idCol, typeCol: typeCol, linesCol: linesCol, snippetCol: snippetCol}
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

		line := fmt.Sprintf("  %s %s %s", arrow, edge.Type, edge.TargetID)

		if edge.TargetTitle != "" {
			line += " " + edge.TargetTitle
		}

		builder.WriteString(line + unitsToken(edge.Units) + "\n")
	}
}

// writePaths emits one indented line per path ref: its edge type, the path,
// where the page names it, and "(not on disk)" when the path is gone:
//
//	⇢ describes server/ledger/core/service.go  lines 40-41 (Core service)
func writePaths(builder *strings.Builder, row CompactRow, fieldSet map[string]struct{}) {
	if !showField(fieldSet, "paths") || len(row.Paths) == 0 {
		return
	}

	for _, ref := range row.Paths {
		line := fmt.Sprintf("  ⇢ %s %s", ref.EdgeType, ref.Path)

		if where := query.FormatMentions(ref.Mentions); where != "" {
			line += "  " + where
		}

		if !ref.Exists {
			line += "  (not on disk)"
		}

		builder.WriteString(line + "\n")
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
