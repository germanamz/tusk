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

// Markdown returns the paths body names, in document order, each (target,
// line) pair once: the candidates among its inline-code spans, and the
// destinations of its links and images resolved against sourcePath (see Link).
// Fenced and indented code blocks hold neither spans nor links, so they never
// contribute. file is the whole file as read and bodyOffset is where body
// starts in it, so lines count from the top of the file under scheme, the way
// sub-unit line ranges do.
func Markdown(sourcePath string, body, file []byte, bodyOffset int, scheme linenum.Scheme) []Mention {
	doc := markdownParser.Parser().Parse(text.NewReader(body))
	collector := newCollector()

	var table *linenum.Table

	record := func(target string, start int) {
		if table == nil {
			table = linenum.NewTable(file, scheme)
		}

		collector.add(Mention{Target: target, Line: table.Line(bodyOffset + start)})
	}

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch typed := node.(type) {
		case *ast.CodeSpan:
			content, start := spanContent(typed, body)

			if target, ok := Candidate(content); ok {
				record(target, start)
			}

			return ast.WalkSkipChildren, nil
		case *ast.Link:
			if target, ok := Link(sourcePath, string(typed.Destination)); ok {
				record(target, inlineStart(typed))
			}
		case *ast.Image:
			if target, ok := Link(sourcePath, string(typed.Destination)); ok {
				record(target, inlineStart(typed))
			}
		}

		// A link's text can hold a code span or an image of its own.
		return ast.WalkContinue, nil
	})

	return collector.mentions
}

// HTML returns the paths an HTML page names, each target once and with no
// line: the candidates among code (the text of its <code> elements outside
// <pre>), then the destinations of links (its <a href> and <img src> values)
// resolved against sourcePath (see Link).
func HTML(sourcePath string, code, links []string) []Mention {
	collector := newCollector()

	for _, span := range code {
		if target, ok := Candidate(span); ok {
			collector.add(Mention{Target: target})
		}
	}

	for _, destination := range links {
		if target, ok := Link(sourcePath, destination); ok {
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

// inlineStart returns the body offset a link or image starts near: the first
// byte of the first text inside it, or, for one with no text (`[](x.go)`), the
// first line of the block that holds it.
func inlineStart(node ast.Node) int {
	start := -1

	_ = ast.Walk(node, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if textNode, isText := child.(*ast.Text); entering && isText {
			start = textNode.Segment.Start

			return ast.WalkStop, nil
		}

		return ast.WalkContinue, nil
	})

	if start >= 0 {
		return start
	}

	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Type() == ast.TypeBlock && parent.Lines().Len() > 0 {
			return parent.Lines().At(0).Start
		}
	}

	return 0
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
