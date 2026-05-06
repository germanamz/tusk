package behavior

import (
	"errors"
	"fmt"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
)

// Engine owns eight dispatch chains, one per (primitive, phase) slot.
// Built once via NewEngine and immutable thereafter; runtime reload
// rebuilds from scratch.
type Engine struct {
	nodeWriteValidate  []nodeWriteValidatorEntry
	nodeWriteAfter     []nodeWriteReactorEntry
	nodeReadValidate   []nodeReadValidatorEntry
	nodeReadAfter      []nodeReadReactorEntry
	edgeAddValidate    []edgeAddValidatorEntry
	edgeAddAfter       []edgeAddReactorEntry
	edgeRemoveValidate []edgeRemoveValidatorEntry
	edgeRemoveAfter    []edgeRemoveReactorEntry
}

type nodeWriteValidatorEntry struct {
	ctx HookContext
	fn  NodeWriteValidator
}

type nodeWriteReactorEntry struct {
	ctx HookContext
	fn  NodeWriteReactor
}

type nodeReadValidatorEntry struct {
	ctx HookContext
	fn  NodeReadValidator
}

type nodeReadReactorEntry struct {
	ctx HookContext
	fn  NodeReadReactor
}

type edgeAddValidatorEntry struct {
	ctx HookContext
	fn  EdgeAddValidator
}

type edgeAddReactorEntry struct {
	ctx HookContext
	fn  EdgeAddReactor
}

type edgeRemoveValidatorEntry struct {
	ctx HookContext
	fn  EdgeRemoveValidator
}

type edgeRemoveReactorEntry struct {
	ctx HookContext
	fn  EdgeRemoveReactor
}

// NewEngine constructs an Engine from a slice of Instances. The chains
// are built in the slice's order. Reservation collision detection runs
// here: two instances reserving the same (NodeType, Property) pair is a
// hard error.
func NewEngine(instances []Instance) (*Engine, error) {
	if collisionErr := detectCollisions(instances); collisionErr != nil {
		return nil, collisionErr
	}

	engine := &Engine{}

	for _, instance := range instances {
		ctx := HookContext{PackKind: instance.Kind(), PackInstance: instance.Name()}
		hooks := instance.Hooks()

		if hooks.OnNodeWriteValidate != nil {
			engine.nodeWriteValidate = append(engine.nodeWriteValidate,
				nodeWriteValidatorEntry{ctx: ctx, fn: hooks.OnNodeWriteValidate})
		}

		if hooks.OnNodeWriteAfter != nil {
			engine.nodeWriteAfter = append(engine.nodeWriteAfter,
				nodeWriteReactorEntry{ctx: ctx, fn: hooks.OnNodeWriteAfter})
		}

		if hooks.OnNodeReadValidate != nil {
			engine.nodeReadValidate = append(engine.nodeReadValidate,
				nodeReadValidatorEntry{ctx: ctx, fn: hooks.OnNodeReadValidate})
		}

		if hooks.OnNodeReadAfter != nil {
			engine.nodeReadAfter = append(engine.nodeReadAfter,
				nodeReadReactorEntry{ctx: ctx, fn: hooks.OnNodeReadAfter})
		}

		if hooks.OnEdgeAddValidate != nil {
			engine.edgeAddValidate = append(engine.edgeAddValidate,
				edgeAddValidatorEntry{ctx: ctx, fn: hooks.OnEdgeAddValidate})
		}

		if hooks.OnEdgeAddAfter != nil {
			engine.edgeAddAfter = append(engine.edgeAddAfter,
				edgeAddReactorEntry{ctx: ctx, fn: hooks.OnEdgeAddAfter})
		}

		if hooks.OnEdgeRemoveValidate != nil {
			engine.edgeRemoveValidate = append(engine.edgeRemoveValidate,
				edgeRemoveValidatorEntry{ctx: ctx, fn: hooks.OnEdgeRemoveValidate})
		}

		if hooks.OnEdgeRemoveAfter != nil {
			engine.edgeRemoveAfter = append(engine.edgeRemoveAfter,
				edgeRemoveReactorEntry{ctx: ctx, fn: hooks.OnEdgeRemoveAfter})
		}
	}

	return engine, nil
}

func detectCollisions(instances []Instance) error {
	type key struct{ nodeType, property string }

	owners := map[key]string{}

	for _, instance := range instances {
		qualified := instance.Kind() + "." + instance.Name()

		for _, reserved := range instance.ReservedKeys() {
			id := key{nodeType: reserved.NodeType, property: reserved.Property}

			if existing, taken := owners[id]; taken {
				return fmt.Errorf("behavior: %s and %s both reserve property %q on type %q",
					existing, qualified, reserved.Property, reserved.NodeType)
			}

			owners[id] = qualified
		}
	}

	return nil
}

