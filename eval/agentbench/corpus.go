package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Section is one markdown heading and the lines it spans: from the heading
// line through the line before the next heading at the same or a higher
// level. Lines are 1-based and End is inclusive.
type Section struct {
	Heading string
	Level   int
	Start   int
	End     int
}

// Contains reports whether other lies inside section (or is section).
func (section Section) Contains(other Section) bool {
	return section.Start <= other.Start && other.End <= section.End
}

// corpusFile is one markdown file of the corpus, split into lines so a
// section's text can be cut out by its line range.
type corpusFile struct {
	lines    []string
	sections []Section
}

// Corpus is the set of markdown files the agents search, keyed by their
// slash-separated path relative to the corpus root.
type Corpus struct {
	files map[string]*corpusFile
}

// LoadCorpus reads every markdown file under root, or only under paths
// (relative to root) when any are given. Files are keyed by their path
// relative to root either way.
func LoadCorpus(root string, paths ...string) (*Corpus, error) {
	corpus := &Corpus{files: make(map[string]*corpusFile)}

	if len(paths) == 0 {
		paths = []string{"."}
	}

	for _, rel := range paths {
		if walkErr := corpus.walk(root, filepath.Join(root, rel)); walkErr != nil {
			return nil, fmt.Errorf("loading corpus %s: %w", filepath.Join(root, rel), walkErr)
		}
	}

	return corpus, nil
}

func (corpus *Corpus) walk(root string, start string) error {
	return filepath.WalkDir(start, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || !isMarkdown(path) {
			return nil
		}

		content, readErr := os.ReadFile(path)

		if readErr != nil {
			return readErr
		}

		rel, relErr := filepath.Rel(root, path)

		if relErr != nil {
			return relErr
		}

		corpus.files[filepath.ToSlash(rel)] = newCorpusFile(string(content))

		return nil
	})
}

// FileCount is the number of markdown files in the corpus.
func (corpus *Corpus) FileCount() int {
	return len(corpus.files)
}

// SectionCount is the number of headed sections across the corpus.
func (corpus *Corpus) SectionCount() int {
	total := 0

	for _, file := range corpus.files {
		total += len(file.sections)
	}

	return total
}

// Has reports whether the corpus holds a file at path.
func (corpus *Corpus) Has(path string) bool {
	_, found := corpus.files[path]

	return found
}

// Sections returns the sections of file whose heading matches heading after
// normalization. Several can match when a file repeats a heading.
func (corpus *Corpus) Sections(path string, heading string) []Section {
	file, found := corpus.files[path]

	if !found {
		return nil
	}

	want := normalizeHeading(heading)

	var matches []Section

	for _, section := range file.sections {
		if normalizeHeading(section.Heading) == want {
			matches = append(matches, section)
		}
	}

	return matches
}

// Text returns the lines of section in file, heading included.
func (corpus *Corpus) Text(path string, section Section) string {
	file, found := corpus.files[path]

	if !found {
		return ""
	}

	return strings.Join(file.lines[section.Start-1:section.End], "\n")
}

func isMarkdown(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))

	return ext == ".md" || ext == ".mdx" || ext == ".markdown"
}

var (
	atxHeading = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$`)
	codeFence  = regexp.MustCompile("^ {0,3}(```|~~~)")
)

func newCorpusFile(content string) *corpusFile {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")

	return &corpusFile{lines: lines, sections: parseSections(lines)}
}

// parseSections finds the ATX headings of a markdown file, skipping YAML
// frontmatter and fenced code, and gives each one its line range. Setext
// headings are not recognized; the corpora use ATX throughout.
func parseSections(lines []string) []Section {
	var sections []Section

	inFrontmatter := len(lines) > 0 && strings.TrimSpace(lines[0]) == "---"
	fence := ""

	for index, line := range lines {
		if inFrontmatter {
			if index > 0 && strings.TrimSpace(line) == "---" {
				inFrontmatter = false
			}

			continue
		}

		if match := codeFence.FindStringSubmatch(line); match != nil {
			switch fence {
			case "":
				fence = match[1]
			case match[1]:
				fence = ""
			}

			continue
		}

		if fence != "" {
			continue
		}

		if match := atxHeading.FindStringSubmatch(line); match != nil {
			sections = append(sections, Section{
				Heading: match[2],
				Level:   len(match[1]),
				Start:   index + 1,
				End:     len(lines),
			})
		}
	}

	for index := range sections {
		for next := index + 1; next < len(sections); next++ {
			if sections[next].Level <= sections[index].Level {
				sections[index].End = sections[next].Start - 1

				break
			}
		}
	}

	return sections
}

// normalizeHeading folds the differences an agent introduces when it quotes
// a heading back: leading hashes, inline-code backticks, emphasis, case,
// runs of whitespace and a trailing colon.
func normalizeHeading(heading string) string {
	heading = strings.TrimLeft(strings.TrimSpace(heading), "#")
	heading = strings.NewReplacer("`", "", "*", "").Replace(heading)
	heading = strings.Join(strings.Fields(heading), " ")
	heading = strings.TrimSuffix(heading, ":")

	return strings.ToLower(strings.TrimSpace(heading))
}
