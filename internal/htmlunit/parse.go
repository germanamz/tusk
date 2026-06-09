package htmlunit

import (
	"bytes"

	"github.com/germanamz/tusk/internal/subunit"
	"golang.org/x/net/html"
)

// Parse converts HTML source into a deterministic flat list of
// sub-units, reusing subunit.Unit. Sectioning is driven ONLY by
// <h1>..<h6> in document order; <section>/<article>/<div> and other
// wrappers contribute no structure. x/net/html never errors on
// malformed input, so a parse failure here is treated as an empty
// document.
func Parse(source []byte) ([]subunit.Unit, error) {
	doc, err := html.Parse(bytes.NewReader(source))
	if err != nil {
		return nil, err
	}

	var units []subunit.Unit
	ctx := newWalkCtx()

	emit := func(unit subunit.Unit) int {
		unit.Ordinal = len(units)
		unit.ParentHash = ctx.currentHash()
		unit.ParentAddress = ctx.currentAddress()
		if unit.EmbedPayload == "" {
			unit.EmbedPayload = unit.Text
		}
		unit.Title = makeTitle(unit.Text)
		unit.Hash = subunit.ComputeHash(unit)
		unit.ContentHash = contentHashFor(unit)
		if unit.Address == "" {
			unit.Address = ctx.leafAddress(unit.Kind)
		}
		units = append(units, unit)
		return unit.Ordinal
	}

	walk(source, doc, ctx, &units, emit, false)
	units = disambiguateFallbackIDs(units)

	return units, nil
}

// walk recurses the DOM in document order. inBlock is true once we are
// inside a pre/code/table subtree, where headings are demoted to block
// content rather than treated as section boundaries.
func walk(
	source []byte,
	node *html.Node,
	ctx *walkCtx,
	units *[]subunit.Unit,
	emit func(subunit.Unit) int,
	inBlock bool,
) {
	if node.Type == html.ElementNode {
		switch node.Data {
		case "head", "script", "style":
			return
		}
		if lvl, ok := headingLevel(node.Data); ok && !inBlock {
			ctx.closeToLevel(lvl)
			addr := ctx.openSectionAddress()
			idx := emit(subunit.Unit{
				Kind:    subunit.KindSection,
				Address: addr,
				Text:    elementText(node),
				Properties: map[string]any{
					"heading-level": lvl,
				},
			})
			ctx.push(lvl, (*units)[idx].Hash, addr)
			return
		}
		switch node.Data {
		case "p":
			emit(subunit.Unit{
				Kind:       subunit.KindParagraph,
				Text:       elementText(node),
				Properties: map[string]any{},
			})
			return
		case "li":
			emit(subunit.Unit{
				Kind:       subunit.KindListItem,
				Text:       elementText(node),
				Properties: map[string]any{},
			})
			return
		case "blockquote":
			emit(subunit.Unit{
				Kind:       subunit.KindBlockquote,
				Text:       elementText(node),
				Properties: map[string]any{},
			})
			return
		case "pre":
			emit(subunit.Unit{
				Kind: subunit.KindCodeBlock,
				Text: elementText(node),
				Properties: map[string]any{
					"lang": "",
				},
			})
			return
		case "table":
			walkTable(node, ctx, emit)
			return
		}
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walk(source, child, ctx, units, emit, inBlock)
	}
}

// walkTable emits one table-cell unit per cell. The <table> container
// itself emits no unit. Header cells (<th> in the first row) populate
// the column-header lookup; body cells get an "<header>: <cell>"
// EmbedPayload when their column has a header, mirroring
// subunit.walkTable (parse.go:231).
func walkTable(
	table *html.Node,
	ctx *walkCtx,
	emit func(subunit.Unit) int,
) {
	tableIdx := ctx.nextTableIndex()
	path := ctx.currentAddress()

	var headers []string
	rowIndex := 0

	for _, row := range tableRows(table) {
		col := 0
		for cell := row.FirstChild; cell != nil; cell = cell.NextSibling {
			if cell.Type != html.ElementNode || (cell.Data != "td" && cell.Data != "th") {
				continue
			}
			txt := elementText(cell)
			if cell.Data == "th" {
				headers = append(headers, txt)
				emit(subunit.Unit{
					Kind:    subunit.KindTableCell,
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
				continue
			}
			colHeader := ""
			if col < len(headers) {
				colHeader = headers[col]
			}
			payload := txt
			if colHeader != "" {
				payload = colHeader + ": " + txt
			}
			emit(subunit.Unit{
				Kind:         subunit.KindTableCell,
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

// tableRows returns the <tr> elements of a table in document order,
// descending through any synthesized thead/tbody/tfoot grouping that
// x/net/html inserts.
func tableRows(table *html.Node) []*html.Node {
	var rows []*html.Node
	var visit func(node *html.Node)
	visit = func(node *html.Node) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.ElementNode {
				continue
			}
			switch child.Data {
			case "thead", "tbody", "tfoot":
				visit(child)
			case "tr":
				rows = append(rows, child)
			}
		}
	}
	visit(table)
	return rows
}
