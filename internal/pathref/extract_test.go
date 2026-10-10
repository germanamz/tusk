package pathref_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/linenum"
	"github.com/germanamz/tusk/internal/pathref"
)

// markdownMentions splits file at its body the way node.ParseFile does (the
// body is everything after the closing frontmatter delimiter) and extracts it
// as the page docs/page.md.
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

	return pathref.Markdown("docs/page.md", []byte(file[bodyOffset:]), []byte(file), bodyOffset, scheme)
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
	got := pathref.Markdown("page.md", []byte(body), []byte(head+body), len(head), linenum.SchemeLF)

	if !slices.Equal(got, []pathref.Mention{{Target: "a/b.go", Line: 6}}) {
		test.Errorf("CRLF = %v, want line 6", got)
	}

	separated := "intro more\n\n`a/b.go`\n"

	lf := pathref.Markdown("page.md", []byte(separated), []byte(separated), 0, linenum.SchemeLF)
	unicode := pathref.Markdown("page.md", []byte(separated), []byte(separated), 0, linenum.SchemeUnicode)

	if len(lf) != 1 || lf[0].Line != 3 {
		test.Errorf("lf scheme = %v, want line 3", lf)
	}

	if len(unicode) != 1 || unicode[0].Line != 4 {
		test.Errorf("unicode scheme = %v, want line 4", unicode)
	}
}

func TestMarkdown_LinksAndImagesResolveAgainstThePage(test *testing.T) {
	file := "---\ntype: technical\n---\n" + // lines 1-3
		"# Ledger\n" + // 4
		"See [the service](../server/ledger/core/service.go#L12) and\n" + // 5
		"[the root](/Makefile), ![flow](img/flow.png).\n" + // 6
		"\n" + // 7
		"[sibling](other.md) [anchor](#intro) [web](https://x.dev/a.go) [up](../../out.go)\n" + // 8
		"\n" + // 9
		"[`server/wire.go`](../server/wire.go) [](empty/target.go) [ref][def]\n" + // 10
		"\n" + // 11
		"[def]: ../server/ref.go\n" + // 12
		"\n" + // 13
		"```\n[fenced](../server/fenced.go)\n```\n" // 14-16

	got := markdownMentions(test, file, linenum.SchemeLF)
	want := []pathref.Mention{
		{Target: "server/ledger/core/service.go", Line: 5},
		{Target: "Makefile", Line: 6},
		{Target: "docs/img/flow.png", Line: 6},
		{Target: "docs/other.md", Line: 8},
		{Target: "server/wire.go", Line: 10},
		{Target: "docs/empty/target.go", Line: 10},
		{Target: "server/ref.go", Line: 10},
	}

	slices.SortFunc(got, byLine)
	slices.SortFunc(want, byLine)

	if !slices.Equal(got, want) {
		test.Errorf("Markdown =\n%v\nwant\n%v", got, want)
	}
}

func TestLink_ResolvesWorkspacePaths(test *testing.T) {
	cases := []struct {
		source, destination, want string
	}{
		{"docs/a.md", "b.md", "docs/b.md"},
		{"docs/a.md", "./b.md", "docs/b.md"},
		{"docs/deep/a.md", "../../server/x.go", "server/x.go"},
		{"docs/a.md", "/server/x.go", "server/x.go"},
		{"a.md", "server/", "server"},
		{"docs/a.md", "b.md?raw=1#top", "docs/b.md"},
		{"docs/a.md", "my%20file.go", "docs/my file.go"},
		{"docs/a.md", " ../Makefile ", "Makefile"},
	}

	for _, each := range cases {
		got, ok := pathref.Link(each.source, each.destination)

		if !ok || got != each.want {
			test.Errorf("Link(%q, %q) = %q, %v; want %q", each.source, each.destination, got, ok, each.want)
		}
	}

	for _, destination := range []string{"", "#intro", "https://x.dev/a.go", "mailto:a@b.c", "//host/x.go", "../../up.go", "/", "?q=1"} {
		if got, ok := pathref.Link("docs/a.md", destination); ok {
			test.Errorf("Link(docs/a.md, %q) = %q, want rejected", destination, got)
		}
	}
}

func TestHTML_CodeThenLinksWithoutLines(test *testing.T) {
	got := pathref.HTML("site/page.html",
		[]string{"server/a.go", "ExtractWikilinks", "server/a.go", "docs/", "Makefile"},
		[]string{"../server/a.go", "img/diagram.svg", "#top", "https://x.dev", "/go.mod"},
	)
	want := []pathref.Mention{
		{Target: "server/a.go"},
		{Target: "docs"},
		{Target: "Makefile"},
		{Target: "site/img/diagram.svg"},
		{Target: "go.mod"},
	}

	if !slices.Equal(got, want) {
		test.Errorf("HTML = %v, want %v", got, want)
	}
}

func byLine(left, right pathref.Mention) int {
	if left.Line != right.Line {
		return left.Line - right.Line
	}

	return strings.Compare(left.Target, right.Target)
}
