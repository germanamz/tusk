package main

import (
	"path/filepath"
	"testing"
)

func TestScoreCitations(test *testing.T) {
	corpus, root := writeCorpus(test, map[string]string{"docs/embed.md": sampleDoc, "docs/other.md": "# Other\n\n## Notes\n"})
	task := Task{ID: "notes", Targets: []Target{{File: "docs/embed.md", Heading: "Notes"}}}

	cases := []struct {
		name      string
		citations []Citation
		want      Score
	}{
		{"exact section", []Citation{{"docs/embed.md", "Notes"}}, Score{FileHit: true, SectionHit: true}},
		{"nested section", []Citation{{"docs/embed.md", "Draining under the right settings"}}, Score{FileHit: true, SectionHit: true}},
		{"enclosing title is too broad", []Citation{{"docs/embed.md", "internal/embed"}}, Score{FileHit: true}},
		{"sibling section", []Citation{{"docs/embed.md", "Public surface"}}, Score{FileHit: true}},
		{"same heading, other file", []Citation{{"docs/other.md", "Notes"}}, Score{}},
		{"heading path", []Citation{{"docs/embed.md", "internal/embed > Notes"}}, Score{FileHit: true, SectionHit: true}},
		{"dot slash and anchor", []Citation{{"./docs/embed.md#notes", "## Notes"}}, Score{FileHit: true, SectionHit: true}},
		{"line suffix", []Citation{{"docs/embed.md:18-27", "Notes"}}, Score{FileHit: true, SectionHit: true}},
		{"absolute path in the run dir", []Citation{{filepath.Join(root, "docs/embed.md"), "Notes"}}, Score{FileHit: true, SectionHit: true}},
		{"any citation can hit", []Citation{{"docs/other.md", "Other"}, {"docs/embed.md", "Notes"}}, Score{FileHit: true, SectionHit: true}},
		{"no citations", nil, Score{}},
	}

	for _, testCase := range cases {
		if got := ScoreCitations(corpus, task, testCase.citations, root); got != testCase.want {
			test.Errorf("%s: got %+v, want %+v", testCase.name, got, testCase.want)
		}
	}
}
