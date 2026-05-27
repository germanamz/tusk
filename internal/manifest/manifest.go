// Package manifest defines the schema and loader for tusk.toml.
package manifest

import "github.com/BurntSushi/toml"

// EdgeTypes is a named map of edge-type declarations keyed by edge-type name.
type EdgeTypes = map[string]EdgeType

// Manifest is the parsed representation of tusk.toml at the workspace root.
type Manifest struct {
	Workspace  WorkspaceSection    `toml:"workspace"`
	EdgeTypes  EdgeTypes           `toml:"edge-types"`
	Embeddings EmbeddingsSection   `toml:"embeddings"`
	Lease      LeaseSection        `toml:"lease"`
	NodeTypes  map[string]NodeType `toml:"node-types"`

	// Behaviors is a two-level map: kind name → instance name → raw TOML
	// table. The kind-specific decode happens inside the pack package
	// (deferred-decode contract).
	Behaviors map[string]map[string]toml.Primitive `toml:"behaviors"`

	// Aliases holds the manifest-declared aliases keyed by alias name.
	// Populated by Load (raw decode) and validated/filtered by
	// ValidateAliases. Invalid aliases are removed from this map and
	// reported via AliasErrors so engine startup never fails on a bad
	// alias.
	Aliases map[string]Alias `toml:"-"`

	// AliasErrors captures per-alias validation failures. Surfaced through
	// doctor; never raised as a load error.
	AliasErrors []AliasError `toml:"-"`

	// rawAliases holds the on-disk [alias.<name>] blocks captured during
	// Load so ValidateAliases can introspect them. Internal to the
	// manifest package; consumers read through Aliases instead.
	rawAliases map[string]aliasTOML `toml:"-"`

	// Context is the parsed [context] block, nil when the manifest does
	// not declare one. Populated by Load (decode-only) and finalised by
	// ValidateContext (resolves the recent alias, validates include).
	Context *Context `toml:"-"`

	// ContextErrors captures per-context validation failures (unknown
	// alias reference, both recent forms set, malformed inline alias).
	// Surfaced through doctor; never raised as a load error.
	ContextErrors []ContextError `toml:"-"`

	// SubUnitConflicts captures reserved-name collisions between the
	// built-in sub-document pack and user-declared node types, edge
	// types, or properties. Populated by MergeBuiltinPacks when the
	// `sub-units` flag is enabled. Surfaced through doctor; the engine
	// prefers the built-in declaration and ignores the user's override.
	SubUnitConflicts []SubUnitConflict `toml:"-"`

	// subdocumentPackApplied records whether MergeBuiltinPacks has
	// already merged the sub-document pack into this manifest. The
	// merger short-circuits when true so a second call (e.g., from a
	// reload path that itself calls Load again) does not re-classify
	// the pack's own declarations as conflicts. Cleared only by Load
	// producing a fresh *Manifest; in-place mutation by tests should
	// reset the flag manually if they re-test the merge path.
	subdocumentPackApplied bool `toml:"-"`

	// contextRecentPrimitive holds the toml.Primitive captured for the
	// `recent` key inside [context] so ValidateContext can discriminate
	// between the string-reference form and the inline [context.recent]
	// sub-table form. Cleared after ValidateContext runs.
	contextRecentPrimitive toml.Primitive `toml:"-"`

	// contextRecentDefined is true when [context.recent] (or recent = ...)
	// was present at decode time. Cleared after ValidateContext runs.
	contextRecentDefined bool `toml:"-"`

	// contextRecentMeta carries the toml.MetaData produced by the context
	// decoder so ValidateContext can PrimitiveDecode against it. Cleared
	// after ValidateContext runs.
	contextRecentMeta *toml.MetaData `toml:"-"`

	// Meta is the BurntSushi/toml MetaData captured at decode time so pack
	// Kinds can call PrimitiveDecode against their subtable. Nil for
	// hand-built manifests (tests that construct a Manifest literal).
	Meta *toml.MetaData `toml:"-"`

	// queryGraphExpansion carries the on-disk [query.graph-expansion]
	// primitives captured by decodeQuerySection. Consumed and cleared by
	// resolveGraphExpansion; consumers should read GraphExpansion instead.
	queryGraphExpansion graphExpansionTOML `toml:"-"`

	// queryGraphExpansionMeta is the toml.MetaData produced by the
	// secondary [query] decode. resolveGraphExpansion uses it to
	// PrimitiveDecode each captured primitive (the outer Manifest meta is
	// not valid for primitives captured by a separate Decode call).
	queryGraphExpansionMeta *toml.MetaData `toml:"-"`

	// GraphExpansion is the resolved [query.graph-expansion] configuration.
	// Populated by the loader's resolveGraphExpansion finaliser; defaults
	// match DefaultGraphExpansion when the block is absent. Task 1 plumbs
	// the field through but query.Run intentionally ignores it; Tasks 2-4
	// of the agent-retrieval-improvements Phase 3 plan wire it into the
	// retrieval pipeline.
	GraphExpansion GraphExpansion `toml:"-"`
}

// queryDecode is the BurntSushi/toml wrapper used by the loader to decode the
// [query] section into a graphExpansionTOML primitive table. Defined here so
// the loader can hand the decoded subtable to resolveGraphExpansion.
type queryDecode struct {
	Query querySection `toml:"query"`
}

// NodeType is a manifest-declared node type with an optional description and
// a list of property declarations.
type NodeType struct {
	Description string         `toml:"description"`
	Properties  []PropertyDecl `toml:"properties"`
}

