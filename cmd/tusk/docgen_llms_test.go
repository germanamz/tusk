package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteRelativeLinks(test *testing.T) {
	const (
		raw  = "https://raw.githubusercontent.com/germanamz/tusk/main/"
		tree = "https://github.com/germanamz/tusk/tree/main/"
	)

	isDir := func(target string) bool {
		return target == "docs/packages"
	}

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "relative file",
			body: "See [the grammar](../cli/tusk_query.md).",
			want: "See [the grammar](" + raw + "docs/cli/tusk_query.md).",
		},
		{
			name: "fragment kept",
			body: "[pages](workspace.md#pages-name-the-files)",
			want: "[pages](" + raw + "docs/guide/workspace.md#pages-name-the-files)",
		},
		{
			name: "in-page anchor points at the source file",
			body: "[filters](#filters)",
			want: "[filters](" + raw + "docs/guide/querying.md#filters)",
		},
		{
			name: "trailing slash is a directory",
			body: "[results](../../eval/results/)",
			want: "[results](" + tree + "eval/results)",
		},
		{
			name: "directory on disk",
			body: "[package docs](../packages)",
			want: "[package docs](" + tree + "docs/packages)",
		},
		{
			name: "image",
			body: "![chart](../assets/chart.svg)",
			want: "![chart](" + raw + "docs/assets/chart.svg)",
		},
		{
			name: "link title kept",
			body: `[grammar](../cli/tusk_query.md "Query")`,
			want: `[grammar](` + raw + `docs/cli/tusk_query.md "Query")`,
		},
		{
			name: "root-relative",
			body: "[readme](/README.md)",
			want: "[readme](" + raw + "README.md)",
		},
		{
			name: "reference definition",
			body: "[grammar]: ../cli/tusk_query.md",
			want: "[grammar]: " + raw + "docs/cli/tusk_query.md",
		},
		{
			name: "html img src",
			body: `<img alt="chart" src="../../eval/chart.svg">`,
			want: `<img alt="chart" src="` + raw + `eval/chart.svg">`,
		},
		{
			name: "html srcset with descriptors",
			body: `<source srcset="../assets/dark.png 2x, ../assets/dark-small.png 1x">`,
			want: `<source srcset="` + raw + `docs/assets/dark.png 2x, ` + raw + `docs/assets/dark-small.png 1x">`,
		},
		{
			name: "html href",
			body: `<a href="mcp.md">MCP</a>`,
			want: `<a href="` + raw + `docs/guide/mcp.md">MCP</a>`,
		},
		{
			name: "absolute url untouched",
			body: "[site](https://example.com/x.md) and [mail](mailto:me@example.com)",
			want: "[site](https://example.com/x.md) and [mail](mailto:me@example.com)",
		},
		{
			name: "above the root untouched",
			body: "[outside](../../../outside.md)",
			want: "[outside](../../../outside.md)",
		},
		{
			name: "code span untouched",
			body: "A link such as `[the service](../server/service.go)` counts.",
			want: "A link such as `[the service](../server/service.go)` counts.",
		},
		{
			name: "fenced block untouched",
			body: "```markdown\n[x](install.md)\n<img src=\"a.png\">\n```\n",
			want: "```markdown\n[x](install.md)\n<img src=\"a.png\">\n```\n",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			got := string(rewriteRelativeLinks("docs/guide/querying.md", []byte(testCase.body), isDir))

			if got != testCase.want {
				test.Errorf("rewrite:\n got %q\nwant %q", got, testCase.want)
			}
		})
	}
}

// writeDocsTree lays out the pages llms-full.txt reads under root: a README,
// every guide in llmsGuideOrder, and a docs/cli folder with its index, a
// hand-written page and two generated command pages.
func writeDocsTree(test *testing.T, root string) {
	test.Helper()

	files := map[string]string{
		"README.md":              "# Tusk\n\nRead [install](docs/guide/install.md).\n",
		"docs/cli/README.md":     "# Tusk CLI reference\n\n- [`tusk`](tusk.md)\n",
		"docs/cli/workflows.md":  "# Tusk workflows\n",
		"docs/cli/tusk.md":       "---\ntitle: tusk\n---\n\n## tusk\n\n* [tusk query](tusk_query.md)\n",
		"docs/cli/tusk_query.md": "---\ntitle: tusk query\n---\n\n## tusk query\n\nQuery the index.\n\n\n",
	}

	for _, guide := range llmsGuideOrder {
		files["docs/guide/"+guide] = "# Guide " + guide + "\n"
	}

	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))

		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			test.Fatal(mkErr)
		}

		if writeErr := os.WriteFile(path, []byte(content), 0o644); writeErr != nil {
			test.Fatal(writeErr)
		}
	}
}

