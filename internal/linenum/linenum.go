// Package linenum turns byte offsets in a source file into 1-based line
// numbers under a configurable line-numbering scheme.
//
// Tools disagree about what ends a line. Every common tool agrees on LF and
// CRLF (CRLF ends in LF), so the schemes differ only on a lone carriage return
// and the Unicode separators. A scheme exists so the line numbers tusk reports
// match the tool a reader opens the file with. It never changes how a file is
// parsed: markdown parsing always follows CommonMark.
package linenum

import (
	"bytes"
	"fmt"
	"sort"
)

// Scheme names which byte sequences end a line.
type Scheme string

const (
	// SchemeLF ends a line at LF only, so CRLF counts once and a lone CR is
	// ordinary text. Matches sed, grep -n, wc -l, cat -n and file-read tools.
	SchemeLF Scheme = "lf"
	// SchemeUniversal adds a lone CR to SchemeLF: the CommonMark line endings,
	// which most editors also follow.
	SchemeUniversal Scheme = "universal"
	// SchemeUnicode adds the Unicode mandatory breaks (UAX #14) to
	// SchemeUniversal: VT, FF, NEL (U+0085), LS (U+2028) and PS (U+2029).
	SchemeUnicode Scheme = "unicode"
)

// DefaultScheme applies when the manifest leaves the scheme unset.
const DefaultScheme = SchemeLF

// ParseScheme resolves a manifest value to a Scheme. The empty string is the
// default; any other value outside the three schemes is an error.
func ParseScheme(value string) (Scheme, error) {
	switch Scheme(value) {
	case "":
		return DefaultScheme, nil
	case SchemeLF, SchemeUniversal, SchemeUnicode:
		return Scheme(value), nil
	}

	return "", fmt.Errorf("line-numbering = %q is not supported (want %s | %s | %s)", value, SchemeLF, SchemeUniversal, SchemeUnicode)
}

// Table maps byte offsets in one source to line numbers. Build it once per
// file and query it for every offset.
type Table struct {
	// lineStarts holds the byte offset where each line after the first
	// begins, ascending.
	lineStarts []int
}

// NewTable scans source once and records where each line begins under
// scheme. An unknown scheme falls back to DefaultScheme.
func NewTable(source []byte, scheme Scheme) *Table {
	table := &Table{}

	for offset := 0; offset < len(source); {
		width := terminatorWidth(source[offset:], scheme)

		if width == 0 {
			offset++

			continue
		}

		offset += width
		table.lineStarts = append(table.lineStarts, offset)
	}

	return table
}

// Line returns the 1-based line holding the byte at offset. A terminator's
// bytes belong to the line they end.
func (table *Table) Line(offset int) int {
	return sort.SearchInts(table.lineStarts, offset+1) + 1
}

// terminatorWidth returns the byte length of the line terminator at the head
// of rest under scheme, or 0 when rest does not start with one.
func terminatorWidth(rest []byte, scheme Scheme) int {
	switch rest[0] {
	case '\n':
		return 1
	case '\r':
		if scheme == SchemeLF {
			return 0
		}

		if len(rest) > 1 && rest[1] == '\n' {
			return 2
		}

		return 1
	}

	if scheme != SchemeUnicode {
		return 0
	}

	switch {
	case rest[0] == '\v', rest[0] == '\f':
		return 1
	case bytes.HasPrefix(rest, nextLine):
		return len(nextLine)
	case bytes.HasPrefix(rest, lineSeparator), bytes.HasPrefix(rest, paragraphSeparator):
		return len(lineSeparator)
	}

	return 0
}

var (
	nextLine           = []byte("\u0085")
	lineSeparator      = []byte(" ")
	paragraphSeparator = []byte(" ")
)