// PropertyDecl declares a single typed property on a node type.
type PropertyDecl struct {
	Name        string   `toml:"name"`
	Type        string   `toml:"type"`
	ItemType    string   `toml:"item-type"`
	Values      []string `toml:"values"`
	Required    bool     `toml:"required"`
	Description string   `toml:"description"`

	// ref-only fields: meaningful only when Type == "ref" or
	// (Type == "list-of" && ItemType == "ref").
	To      string `toml:"to"`
	Inverse string `toml:"inverse"`
	Acyclic bool   `toml:"acyclic"`
}

// WorkspaceSection holds top-level workspace configuration.
type WorkspaceSection struct {
	Name   string   `toml:"name"`
	Ignore []string `toml:"ignore"`

	// SubUnitsRaw mirrors the on-disk `sub-units` key. Decoded as a
	// toml.Primitive so the loader can distinguish "absent" (default to
	// true) from "explicit false". Consumers should call
	// Manifest.SubUnitsEnabled instead of reading this directly.
	SubUnitsRaw toml.Primitive `toml:"sub-units"`
}

// EmbeddingsSection configures the active embedding provider.
//
// Plan 5 supports provider = "ollama" only; the loader rejects other values.
// API providers (openai/voyage/anthropic) land in Plan 5.x.
//
// Workers, BatchSize, and TimeoutSeconds tune the embedding pipeline:
// Workers caps concurrency per node, BatchSize caps prompts per HTTP call
// (Phase 2 of Spec B), TimeoutSeconds sets the HTTP client timeout for
// the embedder. All optional; sensible defaults apply when omitted.
type EmbeddingsSection struct {
	Provider       string `toml:"provider"`
	Model          string `toml:"model"`
	Endpoint       string `toml:"endpoint"`
	Dim            int    `toml:"dim"`
	APIKey         string `toml:"api-key"`
	Workers        int    `toml:"workers"`
	TimeoutSeconds int    `toml:"timeout-seconds"`
}

// Cardinality enumerates the legal values for EdgeType.Cardinality.
type Cardinality string

const (
	CardinalityOneToOne   Cardinality = "one-to-one"
	CardinalityOneToMany  Cardinality = "one-to-many"
	CardinalityManyToOne  Cardinality = "many-to-one"
	CardinalityManyToMany Cardinality = "many-to-many"
)

// EdgeType is a manifest-declared edge type.
type EdgeType struct {
	Description string         `toml:"description"`
	From        []string       `toml:"from"` // allowed source node-types; ["*"] means any
	To          []string       `toml:"to"`   // allowed target node-types; ["*"] means any
	Cardinality Cardinality    `toml:"cardinality"`
	OrderedRaw  toml.Primitive `toml:"ordered"` // decoded by Validate into Ordered + OrderedBy
	Inverse     string         `toml:"inverse"` // optional; name of the derived inverse edge
	Acyclic     bool           `toml:"acyclic"`

	// Hierarchy, when non-empty, opts this edge into the traversal-shortcut
	// family (tree= / parent= / root=) under the given alias. Multiple edge
	// types may each declare a distinct alias.
	Hierarchy string `toml:"hierarchy"`

	// HierarchyDefault marks this edge as the target of unqualified
	// shortcuts. At most one edge per workspace may set this true.
	HierarchyDefault bool `toml:"hierarchy-default"`

	// Wikilinks, when true, makes the indexer materialize body [[wikilinks]]
	// into edges of this type. Replaces the hardcoded "references" special
	// case. Zero, one, or many edge types may set it.
	Wikilinks bool `toml:"wikilinks"`

	// Resolved by manifest.Validate after parsing OrderedRaw.
	// Ordered is true when the source declares any ordering (bool true OR string non-empty).
	// OrderedBy is the source-node property name carrying the order key.
	// When Ordered is true but OrderedBy is empty (i.e., bool `ordered = true`),
	// OrderedBy defaults to "order".
	Ordered   bool   `toml:"-"`
	OrderedBy string `toml:"-"`
}

// AllowsSource returns true if sourceType matches the edge type's `from` list
// (literal match or `*` wildcard).
func (edgeType EdgeType) AllowsSource(sourceType string) bool {
	return matchesTypeList(edgeType.From, sourceType)
}

// AllowsTarget returns true if targetType matches the edge type's `to` list.
func (edgeType EdgeType) AllowsTarget(targetType string) bool {
	return matchesTypeList(edgeType.To, targetType)
}

func matchesTypeList(allowed []string, candidate string) bool {
	for _, entry := range allowed {
		if entry == "*" || entry == candidate {
			return true
		}
	}

	return false
}

// SubUnitsEnabled returns true when the workspace opts into sub-unit
// indexing. The default is true (the built-in pack is on when the manifest
// is silent or `sub-units` is absent); only an explicit `sub-units = false`
// disables it. The discrimination relies on the toml.MetaData captured at
// decode time; hand-built manifests (tests that construct a Manifest
// literal without calling Load) default to true unless the test sets
// Meta + the primitive explicitly.
func (loaded *Manifest) SubUnitsEnabled() bool {
	if loaded == nil {
		return true
	}

	if loaded.Meta == nil {
		// Hand-built manifest — preserve the default.
		return true
	}

	if !loaded.Meta.IsDefined("workspace", "sub-units") {
		return true
	}

	var explicit bool

	if decodeErr := loaded.Meta.PrimitiveDecode(loaded.Workspace.SubUnitsRaw, &explicit); decodeErr != nil {
		// Malformed value — treat as default (true) so the engine still
		// starts. ValidateBuiltinPacks (or a future stricter validator)
		// can surface this through doctor.
		return true
	}

	return explicit
}
