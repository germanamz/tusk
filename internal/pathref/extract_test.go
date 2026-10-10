package pathref_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/linenum"
	"github.com/germanamz/tusk/internal/pathref"
)

// markdownMentions splits file at its body the way node.ParseFile does (the
// body is everything after the closing frontmatter delimiter) and extracts.
func markdownMentions(test *testing.T, file string, scheme linenum.Scheme) []pathref.Mention {
	test.Helper()

	bodyOffset := 0

	if strings.HasPrefix(file, "---\n") {
		closing := strings.Index(file[4:], "\n---\n")

		if closing < 0 {
			test.Fatalf("fixture has no closing frontmatter delimiter")
		}

		bodyOffset = 4 + closing + len("\n---\n")
	}

	return pathref.Markdown([]byte(file[bodyOffset:]), []byte(file), bodyOffset, scheme)
}

func TestMarkdown_FindsSpansWithFileLines(test *testing.T) {
	file := "---\ntype: technical\ntitle: Ledger\n---\n" + // lines 1-4
		"# Ledger\n" + // 5
		"\n" + // 6
		"The service lives in `server/ledger/core/service.go`.\n" + // 7
		"\n" + // 8
		"- migrations: `server/db/migrations/`\n" + // 9
		"> see `docs/api.md:12`\n" + // 10
		"\n" + // 11
		"| file | what |\n" + // 12
		"| --- | --- |\n" + // 13
		"| `server/wire.go` | root |\n" + // 14
		"\n" + // 15
		"## Also `go.mod`\n" // 16

	got := markdownMentions(test, file, linenum.SchemeLF)
	want := []pathref.Mention{
		{Target: "go.mod", Line: 16},
		{Target: "server/ledger/core/service.go", Line: 7},
		{Target: "server/db/migrations", Line: 9},
		{Target: "docs/api.md", Line: 10},
		{Target: "server/wire.go", Line: 14},
	}

	slices.SortFunc(got, byLine)
	slices.SortFunc(want, byLine)

	if !slices.Equal(got, want) {
		test.Errorf("Markdown =\n%v\nwant\n%v", got, want)
	}
}

func TestMarkdown_SkipsCodeBlocksAndNonPaths(test *testing.T) {
	file := "---\ntype: note\n---\n" +
		"```\nsee `server/in/fence.go`\n```\n" +
		"\n" +
		"    `server/in/indented.go`\n" +
		"\n" +
		"Calls `ExtractWikilinks` and `fmt.Println(x)`, not `a path`.\n" +
		"A span that wraps `server/a\nb.go` is not one path.\n"

	if got := markdownMentions(test, file, linenum.SchemeLF); len(got) != 0 {
		test.Errorf("Markdown = %v, want none", got)
	}
}

func TestMarkdown_RepeatsKeepEachLineOnce(test *testing.T) {
	file := "---\ntype: note\n---\n" +
		"`a/b.go` and `a/b.go` again\n" +
		"then `a/b.go:9` later\n"

	got := markdownMentions(test, file, linenum.SchemeLF)
	want := []pathref.Mention{{Target: "a/b.go", Line: 4}, {Target: "a/b.go", Line: 5}}

	if !slices.Equal(got, want) {
		test.Errorf("Markdown = %v, want %v", got, want)
	}
}

func TestMarkdown_CRLFAndSchemes(test *testing.T) {
	crlf := "---\r\ntype: note\r\n---\r\n" + "intro\r\n" + "\r\n" + "`a/b.go`\r\n"

	if got := markdownMentions(test, strings.ReplaceAll(crlf, "\r\n", "\n"), linenum.SchemeLF); !slices.Equal(got, []pathref.Mention{{Target: "a/b.go", Line: 6}}) {
		test.Fatalf("LF fixture = %v", got)
	}

	body := "intro\r\n\r\n`a/b.go`\r\n"
	head := "---\r\ntype: note\r\n---\r\n"
	got := pathref.Markdown([]byte(body), []byte(head+body), len(head), linenum.SchemeLF)

	if !slices.Equal(got, []pathref.Mention{{Target: "a/b.go", Line: 6}}) {
		test.Errorf("CRLF = %v, want line 6", got)
	}

	separated := "intro more\n\n`a/b.go`\n"

	lf := pathref.Markdown([]byte(separated), []byte(separated), 0, linenum.SchemeLF)
	unicode := pathref.Markdown([]byte(separated), []byte(separated), 0, linenum.SchemeUnicode)

	if len(lf) != 1 || lf[0].Line != 3 {
		test.Errorf("lf scheme = %v, want line 3", lf)
	}

	if len(unicode) != 1 || unicode[0].Line != 4 {
		test.Errorf("unicode scheme = %v, want line 4", unicode)
	}
}

func TestSpans_CandidatesWithoutLines(test *testing.T) {
	got := pathref.Spans([]string{"server/a.go", "Makefile", "server/a.go", "docs/"})
	want := []pathref.Mention{{Target: "server/a.go"}, {Target: "docs"}}

	if !slices.Equal(got, want) {
		test.Errorf("Spans = %v, want %v", got, want)
	}
}

func byLine(left, right pathref.Mention) int {
	if left.Line != right.Line {
		return left.Line - right.Line
	}

	return strings.Compare(left.Target, right.Target)
}
