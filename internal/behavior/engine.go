package behavior

import (
	"errors"
	"fmt"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
)

// Engine owns six dispatch chains, one per (primitive, phase) slot.
// Built once via NewEngine and immutable thereafter; runtime reload
// rebuilds from scratch.
type Engine struct {
	nodeWriteValidate  []entry[NodeWriteValidator]
	nodeWriteAfter     []entry[NodeWriteReactor]
	edgeAddValidate    []entry[EdgeAddValidator]
	edgeAddAfter       []entry[EdgeAddReactor]
	edgeRemoveValidate []entry[EdgeRemoveValidator]
	edgeRemoveAfter    []entry[EdgeRemoveReactor]

	// reservedKeys captures every Instance.ReservedKeys() pair so
	// downstream validators (e.g. property-drift in node.Service) can
	// recognize behavior-owned properties as declared. nodeType -> property -> {}.
	reservedKeys map[string]map[string]struct{}
}

// entry pairs a hook context with its handler. One generic type replaces the
// six per-(primitive, phase) entry structs the engine used to declare.
type entry[Fn any] struct {
	ctx HookContext
	fn  Fn
}

// NewEngine constructs an Engine from a slice of Instances and an optional
// slice of DeclaredKeys (from [node-types]). The chains are built in the
// slice's order. Reservation collision detection runs here: two instances
// reserving the same (NodeType, Property) pair is a hard error, as is a
// behavior reservation colliding with a declared key.
func NewEngine(instances []Instance, declaredKeys []DeclaredKey) (*Engine, error) {
	if collisionErr := detectCollisions(instances, declaredKeys); collisionErr != nil {
		return nil, collisionErr
	}

	engine := &Engine{
		reservedKeys: map[string]map[string]struct{}{},
	}

	for _, instance := range instances {
		ctx := HookContext{PackKind: instance.Kind(), PackInstance: instance.Name()}
		hooks := instance.Hooks()

		for _, reserved := range instance.ReservedKeys() {
			byProperty, exists := engine.reservedKeys[reserved.NodeType]

			if !exists {
				byProperty = map[string]struct{}{}
				engine.reservedKeys[reserved.NodeType] = byProperty
			}

			byProperty[reserved.Property] = struct{}{}
		}

		if hooks.OnNodeWriteValidate != nil {
			engine.nodeWriteValidate = append(engine.nodeWriteValidate,
				entry[NodeWriteValidator]{ctx: ctx, fn: hooks.OnNodeWriteValidate})
		}

		if hooks.OnNodeWriteAfter != nil {
			engine.nodeWriteAfter = append(engine.nodeWriteAfter,
				entry[NodeWriteReactor]{ctx: ctx, fn: hooks.OnNodeWriteAfter})
		}

		if hooks.OnEdgeAddValidate != nil {
			engine.edgeAddValidate = append(engine.edgeAddValidate,
				entry[EdgeAddValidator]{ctx: ctx, fn: hooks.OnEdgeAddValidate})
		}

		if hooks.OnEdgeAddAfter != nil {
			engine.edgeAddAfter = append(engine.edgeAddAfter,
				entry[EdgeAddReactor]{ctx: ctx, fn: hooks.OnEdgeAddAfter})
		}

		if hooks.OnEdgeRemoveValidate != nil {
			engine.edgeRemoveValidate = append(engine.edgeRemoveValidate,
				entry[EdgeRemoveValidator]{ctx: ctx, fn: hooks.OnEdgeRemoveValidate})
		}

		if hooks.OnEdgeRemoveAfter != nil {
			engine.edgeRemoveAfter = append(engine.edgeRemoveAfter,
				entry[EdgeRemoveReactor]{ctx: ctx, fn: hooks.OnEdgeRemoveAfter})
		}
	}

	return engine, nil
}

func detectCollisions(instances []Instance, declaredKeys []DeclaredKey) error {
	type key struct{ nodeType, property string }

	owners := map[key]string{}

	// Pre-populate the owner map with declared keys so behavior reservations
	// colliding with node-type declarations are caught.
	for _, dk := range declaredKeys {
		id := key{nodeType: dk.NodeType, property: dk.Property}
		owners[id] = dk.Source
	}

	for _, instance := range instances {
		qualified := instance.Kind() + "." + instance.Name()

		for _, reserved := range instance.ReservedKeys() {
			id := key{nodeType: reserved.NodeType, property: reserved.Property}

			if existing, taken := owners[id]; taken {
				return fmt.Errorf(
					"behavior: behaviors.%s.%s reserves property %q on type %q\nbut it is also declared in %s",
					instance.Kind(), instance.Name(), reserved.Property, reserved.NodeType, existing)
			}

			owners[id] = qualified
		}
	}

	return nil
}

