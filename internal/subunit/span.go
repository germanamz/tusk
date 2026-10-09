package subunit

import (
	"bytes"
	"sort"

	"github.com/germanamz/tusk/internal/linenum"
	"github.com/yuin/goldmark/ast"
)

// AssignLines sets StartLine and EndLine on every unit that carries a byte
// span. file is the whole file as read from disk and bodyOffset is the byte
// offset in file where the source handed to Parse begins (after frontmatter),
// so the numbers count every line of the file. scheme decides which line
// terminators count. Units without a span (EndOffset 0) keep zero lines.
func AssignLines(units []Unit, file []byte, bodyOffset int, scheme linenum.Scheme) {
	table := linenum.NewTable(file, scheme)

	for index := range units {
		unit := &units[index]

		if unit.EndOffset <= 0 {
			continue
		}

		unit.StartLine = table.Line(bodyOffset + unit.StartOffset)
		unit.EndLine = table.Line(bodyOffset + unit.EndOffset - 1)
	}
}

// spanFinder computes unit byte spans over the line-ending-normalized source
// the parser walks, then maps them back to offsets in the source Parse was
// handed, so line numbers count the bytes actually on disk.
type spanFinder struct {
	// source is the normalized markdown the AST segments index into.
	source []byte
	// blockStarts is the sorted Pos of every block in the document, used to
	// tell a closing fence from a fence that opens the next block.
	blockStarts []int
	// crlfAt holds the normalized offset of each LF that replaced a CRLF
	// pair, ascending. Each one sits one byte left of its original position.
	crlfAt []int
}

func newSpanFinder(source []byte, doc ast.Node, crlfAt []int) *spanFinder {
	finder := &spanFinder{source: source, crlfAt: crlfAt}

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && node.Type() == ast.TypeBlock && node.Pos() >= 0 {
			finder.blockStarts = append(finder.blockStarts, node.Pos())
		}

		return ast.WalkContinue, nil
	})

	sort.Ints(finder.blockStarts)

	return finder
}

// span maps a normalized [start, end) span onto the original source and
// writes it to unit.
func (finder *spanFinder) span(unit *Unit, start, end int) {
	if start < 0 || end <= start {
		return
	}

	unit.StartOffset = finder.original(start)
	unit.EndOffset = finder.original(end-1) + 1
}

// original maps a normalized offset back to the source Parse was handed:
// every CRLF pair left of it was one byte longer on disk.
func (finder *spanFinder) original(offset int) int {
	return offset + sort.SearchInts(finder.crlfAt, offset)
}

// blockSpan returns the normalized [start, end) extent of node: from its Pos
// (or its first segment) to its last content byte, trailing whitespace
// trimmed. A block with no content segments ends with its first line. Two
// kinds keep markup outside their segments and extend past them: a fenced
// code block's closing fence and a setext heading's underline.
func (finder *spanFinder) blockSpan(node ast.Node) (int, int) {
	start, end := node.Pos(), -1

	finder.extend(node, &start, &end)

	if start < 0 {
		return -1, -1
	}

	if end <= start {
		end = finder.lineEnd(start)
	}

	end = finder.trimEnd(start, end)

	switch typed := node.(type) {
	case *ast.FencedCodeBlock:
		end = max(end, finder.fencedCodeEnd(typed))
	case *ast.Heading:
		end = finder.setextEnd(start, end)
	}

	return start, end
}

// extend widens [start, end) over every source segment under node: block
// lines, inline text segments, and nested blocks.
func (finder *spanFinder) extend(node ast.Node, start, end *int) {
	include := func(from, to int) {
		if from >= 0 && (*start < 0 || from < *start) {
			*start = from
		}

		if to > *end {
			*end = to
		}
	}

	if node.Type() == ast.TypeBlock {
		lines := node.Lines()

		for index := range lines.Len() {
			segment := lines.At(index)
			include(segment.Start, segment.Stop)
		}

		if htmlBlock, ok := node.(*ast.HTMLBlock); ok && htmlBlock.HasClosure() {
			include(htmlBlock.ClosureLine.Start, htmlBlock.ClosureLine.Stop)
		}
	}

	if text, ok := node.(*ast.Text); ok {
		include(text.Segment.Start, text.Segment.Stop)
	}

	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if child.Type() == ast.TypeBlock {
			childStart, childEnd := finder.blockSpan(child)
			include(childStart, childEnd)

			continue
		}

		finder.extend(child, start, end)
	}
}

