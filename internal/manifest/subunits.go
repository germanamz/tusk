package manifest

import "sort"

// subdocumentReservedNodeTypes are the node-type names owned by the
// built-in sub-document pack. Mirrored at
// internal/typepacks/subdocument.ReservedNodeTypes for external callers
// (Task 2 onward) that need the same list without importing manifest's
// internal merge code.
var subdocumentReservedNodeTypes = []string{
	"section",
	"paragraph",
	"list-item",
	"code-block",
	"blockquote",
	"table-cell",
}

// subdocumentReservedEdgeTypes are the edge-type names owned by the
// built-in sub-document pack. `contains` is the literal type; the
// inverse `contained-by` is reserved too so a user cannot shadow it
// with an explicit declaration.
var subdocumentReservedEdgeTypes = []string{
	"contains",
	"contained-by",
}

// subdocumentReservedProperties maps each reserved node type to the
// property names the pack owns on that type. Used by mergeSubdocumentPack
// to surface property-level conflicts.
var subdocumentReservedProperties = map[string][]string{
	"section":    {"heading-level"},
	"paragraph":  {},
	"list-item":  {"checkbox"},
	"code-block": {"lang"},
	"blockquote": {},
	"table-cell": {"header", "row", "column", "column-header"},
}

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

// SubUnitConflict records a single reserved-name collision between the
// built-in sub-document pack and a user manifest declaration. The kind
// distinguishes node-type, edge-type, and property conflicts so the
// renderer can surface each with a meaningful message.
type SubUnitConflict struct {
	// Kind is one of "node-type", "edge-type", or "property".
	Kind string

	// Name is the reserved name that collided.
	Name string

	// OwnerType is set only when Kind == "property"; it carries the
	// node-type name on which the property was declared.
	OwnerType string

	// Message is a pre-formatted human-readable description of the
	// conflict, suitable for doctor output.
	Message string
}

// mergeSubdocumentPack merges the built-in sub-document pack into
// loaded when the manifest opts into sub-units (default true). The
// merge:
//
//   - Records a SubUnitConflict for every user-declared node-type,
//     edge-type, or reserved-property name that collides with the
//     pack's reserved set.
//   - Drops the user's overriding declaration so downstream consumers
//     see only the built-in declarations.
//   - Adds the pack's node types and edge types to the manifest's
//     effective NodeTypes and EdgeTypes maps.
//
// Idempotent: re-running observes no remaining conflicts because the
// user's overrides were removed on the first pass; re-installing the
// built-in declarations is a deterministic overwrite.
func mergeSubdocumentPack(loaded *Manifest) {
	if !loaded.SubUnitsEnabled() {
		return
	}

	if loaded.subdocumentPackApplied {
		return
	}

	reservedNodes := stringSet(subdocumentReservedNodeTypes)
	reservedEdges := stringSet(subdocumentReservedEdgeTypes)

	// Node-type and property collisions. Iterate over a sorted snapshot
	// so SubUnitConflicts ordering is deterministic.
	for _, name := range sortedMapKeys(loaded.NodeTypes) {
		if _, reserved := reservedNodes[name]; !reserved {
			continue
		}

		loaded.SubUnitConflicts = append(loaded.SubUnitConflicts, SubUnitConflict{
			Kind: "node-type",
			Name: name,
			Message: "node-types." + name +
				": reserved by the sub-document pack; the user declaration is ignored. Disable the pack with `[workspace] sub-units = false` or rename the type.",
		})

		reservedProps := stringSet(subdocumentReservedProperties[name])

		for _, prop := range loaded.NodeTypes[name].Properties {
			if _, isReserved := reservedProps[prop.Name]; !isReserved {
				continue
			}

			loaded.SubUnitConflicts = append(loaded.SubUnitConflicts, SubUnitConflict{
				Kind:      "property",
				Name:      prop.Name,
				OwnerType: name,
				Message: "node-types." + name + "." + prop.Name +
					": reserved by the sub-document pack; the user declaration is ignored.",
			})
		}

		delete(loaded.NodeTypes, name)
	}

	// Edge-type collisions.
	for _, name := range sortedMapKeys(loaded.EdgeTypes) {
		if _, reserved := reservedEdges[name]; !reserved {
			continue
		}

		loaded.SubUnitConflicts = append(loaded.SubUnitConflicts, SubUnitConflict{
			Kind: "edge-type",
			Name: name,
			Message: "edge-types." + name +
				": reserved by the sub-document pack; the user declaration is ignored. Disable the pack with `[workspace] sub-units = false` or rename the edge type.",
		})

		delete(loaded.EdgeTypes, name)
	}

	// Install the built-in declarations.
	if loaded.NodeTypes == nil {
		loaded.NodeTypes = make(map[string]NodeType)
	}

	for name, nodeType := range subdocumentNodeTypes() {
		loaded.NodeTypes[name] = nodeType
	}

	if loaded.EdgeTypes == nil {
		loaded.EdgeTypes = make(EdgeTypes)
	}

	for name, edgeType := range subdocumentEdgeTypes() {
		loaded.EdgeTypes[name] = edgeType
	}

	loaded.subdocumentPackApplied = true
}

// stringSet returns the input slice as a lookup set.
func stringSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))

	for _, item := range items {
		out[item] = struct{}{}
	}

	return out
}

// sortedMapKeys returns the keys of m in lexicographic order.
func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))

	for k := range m {
		out = append(out, k)
	}

	sort.Strings(out)
	return out
}
