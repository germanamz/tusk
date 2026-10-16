package subunit

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// Obsidian names a block for a block reference ([[note#^block-id]]) with a
// `^block-id` marker of Latin letters, digits and dashes (#819). The marker
// ends a paragraph, list item or quote after a space, or sits on a line of its
// own after a list, quote, table or code block. Markers are read from the
// masked source, so one inside code names nothing.
var (
	// trailingBlockID matches a marker ending a line, after whitespace or
	// alone on it.
	trailingBlockID = regexp.MustCompile(`(?:^|[ \t])\^([A-Za-z0-9-]+)[ \t]*$`)
	// standaloneBlockID matches a block that is nothing but a marker.
	standaloneBlockID = regexp.MustCompile(`^\^([A-Za-z0-9-]+)$`)
)

// identifiedBlock is spannedBlock for a block a trailing marker can name: a
// paragraph or a quote. The marker becomes the unit's BlockID and leaves its
// text.
func identifiedBlock(finder *spanFinder, block ast.Node, unit Unit) Unit {
	start, end := finder.blockSpan(block)
	unit.Text, unit.BlockID = finder.cutBlockID(unit.Text, start, end)
	finder.span(&unit, start, end)

	return unit
}

// cutBlockID returns text without the marker that ends the normalized span
// [start, end), and the marker's id. A span whose last line ends with no
// marker, or text that is nothing but the marker, comes back unchanged with no
// id: a unit holds more than its name.
func (finder *spanFinder) cutBlockID(text string, start, end int) (string, string) {
	if start < 0 || end <= start {
		return text, ""
	}

	span := finder.masked[start:end]
	match := trailingBlockID.FindSubmatch(span[bytes.LastIndexByte(span, '\n')+1:])

	if match == nil {
		return text, ""
	}

	id := string(match[1])
	rest, found := strings.CutSuffix(strings.TrimRight(text, " \t\n"), "^"+id)
	rest = strings.TrimRight(rest, " \t\n")

	if !found || rest == "" {
		return text, ""
	}

	return rest, id
}

// standaloneBlockID returns the id of a top-level paragraph that is nothing
// but a marker, which names the block before it instead of being a unit; ""
// for any other block.
func (finder *spanFinder) standaloneBlockID(block ast.Node) string {
	switch block.(type) {
	case *ast.Paragraph, *ast.TextBlock:
	default:
		return ""
	}

	start, end := finder.blockSpan(block)

	if start < 0 {
		return ""
	}

	match := standaloneBlockID.FindSubmatch(bytes.TrimSpace(finder.masked[start:end]))

	if match == nil {
		return ""
	}

	return string(match[1])
}
