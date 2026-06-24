// Package behavior defines the hook surface that v1 behavior packs compose
// against. The surface is fixed: four primitives (NodeWrite, NodeRead,
// EdgeAdd, EdgeRemove) each with two phases (Validate, After), totaling
// eight registration slots. Future behavior packs register handlers on
// these slots without changing the engine.
package behavior

import (
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
)

// HookContext carries per-call identity. Passed by value; future fields
// are additive.
type HookContext struct {
	PackKind     string // e.g. "workflow"
	PackInstance string // e.g. "tickets"
}

// qualified returns the "kind.instance" identifier used as a validator's
// rejection name and as the prefix on aggregated reactor errors.
func (ctx HookContext) qualified() string {
	return ctx.PackKind + "." + ctx.PackInstance
}

// Phase identifies when a hook fires relative to the underlying write.
type Phase int

const (
	// PhaseValidate fires before the write commits. A non-nil error from a
	// Validate handler rejects the write (subject to the chain semantics
	// described in engine.go).
	PhaseValidate Phase = iota

	// PhaseAfter fires after the write commits. After-phase handlers may
	// read the index but must not write. Their return values do not affect
	// control flow; they are aggregated for telemetry only.
	PhaseAfter
)

// Handler types, one per (primitive, phase) slot.

type NodeWriteValidator func(ctx HookContext, before, after *node.Node) error
type NodeWriteReactor func(ctx HookContext, before, after *node.Node) error

type EdgeAddValidator func(ctx HookContext, edge index.EdgeRow) error
type EdgeAddReactor func(ctx HookContext, edge index.EdgeRow) error

type EdgeRemoveValidator func(ctx HookContext, edge index.EdgeRow) error
type EdgeRemoveReactor func(ctx HookContext, edge index.EdgeRow) error
