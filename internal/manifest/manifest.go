// Package manifest defines the schema and loader for tusk.toml.
package manifest

// Manifest is the parsed representation of tusk.toml at the workspace root.
type Manifest struct {
	Workspace WorkspaceSection `toml:"workspace"`
}

// WorkspaceSection holds top-level workspace configuration.
//
// Plan 1b ships only Name and Ignore; type packs, embeddings config, behaviors,
// and the inline node-types/edge-types tables land in later plans.
type WorkspaceSection struct {
	Name   string   `toml:"name"`
	Ignore []string `toml:"ignore"`
}