// FireNodeWriteValidate runs the chain in registration order, short-
// circuiting on the first non-nil error. Returns ("", nil) on accept;
// (qualified-name, err) on reject. The recovery-aware variant lives in
// FireNodeWriteValidateWithRecovery.
func (engine *Engine) FireNodeWriteValidate(before, after *node.Node) (string, error) {
	for _, entry := range engine.nodeWriteValidate {
		if fireErr := entry.fn(entry.ctx, before, after); fireErr != nil {
			return entry.ctx.PackKind + "." + entry.ctx.PackInstance, fireErr
		}
	}

	return "", nil
}

// FireNodeWriteValidateWithRecovery walks the chain in registration order.
// Errors implementing node.Recoverable are converted to node.RecoveredEvent
// entries and the chain continues. Any other non-nil error short-circuits
// and is returned as the rejection.
func (engine *Engine) FireNodeWriteValidateWithRecovery(before, after *node.Node) (node.FireResult, error) {
	var result node.FireResult

	for _, entry := range engine.nodeWriteValidate {
		fireErr := entry.fn(entry.ctx, before, after)

		if fireErr == nil {
			continue
		}

		var recoverable node.Recoverable

		if errors.As(fireErr, &recoverable) {
			result.Recovered = append(result.Recovered,
				recoverable.AsRecoveredEvent(entry.ctx.PackKind, entry.ctx.PackInstance))
			continue
		}

		result.Rejector = entry.ctx.PackKind + "." + entry.ctx.PackInstance

		return result, fireErr
	}

	return result, nil
}

// FireNodeWriteAfter runs every reactor; aggregates non-nil errors into
// a multi-error. Control flow is unaffected.
func (engine *Engine) FireNodeWriteAfter(before, after *node.Node) error {
	var aggregated []error

	for _, entry := range engine.nodeWriteAfter {
		if fireErr := entry.fn(entry.ctx, before, after); fireErr != nil {
			aggregated = append(aggregated, fmt.Errorf("%s.%s: %w", entry.ctx.PackKind, entry.ctx.PackInstance, fireErr))
		}
	}

	if len(aggregated) == 0 {
		return nil
	}

	return errors.Join(aggregated...)
}

// FireEdgeAddValidate runs the chain in registration order; first error
// short-circuits.
func (engine *Engine) FireEdgeAddValidate(edge index.EdgeRow) (string, error) {
	for _, entry := range engine.edgeAddValidate {
		if fireErr := entry.fn(entry.ctx, edge); fireErr != nil {
			return entry.ctx.PackKind + "." + entry.ctx.PackInstance, fireErr
		}
	}

	return "", nil
}

// FireEdgeAddAfter runs every reactor; aggregates non-nil errors.
func (engine *Engine) FireEdgeAddAfter(edge index.EdgeRow) error {
	var aggregated []error

	for _, entry := range engine.edgeAddAfter {
		if fireErr := entry.fn(entry.ctx, edge); fireErr != nil {
			aggregated = append(aggregated, fmt.Errorf("%s.%s: %w", entry.ctx.PackKind, entry.ctx.PackInstance, fireErr))
		}
	}

	if len(aggregated) == 0 {
		return nil
	}

	return errors.Join(aggregated...)
}

// FireEdgeRemoveValidate / FireEdgeRemoveAfter mirror their Add siblings.
func (engine *Engine) FireEdgeRemoveValidate(edge index.EdgeRow) (string, error) {
	for _, entry := range engine.edgeRemoveValidate {
		if fireErr := entry.fn(entry.ctx, edge); fireErr != nil {
			return entry.ctx.PackKind + "." + entry.ctx.PackInstance, fireErr
		}
	}

	return "", nil
}

func (engine *Engine) FireEdgeRemoveAfter(edge index.EdgeRow) error {
	var aggregated []error

	for _, entry := range engine.edgeRemoveAfter {
		if fireErr := entry.fn(entry.ctx, edge); fireErr != nil {
			aggregated = append(aggregated, fmt.Errorf("%s.%s: %w", entry.ctx.PackKind, entry.ctx.PackInstance, fireErr))
		}
	}

	if len(aggregated) == 0 {
		return nil
	}

	return errors.Join(aggregated...)
}

// FireNodeReadValidate / FireNodeReadAfter are reserved in v1: defined
// for API parity with the other primitives but not invoked from the
// production write path. Implementations exist so future v1.x consumers
// can register handlers without changing the engine.
func (engine *Engine) FireNodeReadValidate(snapshot *node.Node) (string, error) {
	for _, entry := range engine.nodeReadValidate {
		if fireErr := entry.fn(entry.ctx, snapshot); fireErr != nil {
			return entry.ctx.PackKind + "." + entry.ctx.PackInstance, fireErr
		}
	}

	return "", nil
}

func (engine *Engine) FireNodeReadAfter(snapshot *node.Node) error {
	var aggregated []error

	for _, entry := range engine.nodeReadAfter {
		if fireErr := entry.fn(entry.ctx, snapshot); fireErr != nil {
			aggregated = append(aggregated, fmt.Errorf("%s.%s: %w", entry.ctx.PackKind, entry.ctx.PackInstance, fireErr))
		}
	}

	if len(aggregated) == 0 {
		return nil
	}

	return errors.Join(aggregated...)
}
