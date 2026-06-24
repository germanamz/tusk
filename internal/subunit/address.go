package subunit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

// sectionFrame is one entry on the open-heading stack. It carries the heading
// level (for nesting), the section's structural address ("S1.2"), and the
// per-kind / per-table counters for the leaves directly beneath it.
type sectionFrame struct {
	level         int
	address       string
	childHeadings int
	kindCounts    map[Kind]int
	tableCount    int
}

// WalkCtx threads the positional state Parse needs to assign structural
// addresses. It is created per Parse call and mutated as sections open/close
// and leaves emit. The root frame (address "") owns the counters for content
// before the first heading (or in a heading-free document).
//
// WalkCtx is the single owner of the structural-address grammar and the
// content-hash bookkeeping for both the markdown walker (internal/subunit) and
// the HTML walker (internal/htmlunit). Only the tree traversal differs between
// the two packages; the address/id/hash machinery is shared here so the two
// formats stay byte-identical on the wire.
type WalkCtx struct {
	stack        []sectionFrame
	root         sectionFrame
	rootHeadings int
}

// NewWalkCtx returns a WalkCtx whose root frame has initialized counters.
func NewWalkCtx() *WalkCtx {
	return &WalkCtx{root: sectionFrame{kindCounts: map[Kind]int{}}}
}

// deepest returns a pointer to the current deepest open section, or the root
// frame when no section is open. The pointer is valid only until the next
// stack append; callers must not retain it across OpenSectionAddress/Push.
func (c *WalkCtx) deepest() *sectionFrame {
	if len(c.stack) == 0 {
		return &c.root
	}
	return &c.stack[len(c.stack)-1]
}

// CurrentAddress is the deepest open section's path, or "" at the root.
func (c *WalkCtx) CurrentAddress() string { return c.deepest().address }

// OpenSectionAddress computes the address for a heading opening under the
// current deepest frame, advancing that frame's child-heading counter. Caller
// must have already closed any sections at or deeper than the new level.
func (c *WalkCtx) OpenSectionAddress() string {
	parent := c.deepest()
	if parent.address == "" {
		c.rootHeadings++
		return "S" + strconv.Itoa(c.rootHeadings)
	}
	parent.childHeadings++
	return parent.address + "." + strconv.Itoa(parent.childHeadings)
}

// Push appends a new open section frame with fresh leaf counters.
func (c *WalkCtx) Push(level int, address string) {
	c.stack = append(c.stack, sectionFrame{
		level:      level,
		address:    address,
		kindCounts: map[Kind]int{},
	})
}

// CloseToLevel pops every open section at or deeper than level so a new
// heading at that level nests correctly.
func (c *WalkCtx) CloseToLevel(level int) {
	for len(c.stack) > 0 && c.stack[len(c.stack)-1].level >= level {
		c.stack = c.stack[:len(c.stack)-1]
	}
}

// LeafAddress returns the address for a leaf of kind k under the deepest
// frame, advancing that kind's counter. Returns "" for kinds with no address
// rule, signalling the caller to fall back to the content hash.
func (c *WalkCtx) LeafAddress(kind Kind) string {
	letter, ok := LeafLetter(kind)
	if !ok {
		return ""
	}
	frame := c.deepest()
	frame.kindCounts[kind]++
	return frame.address + letter + strconv.Itoa(frame.kindCounts[kind])
}

// NextTableIndex advances and returns the deepest frame's table counter.
func (c *WalkCtx) NextTableIndex() int {
	f := c.deepest()
	f.tableCount++
	return f.tableCount
}

// LeafLetter maps a leaf kind to its address letter. The boolean is false for
// kinds that carry no structural address rule (they fall back to the hash).
func LeafLetter(k Kind) (string, bool) {
	switch k {
	case KindParagraph:
		return "P", true
	case KindCodeBlock:
		return "B", true
	case KindBlockquote:
		return "Q", true
	case KindListItem:
		return "L", true
	default:
		return "", false
	}
}

// TableCellAddress builds "<path>T<k>R<row>C<col>" for a table cell.
func TableCellAddress(path string, tableIdx, row, col int) string {
	return fmt.Sprintf("%sT%dR%dC%d", path, tableIdx, row, col)
}

// DisambiguateFallbackIDs suffixes the content hash of any units that would
// otherwise share a row id. Structural addresses are unique by construction
// (position within the file), so this only ever fires for kinds with no
// address rule, which fall back to their content hash. The first occurrence
// keeps the bare hash; later duplicates get "-1", "-2", … Mutates and returns
// the slice. Replaces the former ResolveCollisions, which suffixed every
// content-hash collision back when the hash was the identity.
func DisambiguateFallbackIDs(units []Unit) []Unit {
	seen := map[string]int{}

	for idx := range units {
		id := units[idx].Address
		if id == "" {
			id = units[idx].Hash
		}

		if nth := seen[id]; nth > 0 {
			// Only fallback (hash) ids can reach here; addressed units are
			// unique, so suffixing the hash is safe and never alters a
			// structural address.
			units[idx].Hash += "-" + strconv.Itoa(nth)
		}

		seen[id]++
	}

	return units
}

// ContentHashFor returns the fingerprint stored on nodes.content_hash. For leaf
// kinds it is sha256(EmbedPayload) — exactly the bytes the embedder sends — so a
// leaf that merely shifts position keeps its hash and reuses its vector. For
// sections it is sha256("section\x00<level>\x00<heading-text>") so heading edits
// are detected (sections are never embedded). Lowercase hex.
//
// Shared by the markdown and HTML walkers so the two formats produce
// byte-identical content hashes for equivalent units.
func ContentHashFor(unit Unit) string {
	if unit.Kind == KindSection {
		sum := sha256.Sum256(fmt.Appendf(nil, "section\x00%d\x00%s", intProperty(unit, "heading-level"), firstLine(unit.Text)))
		return hex.EncodeToString(sum[:])
	}
	payload := unit.EmbedPayload
	if payload == "" {
		payload = unit.Text
	}
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// firstLine returns text up to (not including) the first newline.
func firstLine(text string) string {
	for idx := 0; idx < len(text); idx++ {
		if text[idx] == '\n' {
			return text[:idx]
		}
	}
	return text
}
