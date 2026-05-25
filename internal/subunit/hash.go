package subunit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
)

// HashLength is the number of hex characters retained from the
// SHA-256 digest. Twelve chars (six bytes) yields a 2^48-sized hash
// space — enough for per-file collision rates well below the rate at
// which markdown authors edit a single file. Real collisions are
// handled deterministically by ResolveCollisions below.
const HashLength = 12

// ComputeHash returns the 12-hex-char content hash for unit using
// kind-specific inputs:
//
//   - section:    heading text + heading-level + descendant body text
//     (descendant text must be supplied by the caller as
//     unit.Text — Parse assembles it from the heading and
//     all blocks until the next heading at the same or
//     shallower level).
//   - paragraph:  unit.Text.
//   - blockquote: unit.Text.
//   - list-item:  checkbox state marker + unit.Text.
//   - code-block: language tag (or empty string) + unit.Text.
//   - table-cell: row + column + cell text + column-header.
//
// All inputs derive from goldmark's normalized form (the text as
// produced by the parser walker, not the raw file bytes). This keeps
// hashes stable across CRLF/LF differences, mixed trailing
// whitespace, and other formatting drift that goldmark normalizes
// away. Changing these inputs invalidates every workspace's index on
// upgrade — treat the hash function as a stable wire format.
func ComputeHash(unit Unit) string {
	hasher := sha256.New()

	switch unit.Kind {
	case KindSection:
		_, _ = fmt.Fprintf(hasher, "section\x00%d\x00%s", intProperty(unit, "heading-level"), unit.Text)
	case KindParagraph:
		_, _ = fmt.Fprintf(hasher, "paragraph\x00%s", unit.Text)
	case KindBlockquote:
		_, _ = fmt.Fprintf(hasher, "blockquote\x00%s", unit.Text)
	case KindListItem:
		marker := "none"
		if raw, ok := unit.Properties["checkbox"]; ok {
			if boxed, isBool := raw.(bool); isBool {
				if boxed {
					marker = "checked"
				} else {
					marker = "unchecked"
				}
			}
		}
		_, _ = fmt.Fprintf(hasher, "list-item\x00%s\x00%s", marker, unit.Text)
	case KindCodeBlock:
		lang, _ := unit.Properties["lang"].(string)
		_, _ = fmt.Fprintf(hasher, "code-block\x00%s\x00%s", lang, unit.Text)
	case KindTableCell:
		row := intProperty(unit, "row")
		col := intProperty(unit, "column")
		header, _ := unit.Properties["column-header"].(string)
		_, _ = fmt.Fprintf(hasher, "table-cell\x00%d\x00%d\x00%s\x00%s", row, col, unit.Text, header)
	default:
		// Defensive: unknown kinds get a salt-free hash so the
		// parser regression is visible (the caller almost
		// certainly intended to add a new Kind to the switch).
		_, _ = fmt.Fprintf(hasher, "%s\x00%s", unit.Kind, unit.Text)
	}

	sum := hasher.Sum(nil)
	return hex.EncodeToString(sum)[:HashLength]
}

// intProperty returns the int value stored under key, accepting the
// common Go numeric types the parser produces. Returns 0 when
// missing or unparseable.
func intProperty(unit Unit, key string) int {
	raw, ok := unit.Properties[key]
	if !ok {
		return 0
	}

	switch typed := raw.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	}

	if str, isStr := raw.(string); isStr {
		if num, err := strconv.Atoi(str); err == nil {
			return num
		}
	}

	return 0
}

// ResolveCollisions assigns deterministic disambiguating suffixes to
// any units that share the same bare hash. The first occurrence (by
// ordinal) keeps the bare twelve-char hash; subsequent occurrences
// get "-1", "-2", … appended. Mutates the slice in place and returns
// it for chaining ergonomics.
//
// Ordering uses the existing Ordinal field, so the disambiguation
// outcome is independent of slice order: callers may sort or rearrange
// units freely before invoking ResolveCollisions.
func ResolveCollisions(units []Unit) []Unit {
	if len(units) < 2 {
		return units
	}

	// Group indexes by bare hash.
	groups := make(map[string][]int, len(units))
	for idx, unit := range units {
		groups[unit.Hash] = append(groups[unit.Hash], idx)
	}

	for hash, idxs := range groups {
		if len(idxs) < 2 {
			continue
		}

		// Stable order: smallest Ordinal first.
		sort.Slice(idxs, func(left, right int) bool {
			return units[idxs[left]].Ordinal < units[idxs[right]].Ordinal
		})

		// First keeps the bare hash; subsequent get "-N".
		for nth, idx := range idxs[1:] {
			units[idx].Hash = fmt.Sprintf("%s-%d", hash, nth+1)
		}
	}

	return units
}
