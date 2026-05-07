package behavior

import "github.com/BurntSushi/toml"

// Hooks is the per-instance bundle of optional handler slots. A nil slot
// means the instance does not register on that hook.
type Hooks struct {
	OnNodeWriteValidate  NodeWriteValidator
	OnNodeWriteAfter     NodeWriteReactor
	OnNodeReadValidate   NodeReadValidator
	OnNodeReadAfter      NodeReadReactor
	OnEdgeAddValidate    EdgeAddValidator
	OnEdgeAddAfter       EdgeAddReactor
	OnEdgeRemoveValidate EdgeRemoveValidator
	OnEdgeRemoveAfter    EdgeRemoveReactor
}

// ReservedKey is a (node-type, property) pair an instance owns. Two
// instances reserving the same pair is a collision detected at engine
// build time.
type ReservedKey struct {
	NodeType string
	Property string
}

// DeclaredKey is a (node-type, property) pair declared in [node-types].
// The Source field identifies the declaration site for error messages,
// e.g. "node-types.ticket.properties[priority]".
type DeclaredKey struct {
	NodeType string
	Property string
	Source   string
}

// Instance is one configured pack — produced by a Kind.NewInstance call.
type Instance interface {
	Name() string                // instance name from manifest, e.g. "tickets"
	Kind() string                // delegate to its Kind.Name()
	Hooks() Hooks                // handler slots; nils for unfilled
	ReservedKeys() []ReservedKey // (type, property) ownership
}

// Kind constructs Instances from raw TOML config. Registered once per kind
// in the Registry; v1 only registers "workflow".
type Kind interface {
	Name() string
	NewInstance(instanceName string, raw toml.Primitive, meta *toml.MetaData) (Instance, error)
}
