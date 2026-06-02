package subunit

import (
	"bytes"
	"strings"

	"github.com/germanamz/tusk/internal/node"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// titleMaxLen caps the single-line title excerpt so it fits cleanly
// in the `nodes.title` column and in compact render output. Tuned to
// keep the column width comparable to file titles in the existing
// renderer.
const titleMaxLen = 120

// markdownParser is the shared goldmark instance. CommonMark plus the
// Table and TaskList extensions; footnotes deliberately stay
// disabled since footnotes do not emit units per spec §5.1.
var markdownParser = goldmark.New(
	goldmark.WithExtensions(
		extension.Table,
		extension.TaskList,
	),
)

// Parse converts the supplied markdown source into a deterministic
// flat list of sub-units. The returned slice is ordered by
// depth-first AST traversal; Ordinal mirrors the slice index.
//
// Hash collisions within the file are resolved before returning, so
// every Unit.Hash is unique within the result.
func Parse(source []byte) ([]Unit, error) {
	doc := markdownParser.Parser().Parse(text.NewReader(source))

	var units []Unit

	// ctx threads the section stack and per-section address counters
	// through the walk.
	ctx := newWalkCtx()

	// emit appends a unit, assigns its ordinal, parent links, hashes, and
	// structural address, then returns its slice index. Sections and table
	// cells pre-set Address before calling emit; other leaves get their
	// address from the deepest frame's per-kind counter here.
	emit := func(unit Unit) int {
		unit.Ordinal = len(units)
		unit.ParentHash = ctx.currentHash()
		unit.ParentAddress = ctx.currentAddress()
		if unit.EmbedPayload == "" {
			unit.EmbedPayload = unit.Text
		}
		unit.Title = makeTitle(unit.Text)
		unit.Hash = ComputeHash(unit)
		unit.ContentHash = contentHashFor(unit)
		if unit.Address == "" {
			unit.Address = ctx.leafAddress(unit.Kind)
		}
		units = append(units, unit)
		return unit.Ordinal
	}

	// Walk only top-level block children of the document; we
	// dispatch on kind ourselves so we can flatten blockquotes,
	// skip table containers, etc.
	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		walkBlock(source, child, ctx, &units, emit)
	}

	units = disambiguateFallbackIDs(units)

	return units, nil
}

// walkBlock dispatches one top-level (or section-descendant)
// block-kind node. Section opening updates sectionStack; leaf kinds
// emit a Unit.
func walkBlock(
	source []byte,
	block ast.Node,
	ctx *WalkCtx,
	units *[]Unit,
	emit func(Unit) int,
) {
	switch typed := block.(type) {
	case *ast.Heading:
		// Close any sections at or deeper than this heading
		// level so the new section nests correctly.
		ctx.closeToLevel(typed.Level)

		addr := ctx.openSectionAddress()

		headingText := normalizedText(source, typed)
		bodyText := sectionBodyText(source, typed)
		fullText := headingText
		if bodyText != "" {
			fullText = headingText + "\n" + bodyText
		}

		idx := emit(Unit{
			Kind:    KindSection,
			Address: addr,
			Text:    fullText,
			Properties: map[string]any{
				"heading-level": typed.Level,
			},
		})

		ctx.push(typed.Level, (*units)[idx].Hash, addr)

	case *ast.Paragraph:
		emit(Unit{
			Kind:       KindParagraph,
			Text:       normalizedText(source, typed),
			Properties: map[string]any{},
		})

	case *ast.TextBlock:
		// A top-level TextBlock behaves like a paragraph (a
		// single-line note without a trailing blank line).
		// Inside a list item it is consumed by the list-item
		// branch and never reaches here.
		txt := normalizedText(source, typed)
		if strings.TrimSpace(txt) == "" {
			return
		}
		emit(Unit{
			Kind:       KindParagraph,
			Text:       txt,
			Properties: map[string]any{},
		})

	case *ast.List:
		// Walk each list item directly. Nested lists inside a
		// list item are walked recursively by the list-item
		// branch.
		for li := typed.FirstChild(); li != nil; li = li.NextSibling() {
			if item, ok := li.(*ast.ListItem); ok {
				walkListItem(source, item, ctx, units, emit)
			}
		}

	case *ast.FencedCodeBlock:
		emit(Unit{
			Kind: KindCodeBlock,
			Text: string(typed.Lines().Value(source)),
			Properties: map[string]any{
				"lang": string(typed.Language(source)),
			},
		})

	case *ast.CodeBlock:
		emit(Unit{
			Kind: KindCodeBlock,
			Text: string(typed.Lines().Value(source)),
			Properties: map[string]any{
				"lang": "",
			},
		})

	case *ast.Blockquote:
		// Flatten the entire blockquote (including any nested
		// blockquotes) into a single unit per spec §5.1.
		emit(Unit{
			Kind:       KindBlockquote,
			Text:       flattenBlockquoteText(source, typed),
			Properties: map[string]any{},
		})

	case *extast.Table:
		walkTable(source, typed, ctx, emit)

	case *ast.ThematicBreak, *ast.HTMLBlock:
		// Horizontal rules and raw HTML blocks do not emit
		// units (spec §5.1).
		return

	default:
		// Unknown block types are ignored; the parser is
		// conservative — if a future goldmark version
		// introduces a new block kind we don't recognize, we
		// skip it rather than mis-classify it as a paragraph.
		return
	}
}

