package main

import (
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Citation is one section an agent's answer relies on, as the agent
// reported it.
type Citation struct {
	File    string `json:"file"`
	Heading string `json:"heading"`
}

// Score is how an answer's citations line up with a task's targets.
type Score struct {
	// FileHit: some citation names a target file.
	FileHit bool
	// SectionHit: some citation names a target section, or a section nested
	// inside one. Citing only a broader enclosing section (the file's title,
	// say) is a file hit but not a section hit.
	SectionHit bool
}

// ScoreCitations scores citations against task's targets. runDir is the
// directory the agent ran in, so an absolute path it cites can be made
// relative to the corpus.
func ScoreCitations(corpus *Corpus, task Task, citations []Citation, runDir string) Score {
	var score Score

	for _, citation := range citations {
		file := normalizeCitedFile(citation.File, runDir)

		for _, target := range task.Targets {
			if file != target.File {
				continue
			}

			score.FileHit = true

			if citesWithin(corpus, file, citation.Heading, target.Heading) {
				score.SectionHit = true
			}
		}
	}

	return score
}

// citesWithin reports whether the cited heading resolves to a section of
// file that lies inside the target section. Agents sometimes quote a heading
// path ("Notes > Draining"), so each segment is tried as well.
func citesWithin(corpus *Corpus, file string, cited string, target string) bool {
	candidates := []string{cited}

	for _, separator := range []string{">", "›", "/"} {
		if strings.Contains(cited, separator) {
			candidates = append(candidates, strings.Split(cited, separator)...)
		}
	}

	targetSections := corpus.Sections(file, target)

	for _, candidate := range candidates {
		for _, citedSection := range corpus.Sections(file, candidate) {
			for _, targetSection := range targetSections {
				if targetSection.Contains(citedSection) {
					return true
				}
			}
		}
	}

	return false
}

// citedSuffix matches what agents append to a cited path: a "#anchor" or a
// ":12" / ":12-21" line reference.
var citedSuffix = regexp.MustCompile(`(#.*|:\d+(-\d+)?)$`)

// normalizeCitedFile turns the path an agent cited into a corpus-relative,
// slash-separated path: an anchor or line suffix is dropped, an absolute
// path under runDir loses that prefix, and "./" and redundant segments are
// cleaned away.
func normalizeCitedFile(cited string, runDir string) string {
	cited = citedSuffix.ReplaceAllString(strings.TrimSpace(cited), "")

	if filepath.IsAbs(cited) && runDir != "" {
		if rel, relErr := filepath.Rel(resolveDir(runDir), resolveDir(cited)); relErr == nil {
			cited = rel
		}
	}

	return strings.TrimPrefix(path.Clean(filepath.ToSlash(cited)), "./")
}

// resolveDir follows symlinks so /tmp and /private/tmp compare equal on
// macOS. A path that can't be resolved is returned as is.
func resolveDir(dir string) string {
	if resolved, evalErr := filepath.EvalSymlinks(dir); evalErr == nil {
		return resolved
	}

	return dir
}
