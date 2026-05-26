// Package subdocument is the canonical public name for the built-in
// sub-document type pack. The pack ships with the engine — its node
// types, edge type, and reserved-name list are merged into a loaded
// manifest by manifest.MergeBuiltinPacks (gated by [workspace]
// sub-units). CLI command handlers and the MCP runtime call
// MergeBuiltinPacks explicitly after manifest.Load, mirroring the
// ValidateAliases / ValidateContext post-load step.
//
// External consumers (e.g., the Task 2 AST parser, the Task 3 sync
// pipeline) import this package to read the pack's structure without
// depending on manifest's private merge code. The merge itself lives in
// the manifest package to avoid an import cycle between subdocument and
// manifest.
package subdocument

import (
	"sort"

	"github.com/germanamz/tusk/internal/manifest"
)

// Source returns the source-namespace identifier this typepack
// owns: "markdown". Every node-type in ReservedNodeTypes and every
// edge-type in ReservedEdgeTypes is reserved only within rows whose
// `source` column matches this value; the user namespace
// (`source = NULL`) is unaffected.
func Source() string {
	return "markdown"
}

// ReservedNodeTypes are the node-type names owned by the sub-document
// pack within source = Source() (i.e., source='markdown'). A user
// manifest that declares any of these under [node-types.<name>] in
// the user namespace (source = NULL) is allowed; only declarations
// targeting the same source raise manifest.SubUnitConflict (rescoped
// in Phase 4, Task 2).
var ReservedNodeTypes = []string{
	"section",
	"paragraph",
	"list-item",
	"code-block",
	"blockquote",
	"table-cell",
}

// ReservedEdgeTypes are the edge-type names owned (or derived) by
// the pack within source = Source(). `contains` is the literal edge
// type; `contained-by` is the inverse name derived by the grammar
// machinery from the `inverse` field on the `contains` edge type.
// User-namespace declarations (source = NULL) of the same names are
// allowed; only within-source duplicates raise a conflict.
var ReservedEdgeTypes = []string{
	"contains",
	"contained-by",
}

// ReservedProperties maps each reserved node-type name to the set of
// property names the pack owns on that type.
var ReservedProperties = map[string][]string{
	"section":    {"heading-level"},
	"paragraph":  {},
	"list-item":  {"checkbox"},
	"code-block": {"lang"},
	"blockquote": {},
	"table-cell": {"header", "row", "column", "column-header"},
}

// NodeTypes returns the pack's node-type declarations keyed by name.
// Re-exposed from manifest.SubdocumentNodeTypes so the manifest package
// owns the canonical data and this package owns the public API surface
// future tasks consume.
func NodeTypes() map[string]manifest.NodeType {
	return manifest.SubdocumentNodeTypes()
}

// EdgeTypes returns the pack's edge-type declarations keyed by name.
func EdgeTypes() manifest.EdgeTypes {
	return manifest.SubdocumentEdgeTypes()
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