// walkListItem emits a list-item unit plus walks any nested blocks
// (e.g., a sub-list, a code block inside a bullet, a blockquote
// inside a bullet) as their own units.
func walkListItem(
	source []byte,
	item *ast.ListItem,
	ctx *WalkCtx,
	units *[]Unit,
	emit func(Unit) int,
) {
	checked, hasCheckbox := extractCheckbox(item)
	itemText := extractListItemText(source, item)

	props := map[string]any{}
	if hasCheckbox {
		props["checkbox"] = checked
	}

	emit(Unit{
		Kind:       KindListItem,
		Text:       itemText,
		Properties: props,
	})

	// Walk nested block children (sub-lists, code blocks,
	// blockquotes) as their own units. The list-item is the
	// "current" parent in the source order, but per spec §5.1
	// only sections nest sub-units; we emit the nested blocks as
	// peers under the enclosing section.
	for child := item.FirstChild(); child != nil; child = child.NextSibling() {
		switch child.(type) {
		case *ast.List, *ast.FencedCodeBlock, *ast.CodeBlock, *ast.Blockquote:
			walkBlock(source, child, ctx, units, emit)
		}
	}
}

// walkTable iterates a table's header and body rows, emitting one
// table-cell unit per cell. The table container itself does not emit
// a unit (spec §5.1).
func walkTable(
	source []byte,
	tbl *extast.Table,
	ctx *WalkCtx,
	emit func(Unit) int,
) {
	// One table index per table within the enclosing section; cells
	// address as <path>T<k>R<row>C<col>.
	tableIdx := ctx.nextTableIndex()
	path := ctx.currentAddress()

	// First pass: collect the header cells so body rows can look
	// up their column-header by index.
	var headers []string

	rowIndex := 0
	for row := tbl.FirstChild(); row != nil; row = row.NextSibling() {
		switch typedRow := row.(type) {
		case *extast.TableHeader:
			col := 0
			for cell := typedRow.FirstChild(); cell != nil; cell = cell.NextSibling() {
				tc, ok := cell.(*extast.TableCell)
				if !ok {
					continue
				}
				txt := normalizedText(source, tc)
				headers = append(headers, txt)
				emit(Unit{
					Kind:    KindTableCell,
					Address: tableCellAddress(path, tableIdx, rowIndex, col),
					Text:    txt,
					Properties: map[string]any{
						"header":        true,
						"row":           rowIndex,
						"column":        col,
						"column-header": "",
					},
				})
				col++
			}
			rowIndex++

		case *extast.TableRow:
			col := 0
			for cell := typedRow.FirstChild(); cell != nil; cell = cell.NextSibling() {
				tc, ok := cell.(*extast.TableCell)
				if !ok {
					continue
				}
				txt := normalizedText(source, tc)
				colHeader := ""
				if col < len(headers) {
					colHeader = headers[col]
				}
				payload := txt
				if colHeader != "" {
					payload = colHeader + ": " + txt
				}
				emit(Unit{
					Kind:         KindTableCell,
					Address:      tableCellAddress(path, tableIdx, rowIndex, col),
					Text:         txt,
					EmbedPayload: payload,
					Properties: map[string]any{
						"header":        false,
						"row":           rowIndex,
						"column":        col,
						"column-header": colHeader,
					},
				})
				col++
			}
			rowIndex++
		}
	}
}

// normalizedText returns the goldmark-rendered text of node. Uses
// Node.Text(source), which walks the inline tree and concatenates
// text segments (with soft line breaks converted to '\n'). This is
// the "normalized form" the hash function depends on — stable across
// CRLF/LF differences in the source.
func normalizedText(source []byte, n ast.Node) string {
	return string(n.Text(source)) //nolint:staticcheck // Text is deprecated but the replacement (per-kind getters) does not give us a uniform inline-text view.
}

// sectionBodyText concatenates the normalized text of every block
// following heading until the next heading at the same or shallower
// level. Used as part of the section's hash input so that edits
// inside a section invalidate that section (and only that section).
func sectionBodyText(source []byte, heading *ast.Heading) string {
	var buf bytes.Buffer

	for sib := heading.NextSibling(); sib != nil; sib = sib.NextSibling() {
		if next, ok := sib.(*ast.Heading); ok && next.Level <= heading.Level {
			break
		}
		buf.WriteString(descendantBodyText(source, sib))
		buf.WriteByte('\n')
	}

	return strings.TrimRight(buf.String(), "\n")
}

