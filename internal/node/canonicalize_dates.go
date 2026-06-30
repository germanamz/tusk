package node

import (
	"time"

	"github.com/germanamz/tusk/internal/manifest"
)

// CanonicalizeDates rewrites date/datetime property values that the YAML parser
// produced as time.Time — from an unquoted scalar like `due: 2026-06-11` — into
// their canonical string form, choosing the layout from the declared type:
// `date` → "2006-01-02", everything else → RFC3339. A time.Time under a
// `datetime` declaration, a non-date declaration, or no declaration at all uses
// RFC3339, which is lossless. List elements are canonicalized against the list's
// item-type. Returns whether any value changed.
//
// This is the single authority for how a parsed date becomes a stored/rendered
// string. The Service write path and the reindex worker both call it before
// marshaling, validating, or rendering, so the index, the validator, and the
// on-disk file agree on one representation. Keeping the value a string (rather
// than a time.Time) is what lets it round-trip through renderMarkdown — which
// cannot serialize a time.Time — and pass the string-typed date validator.
func CanonicalizeDates(parsed *Node, decls map[string]manifest.NodeType) bool {
	if parsed == nil || len(parsed.Properties) == 0 {
		return false
	}

	declByName := map[string]manifest.PropertyDecl{}

	if nodeType, declared := decls[parsed.Type]; declared {
		for _, decl := range nodeType.Properties {
			declByName[decl.Name] = decl
		}
	}

	changed := false

	for name, value := range parsed.Properties {
		decl := declByName[name]

		switch typed := value.(type) {
		case time.Time:
			parsed.Properties[name] = typed.Format(dateLayoutFor(decl.Type))
			changed = true
		case []any:
			itemType := ""
			if decl.Type == "list-of" {
				itemType = decl.ItemType
			}

			if canonicalizeListDates(typed, itemType) {
				changed = true
			}
		}
	}

	return changed
}

// dateLayoutFor returns the canonical layout for a time.Time value given its
// declared type. A declared `date` is rendered date-only; every other case —
// `datetime`, a non-date declaration, or no declaration — uses RFC3339 so the
// instant is preserved losslessly.
func dateLayoutFor(declType string) string {
	if declType == "date" {
		return time.DateOnly
	}

	return time.RFC3339
}

// canonicalizeListDates canonicalizes any time.Time elements of a sequence
// against itemType (the list's declared item-type, or "" when undeclared).
// Returns whether any element changed.
func canonicalizeListDates(list []any, itemType string) bool {
	layout := dateLayoutFor(itemType)
	changed := false

	for index, elem := range list {
		if typed, ok := elem.(time.Time); ok {
			list[index] = typed.Format(layout)
			changed = true
		}
	}

	return changed
}
