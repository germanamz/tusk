package htmlunit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/germanamz/tusk/internal/subunit"
)

// headingLevel maps an html heading tag name to its 1..6 level. The
// boolean is false for non-heading tags.
func headingLevel(tag string) (int, bool) {
	switch tag {
	case "h1":
		return 1, true
	case "h2":
		return 2, true
	case "h3":
		return 3, true
	case "h4":
		return 4, true
	case "h5":
		return 5, true
	case "h6":
		return 6, true
	default:
		return 0, false
	}
}

// sectionFrame is one entry on the open-heading stack: the heading
// level (for nesting/closing), the section's content hash (for the
// ParentHash wiring), its structural address ("S1.2"), and the
// per-kind counters for the leaves directly beneath it. The per-table
// counter joins this frame in Task 4 when table cells gain addresses.
type sectionFrame struct {
	level         int
	hash          string
	address       string
	childHeadings int
	kindCounts    map[subunitKind]int
}

// subunitKind is an alias so address bookkeeping reads cleanly without
// importing the subunit package into this file's type signatures.
type subunitKind = string

// walkCtx threads positional state through the DOM walk. Created per
// Parse call; mutated as sections open/close and leaves emit. The root
// frame (address "") owns counters for content before the first
// heading or in a heading-free document.
type walkCtx struct {
	stack        []sectionFrame
	root         sectionFrame
	rootHeadings int
}

func newWalkCtx() *walkCtx {
	return &walkCtx{root: sectionFrame{kindCounts: map[subunitKind]int{}}}
}

// deepest returns the current deepest open section, or the root frame
// when none is open. Valid only until the next stack append.
func (c *walkCtx) deepest() *sectionFrame {
	if len(c.stack) == 0 {
		return &c.root
	}
	return &c.stack[len(c.stack)-1]
}

func (c *walkCtx) currentAddress() string { return c.deepest().address }
func (c *walkCtx) currentHash() string    { return c.deepest().hash }

// openSectionAddress computes the address for a heading opening under
// the current deepest frame, advancing that frame's child-heading
// counter. Caller must have already closed sections at or deeper than
// the new level. Address depth follows nesting, not the tag number.
func (c *walkCtx) openSectionAddress() string {
	parent := c.deepest()
	if parent.address == "" {
		c.rootHeadings++
		return "S" + strconv.Itoa(c.rootHeadings)
	}
	parent.childHeadings++
	return parent.address + "." + strconv.Itoa(parent.childHeadings)
}

func (c *walkCtx) push(level int, hash, address string) {
	c.stack = append(c.stack, sectionFrame{
		level:      level,
		hash:       hash,
		address:    address,
		kindCounts: map[subunitKind]int{},
	})
}

// closeToLevel pops every open section at or deeper than level so a new
// heading at that level nests correctly.
func (c *walkCtx) closeToLevel(level int) {
	for len(c.stack) > 0 && c.stack[len(c.stack)-1].level >= level {
		c.stack = c.stack[:len(c.stack)-1]
	}
}

// leafAddress returns the address for a leaf of kind k under the
// deepest frame, advancing that kind's counter. Returns "" for kinds
// with no address rule (caller falls back to the content hash).
func (c *walkCtx) leafAddress(kind subunit.Kind) string {
	letter, ok := leafLetter(kind)
	if !ok {
		return ""
	}
	frame := c.deepest()
	frame.kindCounts[subunitKind(kind)]++
	return frame.address + letter + strconv.Itoa(frame.kindCounts[subunitKind(kind)])
}

// leafLetter maps a leaf kind to its address letter, matching the
// subunit grammar (P/B/Q/L). The boolean is false for kinds with no
// structural address rule (they fall back to the content hash).
func leafLetter(k subunit.Kind) (string, bool) {
	switch k {
	case subunit.KindParagraph:
		return "P", true
	case subunit.KindCodeBlock:
		return "B", true
	case subunit.KindBlockquote:
		return "Q", true
	case subunit.KindListItem:
		return "L", true
	default:
		return "", false
	}
}

// disambiguateFallbackIDs suffixes the Hash of any units that would
// otherwise share a fallback id. Structural addresses are unique by
// construction, so this only fires for kinds with no address rule.
// Mirrors subunit.disambiguateFallbackIDs (address.go:135).
func disambiguateFallbackIDs(units []subunit.Unit) []subunit.Unit {
	seen := map[string]int{}
	for idx := range units {
		id := units[idx].Address
		if id == "" {
			id = units[idx].Hash
		}
		if nth := seen[id]; nth > 0 {
			units[idx].Hash += "-" + strconv.Itoa(nth)
		}
		seen[id]++
	}
	return units
}

// contentHashFor returns the fingerprint stored on nodes.content_hash.
// For leaf kinds it is sha256(EmbedPayload); for sections it is
// sha256("section\x00<level>\x00<heading-text>"). Mirrors
// subunit.contentHashFor (address.go:162) so the HTML and markdown
// namespaces share content-addressing semantics.
func contentHashFor(unit subunit.Unit) string {
	// mirrors internal/subunit/address.go contentHashFor (section salt: sha256("section\x00<level>\x00<heading>"))  — keep in sync; subunit helpers are unexported.
	if unit.Kind == subunit.KindSection {
		sum := sha256.Sum256(fmt.Appendf(nil, "section\x00%d\x00%s", sectionLevel(unit), firstLine(unit.Text)))
		return hex.EncodeToString(sum[:])
	}
	payload := unit.EmbedPayload
	if payload == "" {
		payload = unit.Text
	}
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// sectionLevel reads the integer heading-level property the parser
// stamps on every section unit, returning 0 when absent or malformed.
func sectionLevel(unit subunit.Unit) int {
	if raw, ok := unit.Properties["heading-level"]; ok {
		if lvl, isInt := raw.(int); isInt {
			return lvl
		}
	}
	return 0
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
