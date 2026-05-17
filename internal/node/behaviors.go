package node

import "github.com/germanamz/tusk/internal/index"

// FireResult carries the outcome of a recovery-aware Validate fire.
type FireResult struct {
	Rejector  string           // "" when no rejection
	Recovered []RecoveredEvent // accumulated across the chain
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

// Behaviors is the surface node.Service consumes from the behavior engine.
// *behavior.Engine satisfies this interface via duck typing; node does not
// import internal/behavior.
type Behaviors interface {
	FireNodeWriteValidate(before, after *Node) (rejector string, err error)
	FireNodeWriteValidateWithRecovery(before, after *Node) (FireResult, error)
	FireNodeWriteAfter(before, after *Node) error
	FireEdgeAddValidate(edge index.EdgeRow) (rejector string, err error)
	FireEdgeAddAfter(edge index.EdgeRow) error
	FireEdgeRemoveValidate(edge index.EdgeRow) (rejector string, err error)
	FireEdgeRemoveAfter(edge index.EdgeRow) error

	// ReservedProperties returns the (nodeType, property) pairs reserved
	// by behavior instances. Returned as nodeType -> property -> {}.
	// Empty or nil when no behavior reserves a property.
	ReservedProperties() map[string]map[string]struct{}
}
