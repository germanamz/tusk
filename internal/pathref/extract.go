package pathref

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"

	"github.com/germanamz/tusk/internal/linenum"
)

// Mention is one place a page names a path: the cleaned target and the 1-based
// file line its span starts on. Line is 0 when the format keeps no positions
// (HTML).
type Mention struct {
	Target string
	Line   int
}

// markdownParser matches the sub-unit parser's extensions, so a span inside a
// table cell or a task-list item parses the same way it does there.
var markdownParser = goldmark.New(
	goldmark.WithExtensions(
		extension.Table,
		extension.TaskList,
	),
)

// Markdown returns the path candidates among body's inline-code spans, in
// document order, each (target, line) pair once. Fenced and indented code
// blocks hold no code spans, so they never contribute. file is the whole file
// as read and bodyOffset is where body starts in it, so lines count from the
// top of the file under scheme, the way sub-unit line ranges do.
func Markdown(body, file []byte, bodyOffset int, scheme linenum.Scheme) []Mention {
	doc := markdownParser.Parser().Parse(text.NewReader(body))
	collector := newCollector()

	var table *linenum.Table

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		span, isSpan := node.(*ast.CodeSpan)

		if !entering || !isSpan {
			return ast.WalkContinue, nil
		}

		content, start := spanContent(span, body)
		target, ok := Candidate(content)

		if !ok {
			return ast.WalkSkipChildren, nil
		}

		if table == nil {
			table = linenum.NewTable(file, scheme)
		}

		collector.add(Mention{Target: target, Line: table.Line(bodyOffset + start)})

		return ast.WalkSkipChildren, nil
	})

	return collector.mentions
}

// Spans returns the path candidates among spans (the text of HTML <code>
// elements outside <pre>), in order, each target once and with no line.
func Spans(spans []string) []Mention {
	collector := newCollector()

	for _, span := range spans {
		if target, ok := Candidate(span); ok {
			collector.add(Mention{Target: target})
		}
	}

	return collector.mentions
}

// spanContent joins a code span's raw text segments and returns it with the
// body offset of its first byte. A span that wraps a line has one segment per
// line, so the joined text keeps the line break and Candidate rejects it.
func spanContent(span *ast.CodeSpan, body []byte) (string, int) {
	var builder strings.Builder

	start := -1

	for child := span.FirstChild(); child != nil; child = child.NextSibling() {
		segmentText, isText := child.(*ast.Text)

		if !isText {
			continue
		}

		if start < 0 {
			start = segmentText.Segment.Start
		}

		builder.Write(segmentText.Segment.Value(body))
	}

	return builder.String(), max(start, 0)
}

// collector keeps mentions in first-seen order without repeats.
type collector struct {
	seen     map[Mention]struct{}
	mentions []Mention
}

func newCollector() *collector {
	return &collector{seen: map[Mention]struct{}{}}
}

func (collector *collector) add(mention Mention) {
	if _, already := collector.seen[mention]; already {
		return
	}

	collector.seen[mention] = struct{}{}
	collector.mentions = append(collector.mentions, mention)
}
