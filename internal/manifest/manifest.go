// Package manifest defines the schema and loader for tusk.toml.
package manifest

// EdgeTypes is a named map of edge-type declarations keyed by edge-type name.
type EdgeTypes = map[string]EdgeType

// Manifest is the parsed representation of tusk.toml at the workspace root.
type Manifest struct {
	Workspace  WorkspaceSection  `toml:"workspace"`
	EdgeTypes  EdgeTypes         `toml:"edge-types"`
	Embeddings EmbeddingsSection `toml:"embeddings"`
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
type EmbeddingsSection struct {
	Provider string `toml:"provider"`
	Model    string `toml:"model"`
	Endpoint string `toml:"endpoint"`
	Dim      int    `toml:"dim"`
	APIKey   string `toml:"api-key"`
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
	Description string      `toml:"description"`
	From        []string    `toml:"from"` // allowed source node-types; ["*"] means any
	To          []string    `toml:"to"`   // allowed target node-types; ["*"] means any
	Cardinality Cardinality `toml:"cardinality"`
	Ordered     bool        `toml:"ordered"`
	Inverse     string      `toml:"inverse"` // optional; name of the derived inverse edge
	Acyclic     bool        `toml:"acyclic"`
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
