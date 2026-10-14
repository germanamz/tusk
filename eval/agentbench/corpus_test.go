package main

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleDoc = `---
type: package
title: "# not a heading"
---

# internal/embed

Intro.

## Public surface

- Drainer

` + "```bash" + `
# a comment, not a heading
` + "```" + `

## Notes

### Draining under the right settings

Text.

### What identifies a ` + "`vector`" + `

More.
`

// writeCorpus lays files out under a temp root and loads it.
func writeCorpus(harness testing.TB, files map[string]string) (*Corpus, string) {
	harness.Helper()

	root := harness.TempDir()

	for name, content := range files {
		path := filepath.Join(root, name)

		if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o755); mkdirErr != nil {
			harness.Fatal(mkdirErr)
		}

		if writeErr := os.WriteFile(path, []byte(content), 0o644); writeErr != nil {
			harness.Fatal(writeErr)
		}
	}

	corpus, loadErr := LoadCorpus(root)

	if loadErr != nil {
		harness.Fatal(loadErr)
	}

	return corpus, root
}

func TestParseSections(test *testing.T) {
	corpus, _ := writeCorpus(test, map[string]string{"docs/embed.md": sampleDoc, "docs/image.png": "x"})

	if corpus.FileCount() != 1 {
		test.Fatalf("file count = %d, want 1 (markdown only)", corpus.FileCount())
	}

	sections := corpus.files["docs/embed.md"].sections

	want := []Section{
		{Heading: "internal/embed", Level: 1, Start: 6, End: 27},
		{Heading: "Public surface", Level: 2, Start: 10, End: 17},
		{Heading: "Notes", Level: 2, Start: 18, End: 27},
		{Heading: "Draining under the right settings", Level: 3, Start: 20, End: 23},
		{Heading: "What identifies a `vector`", Level: 3, Start: 24, End: 27},
	}

	if len(sections) != len(want) {
		test.Fatalf("sections = %+v", sections)
	}

	for index, section := range sections {
		if section != want[index] {
			test.Errorf("section %d = %+v, want %+v", index, section, want[index])
		}
	}
}

func TestCorpusSectionsNormalizesHeadings(test *testing.T) {
	corpus, _ := writeCorpus(test, map[string]string{"docs/embed.md": sampleDoc})

	for _, cited := range []string{"What identifies a vector", "### what identifies a `vector`:", "  WHAT   identifies a vector "} {
		if len(corpus.Sections("docs/embed.md", cited)) != 1 {
			test.Errorf("Sections(%q) found nothing", cited)
		}
	}

	if len(corpus.Sections("docs/embed.md", "Missing")) != 0 {
		test.Error("matched a heading that isn't there")
	}

	section := corpus.Sections("docs/embed.md", "Draining under the right settings")[0]

	if text := corpus.Text("docs/embed.md", section); text != "### Draining under the right settings\n\nText.\n" {
		test.Errorf("text = %q", text)
	}
}
