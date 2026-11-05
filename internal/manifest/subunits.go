package manifest

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// subUnitKindNames lists the kinds `[workspace] sub-units` can name, in the
// order SubUnitKinds, the docs and the load errors give them. They are the
// sub-document pack's node types and the subunit.Kind values.
var subUnitKindNames = []string{"section", "paragraph", "list-item", "code-block", "blockquote", "table-cell"}

// SubUnitKindNames returns every kind `[workspace] sub-units` can list, in
// canonical order.
func SubUnitKindNames() []string {
	return slices.Clone(subUnitKindNames)
}

// resolveSubUnits reads `[workspace] sub-units` into subUnitsExcluded, and
// each `[node-types.<name>] sub-units` into subUnitsByType (#867). See
// excludedSubUnitKinds for the values either key takes. Types are read in
// name order, so a manifest with two bad values always reports the same one.
func resolveSubUnits(loaded *Manifest) error {
	loaded.subUnitsExcluded = nil
	loaded.subUnitsByType = nil

	if loaded.Meta == nil {
		return nil
	}

	if loaded.Meta.IsDefined("workspace", "sub-units") {
		excluded, excludedErr := excludedSubUnitKinds(loaded.Meta, "workspace.sub-units", loaded.Workspace.SubUnitsRaw)

		if excludedErr != nil {
			return excludedErr
		}

		loaded.subUnitsExcluded = excluded
	}

	for _, typeName := range slices.Sorted(maps.Keys(loaded.NodeTypes)) {
		if !loaded.Meta.IsDefined("node-types", typeName, "sub-units") {
			continue
		}

		excluded, excludedErr := excludedSubUnitKinds(loaded.Meta, "node-types."+typeName+".sub-units", loaded.NodeTypes[typeName].SubUnitsRaw)

		if excludedErr != nil {
			return excludedErr
		}

		if loaded.subUnitsByType == nil {
			loaded.subUnitsByType = make(map[string]map[string]struct{})
		}

		loaded.subUnitsByType[typeName] = excluded
	}

	return nil
}

// excludedSubUnitKinds decodes a sub-units value, written at key, into the
// kinds it leaves out: true leaves out nothing, false or [] every kind, and a
// list keeps the kinds it names, in any order, duplicates allowed. Any other
// value, or a list item that names no kind, is an error. Before #853 the
// workspace key was decoded as a bool on read and anything else fell back to
// on, so a list or a string loaded without a word and changed nothing.
func excludedSubUnitKinds(meta *toml.MetaData, key string, raw toml.Primitive) (map[string]struct{}, error) {
	var value any

	if decodeErr := meta.PrimitiveDecode(raw, &value); decodeErr != nil {
		return nil, fmt.Errorf("manifest: %s: %w", key, decodeErr)
	}

	wantKinds := strings.Join(subUnitKindNames, " | ")
	var kept []string

	switch typed := value.(type) {
	case bool:
		if typed {
			kept = subUnitKindNames
		}
	case []any:
		for _, item := range typed {
			kind, isString := item.(string)

			if !isString || !slices.Contains(subUnitKindNames, kind) {
				return nil, fmt.Errorf("manifest: %s: %s is not a sub-unit kind (want %s)", key, tomlValueText(item), wantKinds)
			}

			kept = append(kept, kind)
		}
	default:
		return nil, fmt.Errorf("manifest: %s = %s is not supported (want true, false or a list of kinds: %s)", key, tomlValueText(value), wantKinds)
	}

	excluded := make(map[string]struct{}, len(subUnitKindNames))

	for _, kind := range subUnitKindNames {
		if !slices.Contains(kept, kind) {
			excluded[kind] = struct{}{}
		}
	}

	return excluded, nil
}

// tomlValueText renders a decoded TOML value for an error message: a string
// quoted, a table as {...}, anything else as Go prints it.
func tomlValueText(value any) string {
	switch typed := value.(type) {
	case string:
		return strconv.Quote(typed)
	case map[string]any:
		return "{...}"
	default:
		return fmt.Sprint(typed)
	}
}

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
			To:          SubUnitKindNames(),
			Cardinality: CardinalityOneToMany,
			Inverse:     "contained-by",
			Acyclic:     AcyclicError,
		},
	}
}

// mergeSubdocumentPack merges the built-in sub-document pack into
// loaded when the manifest indexes any sub-unit kind (default every
// kind). The merge installs the pack's node and edge types into the
// manifest's effective NodeTypes/EdgeTypes maps: all of them, even when
// `sub-units` lists only some kinds, so a filter or rule that names an
// unindexed kind still validates and matches nothing.
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
