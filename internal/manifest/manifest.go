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