// setextEnd extends a heading whose text spans [start, end) over its setext
// underline, which sits on the line after the text. An ATX heading ends with
// its text and is returned unchanged.
func (finder *spanFinder) setextEnd(start, end int) int {
	if finder.isATX(start) {
		return end
	}

	underline := finder.nextLineStart(end - 1)

	if underline >= len(finder.source) {
		return end
	}

	return finder.trimEnd(underline, finder.lineEnd(underline))
}

// isATX reports whether the heading whose Pos is start opens with an ATX
// marker: one to six '#' followed by whitespace or the end of the line. A
// setext heading whose text happens to start with "#tag" does not qualify.
func (finder *spanFinder) isATX(start int) bool {
	_, run := finder.charRun(start)

	if run < 1 || run > 6 || finder.source[start] != '#' {
		return false
	}

	after := start + run

	return after >= len(finder.source) || isTrailingSpace(finder.source[after])
}

// fencedCodeEnd returns the end of a fenced code block, including its closing
// fence. goldmark keeps only the content lines, so the closing fence is found
// on the line after the last content line (or after the opening fence when the
// block is empty). A fence-looking line there that opens the next block, as
// when the block's container closed first, is not a closing fence.
func (finder *spanFinder) fencedCodeEnd(block *ast.FencedCodeBlock) int {
	start := block.Pos()
	lastLineStart := start
	end := finder.trimEnd(start, finder.lineEnd(start))

	if lines := block.Lines(); lines.Len() > 0 {
		last := lines.At(lines.Len() - 1)
		lastLineStart = last.Start

		if contentEnd := finder.trimEnd(start, last.Stop); contentEnd > end {
			end = contentEnd
		}
	}

	candidate := finder.nextLineStart(lastLineStart)

	if candidate >= len(finder.source) || candidate >= finder.nextBlockStart(start) {
		return end
	}

	fenceChar, fenceLength := finder.charRun(start)
	line := bytes.TrimLeft(finder.source[candidate:finder.lineEnd(candidate)], " \t>")
	run := 0

	for run < len(line) && line[run] == fenceChar {
		run++
	}

	if fenceLength < 3 || run < fenceLength || len(bytes.TrimSpace(line[run:])) > 0 {
		return end
	}

	return finder.trimEnd(candidate, finder.lineEnd(candidate))
}

// charRun returns the character at offset and how many times it
// repeats.
func (finder *spanFinder) charRun(offset int) (byte, int) {
	if offset >= len(finder.source) {
		return 0, 0
	}

	fenceChar := finder.source[offset]
	length := 0

	for offset+length < len(finder.source) && finder.source[offset+length] == fenceChar {
		length++
	}

	return fenceChar, length
}

// nextBlockStart returns the Pos of the first block that starts after
// offset, or the source length when none does.
func (finder *spanFinder) nextBlockStart(offset int) int {
	index := sort.SearchInts(finder.blockStarts, offset+1)

	if index < len(finder.blockStarts) {
		return finder.blockStarts[index]
	}

	return len(finder.source)
}

// lineEnd returns the offset of the LF ending the line that holds offset, or
// the source length on the last line.
func (finder *spanFinder) lineEnd(offset int) int {
	if offset >= len(finder.source) {
		return len(finder.source)
	}

	if newline := bytes.IndexByte(finder.source[offset:], '\n'); newline >= 0 {
		return offset + newline
	}

	return len(finder.source)
}

// nextLineStart returns the offset just past the LF ending the line that
// holds offset, or the source length on the last line.
func (finder *spanFinder) nextLineStart(offset int) int {
	return min(finder.lineEnd(offset)+1, len(finder.source))
}

// trimEnd pulls end back over trailing whitespace so the span ends at its
// last content byte. It never trims below one byte past start.
func (finder *spanFinder) trimEnd(start, end int) int {
	end = min(end, len(finder.source))

	for end > start+1 && isTrailingSpace(finder.source[end-1]) {
		end--
	}

	return end
}

func isTrailingSpace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\n' || char == '\r'
}