// ReservedProperties returns the (node-type, property) pairs reserved by
// behavior instances, as a nodeType -> property -> {} map. Property-drift
// validators consult this so behavior-owned properties (e.g. workflow's
// `status`) are not flagged as undeclared. The returned map shares storage
// with the engine; callers must treat it as read-only.
func (engine *Engine) ReservedProperties() map[string]map[string]struct{} {
	return engine.reservedKeys
}

// fireValidate runs a validator chain in registration order, short-circuiting
// on the first non-nil error. Returns ("", nil) on accept; (qualified-name,
// err) on reject. invoke adapts the slot's typed handler to the call's args.
func fireValidate[Fn any](entries []entry[Fn], invoke func(Fn, HookContext) error) (string, error) {
	for _, ent := range entries {
		if fireErr := invoke(ent.fn, ent.ctx); fireErr != nil {
			return ent.ctx.qualified(), fireErr
		}
	}

	return "", nil
}

// fireReactors runs every reactor and joins non-nil errors into a multi-error.
// Control flow is unaffected; the joined errors are for telemetry only.
func fireReactors[Fn any](entries []entry[Fn], invoke func(Fn, HookContext) error) error {
	var aggregated []error

	for _, ent := range entries {
		if fireErr := invoke(ent.fn, ent.ctx); fireErr != nil {
			aggregated = append(aggregated, fmt.Errorf("%s: %w", ent.ctx.qualified(), fireErr))
		}
	}

	if len(aggregated) == 0 {
		return nil
	}

	return errors.Join(aggregated...)
}

// FireNodeWriteValidate runs the chain in registration order, short-
// circuiting on the first non-nil error. Returns ("", nil) on accept;
// (qualified-name, err) on reject. The recovery-aware variant lives in
// FireNodeWriteValidateWithRecovery.
func (engine *Engine) FireNodeWriteValidate(before, after *node.Node) (string, error) {
	return fireValidate(engine.nodeWriteValidate, func(fn NodeWriteValidator, ctx HookContext) error {
		return fn(ctx, before, after)
	})
}

// FireNodeWriteValidateWithRecovery walks the chain in registration order.
// Errors implementing node.Recoverable are converted to node.RecoveredEvent
// entries and the chain continues. Any other non-nil error short-circuits
// and is returned as the rejection.
func (engine *Engine) FireNodeWriteValidateWithRecovery(before, after *node.Node) (node.FireResult, error) {
	var result node.FireResult

	for _, ent := range engine.nodeWriteValidate {
		fireErr := ent.fn(ent.ctx, before, after)

		if fireErr == nil {
			continue
		}

		if recoverable, ok := errors.AsType[node.Recoverable](fireErr); ok {
			result.Recovered = append(result.Recovered,
				recoverable.AsRecoveredEvent(ent.ctx.PackKind, ent.ctx.PackInstance))
			continue
		}

		result.Rejector = ent.ctx.qualified()

		return result, fireErr
	}

	return result, nil
}

// FireNodeWriteAfter runs every reactor; aggregates non-nil errors into
// a multi-error. Control flow is unaffected.
func (engine *Engine) FireNodeWriteAfter(before, after *node.Node) error {
	return fireReactors(engine.nodeWriteAfter, func(fn NodeWriteReactor, ctx HookContext) error {
		return fn(ctx, before, after)
	})
}

// FireEdgeAddValidate runs the chain in registration order; first error
// short-circuits.
func (engine *Engine) FireEdgeAddValidate(edge index.EdgeRow) (string, error) {
	return fireValidate(engine.edgeAddValidate, func(fn EdgeAddValidator, ctx HookContext) error {
		return fn(ctx, edge)
	})
}

// FireEdgeAddAfter runs every reactor; aggregates non-nil errors.
func (engine *Engine) FireEdgeAddAfter(edge index.EdgeRow) error {
	return fireReactors(engine.edgeAddAfter, func(fn EdgeAddReactor, ctx HookContext) error {
		return fn(ctx, edge)
	})
}

// FireEdgeRemoveValidate / FireEdgeRemoveAfter mirror their Add siblings.
func (engine *Engine) FireEdgeRemoveValidate(edge index.EdgeRow) (string, error) {
	return fireValidate(engine.edgeRemoveValidate, func(fn EdgeRemoveValidator, ctx HookContext) error {
		return fn(ctx, edge)
	})
}

func (engine *Engine) FireEdgeRemoveAfter(edge index.EdgeRow) error {
	return fireReactors(engine.edgeRemoveAfter, func(fn EdgeRemoveReactor, ctx HookContext) error {
		return fn(ctx, edge)
	})
}