// descendantBodyText returns a normalized text rendering of node that
// reads each block kind from its authoritative source instead of
// relying on goldmark's deprecated Node.Text(source). Block kinds
// like FencedCodeBlock, CodeBlock, Table, and List have varying
// Text(source) behavior across goldmark versions — pulling the
// content from Lines() / per-kind walkers (the same sources the
// leaf-unit constructors use) keeps section hashes stable and
// guarantees that edits inside any nested block invalidate the
// enclosing section.
func descendantBodyText(source []byte, node ast.Node) string {
	switch typed := node.(type) {
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		return string(typed.Lines().Value(source))

	case *ast.Blockquote:
		return flattenBlockquoteText(source, typed)

	case *ast.List:
		var parts []string
		for item := typed.FirstChild(); item != nil; item = item.NextSibling() {
			if _, ok := item.(*ast.ListItem); !ok {
				continue
			}
			if text := descendantBodyText(source, item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")

	case *ast.ListItem:
		var parts []string
		for child := typed.FirstChild(); child != nil; child = child.NextSibling() {
			if text := descendantBodyText(source, child); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")

	case *extast.Table:
		var parts []string
		for row := typed.FirstChild(); row != nil; row = row.NextSibling() {
			for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
				tc, ok := cell.(*extast.TableCell)
				if !ok {
					continue
				}
				if text := normalizedText(source, tc); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")

	default:
		return normalizedText(source, node)
	}
}

// flattenBlockquoteText walks a blockquote (including nested
// blockquotes) and concatenates the text of every leaf block child.
// Per spec §5.1, nested blockquotes count as one unit.
func flattenBlockquoteText(source []byte, bq *ast.Blockquote) string {
	var parts []string
	var visit func(n ast.Node)
	visit = func(n ast.Node) {
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			if _, ok := child.(*ast.Blockquote); ok {
				visit(child)
				continue
			}
			txt := normalizedText(source, child)
			if strings.TrimSpace(txt) != "" {
				parts = append(parts, txt)
			}
		}
	}
	visit(bq)
	return strings.Join(parts, "\n")
}

// extractCheckbox inspects a list item's first text/textblock child
// for a goldmark TaskCheckBox node; returns the checked state and
// whether one was found.
func extractCheckbox(item *ast.ListItem) (bool, bool) {
	// The TaskCheckBox is an inline child of the item's first
	// child block (typically a TextBlock or Paragraph).
	first := item.FirstChild()
	if first == nil {
		return false, false
	}
	for child := first.FirstChild(); child != nil; child = child.NextSibling() {
		if cb, ok := child.(*extast.TaskCheckBox); ok {
			return cb.IsChecked, true
		}
	}
	return false, false
}

// extractListItemText returns the item's own text (the first
// paragraph or text block) excluding nested sub-lists and excluding
// the TaskCheckBox marker itself. Nested blocks are walked
// separately by walkListItem.
func extractListItemText(source []byte, item *ast.ListItem) string {
	first := item.FirstChild()
	if first == nil {
		return ""
	}

	// Walk inline children of the first block, skipping the
	// TaskCheckBox marker; concatenate everything else.
	var buf bytes.Buffer
	for child := first.FirstChild(); child != nil; child = child.NextSibling() {
		if _, ok := child.(*extast.TaskCheckBox); ok {
			continue
		}
		buf.Write(child.Text(source)) //nolint:staticcheck // see normalizedText.
		if sb, ok := child.(interface{ SoftLineBreak() bool }); ok && sb.SoftLineBreak() {
			buf.WriteByte('\n')
		}
	}

	return strings.TrimSpace(buf.String())
}

// makeTitle returns a single-line excerpt of body suitable for the
// nodes.title column. Strips line breaks and truncates with an
// ellipsis. Never empty unless body is empty.
func makeTitle(body string) string {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return ""
	}

	// Take up to the first newline.
	if idx := strings.IndexByte(trimmed, '\n'); idx >= 0 {
		trimmed = trimmed[:idx]
	}

	// Collapse runs of whitespace to single spaces.
	trimmed = strings.Join(strings.Fields(trimmed), " ")

	runes := []rune(trimmed)
	if len(runes) <= titleMaxLen {
		return trimmed
	}
	return string(runes[:titleMaxLen-1]) + "…"
}

// DeriveEdges scans unit.Text for wikilink targets (reusing the
// existing internal/node extractor) and returns one EdgeSpec per
// target per edge type in wikilinkEdgeTypes. Order is preserved:
// targets are emitted in first-seen order, and within a target the
// edge types are emitted in the caller-supplied order.
//
// The function is pure: it does not touch the index, the embed queue,
// or any repository. Resolution of TargetID to a file id (and the
// actual edge insert) is the caller's responsibility — see the Task 3
// reindex pipeline.
//
// Per spec §5.4, sub-units never target sub-units; the extractor
// returns the raw target string and downstream code resolves to a
// file id.
func DeriveEdges(unit Unit, wikilinkEdgeTypes []string) []EdgeSpec {
	if len(wikilinkEdgeTypes) == 0 {
		return nil
	}

	targets := node.ExtractWikilinks([]byte(unit.Text))
	if len(targets) == 0 {
		return nil
	}

	out := make([]EdgeSpec, 0, len(targets)*len(wikilinkEdgeTypes))
	for _, target := range targets {
		for _, edgeType := range wikilinkEdgeTypes {
			out = append(out, EdgeSpec{
				EdgeType: edgeType,
				TargetID: target,
			})
		}
	}

	return out
}
