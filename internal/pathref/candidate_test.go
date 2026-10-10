package pathref_test

import (
	"slices"
	"testing"

	"github.com/germanamz/tusk/internal/pathref"
)

func TestCandidate_AcceptsWorkspacePaths(test *testing.T) {
	cases := map[string]string{
		"server/ledger/core/service.go": "server/ledger/core/service.go",
		"server/ledger/":                "server/ledger",
		"go.mod":                        "go.mod",
		"internal/node":                 "internal/node",
		"docs//cli/x.md":                "docs/cli/x.md",
		" internal/node/edges.go ":      "internal/node/edges.go",
		"internal/node/edges.go:42":     "internal/node/edges.go",
		"internal/node/edges.go:40-52":  "internal/node/edges.go",
		"internal/node/edges.go:40:7":   "internal/node/edges.go",
		"node_modules/@scope/pkg/x.js":  "node_modules/@scope/pkg/x.js",
		"docs/café.md":                  "docs/café.md",
		".github/workflows/ci.yml":      ".github/workflows/ci.yml",
		"Makefile":                      "Makefile",
		"Dockerfile:12":                 "Dockerfile",
		"LICENSE":                       "LICENSE",
		"justfile":                      "justfile",
	}

	for span, want := range cases {
		got, ok := pathref.Candidate(span)

		if !ok {
			test.Errorf("Candidate(%q) rejected, want %q", span, want)

			continue
		}

		if got != want {
			test.Errorf("Candidate(%q) = %q, want %q", span, got, want)
		}
	}
}

func TestCandidate_RejectsNonPaths(test *testing.T) {
	spans := []string{
		"",
		"   ",
		"ExtractWikilinks",
		"Readme",
		"MAKEFILE",
		"tusk query type=note",
		"https://example.com/a.go",
		"/etc/hosts",
		"./scripts/build.sh",
		"../server/x.go",
		"~/notes/a.md",
		"server/../etc/passwd",
		"docs/*.md",
		"docs/**",
		"a/b?.go",
		"x[1]/y.md",
		"{a,b}/c",
		"a/b|c",
		"$HOME/x.go",
		"C:\\x\\y.go",
		"foo:bar/baz",
		"--include=paths",
		"-v.x",
		"...",
		"./...",
		"a/b c.go",
		"a/\tb.go",
		"fmt.Println(x)",
		"a=b/c",
		"node.Node#Edges",
		"'quoted/x.go'",
	}

	for _, span := range spans {
		if got, ok := pathref.Candidate(span); ok {
			test.Errorf("Candidate(%q) = %q, want rejected", span, got)
		}
	}
}

func TestClean_NormalizesQueryValues(test *testing.T) {
	cases := map[string]string{
		"server/ledger/":           "server/ledger",
		"./server/ledger/x.go":     "server/ledger/x.go",
		"Makefile":                 "Makefile",
		"server/ledger/x.go:42":    "server/ledger/x.go",
		"  server//ledger/./x.go ": "server/ledger/x.go",
	}

	for value, want := range cases {
		got, ok := pathref.Clean(value)

		if !ok || got != want {
			test.Errorf("Clean(%q) = %q, %v; want %q, true", value, got, ok, want)
		}
	}

	for _, value := range []string{"", "/abs/x.go", "../up.go", "a/../../b", "."} {
		if got, ok := pathref.Clean(value); ok {
			test.Errorf("Clean(%q) = %q, want rejected", value, got)
		}
	}
}

func TestAncestors_NearestFirst(test *testing.T) {
	got := pathref.Ancestors("server/ledger/core/service.go")
	want := []string{"server/ledger/core/service.go", "server/ledger/core", "server/ledger", "server"}

	if !slices.Equal(got, want) {
		test.Errorf("Ancestors = %v, want %v", got, want)
	}

	if got := pathref.Ancestors("go.mod"); !slices.Equal(got, []string{"go.mod"}) {
		test.Errorf("Ancestors(go.mod) = %v", got)
	}
}
