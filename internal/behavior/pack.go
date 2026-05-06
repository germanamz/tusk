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

// RecoveredEvent describes a non-fatal recovery event observed during
// Validate-phase dispatch. Constructed by the engine from a Recoverable
// error returned by a handler.
type RecoveredEvent struct {
	PackKind     string
	PackInstance string
	Property     string
	From         string
	To           string
	Message      string
}

// Recoverable is the contract a Validate-phase handler error implements
// when it represents a non-fatal recovery rather than a rejection. The
// recovery-aware Fire variant uses errors.As against this interface to
// distinguish "carry information through the chain" from "abort".
type Recoverable interface {
	error
	AsRecoveredEvent(packKind, packInstance string) RecoveredEvent
}
