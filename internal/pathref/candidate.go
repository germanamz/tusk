// Package pathref finds the workspace paths a page names in inline code. The
// rules here decide what a span looks like (Candidate), Markdown and Spans pull
// the candidates out of a page, and Disk answers whether one names something on
// disk. The stored refs depend on page text alone; the disk is consulted only
// when a reader asks, so a fresh index and a long-lived one agree.
package pathref

import (
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// RulesVersion tags the candidate and extraction rules. Reindex folds it into
// the marker that forces one full re-process, so bump it whenever a change
// here would record different refs for a page already on disk.
const RulesVersion = "1"

// maxSpanBytes caps the span length worth testing. A real path is far shorter;
// anything longer is a code sample, not a reference.
const maxSpanBytes = 512

// lineSuffix matches a trailing line reference: ":42", ":40-52" or ":40:7".
var lineSuffix = regexp.MustCompile(`:[0-9]+(?:[-:][0-9]+)?$`)

// forbiddenRunes can't appear in a candidate. They mark globs, shell syntax,
// URLs, quoting, anchors and assignments rather than plain paths.
const forbiddenRunes = "*?[]{}<>|\"'`$\\;,=()!&#%^:"

// Candidate reports whether a code span is, as a whole, a workspace-relative
// path, and returns it cleaned (no trailing slash, no doubled separators).
//
// The span is trimmed and a trailing line reference (":42", ":40-52") is
// dropped. It is then rejected when it holds whitespace, a control character
// or one of forbiddenRunes; when it starts with "/", "./", "../", "~" or "-";
// when any segment is ".."; when it has no letter or digit; or when it holds
// neither a "/" nor a ".". The last rule keeps bare identifiers like
// `ExtractWikilinks` out, at the cost of extensionless root files (`Makefile`).
// Whether the path exists is not checked here; see Disk.
func Candidate(span string) (string, bool) {
	span = strings.TrimSpace(span)

	if span == "" || len(span) > maxSpanBytes {
		return "", false
	}

	span = lineSuffix.ReplaceAllString(span, "")

	if !strings.ContainsAny(span, "/.") || strings.ContainsAny(span, forbiddenRunes) {
		return "", false
	}

	for _, prefix := range []string{"/", "./", "../", "~", "-"} {
		if strings.HasPrefix(span, prefix) {
			return "", false
		}
	}

	hasWord := false

	for _, char := range span {
		if unicode.IsSpace(char) || unicode.IsControl(char) {
			return "", false
		}

		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			hasWord = true
		}
	}

	if !hasWord {
		return "", false
	}

	if slices.Contains(strings.Split(span, "/"), "..") {
		return "", false
	}

	cleaned := path.Clean(span)

	if cleaned == "." {
		return "", false
	}

	return cleaned, true
}

// Clean normalizes a path someone asks about (a names-path filter value, a file
// the plugin hook saw edited) into the form refs are stored in. It is looser
// than Candidate: any workspace-relative path is fine, including `Makefile` and
// a leading "./". A trailing line reference is dropped. It rejects an empty
// value, an absolute path, and one that climbs out of the workspace.
func Clean(value string) (string, bool) {
	value = lineSuffix.ReplaceAllString(strings.TrimSpace(value), "")

	if value == "" || strings.HasPrefix(value, "/") {
		return "", false
	}

	cleaned := path.Clean(value)

	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}

	return cleaned, true
}

// Ancestors returns target followed by every directory above it, nearest
// first: "a/b/c.go" gives ["a/b/c.go", "a/b", "a"]. A page that names any of
// them names target too, since a directory ref covers what it holds.
func Ancestors(target string) []string {
	ancestors := []string{target}

	for current := target; ; {
		parent := path.Dir(current)

		if parent == "." || parent == "/" || parent == current {
			return ancestors
		}

		ancestors = append(ancestors, parent)
		current = parent
	}
}
