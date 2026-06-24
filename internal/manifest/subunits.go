package manifest

import "maps"

// The canonical public list of reserved names lives at
// internal/typepacks/subdocument.{ReservedNodeTypes,ReservedEdgeTypes,
// ReservedProperties}. The mirror that previously lived here was used
// only by the user-vs-pack conflict-detection loop, which was removed
// in Phase 4 Task 2 — those collisions are no longer real conflicts
// because the pack's reservations are scoped to source = "markdown"
// while user declarations live at source = NULL.

// SubdocumentNodeTypes returns a freshly allocated map of the built-in
// sub-document pack's node-type declarations. Mirrored by
// internal/typepacks/subdocument.NodeTypes for external callers.
func SubdocumentNodeTypes() map[string]NodeType {
	return subdocumentNodeTypes()
}

// SubdocumentEdgeTypes returns a freshly allocated map of the built-in
// sub-document pack's edge-type declarations.
func SubdocumentEdgeTypes() EdgeTypes {
	return subdocumentEdgeTypes()
}

// subdocumentNodeTypes returns a freshly allocated map of the pack's
// node-type declarations. Used both by the in-package merger and by the
// public re-exporter in internal/typepacks/subdocument.
func subdocumentNodeTypes() map[string]NodeType {
	return map[string]NodeType{
		"section": {
			Description: "A heading and its descendant content within a markdown document.",
			Properties: []PropertyDecl{
				{Name: "heading-level", Type: "int", Required: true, Description: "Heading depth (1-6)."},
			},
		},
		"paragraph": {
			Description: "A paragraph block within a markdown document.",
		},
		"list-item": {
			Description: "A list item within a bulleted, numbered, or task list.",
			Properties: []PropertyDecl{
				{Name: "checkbox", Type: "bool", Required: false, Description: "Checkbox state for task-list items; absent for plain list items."},
			},
		},
		"code-block": {
			Description: "A fenced code block within a markdown document.",
			Properties: []PropertyDecl{
				{Name: "lang", Type: "string", Required: false, Description: "Language tag declared on the fence, when present."},
			},
		},
		"blockquote": {
			Description: "A blockquote within a markdown document; nested blockquotes flatten to one unit.",
		},
		"table-cell": {
			Description: "A single cell of a markdown table (header or body).",
			Properties: []PropertyDecl{
				{Name: "header", Type: "bool", Required: true, Description: "True for cells in the table's header row."},
				{Name: "row", Type: "int", Required: true, Description: "0-based row index (header row is 0 when present)."},
				{Name: "column", Type: "int", Required: true, Description: "0-based column index."},
				{Name: "column-header", Type: "string", Required: false, Description: "Header-cell text for this column when the table has a header row."},
			},
		},
	}
}

// subdocumentEdgeTypes returns a freshly allocated map of the pack's
// edge-type declarations. `contains` is one-to-many, acyclic, with
// `contained-by` as its derived inverse.
//
// The Ordered/OrderedBy fields are intentionally left zero. The
// `contains` edge is ordered by the target's `nodes.ordinal` column
// (populated by Task 3's sync pipeline), not by a property on the
// source. The current grammar's per-edge ordering plumbing assumes a
// source-side property, so this edge leaves the field unset to avoid
// sending a half-correct hint; Task 3 wires the actual ordering.
func subdocumentEdgeTypes() EdgeTypes {
	return EdgeTypes{
		"contains": {
			Description: "A file contains a sub-unit (heading, paragraph, list-item, code-block, blockquote, or table-cell).",
			From:        []string{"*"},
			To: []string{
				"section", "paragraph", "list-item", "code-block", "blockquote", "table-cell",
			},
			Cardinality: CardinalityOneToMany,
			Inverse:     "contained-by",
			Acyclic:     true,
		},
	}
}

// mergeSubdocumentPack merges the built-in sub-document pack into
// loaded when the manifest opts into sub-units (default true). The
// merge installs the pack's node and edge types into the manifest's
// effective NodeTypes/EdgeTypes maps.
//
// Reservation scope: the pack reserves its names within
// source = subdocument.Source() (i.e., source = "markdown"). User
// declarations of the same names live in the user namespace
// (source = NULL) and are NOT in conflict with the pack — they
// describe a different slice of the index, so a user-vs-pack collision
// cannot occur under today's manifest grammar.
//
// Map mechanics: the pack's declarations overwrite any user entry of
// the same name in the flat NodeTypes/EdgeTypes maps. Source-keyed
// grammar storage is out of scope for this phase; full source
// scoping of the in-memory manifest lands with the
// user-configurable-sources extension.
//
// Idempotent: re-running re-installs the pack's declarations
// deterministically.
func mergeSubdocumentPack(loaded *Manifest) {
	if !loaded.SubUnitsEnabled() {
		return
	}

	if loaded.subdocumentPackApplied {
		return
	}

	// Install the built-in declarations.
	if loaded.NodeTypes == nil {
		loaded.NodeTypes = make(map[string]NodeType)
	}

	maps.Copy(loaded.NodeTypes, subdocumentNodeTypes())

	if loaded.EdgeTypes == nil {
		loaded.EdgeTypes = make(EdgeTypes)
	}

	maps.Copy(loaded.EdgeTypes, subdocumentEdgeTypes())

	loaded.subdocumentPackApplied = true
}
