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
			// Table cells are addressed in Task 4; recurse with
			// inBlock so any nested headings are demoted.
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(source, child, ctx, units, emit, true)
			}
			return
		}
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walk(source, child, ctx, units, emit, inBlock)
	}
}
