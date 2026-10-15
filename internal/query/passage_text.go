package query

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/linenum"
)

// passageTextCap bounds the text a matched unit carries (#814). Most sections'
// own text fits whole (86% of the package docs' sections did); a longer one
// gets a window of its leaves around the one that scored.
const passageTextCap = 2048

// lineRange is a leaf's 1-based, inclusive line range within its file.
type lineRange struct {
	first int
	last  int
}

// passageFile is one file's bytes with the line table that slices them under
// the workspace's line-numbering scheme.
type passageFile struct {
	source []byte
	table  *linenum.Table
}

func newPassageFile(source []byte, scheme linenum.Scheme) passageFile {
	return passageFile{source: source, table: linenum.NewTable(source, scheme)}
}

// lines returns lines first..last of the file, CRLF folded to LF and the
// trailing line break trimmed.
func (file passageFile) lines(first, last int) string {
	text := string(file.source[file.table.Offset(first):file.table.Offset(last+1)])

	return strings.TrimRightFunc(strings.ReplaceAll(text, "\r\n", "\n"), unicode.IsSpace)
}

// passageWindow returns the passage around leaves[best] that fits in limit
// bytes, and the lines it covers. It starts at the leaf that scored and takes
// whole neighbors, the following one first, until the next leaf on either
// side would not fit. A best leaf over the limit on its own keeps the whole
// lines that fit from its start, and a first line over the limit is cut at a
// word boundary with an ellipsis.
func passageWindow(file passageFile, leaves []lineRange, best, limit int) (string, int, int) {
	span := func(low, high int) (int, int) {
		first, last := leaves[low].first, leaves[low].last

		for _, leaf := range leaves[low+1 : high+1] {
			first = min(first, leaf.first)
			last = max(last, leaf.last)
		}

		return first, last
	}

	fits := func(low, high int) bool {
		first, last := span(low, high)

		return len(file.lines(first, last)) <= limit
	}

	if !fits(best, best) {
		return clipLeaf(file, leaves[best], limit)
	}

	low, high := best, best

	for grew := true; grew; {
		grew = false

		if high+1 < len(leaves) && fits(low, high+1) {
			high++
			grew = true
		}

		if low > 0 && fits(low-1, high) {
			low--
			grew = true
		}
	}

	first, last := span(low, high)

	return file.lines(first, last), first, last
}

// clipLeaf returns the start of a leaf too long to carry whole: its whole
// lines that fit in limit bytes, or its first line cut at a word boundary
// with an ellipsis.
func clipLeaf(file passageFile, leaf lineRange, limit int) (string, int, int) {
	last := leaf.first

	for last < leaf.last && len(file.lines(leaf.first, last+1)) <= limit {
		last++
	}

	text := file.lines(leaf.first, last)

	if len(text) <= limit {
		return text, leaf.first, last
	}

	const ellipsis = "…"

	cut := limit - len(ellipsis)

	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}

	if space := strings.LastIndexFunc(text[:cut], unicode.IsSpace); space > cut/2 {
		cut = space
	}

	return strings.TrimRightFunc(text[:cut], unicode.IsSpace) + ellipsis, leaf.first, leaf.first
}

// passageReader reads matched passages' text from the files on disk for one
// query, so an agent gets the markdown a Read would show without the Read.
type passageReader struct {
	root   string
	scheme linenum.Scheme
	nodes  *index.NodeRepo
	// leaves maps a parent id (a section, or the file for leaves before the
	// first heading) to its leaf rows with line ranges, in document order.
	leaves map[string][]index.NodeRow
}

func newPassageReader(root string, scheme linenum.Scheme, nodes *index.NodeRepo, subUnits []index.NodeRow) *passageReader {
	leaves := make(map[string][]index.NodeRow)

	for _, row := range subUnits {
		if row.Type == "section" || !row.ParentID.Valid || !row.StartLine.Valid || !row.EndLine.Valid {
			continue
		}

		leaves[row.ParentID.String] = append(leaves[row.ParentID.String], row)
	}

	for _, siblings := range leaves {
		sort.SliceStable(siblings, func(left, right int) bool {
			return siblings[left].Ordinal.Int64 < siblings[right].Ordinal.Int64
		})
	}

	return &passageReader{root: root, scheme: scheme, nodes: nodes, leaves: leaves}
}

// attach sets unit's Text to the passage around bestLeaf: the leaves that
// share its parent, the reported section's own text without its heading line
// or subsections, windowed by passageWindow. The unit is left without text
// when the leaf has no lines (HTML) or the file on disk no longer matches the
// index (edited since the last reindex), so the text never comes from lines
// that moved; a missing or unreadable file is skipped the same way.
func (reader *passageReader) attach(unit *MatchedUnit, fileID, path string, bestLeaf index.NodeRow) error {
	siblings := reader.leaves[bestLeaf.ParentID.String]
	best := slices.IndexFunc(siblings, func(sibling index.NodeRow) bool { return sibling.ID == bestLeaf.ID })

	if best < 0 || path == "" {
		return nil
	}

	fileRow, getErr := reader.nodes.Get(fileID)

	if errors.Is(getErr, index.ErrNodeNotFound) {
		return nil
	}

	if getErr != nil {
		return getErr
	}

	source, readErr := os.ReadFile(filepath.Join(reader.root, filepath.FromSlash(path)))

	if readErr != nil {
		return nil
	}

	checksum := sha256.Sum256(source)

	if hex.EncodeToString(checksum[:]) != fileRow.LastChecksum {
		return nil
	}

	ranges := make([]lineRange, len(siblings))

	for position, sibling := range siblings {
		ranges[position] = lineRange{first: int(sibling.StartLine.Int64), last: int(sibling.EndLine.Int64)}
	}

	unit.Text, unit.TextStartLine, unit.TextEndLine = passageWindow(newPassageFile(source, reader.scheme), ranges, best, passageTextCap)

	return nil
}
