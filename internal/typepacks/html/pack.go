// Package html is the canonical public name for the built-in HTML
// sub-document type pack. It mirrors internal/typepacks/subdocument
// VERBATIM — the same six reserved node types, the same contains /
// contained-by edge types, and the same per-type typed properties —
// distinguished only by Source() = "html".
//
// This is a public constants/declarations package with no observable
// schema effect. It installs nothing and merges nothing: the manifest
// stores node/edge types in flat, source-blind maps, so a "merge" of
// this pack would re-install the exact declarations the subdocument
// pack already installs (a no-op). There is deliberately no
// mergeHTMLPack and no htmlPackApplied flag.
//
// The only load-bearing markdown/html distinction in the feature is the
// per-row `source` column set on sub-unit rows in Phase 5; this package
// is merely the canonical Go home for the "html" source identifier
// (Source()) and the reserved-name lists Phase 5 consumes.
//
// Note: ReservedProperties does NOT carry a "data" signals key. The
// drift exemption for the HTML signals bag is owned entirely by Phase 4
// (htmlReservedDrift, keyed on the parsed node's user-declared type),
// not by this pack.
//
// Like subdocument, the canonical node/edge declarations live in the
// manifest package (manifest.SubdocumentNodeTypes / SubdocumentEdgeTypes)
// to avoid an import cycle; this package re-exports them and owns the
// public reserved-name surface Phase 5 consumes.
package html

import (
	"sort"

	"github.com/germanamz/tusk/internal/manifest"
)

// Source returns the source-namespace identifier this typepack owns:
// "html". It is the canonical home for this string, consumed by Phase 5
// when it sets the per-row `source` column on HTML sub-unit rows. As of
// this phase it has no runtime effect.
func Source() string {
	return "html"
}

// ReservedNodeTypes are the node-type names mirrored from the
// subdocument pack. Verbatim copy of subdocument.ReservedNodeTypes.
var ReservedNodeTypes = []string{
	"section",
	"paragraph",
	"list-item",
	"code-block",
	"blockquote",
	"table-cell",
}

// ReservedEdgeTypes are the edge-type names mirrored from the
// subdocument pack. Verbatim copy of subdocument.ReservedEdgeTypes:
// `contains` is the literal edge type; `contained-by` is the derived
// inverse.
var ReservedEdgeTypes = []string{
	"contains",
	"contained-by",
}

// ReservedProperties maps each reserved node-type name to the set of
// property names the pack owns on that type. Verbatim mirror of
// subdocument.ReservedProperties — NO extra keys (in particular, no
// "data" key: the HTML signals-bag drift exemption is owned by Phase 4).
var ReservedProperties = map[string][]string{
	"section":    {"heading-level"},
	"paragraph":  {},
	"list-item":  {"checkbox"},
	"code-block": {"lang"},
	"blockquote": {},
	"table-cell": {"header", "row", "column", "column-header"},
}

// SortedReservedNodeTypes returns ReservedNodeTypes in a freshly
// allocated, lexicographically sorted slice.
func SortedReservedNodeTypes() []string {
	out := append([]string(nil), ReservedNodeTypes...)
	sort.Strings(out)
	return out
}

// SortedReservedEdgeTypes returns ReservedEdgeTypes in a freshly
// allocated, lexicographically sorted slice.
func SortedReservedEdgeTypes() []string {
	out := append([]string(nil), ReservedEdgeTypes...)
	sort.Strings(out)
	return out
}

// NodeTypes returns the pack's node-type declarations keyed by name.
// Re-exposed from manifest.SubdocumentNodeTypes: the HTML pack's
// node-type schema is identical to the markdown sub-document pack's
// (same six kinds, same typed properties); only Source() differs, so
// no separate Go declaration is warranted.
func NodeTypes() map[string]manifest.NodeType {
	return manifest.SubdocumentNodeTypes()
}

// EdgeTypes returns the pack's edge-type declarations keyed by name.
// Re-exposed from manifest.SubdocumentEdgeTypes (the `contains` edge
// is identical across both content kinds).
func EdgeTypes() manifest.EdgeTypes {
	return manifest.SubdocumentEdgeTypes()
}