func TestBuildLLMsFullOrdersPagesUnderPathHeadings(test *testing.T) {
	root := test.TempDir()

	writeDocsTree(test, root)

	full, buildErr := buildLLMsFull(root, "docs/cli", []string{"tusk.md", "tusk_query.md"})

	if buildErr != nil {
		test.Fatalf("buildLLMsFull: %v", buildErr)
	}

	got := string(full)

	order := []string{"README.md"}

	for _, guide := range llmsGuideOrder {
		order = append(order, "docs/guide/"+guide)
	}

	order = append(order, "docs/cli/README.md", "docs/cli/workflows.md", "docs/cli/tusk.md", "docs/cli/tusk_query.md")

	previous := -1

	for _, page := range order {
		heading := "\n# " + page + "\n\nSource: https://raw.githubusercontent.com/germanamz/tusk/main/" + page + "\n\n"
		index := strings.Index(got, heading)

		if index < 0 {
			test.Fatalf("missing heading for %s in:\n%s", page, got)
		}

		if index <= previous {
			test.Errorf("%s is out of order", page)
		}

		previous = index
	}

	if strings.Contains(got, "title: tusk query") {
		test.Errorf("generated page frontmatter was not stripped:\n%s", got)
	}

	if !strings.Contains(got, "[install](https://raw.githubusercontent.com/germanamz/tusk/main/docs/guide/install.md)") {
		test.Errorf("README link was not rewritten:\n%s", got)
	}

	if !strings.Contains(got, "[tusk query](https://raw.githubusercontent.com/germanamz/tusk/main/docs/cli/tusk_query.md)") {
		test.Errorf("generated page link was not resolved against docs/cli:\n%s", got)
	}

	if !strings.HasSuffix(got, "Query the index.\n") {
		test.Errorf("output should end with the last page and one newline, got tail %q", got[max(0, len(got)-40):])
	}
}

func TestBuildLLMsFullRejectsUnlistedGuide(test *testing.T) {
	root := test.TempDir()

	writeDocsTree(test, root)

	if writeErr := os.WriteFile(filepath.Join(root, "docs", "guide", "extra.md"), []byte("# Extra\n"), 0o644); writeErr != nil {
		test.Fatal(writeErr)
	}

	_, buildErr := buildLLMsFull(root, "docs/cli", []string{"tusk.md"})

	if buildErr == nil {
		test.Fatal("expected an error for a guide missing from llmsGuideOrder")
	}

	if !strings.Contains(buildErr.Error(), "docs/guide/extra.md") || !strings.Contains(buildErr.Error(), "llmsGuideOrder") {
		test.Errorf("error should name the guide and the order list, got: %v", buildErr)
	}
}

func TestGenerateDocsWritesLLMsFull(test *testing.T) {
	root := test.TempDir()

	writeDocsTree(test, root)

	llmsPath := filepath.Join(root, "llms-full.txt")

	if generateErr := generateDocs(filepath.Join(root, "man"), filepath.Join(root, "docs", "cli"), llmsPath); generateErr != nil {
		test.Fatalf("generateDocs: %v", generateErr)
	}

	full, readErr := os.ReadFile(llmsPath)

	if readErr != nil {
		test.Fatalf("read llms-full.txt: %v", readErr)
	}

	// Pages come from the regenerated tree: a real command page, and the
	// hand-written workflows page ahead of the command pages.
	workflows := strings.Index(string(full), "\n# docs/cli/workflows.md\n")
	query := strings.Index(string(full), "\n# docs/cli/tusk_query.md\n")
	nodeCreate := strings.Index(string(full), "\n# docs/cli/tusk_node_create.md\n")

	if workflows < 0 || query < 0 || nodeCreate < 0 {
		test.Fatalf("llms-full.txt is missing CLI pages:\n%s", full)
	}

	if workflows > nodeCreate || nodeCreate > query {
		test.Errorf("CLI pages out of order: workflows %d, node create %d, query %d", workflows, nodeCreate, query)
	}

	if strings.Contains(string(full), "tusk_docgen") {
		test.Error("the hidden docgen command should not appear")
	}
}
