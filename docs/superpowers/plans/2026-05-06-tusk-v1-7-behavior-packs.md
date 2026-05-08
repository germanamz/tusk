---
type: plan
title: Plan 7
status: shipped
pr: 358
shipped-at: "2026-05-06"
implements:
  - Plan 7 — Behavior Packs Spec
  - Tusk v1 Rebuild
---

# Tusk v1 — Plan 7: Behavior Packs

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the behavior-pack engine — the v1 composition primitive for per-write effects — and the workflow pack as the reference implementation. Workflow validates state-machine transitions on `tusk node modify`, accepts external edits with off-schema status as drift surfaced by `tusk doctor`, and lays a stable hook surface for v1.x cascading behaviors.

**Architecture:** A new `internal/behavior` package owns the eight-slot hook surface (`OnNodeWriteValidate`/`OnNodeWriteAfter`/`OnNodeReadValidate`/`OnNodeReadAfter`/`OnEdgeAddValidate`/`OnEdgeAddAfter`/`OnEdgeRemoveValidate`/`OnEdgeRemoveAfter`), the `Engine` that builds eight dispatch chains from a `Registry` of `Pack` instances, and the recovery-aware `Validate` variant that lets the workflow pack carry orphan-state recovery information through the chain without short-circuiting. A new `internal/behavior/workflow` package implements the lone v1 pack — declarative state machines parsed from `[behaviors.workflow.<name>]` TOML subtables. `node.Service.Create` and `node.Service.Modify` fire hooks at the validate-and-after points; `internal/reindex` runs the same validator in warn mode and persists drift to a new `workflow_drift` SQLite table that `internal/doctor` reads as `workflow-violation` Issues. CLI and MCP surfaces grow no new commands — workflow enforcement is plumbed through the existing `tusk node modify` and `tusk_node_modify`.

**Tech Stack:** Go 1.26, the existing `internal/{manifest,index,node,reindex,doctor,mcp,workspace,lock}` packages, plus `github.com/BurntSushi/toml` (already in use; no new dependency).

**Spec reference:** `docs/superpowers/specs/2026-05-06-tusk-v1-behavior-packs-design.md` (Plan 7 sub-spec) and `docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md` §8 + §13.2.

**Style rules:** Code respects `STYLE.md` — minimum 2-character identifiers (`*testing.T` → `test *testing.T`), blank lines around `err` guards, named errors on shadow. Lefthook enforces pre-commit; never `--no-verify`.

---

## File Structure

**Created:**

```
internal/behavior/
  hooks.go                   # HookContext, Phase, NodeWriteValidator + 7 sibling handler types
  pack.go                    # Kind, Instance, ReservedKey, Hooks, RecoveredEvent, Recoverable
  engine.go                  # Engine struct, NewEngine, Fire* methods, recovery-aware variant
  engine_test.go             # dispatch tests using an in-test fake pack
  registry.go                # Registry, Register, BuildEngine
  registry_test.go           # registry + collision-detection tests
  testpack_test.go           # fakePack helper for engine_test.go and registry_test.go

internal/behavior/workflow/
  errors.go                  # Error, RecoveredError, ErrorCode constants
  config.go                  # workflowConfig + decoder + schema validation; constructed by Kind.NewInstance
  config_test.go             # decoder schema-validation cases
  instance.go                # instance struct, Hooks(), ReservedKeys(), validate function
  instance_test.go           # validator algorithm cases (every branch in spec §5.2)
  kind.go                    # Kind type — Name() + NewInstance()
  kind_test.go               # Kind smoke test (NewInstance success + decode error path)

internal/index/
  workflow_drift_repo.go     # WorkflowDriftRepo: Append, ListAll, ClearForNode, CountAll
  workflow_drift_repo_test.go

cmd/tusk/
  behavior_registry.go       # newBehaviorEngine(loaded) helper used by every command and the MCP runtime
```

**Modified:**

```
internal/manifest/manifest.go        # add Behaviors map[string]map[string]toml.Primitive field
internal/manifest/loader.go          # validate behaviors structural shape (kind/instance names non-empty)
internal/manifest/loader_test.go     # cover well-formed multi-instance + structural rejections

internal/index/index.go              # add `workflow_drift` table to schema
internal/index/index_test.go         # assert `workflow_drift` table is present

internal/node/service.go             # NewServiceWithBehaviors constructor; behavior + drift + warnings fields;
                                     # hook dispatch in Create + Modify; recovery accumulation; drift writes
internal/node/service_test.go        # cover hook dispatch + recovery + warnings + drift writes

internal/reindex/reindex.go          # Config grows Behaviors + DriftLog; warn-mode validate;
                                     # clears drift on clean pass; report grows WorkflowViolations count
internal/reindex/reindex_test.go     # cover off-schema status producing drift rows + summary count

internal/doctor/doctor.go            # IssueWorkflowViolation kind; Config.WorkflowDrift; Run reads drift
internal/doctor/doctor_test.go       # cover workflow-violation Issue surfacing

cmd/tusk/cmd_node_create.go          # migrate to NewServiceWithBehaviors via behavior_registry helper
cmd/tusk/cmd_node_create_test.go     # cover non-initial-on-create rejection
cmd/tusk/cmd_node_modify.go          # migrate to NewServiceWithBehaviors; warnings to cmd.ErrOrStderr()
cmd/tusk/cmd_node_modify_test.go     # cover legal/illegal/recovery/unset cases
cmd/tusk/cmd_reindex.go              # wire Behaviors + DriftLog; emit summary count + hint
cmd/tusk/cmd_reindex_test.go         # cover summary count
cmd/tusk/cmd_doctor.go               # wire WorkflowDrift; render workflow-violation rows
cmd/tusk/cmd_doctor_test.go          # cover drift-row rendering

internal/mcp/runtime.go              # Runtime gains BehaviorEngine + WorkflowDrift; Open + ReloadManifest build engine;
                                     # NodeService uses NewServiceWithBehaviors
internal/mcp/runtime_test.go         # cover ReloadManifest rebuilding the engine
internal/mcp/tools.go                # node_modify + node_create return structured workflow-rejection on Error;
                                     # success payload grows `warnings` field on RecoveredError; doctor reads drift
internal/mcp/tools_test.go           # cover structured rejection shape + warnings shape
```

**Excluded for Plan 7** (per spec §1.2 / §10 ledger):

- `[node-types.<name>]` declarations and the property-type validator. Coherent separate concern; lands in its own plan.
- Built-in type packs (`kanban`, `vault`, `tags`) and their ergonomic shortcuts (`tusk ticket start`, etc.). Land alongside the type-packs work.
- Runtime activation of `auto-complete-parent` / `auto-revert-parent`. Schema reserved here; cascade implementation depends on re-entrant write support.
- `+tag` / `-tag` filter shorthand. Owned by the tag pack; lands when the tag pack does.
- User-defined / out-of-tree behavior packs. v2+; the in-tree registration model is the only Plan 7 surface.
- `OnNodeRead*` firing site (`Service.Get` / `List`). Reserved in the surface; no v1 consumer.
- `tusk_doctor` MCP tool surfacing the workflow-violation Issue kind (the `tusk doctor` CLI command does surface it; MCP tool addition is a Plan 6 follow-up).

---

## Module Conventions for Plan 7

**Behavior package layering.** `internal/behavior` depends only on `internal/node` (for `*node.Node`) and `internal/index` (for `EdgeRow`). It never imports `internal/behavior/workflow`. Workflow-pack-specific knowledge is encoded as a `Recoverable` interface implemented by `*workflow.RecoveredError` and consumed generically by the engine.

**TOML deferred decode.** `manifest.Manifest.Behaviors` is `map[string]map[string]toml.Primitive`. The manifest loader validates only structural shape (kind name non-empty, instance name non-empty, value is a table). The pack `Kind.NewInstance` decodes its primitive into a kind-specific config struct using `meta.PrimitiveDecode` from `BurntSushi/toml`. The `Manifest` carries the original `*toml.MetaData` so packs can call `meta.PrimitiveDecode`.

```go
// internal/manifest/manifest.go (exists; gains these)
type Manifest struct {
    // existing fields ...
    Behaviors map[string]map[string]toml.Primitive `toml:"behaviors"`

    // Internal: the meta data captured at decode time so pack Kinds can
    // PrimitiveDecode their own subtables. Set by Load; nil for hand-built
    // manifests in tests (which is fine — tests construct Instances directly).
    Meta *toml.MetaData `toml:"-"`
}
```

**Test fake pack.** `internal/behavior/testpack_test.go` defines a `fakePack` that fills any subset of the eight Hook slots and tracks call order. It is reused by `engine_test.go` and `registry_test.go`. Pattern:

```go
type fakePack struct {
    name     string
    kind     string
    hooks    behavior.Hooks
    reserved []behavior.ReservedKey
    calls    []string  // appended on every hook fire
}

func (pack *fakePack) Name() string  { return pack.name }
func (pack *fakePack) Kind() string  { return pack.kind }
func (pack *fakePack) Hooks() behavior.Hooks { return pack.hooks }
func (pack *fakePack) ReservedKeys() []behavior.ReservedKey { return pack.reserved }
```

Test helpers append to `calls` from inside each handler so the tests can assert order.

**HookContext.** Concrete type in v1 — exported as `behavior.HookContext` rather than an interface, since the engine constructs the value:

```go
type HookContext struct {
    PackKind     string  // e.g. "workflow"
    PackInstance string  // e.g. "tickets"
}
```

The HookContext is passed by value. Future versions can add fields (read-access methods) without breaking handler signatures.

**Recovery contract.** The behavior package defines:

```go
type RecoveredEvent struct {
    PackKind     string
    PackInstance string
    Property     string
    From         string
    To           string
    Message      string
}

type Recoverable interface {
    error
    AsRecoveredEvent(packKind, packInstance string) RecoveredEvent
}
```

`workflow.RecoveredError` implements `Recoverable`. The recovery-aware Fire variant uses `errors.As(err, &target)` against `behavior.Recoverable` to detect recovery; non-recoverable errors short-circuit.

**Service.Create / Modify dispatch order.** Validate phase runs in order: NodeWrite → EdgeRemove (for diffs) → EdgeAdd (for diffs). After phase mirrors that order. This is the contract documented in spec §6.6.

**Reindex `before` is always nil.** The reindex validator path runs with `before = nil`. Drift rows from rejection still surface; recovery events are technically reachable through the surface but never fire in v1 because the validator only emits `RecoveredError` when `before` is non-empty (spec §6.4 / §10 ledger #10).

**Module conventions for tests.** Use `*testing.T` named `test`, parallel-safe (no `t.Parallel()` unless stated). Workspace fixtures use the existing `cmd/tusk` test helpers. Behavior tests construct `*behavior.Engine` via `behavior.NewEngine([]Instance{...})` rather than going through `Registry.BuildEngine`.

---

## Task 0: Pre-flight verification

**Files:** none.

- [ ] **Step 1: Verify branch + clean state**

```bash
git rev-parse --abbrev-ref HEAD
git status --porcelain
```

Expected: `feat/plan-7`; only the plan/spec docs in `docs/superpowers/` (already committed) and possibly the local `tusk` binary as untracked.

- [ ] **Step 2: Verify build + test green pre-Plan-7**

```bash
make build && make test
```

Expected: build succeeds; all tests pass. Plan 7 starts from a green tree.

- [ ] **Step 3: Verify spec is in tree**

```bash
test -f docs/superpowers/specs/2026-05-06-tusk-v1-behavior-packs-design.md && echo OK
```

Expected: `OK`.

---

## Task 1: behavior package — hook types + HookContext

**Files:** Create `internal/behavior/hooks.go`.

This task lands the type-only foundation: `HookContext`, `Phase`, the eight handler function types. No tests (types only); subsequent tasks exercise them.

- [ ] **Step 1: Write `internal/behavior/hooks.go`**

```go
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

type NodeReadValidator func(ctx HookContext, snapshot *node.Node) error
type NodeReadReactor func(ctx HookContext, snapshot *node.Node) error

type EdgeAddValidator func(ctx HookContext, edge index.EdgeRow) error
type EdgeAddReactor func(ctx HookContext, edge index.EdgeRow) error

type EdgeRemoveValidator func(ctx HookContext, edge index.EdgeRow) error
type EdgeRemoveReactor func(ctx HookContext, edge index.EdgeRow) error
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/behavior/...
```

Expected: success. Imports resolve via existing `internal/node` and `internal/index`.

- [ ] **Step 3: Commit**

```bash
git add internal/behavior/hooks.go
git commit -m "feat(behavior): hook types and HookContext"
```

---

## Task 2: behavior package — Pack, Instance, Hooks, recovery contract

**Files:** Create `internal/behavior/pack.go`.

- [ ] **Step 1: Write `internal/behavior/pack.go`**

```go
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
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/behavior/...
```

Expected: success. The `BurntSushi/toml` package is already a transitive dependency via `internal/manifest`.

- [ ] **Step 3: Commit**

```bash
git add internal/behavior/pack.go
git commit -m "feat(behavior): Pack/Instance/Kind types and recovery contract"
```

---

## Task 3: behavior package — Engine with simple Fire dispatch

**Files:** Create `internal/behavior/engine.go`, `internal/behavior/testpack_test.go`, `internal/behavior/engine_test.go`.

- [ ] **Step 1: Write the test fake (`testpack_test.go`)**

```go
package behavior_test

import (
	"github.com/germanamz/tusk/internal/behavior"
)

// fakePack is a minimal behavior.Instance for engine and registry tests.
// Tests configure Hooks, ReservedKeys, Name, and Kind directly.
type fakePack struct {
	name     string
	kind     string
	hooks    behavior.Hooks
	reserved []behavior.ReservedKey
}

func (pack *fakePack) Name() string                          { return pack.name }
func (pack *fakePack) Kind() string                          { return pack.kind }
func (pack *fakePack) Hooks() behavior.Hooks                 { return pack.hooks }
func (pack *fakePack) ReservedKeys() []behavior.ReservedKey  { return pack.reserved }
```

- [ ] **Step 2: Write the failing test (`engine_test.go`)**

```go
package behavior_test

import (
	"errors"
	"testing"

	"github.com/germanamz/tusk/internal/behavior"
	"github.com/germanamz/tusk/internal/node"
)

func TestNewEngine_SimpleNodeWriteValidate_ChainOrder(test *testing.T) {
	var calls []string

	first := &fakePack{
		name: "first",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "first")
				return nil
			},
		},
	}

	second := &fakePack{
		name: "second",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "second")
				return nil
			},
		},
	}

	engine, newErr := behavior.NewEngine([]behavior.Instance{first, second})

	if newErr != nil {
		test.Fatalf("NewEngine: %v", newErr)
	}

	rejector, fireErr := engine.FireNodeWriteValidate(nil, &node.Node{Type: "ticket"})

	if fireErr != nil {
		test.Fatalf("FireNodeWriteValidate: %v", fireErr)
	}

	if rejector != "" {
		test.Errorf("rejector = %q, want empty", rejector)
	}

	if len(calls) != 2 || calls[0] != "first" || calls[1] != "second" {
		test.Errorf("calls = %v, want [first second]", calls)
	}
}

func TestNewEngine_NodeWriteValidate_ShortCircuitsOnFirstRejection(test *testing.T) {
	var calls []string
	rejection := errors.New("boom")

	first := &fakePack{
		name: "first",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "first")
				return rejection
			},
		},
	}

	second := &fakePack{
		name: "second",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "second")
				return nil
			},
		},
	}

	engine, _ := behavior.NewEngine([]behavior.Instance{first, second})

	rejector, fireErr := engine.FireNodeWriteValidate(nil, &node.Node{Type: "ticket"})

	if !errors.Is(fireErr, rejection) {
		test.Errorf("fireErr = %v, want wrapped %v", fireErr, rejection)
	}

	if rejector != "fake.first" {
		test.Errorf("rejector = %q, want %q", rejector, "fake.first")
	}

	if len(calls) != 1 || calls[0] != "first" {
		test.Errorf("calls = %v, want [first] only (short-circuit)", calls)
	}
}

func TestNewEngine_NodeWriteAfter_FansOutUnconditionally(test *testing.T) {
	var calls []string

	first := &fakePack{
		name: "first",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteAfter: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "first")
				return errors.New("first error")
			},
		},
	}

	second := &fakePack{
		name: "second",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteAfter: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "second")
				return nil
			},
		},
	}

	engine, _ := behavior.NewEngine([]behavior.Instance{first, second})

	if fireErr := engine.FireNodeWriteAfter(nil, &node.Node{Type: "ticket"}); fireErr == nil {
		test.Fatalf("FireNodeWriteAfter: expected aggregated error")
	}

	if len(calls) != 2 || calls[0] != "first" || calls[1] != "second" {
		test.Errorf("calls = %v, want [first second]", calls)
	}
}
```

- [ ] **Step 3: Run, verify fail**

```bash
go test ./internal/behavior/...
```

Expected: FAIL — `behavior.NewEngine`, `engine.FireNodeWriteValidate`, `engine.FireNodeWriteAfter` undefined.

- [ ] **Step 4: Write `internal/behavior/engine.go`**

```go
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
```

- [ ] **Step 5: Run, verify pass**

```bash
go test ./internal/behavior/... -run TestNewEngine -v
```

Expected: 3 PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/behavior/engine.go internal/behavior/engine_test.go internal/behavior/testpack_test.go
git commit -m "feat(behavior): Engine with eight-slot dispatch + collision detection"
```

---

## Task 4: behavior package — recovery-aware Fire variant

**Files:** Modify `internal/behavior/engine.go`, append to `internal/behavior/engine_test.go`.

The recovery-aware variant is what `node.Service.Modify` and `internal/reindex` use. It treats errors implementing `Recoverable` as "carry information through the chain" rather than "abort". Non-Recoverable errors short-circuit normally.

- [ ] **Step 1: Append failing tests to `engine_test.go`**

```go
// recoverableErr is a test-only Recoverable used to exercise the
// recovery-aware Fire variant.
type recoverableErr struct {
	property string
	from     string
	to       string
	message  string
}

func (err *recoverableErr) Error() string { return err.message }

func (err *recoverableErr) AsRecoveredEvent(packKind, packInstance string) behavior.RecoveredEvent {
	return behavior.RecoveredEvent{
		PackKind:     packKind,
		PackInstance: packInstance,
		Property:     err.property,
		From:         err.from,
		To:           err.to,
		Message:      err.message,
	}
}

func TestFireNodeWriteValidateWithRecovery_RecoverableContinuesChain(test *testing.T) {
	var calls []string

	first := &fakePack{
		name: "first",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "first")
				return &recoverableErr{property: "status", from: "blocked", to: "active", message: "recovered"}
			},
		},
	}

	second := &fakePack{
		name: "second",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "second")
				return nil
			},
		},
	}

	engine, _ := behavior.NewEngine([]behavior.Instance{first, second})

	result, fireErr := engine.FireNodeWriteValidateWithRecovery(nil, &node.Node{Type: "ticket"})

	if fireErr != nil {
		test.Fatalf("FireNodeWriteValidateWithRecovery: %v", fireErr)
	}

	if result.Rejector != "" {
		test.Errorf("Rejector = %q, want empty", result.Rejector)
	}

	if len(result.Recovered) != 1 {
		test.Fatalf("Recovered = %v, want 1 event", result.Recovered)
	}

	got := result.Recovered[0]

	if got.PackKind != "fake" || got.PackInstance != "first" {
		test.Errorf("Recovered[0] qualifier = %q.%q, want fake.first", got.PackKind, got.PackInstance)
	}

	if got.Property != "status" || got.From != "blocked" || got.To != "active" {
		test.Errorf("Recovered[0] payload = %+v", got)
	}

	if len(calls) != 2 || calls[0] != "first" || calls[1] != "second" {
		test.Errorf("calls = %v, want [first second]", calls)
	}
}

func TestFireNodeWriteValidateWithRecovery_NonRecoverableShortCircuits(test *testing.T) {
	var calls []string
	rejection := errors.New("hard reject")

	first := &fakePack{
		name: "first",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "first")
				return rejection
			},
		},
	}

	second := &fakePack{
		name: "second",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				calls = append(calls, "second")
				return nil
			},
		},
	}

	engine, _ := behavior.NewEngine([]behavior.Instance{first, second})

	result, fireErr := engine.FireNodeWriteValidateWithRecovery(nil, &node.Node{Type: "ticket"})

	if !errors.Is(fireErr, rejection) {
		test.Errorf("fireErr = %v, want wrapped %v", fireErr, rejection)
	}

	if result.Rejector != "fake.first" {
		test.Errorf("Rejector = %q, want fake.first", result.Rejector)
	}

	if len(calls) != 1 {
		test.Errorf("calls = %v, want [first] only (short-circuit)", calls)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/behavior/... -run TestFireNodeWriteValidateWithRecovery
```

Expected: FAIL — `engine.FireNodeWriteValidateWithRecovery` undefined.

- [ ] **Step 3: Append to `internal/behavior/engine.go`**

Add after the `FireNodeWriteValidate` method:

```go
// FireResult carries the outcome of a recovery-aware Validate fire.
type FireResult struct {
	Rejector  string           // "" when no rejection
	Recovered []RecoveredEvent // accumulated across the chain
}

// FireNodeWriteValidateWithRecovery walks the chain in registration order.
// Errors implementing Recoverable are converted to RecoveredEvent entries
// and the chain continues. Any other non-nil error short-circuits and is
// returned as the rejection.
func (engine *Engine) FireNodeWriteValidateWithRecovery(before, after *node.Node) (FireResult, error) {
	var result FireResult

	for _, entry := range engine.nodeWriteValidate {
		fireErr := entry.fn(entry.ctx, before, after)

		if fireErr == nil {
			continue
		}

		var recoverable Recoverable

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
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/behavior/... -run TestFireNodeWriteValidateWithRecovery -v
```

Expected: 2 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/behavior/engine.go internal/behavior/engine_test.go
git commit -m "feat(behavior): recovery-aware Validate fire variant"
```

---

## Task 5: behavior package — Registry + BuildEngine

**Files:** Create `internal/behavior/registry.go`, `internal/behavior/registry_test.go`.

`Registry` is the production wiring layer between the manifest and the engine. Tests can construct an engine directly via `NewEngine`; production code uses `Registry.Register(kind)` once per Kind and `Registry.BuildEngine(loaded)` to resolve manifest entries.

- [ ] **Step 1: Write the failing test**

```go
// internal/behavior/registry_test.go
package behavior_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/germanamz/tusk/internal/behavior"
	"github.com/germanamz/tusk/internal/manifest"
)

// fakeKind constructs fakePack instances. Used by registry tests.
type fakeKind struct {
	name           string
	failOnInstance string // when non-empty, NewInstance for that name returns an error
	produced       func(instanceName string) *fakePack
}

func (kind *fakeKind) Name() string { return kind.name }

func (kind *fakeKind) NewInstance(instanceName string, raw toml.Primitive, meta *toml.MetaData) (behavior.Instance, error) {
	if kind.failOnInstance == instanceName {
		return nil, errors.New("decode error")
	}

	if kind.produced != nil {
		return kind.produced(instanceName), nil
	}

	return &fakePack{name: instanceName, kind: kind.name}, nil
}

func TestRegistry_RegisterDuplicateRejected(test *testing.T) {
	reg := behavior.NewRegistry()

	if registerErr := reg.Register(&fakeKind{name: "workflow"}); registerErr != nil {
		test.Fatalf("first Register: %v", registerErr)
	}

	registerErr := reg.Register(&fakeKind{name: "workflow"})

	if registerErr == nil {
		test.Errorf("second Register: expected duplicate-name error")
	}
}

func TestRegistry_BuildEngine_UnknownKindRejected(test *testing.T) {
	reg := behavior.NewRegistry()

	loaded := &manifest.Manifest{
		Behaviors: map[string]map[string]toml.Primitive{
			"missing": {"any": toml.Primitive{}},
		},
	}

	_, buildErr := reg.BuildEngine(loaded)

	if buildErr == nil {
		test.Fatalf("BuildEngine: expected error for unknown kind")
	}

	if !strings.Contains(buildErr.Error(), "missing") {
		test.Errorf("BuildEngine error = %v, want mention of unknown kind", buildErr)
	}
}

func TestRegistry_BuildEngine_PropagatesNewInstanceError(test *testing.T) {
	reg := behavior.NewRegistry()

	if registerErr := reg.Register(&fakeKind{name: "workflow", failOnInstance: "broken"}); registerErr != nil {
		test.Fatalf("Register: %v", registerErr)
	}

	loaded := &manifest.Manifest{
		Behaviors: map[string]map[string]toml.Primitive{
			"workflow": {"broken": toml.Primitive{}},
		},
	}

	_, buildErr := reg.BuildEngine(loaded)

	if buildErr == nil {
		test.Fatalf("BuildEngine: expected NewInstance error to surface")
	}
}

func TestRegistry_BuildEngine_CollisionDetected(test *testing.T) {
	reg := behavior.NewRegistry()

	colliding := func(instanceName string) *fakePack {
		return &fakePack{
			name: instanceName,
			kind: "workflow",
			reserved: []behavior.ReservedKey{
				{NodeType: "ticket", Property: "status"},
			},
		}
	}

	if registerErr := reg.Register(&fakeKind{name: "workflow", produced: colliding}); registerErr != nil {
		test.Fatalf("Register: %v", registerErr)
	}

	loaded := &manifest.Manifest{
		Behaviors: map[string]map[string]toml.Primitive{
			"workflow": {
				"a": toml.Primitive{},
				"b": toml.Primitive{},
			},
		},
	}

	_, buildErr := reg.BuildEngine(loaded)

	if buildErr == nil {
		test.Fatalf("BuildEngine: expected collision error")
	}

	if !strings.Contains(buildErr.Error(), "ticket") || !strings.Contains(buildErr.Error(), "status") {
		test.Errorf("BuildEngine error = %v, want collision mentioning ticket/status", buildErr)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/behavior/... -run TestRegistry
```

Expected: FAIL — `behavior.NewRegistry` and `Registry.BuildEngine` undefined; `manifest.Manifest.Behaviors` field missing.

- [ ] **Step 3: Add `Behaviors` field to `internal/manifest/manifest.go`**

Modify the existing `Manifest` struct to grow a `Behaviors` field. Insert just before the closing brace of the struct:

```go
// Behaviors is a two-level map: kind name → instance name → raw TOML
// table. The kind-specific decode happens inside the pack package
// (deferred-decode contract).
Behaviors map[string]map[string]toml.Primitive `toml:"behaviors"`

// Meta is the BurntSushi/toml MetaData captured at decode time so pack
// Kinds can call PrimitiveDecode against their subtable. Nil for
// hand-built manifests (tests that construct a Manifest literal).
Meta *toml.MetaData `toml:"-"`
```

Add `"github.com/BurntSushi/toml"` to the imports of `manifest.go`.

- [ ] **Step 4: Verify manifest still compiles**

```bash
go build ./internal/manifest/...
```

Expected: success.

- [ ] **Step 5: Write `internal/behavior/registry.go`**

```go
package behavior

import (
	"fmt"

	"github.com/germanamz/tusk/internal/manifest"
)

// Registry maps kind names to constructors. Populated explicitly — no
// init() side effects — so tests can build a Registry from scratch.
type Registry struct {
	kinds map[string]Kind
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{kinds: map[string]Kind{}}
}

// Register adds a Kind to the Registry. Duplicate names return an error.
func (registry *Registry) Register(kind Kind) error {
	name := kind.Name()

	if _, taken := registry.kinds[name]; taken {
		return fmt.Errorf("behavior: kind %q already registered", name)
	}

	registry.kinds[name] = kind

	return nil
}

// Lookup returns (kind, true) when name is registered.
func (registry *Registry) Lookup(name string) (Kind, bool) {
	kind, found := registry.kinds[name]

	return kind, found
}

// BuildEngine resolves loaded.Behaviors into Instances by calling each
// Kind.NewInstance, then constructs and returns an *Engine. Reservation
// collisions surface here via NewEngine.
func (registry *Registry) BuildEngine(loaded *manifest.Manifest) (*Engine, error) {
	if loaded == nil {
		return NewEngine(nil)
	}

	var instances []Instance

	for kindName, perInstance := range loaded.Behaviors {
		kind, found := registry.Lookup(kindName)

		if !found {
			return nil, fmt.Errorf("behavior: manifest references unknown kind %q (registered: %v)", kindName, registry.knownKinds())
		}

		for instanceName, raw := range perInstance {
			instance, newErr := kind.NewInstance(instanceName, raw, loaded.Meta)

			if newErr != nil {
				return nil, fmt.Errorf("behavior: %s.%s: %w", kindName, instanceName, newErr)
			}

			instances = append(instances, instance)
		}
	}

	return NewEngine(instances)
}

func (registry *Registry) knownKinds() []string {
	names := make([]string, 0, len(registry.kinds))

	for name := range registry.kinds {
		names = append(names, name)
	}

	return names
}
```

- [ ] **Step 6: Run, verify pass**

```bash
go test ./internal/behavior/... -run TestRegistry -v
```

Expected: 4 PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/behavior/registry.go internal/behavior/registry_test.go internal/manifest/manifest.go
git commit -m "feat(behavior): Registry + BuildEngine; manifest grows Behaviors field"
```

---

## Task 6: workflow pack — Error and RecoveredError types

**Files:** Create `internal/behavior/workflow/errors.go`.

These two error types are the contract between the workflow validator and downstream surfaces (CLI, MCP, doctor). `Error` is a hard rejection; `RecoveredError` implements `behavior.Recoverable` so the engine carries it through the chain as informational drift.

- [ ] **Step 1: Write `internal/behavior/workflow/errors.go`**

```go
// Package workflow implements the v1 reference behavior pack: declarative
// state-machine validation hooked to OnNodeWriteValidate.
package workflow

import (
	"fmt"
	"strings"

	"github.com/germanamz/tusk/internal/behavior"
)

// ErrorCode enumerates the rejection codes the workflow pack returns.
type ErrorCode string

const (
	ErrIllegalTransition  ErrorCode = "illegal-transition"
	ErrUnknownTargetState ErrorCode = "unknown-target-state"
	ErrNonInitialOnCreate ErrorCode = "non-initial-on-create"
	ErrCannotUnsetStatus  ErrorCode = "cannot-unset-status"
)

// Error is an outright rejection. The Modify path returns it; reindex
// captures it as a drift row.
type Error struct {
	Code         ErrorCode
	Property     string
	From         string   // current state; "" when setting for the first time
	To           string   // target state; "" when unsetting
	KnownStates  []string // populated for ErrUnknownTargetState
	ValidTargets []string // populated for ErrIllegalTransition
	PackInstance string   // e.g. "tickets"
}

func (err *Error) Error() string {
	header := fmt.Sprintf("workflow %q", err.PackInstance)

	switch err.Code {
	case ErrIllegalTransition:
		return fmt.Sprintf("%s: cannot transition %s %q → %q\n  valid targets from %q: %s",
			header, err.Property, err.From, err.To, err.From, joinOrEmpty(err.ValidTargets))

	case ErrUnknownTargetState:
		return fmt.Sprintf("%s: %q is not a declared state for property %q\n  declared states: %s",
			header, err.To, err.Property, joinOrEmpty(err.KnownStates))

	case ErrNonInitialOnCreate:
		return fmt.Sprintf("%s: %s must be set to an initial state on create\n  initial state(s): %s; got: %q",
			header, err.Property, joinOrEmpty(err.KnownStates), err.To)

	case ErrCannotUnsetStatus:
		return fmt.Sprintf("%s: cannot unset managed property %q (currently %q)\n  edit the file directly to remove the field, then reindex",
			header, err.Property, err.From)
	}

	return fmt.Sprintf("%s: unknown workflow error", header)
}

// RecoveredError is orphan-state recovery: returned when the validator
// allows a transition out of a status the manifest no longer declares.
// It implements behavior.Recoverable.
type RecoveredError struct {
	Property     string
	From         string
	To           string
	PackInstance string
}

func (err *RecoveredError) Error() string {
	return fmt.Sprintf("workflow %q recovered from unknown status %q → %q; transition not validated",
		err.PackInstance, err.From, err.To)
}

// AsRecoveredEvent satisfies behavior.Recoverable. The engine populates
// PackKind from the HookContext at fire time; PackInstance carried on
// the error is reaffirmed for symmetry.
func (err *RecoveredError) AsRecoveredEvent(packKind, packInstance string) behavior.RecoveredEvent {
	return behavior.RecoveredEvent{
		PackKind:     packKind,
		PackInstance: packInstance,
		Property:     err.Property,
		From:         err.From,
		To:           err.To,
		Message:      err.Error(),
	}
}

func joinOrEmpty(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}

	return strings.Join(values, ", ")
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/behavior/workflow/...
```

Expected: success.

- [ ] **Step 3: Commit**

```bash
git add internal/behavior/workflow/errors.go
git commit -m "feat(workflow): Error and RecoveredError types"
```

---

## Task 7: workflow pack — Kind and config decoder

**Files:** Create `internal/behavior/workflow/config.go`, `internal/behavior/workflow/config_test.go`, `internal/behavior/workflow/kind.go`, `internal/behavior/workflow/kind_test.go`.

The config decoder lives separately from the runtime instance for testability. `Kind.NewInstance` is the production path: it reads a `toml.Primitive` from the manifest, decodes it via `meta.PrimitiveDecode`, validates the schema (every rule from spec §4.3), and constructs the runtime instance.

- [ ] **Step 1: Write the failing config decoder test (`config_test.go`)**

```go
package workflow

import (
	"strings"
	"testing"
)

func TestDecodeConfig_AppliesToRequired(test *testing.T) {
	cfg := workflowConfig{
		States: []stateDecl{{Name: "open", Initial: true}},
	}

	if validateErr := cfg.validate(); validateErr == nil || !strings.Contains(validateErr.Error(), "applies-to") {
		test.Errorf("validate: expected applies-to error, got %v", validateErr)
	}
}

func TestDecodeConfig_StatesRequired(test *testing.T) {
	cfg := workflowConfig{AppliesTo: []string{"ticket"}}

	if validateErr := cfg.validate(); validateErr == nil || !strings.Contains(validateErr.Error(), "states") {
		test.Errorf("validate: expected states error, got %v", validateErr)
	}
}

func TestDecodeConfig_DuplicateStateName(test *testing.T) {
	cfg := workflowConfig{
		AppliesTo: []string{"ticket"},
		States: []stateDecl{
			{Name: "open"},
			{Name: "open"},
		},
	}

	if validateErr := cfg.validate(); validateErr == nil || !strings.Contains(validateErr.Error(), "duplicate") {
		test.Errorf("validate: expected duplicate error, got %v", validateErr)
	}
}

func TestDecodeConfig_MultipleInitial(test *testing.T) {
	cfg := workflowConfig{
		AppliesTo: []string{"ticket"},
		States: []stateDecl{
			{Name: "a", Initial: true},
			{Name: "b", Initial: true},
		},
	}

	if validateErr := cfg.validate(); validateErr == nil || !strings.Contains(validateErr.Error(), "initial") {
		test.Errorf("validate: expected initial error, got %v", validateErr)
	}
}

func TestDecodeConfig_MultipleStart(test *testing.T) {
	cfg := workflowConfig{
		AppliesTo: []string{"ticket"},
		States: []stateDecl{
			{Name: "a", Start: true},
			{Name: "b", Start: true},
		},
	}

	if validateErr := cfg.validate(); validateErr == nil || !strings.Contains(validateErr.Error(), "start") {
		test.Errorf("validate: expected start error, got %v", validateErr)
	}
}

func TestDecodeConfig_DoneWithoutTerminalRejected(test *testing.T) {
	cfg := workflowConfig{
		AppliesTo: []string{"ticket"},
		States: []stateDecl{
			{Name: "shipped", Done: true, Terminal: false},
		},
	}

	if validateErr := cfg.validate(); validateErr == nil || !strings.Contains(validateErr.Error(), "terminal") {
		test.Errorf("validate: expected terminal-without-done error, got %v", validateErr)
	}
}

func TestDecodeConfig_TransitionReferencesUnknownState(test *testing.T) {
	cfg := workflowConfig{
		AppliesTo: []string{"ticket"},
		States: []stateDecl{
			{Name: "a"},
		},
		Transitions: []transitionDecl{
			{From: "a", To: "missing"},
		},
	}

	if validateErr := cfg.validate(); validateErr == nil || !strings.Contains(validateErr.Error(), "missing") {
		test.Errorf("validate: expected unknown-state error, got %v", validateErr)
	}
}

func TestDecodeConfig_DuplicateTransition(test *testing.T) {
	cfg := workflowConfig{
		AppliesTo: []string{"ticket"},
		States: []stateDecl{
			{Name: "a"},
			{Name: "b"},
		},
		Transitions: []transitionDecl{
			{From: "a", To: "b"},
			{From: "a", To: "b"},
		},
	}

	if validateErr := cfg.validate(); validateErr == nil || !strings.Contains(validateErr.Error(), "duplicate") {
		test.Errorf("validate: expected duplicate transition error, got %v", validateErr)
	}
}

func TestDecodeConfig_StatusPropertyDefaultsToStatus(test *testing.T) {
	cfg := workflowConfig{
		AppliesTo: []string{"ticket"},
		States:    []stateDecl{{Name: "open"}},
	}

	if cfg.normalize(); cfg.StatusProperty != "status" {
		test.Errorf("StatusProperty after normalize = %q, want %q", cfg.StatusProperty, "status")
	}
}

func TestDecodeConfig_HappyPath(test *testing.T) {
	cfg := workflowConfig{
		AppliesTo:      []string{"ticket"},
		StatusProperty: "status",
		States: []stateDecl{
			{Name: "pending", Initial: true},
			{Name: "active", Start: true},
			{Name: "completed", Terminal: true, Done: true},
		},
		Transitions: []transitionDecl{
			{From: "pending", To: "active"},
			{From: "active", To: "completed"},
		},
	}

	if validateErr := cfg.validate(); validateErr != nil {
		test.Errorf("validate: %v", validateErr)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/behavior/workflow/...
```

Expected: FAIL — `workflowConfig`, `stateDecl`, `transitionDecl`, `validate`, `normalize` undefined.

- [ ] **Step 3: Write `internal/behavior/workflow/config.go`**

```go
package workflow

import (
	"errors"
	"fmt"
)

// workflowConfig is the decoded TOML form of one [behaviors.workflow.<name>]
// table. Field tags match the manifest's snake-cased keys.
type workflowConfig struct {
	AppliesTo          []string         `toml:"applies-to"`
	StatusProperty     string           `toml:"status-property"`
	States             []stateDecl      `toml:"states"`
	Transitions        []transitionDecl `toml:"transitions"`
	AutoCompleteParent bool             `toml:"auto-complete-parent"`
	AutoRevertParent   bool             `toml:"auto-revert-parent"`
}

type stateDecl struct {
	Name     string `toml:"name"`
	Initial  bool   `toml:"initial"`
	Start    bool   `toml:"start"`
	Terminal bool   `toml:"terminal"`
	Done     bool   `toml:"done"`
}

type transitionDecl struct {
	From string `toml:"from"`
	To   string `toml:"to"`
}

// normalize applies defaults. Call after decode, before validate.
func (cfg *workflowConfig) normalize() {
	if cfg.StatusProperty == "" {
		cfg.StatusProperty = "status"
	}
}

// validate rejects schema violations per spec §4.3. Call after normalize.
func (cfg *workflowConfig) validate() error {
	if len(cfg.AppliesTo) == 0 {
		return errors.New("applies-to: required, must list at least one node type")
	}

	for index, typeName := range cfg.AppliesTo {
		if typeName == "" {
			return fmt.Errorf("applies-to[%d]: empty string is not a valid type name", index)
		}
	}

	if len(cfg.States) == 0 {
		return errors.New("states: required, must declare at least one state")
	}

	stateNames := map[string]struct{}{}

	var initialCount, startCount int

	for index, state := range cfg.States {
		if state.Name == "" {
			return fmt.Errorf("states[%d]: empty name", index)
		}

		if _, taken := stateNames[state.Name]; taken {
			return fmt.Errorf("states: duplicate state name %q", state.Name)
		}

		stateNames[state.Name] = struct{}{}

		if state.Initial {
			initialCount++
		}

		if state.Start {
			startCount++
		}

		if state.Done && !state.Terminal {
			return fmt.Errorf("states[%q]: done = true requires terminal = true (done implies terminal in v1)", state.Name)
		}
	}

	if initialCount > 1 {
		return errors.New("states: at most one state may set initial = true")
	}

	if startCount > 1 {
		return errors.New("states: at most one state may set start = true")
	}

	transitionPairs := map[transitionDecl]struct{}{}

	for index, trans := range cfg.Transitions {
		if _, ok := stateNames[trans.From]; !ok {
			return fmt.Errorf("transitions[%d]: from references undeclared state %q", index, trans.From)
		}

		if _, ok := stateNames[trans.To]; !ok {
			return fmt.Errorf("transitions[%d]: to references undeclared state %q", index, trans.To)
		}

		if _, taken := transitionPairs[trans]; taken {
			return fmt.Errorf("transitions: duplicate (from=%q, to=%q)", trans.From, trans.To)
		}

		transitionPairs[trans] = struct{}{}
	}

	return nil
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/behavior/workflow/... -v
```

Expected: 10 PASS.

- [ ] **Step 5: Write the failing Kind test (`kind_test.go`)**

```go
package workflow_test

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/germanamz/tusk/internal/behavior/workflow"
)

const sampleManifest = `
[behaviors.workflow.tickets]
applies-to = ["ticket"]
states = [
  { name = "pending", initial = true },
  { name = "active" },
  { name = "completed", terminal = true, done = true },
]
transitions = [
  { from = "pending", to = "active" },
  { from = "active", to = "completed" },
]
`

func TestKind_NewInstance_HappyPath(test *testing.T) {
	var decoded struct {
		Behaviors map[string]map[string]toml.Primitive `toml:"behaviors"`
	}

	meta, decodeErr := toml.Decode(sampleManifest, &decoded)

	if decodeErr != nil {
		test.Fatalf("toml decode: %v", decodeErr)
	}

	primitive := decoded.Behaviors["workflow"]["tickets"]

	kind := workflow.Kind{}
	instance, newErr := kind.NewInstance("tickets", primitive, &meta)

	if newErr != nil {
		test.Fatalf("NewInstance: %v", newErr)
	}

	if instance.Name() != "tickets" || instance.Kind() != "workflow" {
		test.Errorf("instance qualifier = %s.%s, want workflow.tickets", instance.Kind(), instance.Name())
	}

	reserved := instance.ReservedKeys()

	if len(reserved) != 1 || reserved[0].NodeType != "ticket" || reserved[0].Property != "status" {
		test.Errorf("ReservedKeys = %v, want [{ticket status}]", reserved)
	}

	hooks := instance.Hooks()

	if hooks.OnNodeWriteValidate == nil {
		test.Errorf("Hooks.OnNodeWriteValidate is nil")
	}
}

func TestKind_NewInstance_DecodeError(test *testing.T) {
	const broken = `
[behaviors.workflow.bad]
applies-to = ["ticket"]
# states missing → schema rejection
`

	var decoded struct {
		Behaviors map[string]map[string]toml.Primitive `toml:"behaviors"`
	}

	meta, decodeErr := toml.Decode(broken, &decoded)

	if decodeErr != nil {
		test.Fatalf("toml decode: %v", decodeErr)
	}

	primitive := decoded.Behaviors["workflow"]["bad"]

	kind := workflow.Kind{}
	_, newErr := kind.NewInstance("bad", primitive, &meta)

	if newErr == nil || !strings.Contains(newErr.Error(), "states") {
		test.Errorf("NewInstance: expected states error, got %v", newErr)
	}
}
```

- [ ] **Step 6: Run, verify fail**

```bash
go test ./internal/behavior/workflow/... -run TestKind
```

Expected: FAIL — `workflow.Kind` undefined.

- [ ] **Step 7: Write `internal/behavior/workflow/kind.go`**

```go
package workflow

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"

	"github.com/germanamz/tusk/internal/behavior"
)

// Kind is the workflow pack constructor. v1 registers exactly one
// behavior.Kind with the registry: workflow.Kind{}.
type Kind struct{}

// Name satisfies behavior.Kind.
func (Kind) Name() string { return "workflow" }

// NewInstance decodes raw into a workflowConfig, validates the schema,
// and produces a runtime *instance. Schema errors carry the (kind,
// instance) qualifier in their message via behavior.Registry.BuildEngine.
func (kind Kind) NewInstance(instanceName string, raw toml.Primitive, meta *toml.MetaData) (behavior.Instance, error) {
	if instanceName == "" {
		return nil, errors.New("instance name: empty")
	}

	if meta == nil {
		return nil, errors.New("manifest meta: nil — workflow pack requires a TOML-loaded manifest")
	}

	var cfg workflowConfig

	if decodeErr := meta.PrimitiveDecode(raw, &cfg); decodeErr != nil {
		return nil, fmt.Errorf("decode: %w", decodeErr)
	}

	cfg.normalize()

	if validateErr := cfg.validate(); validateErr != nil {
		return nil, validateErr
	}

	if cfg.AutoCompleteParent || cfg.AutoRevertParent {
		fmt.Fprintln(os.Stderr, "workflow: auto-* directives accepted but not yet active")
	}

	return newInstance(instanceName, cfg), nil
}
```

The `newInstance` constructor is defined in the next task; this Step expects to fail compilation until then. We will land Step 7 alongside Task 8's instance.go.

- [ ] **Step 8: Defer commit until instance.go lands**

Hold the commit. The next task lands `instance.go` which adds `newInstance`. Both files are committed together at the end of Task 8.

---

## Task 8: workflow pack — instance + validate algorithm

**Files:** Create `internal/behavior/workflow/instance.go`, `internal/behavior/workflow/instance_test.go`.

The instance is the runtime artifact. It holds the resolved state set, transition table, applies-to set, and exposes `Hooks()`, `ReservedKeys()`, and the `validate` function that fills `OnNodeWriteValidate`.

- [ ] **Step 1: Write the failing validator tests (`instance_test.go`)**

```go
package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/behavior"
	"github.com/germanamz/tusk/internal/node"
)

func newTestInstance(test *testing.T) *instance {
	test.Helper()

	cfg := workflowConfig{
		AppliesTo:      []string{"ticket"},
		StatusProperty: "status",
		States: []stateDecl{
			{Name: "pending", Initial: true},
			{Name: "active"},
			{Name: "completed", Terminal: true, Done: true},
		},
		Transitions: []transitionDecl{
			{From: "pending", To: "active"},
			{From: "active", To: "completed"},
			{From: "active", To: "pending"},
		},
	}

	cfg.normalize()

	if validateErr := cfg.validate(); validateErr != nil {
		test.Fatalf("config validate: %v", validateErr)
	}

	return newInstance("tickets", cfg)
}

func makeNode(typ string, props map[string]any) *node.Node {
	return &node.Node{Type: typ, Properties: props}
}

func TestValidate_TypeOutsideAppliesToReturnsNil(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	err := inst.validate(ctx, nil, makeNode("note", nil))

	if err != nil {
		test.Errorf("validate: type outside applies-to should be no-op, got %v", err)
	}
}

func TestValidate_BothSidesEmptyReturnsNil(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	err := inst.validate(ctx, makeNode("ticket", map[string]any{}), makeNode("ticket", map[string]any{}))

	if err != nil {
		test.Errorf("validate: both sides empty should be no-op, got %v", err)
	}
}

func TestValidate_SetFromEmptyMustBeInitial(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	err := inst.validate(ctx, nil, makeNode("ticket", map[string]any{"status": "active"}))

	var workflowErr *Error

	if !errors.As(err, &workflowErr) || workflowErr.Code != ErrNonInitialOnCreate {
		test.Errorf("validate: expected non-initial-on-create, got %v", err)
	}
}

func TestValidate_SetFromEmptyToInitialAccepted(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	err := inst.validate(ctx, nil, makeNode("ticket", map[string]any{"status": "pending"}))

	if err != nil {
		test.Errorf("validate: pending is initial, expected nil, got %v", err)
	}
}

func TestValidate_SetFromEmptyToUnknownState(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	err := inst.validate(ctx, nil, makeNode("ticket", map[string]any{"status": "donee"}))

	var workflowErr *Error

	if !errors.As(err, &workflowErr) || workflowErr.Code != ErrUnknownTargetState {
		test.Errorf("validate: expected unknown-target-state, got %v", err)
	}
}

func TestValidate_UnsetRejected(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	before := makeNode("ticket", map[string]any{"status": "active"})
	after := makeNode("ticket", map[string]any{})

	err := inst.validate(ctx, before, after)

	var workflowErr *Error

	if !errors.As(err, &workflowErr) || workflowErr.Code != ErrCannotUnsetStatus {
		test.Errorf("validate: expected cannot-unset-status, got %v", err)
	}
}

func TestValidate_OrphanRecoveryToDeclared(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	before := makeNode("ticket", map[string]any{"status": "blocked"})
	after := makeNode("ticket", map[string]any{"status": "active"})

	err := inst.validate(ctx, before, after)

	var recovered *RecoveredError

	if !errors.As(err, &recovered) {
		test.Errorf("validate: expected RecoveredError, got %v", err)

		return
	}

	if recovered.From != "blocked" || recovered.To != "active" || recovered.Property != "status" {
		test.Errorf("RecoveredError fields = %+v", recovered)
	}
}

func TestValidate_OrphanToUnknownTargetIsHardError(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	before := makeNode("ticket", map[string]any{"status": "blocked"})
	after := makeNode("ticket", map[string]any{"status": "alsoBogus"})

	err := inst.validate(ctx, before, after)

	var workflowErr *Error

	if !errors.As(err, &workflowErr) || workflowErr.Code != ErrUnknownTargetState {
		test.Errorf("validate: expected unknown-target-state, got %v", err)
	}
}

func TestValidate_NoOpSelfTransitionAllowed(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	before := makeNode("ticket", map[string]any{"status": "active"})
	after := makeNode("ticket", map[string]any{"status": "active"})

	if err := inst.validate(ctx, before, after); err != nil {
		test.Errorf("validate: self-transition should be allowed, got %v", err)
	}
}

func TestValidate_LegalTransition(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	before := makeNode("ticket", map[string]any{"status": "active"})
	after := makeNode("ticket", map[string]any{"status": "completed"})

	if err := inst.validate(ctx, before, after); err != nil {
		test.Errorf("validate: legal transition rejected: %v", err)
	}
}

func TestValidate_IllegalTransition(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	before := makeNode("ticket", map[string]any{"status": "pending"})
	after := makeNode("ticket", map[string]any{"status": "completed"})

	err := inst.validate(ctx, before, after)

	var workflowErr *Error

	if !errors.As(err, &workflowErr) || workflowErr.Code != ErrIllegalTransition {
		test.Errorf("validate: expected illegal-transition, got %v", err)
	}

	// ValidTargets should list "active" (the only legal next-state from "pending").
	if !strings.Contains(strings.Join(workflowErr.ValidTargets, ","), "active") {
		test.Errorf("ValidTargets = %v, want to include 'active'", workflowErr.ValidTargets)
	}
}

func TestValidate_NormalUnknownTarget(test *testing.T) {
	inst := newTestInstance(test)
	ctx := behavior.HookContext{PackKind: "workflow", PackInstance: "tickets"}

	before := makeNode("ticket", map[string]any{"status": "active"})
	after := makeNode("ticket", map[string]any{"status": "donee"})

	err := inst.validate(ctx, before, after)

	var workflowErr *Error

	if !errors.As(err, &workflowErr) || workflowErr.Code != ErrUnknownTargetState {
		test.Errorf("validate: expected unknown-target-state, got %v", err)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/behavior/workflow/... -run TestValidate
```

Expected: FAIL — `newInstance`, `instance.validate` undefined.

- [ ] **Step 3: Write `internal/behavior/workflow/instance.go`**

```go
package workflow

import (
	"sort"

	"github.com/germanamz/tusk/internal/behavior"
	"github.com/germanamz/tusk/internal/node"
)

// instance is the runtime form of one workflow configuration.
type instance struct {
	name           string
	appliesTo      map[string]struct{}
	statusProperty string
	states         map[string]roleSet
	transitions    map[transitionDecl]struct{}
	hasInitial     bool
	initialNames   []string
}

type roleSet struct {
	initial  bool
	start    bool
	terminal bool
	done     bool
}

func newInstance(name string, cfg workflowConfig) *instance {
	inst := &instance{
		name:           name,
		appliesTo:      map[string]struct{}{},
		statusProperty: cfg.StatusProperty,
		states:         map[string]roleSet{},
		transitions:    map[transitionDecl]struct{}{},
	}

	for _, typeName := range cfg.AppliesTo {
		inst.appliesTo[typeName] = struct{}{}
	}

	for _, state := range cfg.States {
		inst.states[state.Name] = roleSet{
			initial:  state.Initial,
			start:    state.Start,
			terminal: state.Terminal,
			done:     state.Done,
		}

		if state.Initial {
			inst.hasInitial = true
			inst.initialNames = append(inst.initialNames, state.Name)
		}
	}

	for _, trans := range cfg.Transitions {
		inst.transitions[trans] = struct{}{}
	}

	return inst
}

// Name satisfies behavior.Instance.
func (inst *instance) Name() string { return inst.name }

// Kind satisfies behavior.Instance.
func (inst *instance) Kind() string { return "workflow" }

// Hooks satisfies behavior.Instance — fills only OnNodeWriteValidate.
func (inst *instance) Hooks() behavior.Hooks {
	return behavior.Hooks{
		OnNodeWriteValidate: inst.validate,
	}
}

// ReservedKeys satisfies behavior.Instance — one key per type in
// applies-to, all reserving the configured status-property.
func (inst *instance) ReservedKeys() []behavior.ReservedKey {
	keys := make([]behavior.ReservedKey, 0, len(inst.appliesTo))

	for typeName := range inst.appliesTo {
		keys = append(keys, behavior.ReservedKey{NodeType: typeName, Property: inst.statusProperty})
	}

	sort.Slice(keys, func(a, b int) bool { return keys[a].NodeType < keys[b].NodeType })

	return keys
}

// validate is the OnNodeWriteValidate handler. Implements the algorithm
// in spec §5.2.
func (inst *instance) validate(ctx behavior.HookContext, before, after *node.Node) error {
	if after == nil {
		return nil
	}

	if _, governs := inst.appliesTo[after.Type]; !governs {
		return nil
	}

	beforeStatus := readStatus(before, inst.statusProperty)
	afterStatus := readStatus(after, inst.statusProperty)

	if beforeStatus == "" && afterStatus == "" {
		return nil
	}

	if beforeStatus == "" {
		// Setting status for the first time.
		if !inst.isKnownState(afterStatus) {
			return &Error{
				Code:         ErrUnknownTargetState,
				Property:     inst.statusProperty,
				From:         "",
				To:           afterStatus,
				KnownStates:  inst.knownStateNames(),
				PackInstance: inst.name,
			}
		}

		if inst.hasInitial && !inst.states[afterStatus].initial {
			return &Error{
				Code:         ErrNonInitialOnCreate,
				Property:     inst.statusProperty,
				From:         "",
				To:           afterStatus,
				KnownStates:  inst.initialNames,
				PackInstance: inst.name,
			}
		}

		return nil
	}

	if afterStatus == "" {
		// Unsetting a managed status.
		return &Error{
			Code:         ErrCannotUnsetStatus,
			Property:     inst.statusProperty,
			From:         beforeStatus,
			To:           "",
			PackInstance: inst.name,
		}
	}

	if !inst.isKnownState(beforeStatus) {
		// Orphan-state recovery.
		if !inst.isKnownState(afterStatus) {
			return &Error{
				Code:         ErrUnknownTargetState,
				Property:     inst.statusProperty,
				From:         beforeStatus,
				To:           afterStatus,
				KnownStates:  inst.knownStateNames(),
				PackInstance: inst.name,
			}
		}

		return &RecoveredError{
			Property:     inst.statusProperty,
			From:         beforeStatus,
			To:           afterStatus,
			PackInstance: inst.name,
		}
	}

	if beforeStatus == afterStatus {
		return nil
	}

	if !inst.isKnownState(afterStatus) {
		return &Error{
			Code:         ErrUnknownTargetState,
			Property:     inst.statusProperty,
			From:         beforeStatus,
			To:           afterStatus,
			KnownStates:  inst.knownStateNames(),
			PackInstance: inst.name,
		}
	}

	if _, ok := inst.transitions[transitionDecl{From: beforeStatus, To: afterStatus}]; !ok {
		return &Error{
			Code:         ErrIllegalTransition,
			Property:     inst.statusProperty,
			From:         beforeStatus,
			To:           afterStatus,
			ValidTargets: inst.validTargetsFrom(beforeStatus),
			PackInstance: inst.name,
		}
	}

	return nil
}

func (inst *instance) isKnownState(name string) bool {
	_, ok := inst.states[name]
	return ok
}

func (inst *instance) knownStateNames() []string {
	names := make([]string, 0, len(inst.states))

	for name := range inst.states {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

func (inst *instance) validTargetsFrom(from string) []string {
	var targets []string

	for trans := range inst.transitions {
		if trans.From == from {
			targets = append(targets, trans.To)
		}
	}

	sort.Strings(targets)

	return targets
}

func readStatus(node *node.Node, property string) string {
	if node == nil || node.Properties == nil {
		return ""
	}

	value, found := node.Properties[property]

	if !found {
		return ""
	}

	stringValue, ok := value.(string)

	if !ok {
		return ""
	}

	return stringValue
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/behavior/workflow/... -v
```

Expected: every test PASS — config tests + Kind tests + validator tests.

- [ ] **Step 5: Commit Tasks 7 and 8 together**

```bash
git add internal/behavior/workflow/
git commit -m "feat(workflow): config decoder, Kind, instance + validator algorithm"
```

---

## Task 9: Manifest loader captures TOML meta + structural validation

**Files:** Modify `internal/manifest/loader.go`, `internal/manifest/loader_test.go`.

The manifest's `Behaviors` field is in place from Task 5. The loader still needs to (a) capture the `*toml.MetaData` so pack Kinds can `PrimitiveDecode` their subtables, and (b) run structural validation — kind name non-empty, instance name non-empty.

- [ ] **Step 1: Append failing tests to `internal/manifest/loader_test.go`**

```go
func TestLoad_CapturesBehaviorsTomlMeta(test *testing.T) {
	dir := test.TempDir()
	manifestPath := filepath.Join(dir, "tusk.toml")

	content := `
[workspace]
name = "test"

[behaviors.workflow.tickets]
applies-to = ["ticket"]
states = [
  { name = "open", initial = true },
]
`
	if writeErr := os.WriteFile(manifestPath, []byte(content), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if loaded.Meta == nil {
		test.Errorf("Meta: nil; expected captured TOML meta data")
	}

	subtable, found := loaded.Behaviors["workflow"]["tickets"]

	if !found {
		test.Fatalf("Behaviors[workflow][tickets]: missing")
	}

	// Sanity: PrimitiveDecode the subtable into a partial struct.
	var partial struct {
		AppliesTo []string `toml:"applies-to"`
	}

	if decodeErr := loaded.Meta.PrimitiveDecode(subtable, &partial); decodeErr != nil {
		test.Fatalf("PrimitiveDecode: %v", decodeErr)
	}

	if len(partial.AppliesTo) != 1 || partial.AppliesTo[0] != "ticket" {
		test.Errorf("AppliesTo = %v, want [ticket]", partial.AppliesTo)
	}
}

func TestLoad_RejectsBehaviorsWithEmptyInstanceName(test *testing.T) {
	dir := test.TempDir()
	manifestPath := filepath.Join(dir, "tusk.toml")

	// Empty TOML key isn't expressible directly; we test the validation by
	// constructing a Manifest in-memory through the validate function.
	loaded := &manifest.Manifest{
		Behaviors: map[string]map[string]toml.Primitive{
			"workflow": {"": toml.Primitive{}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil {
		test.Errorf("Validate: expected empty-instance-name rejection")
	}

	_ = manifestPath
}

func TestLoad_RejectsBehaviorsWithEmptyKindName(test *testing.T) {
	loaded := &manifest.Manifest{
		Behaviors: map[string]map[string]toml.Primitive{
			"": {"any": toml.Primitive{}},
		},
	}

	if validateErr := manifest.Validate(loaded); validateErr == nil {
		test.Errorf("Validate: expected empty-kind-name rejection")
	}
}
```

Add `"github.com/BurntSushi/toml"` to the test file imports if missing.

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/manifest/... -run "TestLoad_CapturesBehaviorsTomlMeta|TestLoad_RejectsBehaviorsWithEmptyInstanceName|TestLoad_RejectsBehaviorsWithEmptyKindName"
```

Expected: FAIL — `loaded.Meta` is nil; `manifest.Validate` not exported.

- [ ] **Step 3: Modify `internal/manifest/loader.go`**

Update `Load` to capture the `*toml.MetaData` and call structural behaviors validation. Replace the body of `Load` (and add a new `Validate` exported function plus `validateBehaviors` helper):

```go
// Load reads and decodes a tusk.toml at manifestPath, validating its shape.
func Load(manifestPath string) (*Manifest, error) {
	body, readErr := os.ReadFile(manifestPath)

	if readErr != nil {
		return nil, fmt.Errorf("manifest: read %s: %w", manifestPath, readErr)
	}

	loaded := &Manifest{}

	meta, decodeErr := toml.Decode(string(body), loaded)

	if decodeErr != nil {
		return nil, fmt.Errorf("manifest: decode %s: %w", manifestPath, decodeErr)
	}

	loaded.Meta = &meta

	if validateErr := Validate(loaded); validateErr != nil {
		return nil, validateErr
	}

	return loaded, nil
}

// Validate is exported so tests can validate hand-constructed manifests.
// Production code should use Load.
func Validate(loaded *Manifest) error {
	if validateErr := validate(loaded); validateErr != nil {
		return validateErr
	}

	return validateBehaviors(loaded)
}

// validateBehaviors enforces the structural rules that apply to every
// behavior pack regardless of kind: non-empty kind name, non-empty
// instance name. Kind-specific schema lives in each pack's NewInstance.
func validateBehaviors(loaded *Manifest) error {
	for kindName, perInstance := range loaded.Behaviors {
		if kindName == "" {
			return fmt.Errorf("manifest: behaviors: empty kind name")
		}

		for instanceName := range perInstance {
			if instanceName == "" {
				return fmt.Errorf("manifest: behaviors.%s: empty instance name", kindName)
			}
		}
	}

	return nil
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/manifest/... -v
```

Expected: every test PASS, including the new three.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/loader.go internal/manifest/loader_test.go
git commit -m "feat(manifest): capture TOML meta + structural validation for behaviors"
```

---

## Task 10: Index — `workflow_drift` table + `WorkflowDriftRepo`

**Files:** Modify `internal/index/index.go`, `internal/index/index_test.go`. Create `internal/index/workflow_drift_repo.go`, `internal/index/workflow_drift_repo_test.go`.

- [ ] **Step 1: Append failing schema test to `internal/index/index_test.go`**

```go
func TestOpen_CreatesWorkflowDriftTable(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	tables, listErr := store.ListTables()

	if listErr != nil {
		test.Fatalf("ListTables: %v", listErr)
	}

	if !contains(tables, "workflow_drift") {
		test.Errorf("missing table %q in %v", "workflow_drift", tables)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/index/... -run TestOpen_CreatesWorkflowDriftTable
```

Expected: FAIL — schema lacks the `workflow_drift` table.

- [ ] **Step 3: Append the table to the `schema` const in `internal/index/index.go`**

Insert just before the closing backtick:

```sql
CREATE TABLE IF NOT EXISTS workflow_drift (
	node_id          TEXT NOT NULL,
	pack_instance    TEXT NOT NULL,
	pack_kind        TEXT NOT NULL,
	observed_status  TEXT NOT NULL,
	property         TEXT NOT NULL,
	observed_at      INTEGER NOT NULL,
	PRIMARY KEY (node_id, pack_instance, observed_status)
);

CREATE INDEX IF NOT EXISTS workflow_drift_node_idx ON workflow_drift(node_id);
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/index/... -run TestOpen_CreatesWorkflowDriftTable
```

Expected: PASS.

- [ ] **Step 5: Write the failing repo tests (`workflow_drift_repo_test.go`)**

```go
package index_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestWorkflowDriftRepo_AppendThenList(test *testing.T) {
	store, closer := newTestIndex(test)
	defer closer()

	repo := index.NewWorkflowDriftRepo(store)

	row := index.WorkflowDriftRow{
		NodeID:         "tickets/foo",
		PackInstance:   "tickets",
		PackKind:       "workflow",
		ObservedStatus: "blocked",
		Property:       "status",
		ObservedAt:     1700_000_000,
	}

	if appendErr := repo.Append(row); appendErr != nil {
		test.Fatalf("Append: %v", appendErr)
	}

	rows, listErr := repo.ListAll()

	if listErr != nil {
		test.Fatalf("ListAll: %v", listErr)
	}

	if len(rows) != 1 || rows[0].NodeID != "tickets/foo" || rows[0].ObservedStatus != "blocked" {
		test.Errorf("ListAll = %+v", rows)
	}
}

func TestWorkflowDriftRepo_AppendIdempotentOnPK(test *testing.T) {
	store, closer := newTestIndex(test)
	defer closer()

	repo := index.NewWorkflowDriftRepo(store)

	row := index.WorkflowDriftRow{
		NodeID: "tickets/foo", PackInstance: "tickets", PackKind: "workflow",
		ObservedStatus: "blocked", Property: "status", ObservedAt: 100,
	}

	for _, observedAt := range []int64{100, 200, 300} {
		row.ObservedAt = observedAt

		if appendErr := repo.Append(row); appendErr != nil {
			test.Fatalf("Append: %v", appendErr)
		}
	}

	rows, _ := repo.ListAll()

	if len(rows) != 1 {
		test.Errorf("ListAll: want 1 row (PK collapsed), got %d", len(rows))
	}

	if rows[0].ObservedAt != 300 {
		test.Errorf("ObservedAt = %d, want most recent (300)", rows[0].ObservedAt)
	}
}

func TestWorkflowDriftRepo_ClearForNode(test *testing.T) {
	store, closer := newTestIndex(test)
	defer closer()

	repo := index.NewWorkflowDriftRepo(store)

	rows := []index.WorkflowDriftRow{
		{NodeID: "tickets/foo", PackInstance: "tickets", PackKind: "workflow", ObservedStatus: "x", Property: "status", ObservedAt: 1},
		{NodeID: "tickets/foo", PackInstance: "tickets", PackKind: "workflow", ObservedStatus: "y", Property: "status", ObservedAt: 2},
		{NodeID: "tickets/bar", PackInstance: "tickets", PackKind: "workflow", ObservedStatus: "z", Property: "status", ObservedAt: 3},
	}

	for _, row := range rows {
		if appendErr := repo.Append(row); appendErr != nil {
			test.Fatalf("Append: %v", appendErr)
		}
	}

	if clearErr := repo.ClearForNode("tickets/foo"); clearErr != nil {
		test.Fatalf("ClearForNode: %v", clearErr)
	}

	remaining, _ := repo.ListAll()

	if len(remaining) != 1 || remaining[0].NodeID != "tickets/bar" {
		test.Errorf("after Clear: remaining = %+v, want only tickets/bar", remaining)
	}
}

func TestWorkflowDriftRepo_CountAll(test *testing.T) {
	store, closer := newTestIndex(test)
	defer closer()

	repo := index.NewWorkflowDriftRepo(store)

	rows := []index.WorkflowDriftRow{
		{NodeID: "a", PackInstance: "p", PackKind: "workflow", ObservedStatus: "x", Property: "status", ObservedAt: 1},
		{NodeID: "b", PackInstance: "p", PackKind: "workflow", ObservedStatus: "y", Property: "status", ObservedAt: 2},
	}

	for _, row := range rows {
		_ = repo.Append(row)
	}

	count, countErr := repo.CountAll()

	if countErr != nil {
		test.Fatalf("CountAll: %v", countErr)
	}

	if count != 2 {
		test.Errorf("CountAll = %d, want 2", count)
	}
}

func newTestIndex(test *testing.T) (*index.Index, func()) {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return store, func() { store.Close() }
}
```

- [ ] **Step 6: Run, verify fail**

```bash
go test ./internal/index/... -run TestWorkflowDriftRepo
```

Expected: FAIL — `index.NewWorkflowDriftRepo`, `index.WorkflowDriftRow` undefined.

- [ ] **Step 7: Write `internal/index/workflow_drift_repo.go`**

```go
package index

import (
	"database/sql"
	"fmt"
)

// WorkflowDriftRow is one observed off-schema status. The primary key
// (node_id, pack_instance, observed_status) collapses repeated
// observations of the same drift into a single row.
type WorkflowDriftRow struct {
	NodeID         string
	PackInstance   string
	PackKind       string
	ObservedStatus string
	Property       string
	ObservedAt     int64
}

// WorkflowDriftRepo persists workflow-validation drift events for
// `tusk doctor` to surface.
type WorkflowDriftRepo struct {
	db *sql.DB
}

// NewWorkflowDriftRepo constructs a repo backed by idx.
func NewWorkflowDriftRepo(idx *Index) *WorkflowDriftRepo {
	return &WorkflowDriftRepo{db: idx.DB()}
}

// Append upserts a drift row. Idempotent on the primary key; the latest
// observed_at + property + pack_kind win.
func (repo *WorkflowDriftRepo) Append(row WorkflowDriftRow) error {
	_, execErr := repo.db.Exec(`
		INSERT INTO workflow_drift (node_id, pack_instance, pack_kind, observed_status, property, observed_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, pack_instance, observed_status) DO UPDATE SET
			pack_kind = excluded.pack_kind,
			property = excluded.property,
			observed_at = excluded.observed_at
	`, row.NodeID, row.PackInstance, row.PackKind, row.ObservedStatus, row.Property, row.ObservedAt)

	if execErr != nil {
		return fmt.Errorf("workflowDriftRepo: append: %w", execErr)
	}

	return nil
}

// ListAll returns every drift row, sorted by (node_id, pack_instance,
// observed_status) for stable rendering.
func (repo *WorkflowDriftRepo) ListAll() ([]WorkflowDriftRow, error) {
	rows, queryErr := repo.db.Query(`
		SELECT node_id, pack_instance, pack_kind, observed_status, property, observed_at
		FROM workflow_drift
		ORDER BY node_id, pack_instance, observed_status
	`)

	if queryErr != nil {
		return nil, fmt.Errorf("workflowDriftRepo: list: %w", queryErr)
	}

	defer rows.Close()

	var results []WorkflowDriftRow

	for rows.Next() {
		var row WorkflowDriftRow

		if scanErr := rows.Scan(&row.NodeID, &row.PackInstance, &row.PackKind, &row.ObservedStatus, &row.Property, &row.ObservedAt); scanErr != nil {
			return nil, fmt.Errorf("workflowDriftRepo: scan: %w", scanErr)
		}

		results = append(results, row)
	}

	return results, rows.Err()
}

// ClearForNode removes every drift row for nodeID. Called by Modify and
// reindex on a clean validation pass.
func (repo *WorkflowDriftRepo) ClearForNode(nodeID string) error {
	_, execErr := repo.db.Exec(`DELETE FROM workflow_drift WHERE node_id = ?`, nodeID)

	if execErr != nil {
		return fmt.Errorf("workflowDriftRepo: clear %s: %w", nodeID, execErr)
	}

	return nil
}

// CountAll returns the total number of drift rows. Used by reindex's
// summary line.
func (repo *WorkflowDriftRepo) CountAll() (int, error) {
	var count int

	scanErr := repo.db.QueryRow(`SELECT COUNT(*) FROM workflow_drift`).Scan(&count)

	if scanErr != nil {
		return 0, fmt.Errorf("workflowDriftRepo: count: %w", scanErr)
	}

	return count, nil
}
```

- [ ] **Step 8: Run, verify pass**

```bash
go test ./internal/index/... -run "TestWorkflowDriftRepo|TestOpen_CreatesWorkflowDriftTable" -v
```

Expected: 5 PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/index/index.go internal/index/index_test.go internal/index/workflow_drift_repo.go internal/index/workflow_drift_repo_test.go
git commit -m "feat(index): workflow_drift table + WorkflowDriftRepo"
```

---

## Task 11: NodeService — `NewServiceWithBehaviors` constructor + warnings/drift fields

**Files:** Modify `internal/node/service.go`.

The Service struct grows three optional fields: `behaviors *behavior.Engine`, `drift *index.WorkflowDriftRepo`, `warnings io.Writer`. The new constructor `NewServiceWithBehaviors` is the production entry point starting in Plan 7. The pre-Plan-7 constructors stay in place — when called, they leave the new fields nil (which the dispatch sites treat as "no-op").

- [ ] **Step 1: Add fields and constructor to `internal/node/service.go`**

Modify the struct definition:

```go
// Service orchestrates filesystem and index for nodes.
type Service struct {
	root       string
	repo       *index.NodeRepo
	edges      *index.EdgeRepo
	edgeTypes  manifest.EdgeTypes
	embedQueue *index.EmbedQueueRepo

	behaviors *behavior.Engine          // optional; nil = no hook dispatch
	drift     *index.WorkflowDriftRepo  // optional; nil = no drift persistence
	warnings  io.Writer                 // optional; nil = io.Discard
}
```

Add `"io"` to imports. Add `"github.com/germanamz/tusk/internal/behavior"` to imports.

Add the new constructor below `NewServiceWithEmbedQueue`:

```go
// NewServiceWithBehaviors is the Plan 7 production constructor: like
// NewServiceWithEmbedQueue, but also wires the behavior engine, the
// drift log, and a warnings writer (defaults to io.Discard when nil).
func NewServiceWithBehaviors(
	workspaceRoot string,
	repo *index.NodeRepo,
	edges *index.EdgeRepo,
	edgeTypes manifest.EdgeTypes,
	embedQueue *index.EmbedQueueRepo,
	behaviors *behavior.Engine,
	drift *index.WorkflowDriftRepo,
	warnings io.Writer,
) *Service {
	if warnings == nil {
		warnings = io.Discard
	}

	return &Service{
		root:       workspaceRoot,
		repo:       repo,
		edges:      edges,
		edgeTypes:  edgeTypes,
		embedQueue: embedQueue,
		behaviors:  behaviors,
		drift:      drift,
		warnings:   warnings,
	}
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/node/...
```

Expected: success. Existing tests still pass because the pre-Plan-7 constructors are untouched and zero-value `behaviors` / `drift` / `warnings` cause the new dispatch sites (added in Tasks 12–13) to short-circuit harmlessly.

- [ ] **Step 3: Commit**

```bash
git add internal/node/service.go
git commit -m "feat(node): NewServiceWithBehaviors constructor + dispatch fields"
```

---

## Task 12: NodeService.Create — fire OnNodeWrite + OnEdgeAdd hooks

**Files:** Modify `internal/node/service.go`, `internal/node/service_test.go`.

`Create` fires the Validate phase before the file write and the After phase after the index commit.

- [ ] **Step 1: Append failing tests to `internal/node/service_test.go`**

```go
func TestCreate_HookValidatePhaseRejectsBeforeWrite(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	// Build a behavior engine with a Validator that always rejects.
	rejector := &fakeServicePack{
		name: "rejector",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				return errors.New("denied")
			},
		},
	}

	engine, _ := behavior.NewEngine([]behavior.Instance{rejector})

	service := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		engine,
		index.NewWorkflowDriftRepo(store),
		io.Discard,
	)

	_, createErr := service.Create(node.CreateInput{
		RelPath: "tickets/foo.md",
		Type:    "ticket",
	})

	if createErr == nil || !strings.Contains(createErr.Error(), "denied") {
		test.Errorf("Create: expected rejection, got %v", createErr)
	}

	// File must not exist (rejection happens before write).
	if _, statErr := os.Stat(filepath.Join(root, "tickets/foo.md")); !os.IsNotExist(statErr) {
		test.Errorf("file present after rejection; statErr = %v", statErr)
	}
}

func TestCreate_HookAfterPhaseFiresAfterCommit(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	var afterCalled int

	tracker := &fakeServicePack{
		name: "tracker",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteAfter: func(ctx behavior.HookContext, before, after *node.Node) error {
				afterCalled++
				return nil
			},
		},
	}

	engine, _ := behavior.NewEngine([]behavior.Instance{tracker})

	service := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		engine,
		index.NewWorkflowDriftRepo(store),
		io.Discard,
	)

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "tickets/foo.md",
		Type:    "ticket",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if afterCalled != 1 {
		test.Errorf("OnNodeWriteAfter: called %d times, want 1", afterCalled)
	}
}

// fakeServicePack mirrors the engine-package fake but lives in the
// node-package tests so we don't import test files across packages.
type fakeServicePack struct {
	name     string
	kind     string
	hooks    behavior.Hooks
	reserved []behavior.ReservedKey
}

func (pack *fakeServicePack) Name() string                         { return pack.name }
func (pack *fakeServicePack) Kind() string                         { return pack.kind }
func (pack *fakeServicePack) Hooks() behavior.Hooks                { return pack.hooks }
func (pack *fakeServicePack) ReservedKeys() []behavior.ReservedKey { return pack.reserved }
```

Add imports as needed: `errors`, `io`, `os`, `path/filepath`, `strings`, `github.com/germanamz/tusk/internal/behavior`, `github.com/germanamz/tusk/internal/index`, `github.com/germanamz/tusk/internal/manifest`, `github.com/germanamz/tusk/internal/node`.

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/node/... -run "TestCreate_Hook"
```

Expected: FAIL — Create doesn't fire hooks yet.

- [ ] **Step 3: Wire hook dispatch into `Service.Create`**

In `internal/node/service.go`, modify `Create`. Locate the existing block that runs after `validateErr := ValidateEdges(...)` and `cycleErr := service.detectCyclesForAcyclicEdges(...)` succeed but before `os.WriteFile(absPath, rendered, ...)`. Insert validate-phase hook dispatch there. After the existing `embedQueue.Enqueue` block at the end, insert after-phase hook dispatch.

The full `Create` becomes (changes marked with `// + Plan 7`):

```go
func (service *Service) Create(input CreateInput) (*Node, error) {
	absPath := filepath.Join(service.root, input.RelPath)

	if _, statErr := os.Stat(absPath); statErr == nil {
		return nil, ErrAlreadyExists
	}

	properties := map[string]any{"type": input.Type}

	if input.Title != "" {
		properties["title"] = input.Title
	}

	for key, value := range input.Properties {
		properties[key] = value
	}

	rendered, renderErr := renderMarkdown(properties, input.Body)

	if renderErr != nil {
		return nil, renderErr
	}

	parsed, parseErr := ParseFile(input.RelPath, rendered)

	if parseErr != nil {
		return nil, parseErr
	}

	if resolveErr := ResolveEdges(parsed, service.edgeTypes); resolveErr != nil {
		return nil, resolveErr
	}

	if _, hasReferences := service.edgeTypes["references"]; hasReferences {
		for _, target := range ExtractWikilinks(parsed.Body) {
			parsed.Edges["references"] = appendUnique(parsed.Edges["references"], target)
		}
	}

	if validateErr := ValidateEdges(parsed, service.edgeTypes, EdgeContext{
		ResolveTargetType: service.resolveTargetType,
	}); validateErr != nil {
		return nil, validateErr
	}

	if cycleErr := service.detectCyclesForAcyclicEdges(parsed); cycleErr != nil {
		return nil, cycleErr
	}

	// + Plan 7: validate-phase hook dispatch (NodeWrite then EdgeAdd per row).
	if service.behaviors != nil {
		if rejector, fireErr := service.behaviors.FireNodeWriteValidate(nil, parsed); fireErr != nil {
			return nil, fmt.Errorf("behavior %s rejected create: %w", rejector, fireErr)
		}

		for _, edgeRow := range flattenEdges(parsed) {
			if rejector, fireErr := service.behaviors.FireEdgeAddValidate(edgeRow); fireErr != nil {
				return nil, fmt.Errorf("behavior %s rejected edge add: %w", rejector, fireErr)
			}
		}
	}

	if mkErr := os.MkdirAll(filepath.Dir(absPath), 0o755); mkErr != nil {
		return nil, fmt.Errorf("node: mkdir %s: %w", filepath.Dir(absPath), mkErr)
	}

	if writeErr := os.WriteFile(absPath, rendered, 0o644); writeErr != nil {
		return nil, fmt.Errorf("node: write %s: %w", absPath, writeErr)
	}

	stat, statErr := os.Stat(absPath)

	if statErr != nil {
		return nil, fmt.Errorf("node: stat %s: %w", absPath, statErr)
	}

	checksum := sha256Hex(rendered)
	propertiesJSON, marshalErr := json.Marshal(parsed.Properties)

	if marshalErr != nil {
		return nil, fmt.Errorf("node: marshal properties: %w", marshalErr)
	}

	if upsertErr := service.repo.Upsert(index.NodeRow{
		ID:             parsed.ID,
		Type:           parsed.Type,
		Path:           parsed.Path,
		Title:          parsed.Title,
		PropertiesJSON: string(propertiesJSON),
		LastMtime:      stat.ModTime().UnixNano(),
		LastSize:       stat.Size(),
		LastChecksum:   checksum,
	}); upsertErr != nil {
		return nil, upsertErr
	}

	edgeRows := flattenEdges(parsed)

	if service.edges != nil {
		if upsertErr := service.edges.UpsertAll(parsed.ID, parsed.Path, edgeRows); upsertErr != nil {
			return nil, upsertErr
		}
	}

	if service.embedQueue != nil {
		if enqueueErr := service.embedQueue.Enqueue(parsed.ID); enqueueErr != nil {
			return nil, enqueueErr
		}
	}

	// + Plan 7: after-phase hook dispatch. Errors aggregated for telemetry;
	// do not affect control flow.
	if service.behaviors != nil {
		_ = service.behaviors.FireNodeWriteAfter(nil, parsed)

		for _, edgeRow := range edgeRows {
			_ = service.behaviors.FireEdgeAddAfter(edgeRow)
		}
	}

	return parsed, nil
}
```

(The pre-existing block was rearranged: `parseErr`/`ResolveEdges`/`ValidateEdges`/`detectCycles` happen before `MkdirAll` so the hook firing site sits in the right place. Compare against the existing service.go to keep the diff minimal.)

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/node/... -run "TestCreate_Hook" -v
```

Expected: 2 PASS.

- [ ] **Step 5: Verify the full node test suite still passes**

```bash
go test ./internal/node/... -v
```

Expected: every existing test still PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/node/service.go internal/node/service_test.go
git commit -m "feat(node): Create fires OnNodeWrite + OnEdgeAdd hooks"
```

---

## Task 13: NodeService.Modify — recovery-aware dispatch + edge diff + drift writes

**Files:** Modify `internal/node/service.go`, `internal/node/service_test.go`.

Modify is the workflow pack's primary path. It runs the recovery-aware Validate variant on `OnNodeWrite`; diffs edges; runs `OnEdgeRemoveValidate` for removals then `OnEdgeAddValidate` for additions; commits; runs After-phase reactors; and on `RecoveredEvent`s emits a stderr warning + a drift row.

- [ ] **Step 1: Append failing tests to `internal/node/service_test.go`**

```go
func TestModify_HookValidatePhaseRejects(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	// Pre-create a node without behaviors so the seed write succeeds.
	seed := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		nil, nil, io.Discard,
	)

	if _, createErr := seed.Create(node.CreateInput{
		RelPath: "tickets/foo.md",
		Type:    "ticket",
		Properties: map[string]any{"status": "active"},
	}); createErr != nil {
		test.Fatalf("seed Create: %v", createErr)
	}

	// Build a Modify-time service with a rejecting validator.
	rejector := &fakeServicePack{
		name: "rejector",
		kind: "fake",
		hooks: behavior.Hooks{
			OnNodeWriteValidate: func(ctx behavior.HookContext, before, after *node.Node) error {
				return errors.New("denied")
			},
		},
	}

	engine, _ := behavior.NewEngine([]behavior.Instance{rejector})

	service := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		engine,
		index.NewWorkflowDriftRepo(store),
		io.Discard,
	)

	_, modifyErr := service.Modify(node.ModifyInput{
		ID:       "tickets/foo",
		SetProps: map[string]any{"status": "completed"},
	})

	if modifyErr == nil || !strings.Contains(modifyErr.Error(), "denied") {
		test.Errorf("Modify: expected rejection, got %v", modifyErr)
	}
}

func TestModify_HookRecoveryWritesDriftAndWarns(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	seed := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		nil, nil, io.Discard,
	)

	if _, createErr := seed.Create(node.CreateInput{
		RelPath:    "tickets/foo.md",
		Type:       "ticket",
		Properties: map[string]any{"status": "blocked"},  // off-schema
	}); createErr != nil {
		test.Fatalf("seed Create: %v", createErr)
	}

	// Build a workflow instance that doesn't know about "blocked".
	cfg := workflowConfigForTest(test)

	driftRepo := index.NewWorkflowDriftRepo(store)
	engine, _ := behavior.NewEngine([]behavior.Instance{cfg.Instance})

	var warnings bytes.Buffer

	service := node.NewServiceWithBehaviors(
		root,
		index.NewNodeRepo(store),
		index.NewEdgeRepo(store),
		manifest.EdgeTypes{},
		index.NewEmbedQueueRepo(store),
		engine,
		driftRepo,
		&warnings,
	)

	if _, modifyErr := service.Modify(node.ModifyInput{
		ID:       "tickets/foo",
		SetProps: map[string]any{"status": "active"},
	}); modifyErr != nil {
		test.Fatalf("Modify (recovery): %v", modifyErr)
	}

	if !strings.Contains(warnings.String(), "blocked") {
		test.Errorf("warnings = %q, want mention of 'blocked'", warnings.String())
	}

	rows, _ := driftRepo.ListAll()

	if len(rows) != 1 || rows[0].ObservedStatus != "blocked" {
		test.Errorf("drift rows = %+v, want one row for 'blocked'", rows)
	}
}

func TestModify_HookCleanPassClearsDrift(test *testing.T) {
	root := test.TempDir()
	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))

	defer store.Close()

	driftRepo := index.NewWorkflowDriftRepo(store)

	// Seed: a drift row already present for the node we're about to modify.
	if appendErr := driftRepo.Append(index.WorkflowDriftRow{
		NodeID: "tickets/foo", PackInstance: "tickets", PackKind: "workflow",
		ObservedStatus: "stale", Property: "status", ObservedAt: 1,
	}); appendErr != nil {
		test.Fatalf("seed drift Append: %v", appendErr)
	}

	seed := node.NewServiceWithBehaviors(
		root, index.NewNodeRepo(store), index.NewEdgeRepo(store),
		manifest.EdgeTypes{}, index.NewEmbedQueueRepo(store),
		nil, nil, io.Discard,
	)

	if _, createErr := seed.Create(node.CreateInput{
		RelPath:    "tickets/foo.md",
		Type:       "ticket",
		Properties: map[string]any{"status": "pending"},
	}); createErr != nil {
		test.Fatalf("seed Create: %v", createErr)
	}

	cfg := workflowConfigForTest(test)
	engine, _ := behavior.NewEngine([]behavior.Instance{cfg.Instance})

	service := node.NewServiceWithBehaviors(
		root, index.NewNodeRepo(store), index.NewEdgeRepo(store),
		manifest.EdgeTypes{}, index.NewEmbedQueueRepo(store),
		engine, driftRepo, io.Discard,
	)

	if _, modifyErr := service.Modify(node.ModifyInput{
		ID:       "tickets/foo",
		SetProps: map[string]any{"status": "active"},  // legal: pending → active
	}); modifyErr != nil {
		test.Fatalf("Modify (clean): %v", modifyErr)
	}

	rows, _ := driftRepo.ListAll()

	if len(rows) != 0 {
		test.Errorf("drift after clean Modify = %+v, want empty (clean pass clears)", rows)
	}
}

// workflowConfigForTest returns a workflow Instance with the standard
// 3-state machine. Helper used by the Modify recovery + clean tests.
type workflowConfigBundle struct {
	Instance behavior.Instance
}

func workflowConfigForTest(test *testing.T) workflowConfigBundle {
	test.Helper()

	const sample = `
[behaviors.workflow.tickets]
applies-to = ["ticket"]
states = [
  { name = "pending", initial = true },
  { name = "active" },
  { name = "completed", terminal = true, done = true },
]
transitions = [
  { from = "pending", to = "active" },
  { from = "active", to = "completed" },
]
`

	var decoded struct {
		Behaviors map[string]map[string]toml.Primitive `toml:"behaviors"`
	}

	meta, decodeErr := toml.Decode(sample, &decoded)

	if decodeErr != nil {
		test.Fatalf("decode: %v", decodeErr)
	}

	primitive := decoded.Behaviors["workflow"]["tickets"]

	instance, newErr := workflow.Kind{}.NewInstance("tickets", primitive, &meta)

	if newErr != nil {
		test.Fatalf("workflow.NewInstance: %v", newErr)
	}

	return workflowConfigBundle{Instance: instance}
}
```

Add imports: `bytes`, `github.com/BurntSushi/toml`, `github.com/germanamz/tusk/internal/behavior/workflow`.

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/node/... -run "TestModify_Hook"
```

Expected: FAIL — Modify doesn't dispatch hooks yet.

- [ ] **Step 3: Wire dispatch into `Service.Modify`**

Modify `Service.Modify` in `internal/node/service.go`. Insert validate-phase hook dispatch after `detectCyclesForAcyclicEdges` and before the `atomicWrite`; insert after-phase + recovery + drift writes after `embedQueue.Enqueue`.

```go
func (service *Service) Modify(input ModifyInput) (*Node, error) {
	row, getErr := service.repo.Get(input.ID)

	if getErr != nil {
		return nil, getErr
	}

	absPath := filepath.Join(service.root, row.Path)

	original, readErr := os.ReadFile(absPath)

	if readErr != nil {
		return nil, fmt.Errorf("node: read %s: %w", row.Path, readErr)
	}

	beforeNode, parseBeforeErr := ParseFile(row.Path, original)

	if parseBeforeErr != nil {
		return nil, parseBeforeErr
	}

	// Resolve edges on the before-node so the diff against the after-node
	// is well-defined.
	if resolveErr := ResolveEdges(beforeNode, service.edgeTypes); resolveErr != nil {
		return nil, resolveErr
	}

	// Apply Set/Unset/Body to produce after-node.
	parsed := beforeNode.Clone()  // shallow clone; Properties map is copied below

	for _, key := range input.UnsetKeys {
		if key == "type" {
			return nil, fmt.Errorf("node: cannot unset reserved key %q", key)
		}

		delete(parsed.Properties, key)
	}

	for key, value := range input.SetProps {
		if key == "type" && value != parsed.Type {
			return nil, fmt.Errorf("node: cannot change type via Modify (current=%q, requested=%v)", parsed.Type, value)
		}

		parsed.Properties[key] = value
	}

	body := parsed.Body

	if input.Body != nil {
		body = *input.Body
		parsed.Body = body
	}

	rendered, renderErr := renderMarkdown(parsed.Properties, body)

	if renderErr != nil {
		return nil, renderErr
	}

	reparsed, reparseErr := ParseFile(row.Path, rendered)

	if reparseErr != nil {
		return nil, reparseErr
	}

	if resolveErr := ResolveEdges(reparsed, service.edgeTypes); resolveErr != nil {
		return nil, resolveErr
	}

	if validateErr := ValidateEdges(reparsed, service.edgeTypes, EdgeContext{
		ResolveTargetType: service.resolveTargetType,
	}); validateErr != nil {
		return nil, validateErr
	}

	if cycleErr := service.detectCyclesForAcyclicEdges(reparsed); cycleErr != nil {
		return nil, cycleErr
	}

	// + Plan 7: recovery-aware validate phase + edge diff hooks.
	var fireResult behavior.FireResult

	if service.behaviors != nil {
		result, fireErr := service.behaviors.FireNodeWriteValidateWithRecovery(beforeNode, reparsed)

		if fireErr != nil {
			return nil, fmt.Errorf("behavior %s rejected modify: %w", result.Rejector, fireErr)
		}

		fireResult = result

		removed, added := diffEdgeSets(beforeNode, reparsed)

		for _, edgeRow := range removed {
			if rejector, edgeFireErr := service.behaviors.FireEdgeRemoveValidate(edgeRow); edgeFireErr != nil {
				return nil, fmt.Errorf("behavior %s rejected edge remove: %w", rejector, edgeFireErr)
			}
		}

		for _, edgeRow := range added {
			if rejector, edgeFireErr := service.behaviors.FireEdgeAddValidate(edgeRow); edgeFireErr != nil {
				return nil, fmt.Errorf("behavior %s rejected edge add: %w", rejector, edgeFireErr)
			}
		}
	}

	if writeErr := atomicWrite(absPath, rendered); writeErr != nil {
		return nil, fmt.Errorf("node: write %s: %w", absPath, writeErr)
	}

	stat, statErr := os.Stat(absPath)

	if statErr != nil {
		return nil, fmt.Errorf("node: stat %s: %w", absPath, statErr)
	}

	checksum := sha256Hex(rendered)
	propertiesJSON, marshalErr := json.Marshal(reparsed.Properties)

	if marshalErr != nil {
		return nil, fmt.Errorf("node: marshal properties: %w", marshalErr)
	}

	if upsertErr := service.repo.Upsert(index.NodeRow{
		ID:             reparsed.ID,
		Type:           reparsed.Type,
		Path:           reparsed.Path,
		Title:          reparsed.Title,
		PropertiesJSON: string(propertiesJSON),
		LastMtime:      stat.ModTime().UnixNano(),
		LastSize:       stat.Size(),
		LastChecksum:   checksum,
	}); upsertErr != nil {
		return nil, upsertErr
	}

	if service.edges != nil {
		if upsertErr := service.edges.UpsertAll(reparsed.ID, reparsed.Path, flattenEdges(reparsed)); upsertErr != nil {
			return nil, upsertErr
		}
	}

	if service.embedQueue != nil {
		if enqueueErr := service.embedQueue.Enqueue(reparsed.ID); enqueueErr != nil {
			return nil, enqueueErr
		}
	}

	// + Plan 7: after-phase + recovery surface.
	if service.behaviors != nil {
		_ = service.behaviors.FireNodeWriteAfter(beforeNode, reparsed)

		removed, added := diffEdgeSets(beforeNode, reparsed)

		for _, edgeRow := range removed {
			_ = service.behaviors.FireEdgeRemoveAfter(edgeRow)
		}

		for _, edgeRow := range added {
			_ = service.behaviors.FireEdgeAddAfter(edgeRow)
		}

		// Surface recovered events: stderr warning + drift row.
		now := time.Now().UnixNano()

		for _, recovered := range fireResult.Recovered {
			fmt.Fprintf(service.warnings,
				"warning: workflow %q recovered from unknown status %q → %q on %s; transition not validated\n",
				recovered.PackInstance, recovered.From, recovered.To, reparsed.ID)

			if service.drift != nil {
				_ = service.drift.Append(index.WorkflowDriftRow{
					NodeID:         reparsed.ID,
					PackInstance:   recovered.PackInstance,
					PackKind:       recovered.PackKind,
					ObservedStatus: recovered.From,
					Property:       recovered.Property,
					ObservedAt:     now,
				})
			}
		}

		// Clean pass: no rejection, no recovery — clear any prior drift for this node.
		if len(fireResult.Recovered) == 0 && service.drift != nil {
			_ = service.drift.ClearForNode(reparsed.ID)
		}
	}

	return reparsed, nil
}
```

- [ ] **Step 4: Add a `Clone` helper to `internal/node/node.go` (or wherever `Node` is defined)**

```go
// Clone returns a shallow copy of the Node with the Properties and Edges
// maps deep-copied so callers can mutate the clone without affecting the
// original.
func (node *Node) Clone() *Node {
	if node == nil {
		return nil
	}

	cloned := *node

	if node.Properties != nil {
		cloned.Properties = make(map[string]any, len(node.Properties))

		for key, value := range node.Properties {
			cloned.Properties[key] = value
		}
	}

	if node.Edges != nil {
		cloned.Edges = make(map[string][]string, len(node.Edges))

		for key, targets := range node.Edges {
			copied := make([]string, len(targets))
			copy(copied, targets)
			cloned.Edges[key] = copied
		}
	}

	return &cloned
}
```

- [ ] **Step 5: Add `diffEdgeSets` helper to `internal/node/service.go`**

```go
// diffEdgeSets compares before vs. after edge sets and returns the rows
// to fire EdgeRemove / EdgeAdd hooks for. A row identifies its edge by
// (Type, SourceID, TargetID, Ordinal); ordering matches flattenEdges.
func diffEdgeSets(before, after *Node) (removed, added []index.EdgeRow) {
	beforeRows := flattenEdges(before)
	afterRows := flattenEdges(after)

	type key struct {
		typeName string
		sourceID string
		targetID string
		ordinal  int
	}

	beforeSet := make(map[key]index.EdgeRow, len(beforeRows))

	for _, row := range beforeRows {
		beforeSet[key{row.Type, row.SourceID, row.TargetID, row.Ordinal}] = row
	}

	afterSet := make(map[key]index.EdgeRow, len(afterRows))

	for _, row := range afterRows {
		afterSet[key{row.Type, row.SourceID, row.TargetID, row.Ordinal}] = row
	}

	for k, row := range beforeSet {
		if _, present := afterSet[k]; !present {
			removed = append(removed, row)
		}
	}

	for k, row := range afterSet {
		if _, present := beforeSet[k]; !present {
			added = append(added, row)
		}
	}

	return removed, added
}
```

Add `"time"` import.

- [ ] **Step 6: Run, verify pass**

```bash
go test ./internal/node/... -run "TestModify_Hook" -v
```

Expected: 3 PASS.

- [ ] **Step 7: Verify the full node test suite still passes**

```bash
go test ./internal/node/... -v
```

Expected: every existing test still PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/node/
git commit -m "feat(node): Modify dispatches recovery-aware hooks + edge diff + drift writes"
```

---

## Task 14: Reindex — warn-mode validate + drift writes + summary count

**Files:** Modify `internal/reindex/reindex.go`, `internal/reindex/reindex_test.go`.

Reindex sees only on-disk state (`before = nil`). It runs the recovery-aware Validate fire; rejection becomes a drift row, recovery becomes a drift row, clean passes clear drift. Rejections do not abort indexing.

- [ ] **Step 1: Append failing tests to `internal/reindex/reindex_test.go`**

```go
func TestRun_OffSchemaStatusProducesDriftRow(test *testing.T) {
	root := test.TempDir()
	dbPath := filepath.Join(root, "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	// Write a ticket with off-schema status.
	if writeErr := os.WriteFile(filepath.Join(root, "ticket.md"), []byte(`---
type: ticket
status: bogus
---
body
`), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	driftRepo := index.NewWorkflowDriftRepo(store)
	engine := buildWorkflowEngineForReindexTest(test)

	report, runErr := reindex.Run(reindex.Config{
		Root:      root,
		Repo:      index.NewNodeRepo(store),
		Behaviors: engine,
		DriftLog:  driftRepo,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.WorkflowViolations != 1 {
		test.Errorf("WorkflowViolations = %d, want 1", report.WorkflowViolations)
	}

	rows, _ := driftRepo.ListAll()

	if len(rows) != 1 || rows[0].ObservedStatus != "bogus" {
		test.Errorf("drift rows = %+v, want one row for 'bogus'", rows)
	}

	// Indexing still upserted the row.
	if _, getErr := index.NewNodeRepo(store).Get("ticket"); getErr != nil {
		test.Errorf("Get: %v (reindex should still upsert despite drift)", getErr)
	}
}

func TestRun_CleanPassClearsDrift(test *testing.T) {
	root := test.TempDir()
	dbPath := filepath.Join(root, "index.db")

	store, _ := index.Open(dbPath)

	defer store.Close()

	driftRepo := index.NewWorkflowDriftRepo(store)

	// Seed a stale drift row.
	if appendErr := driftRepo.Append(index.WorkflowDriftRow{
		NodeID: "ticket", PackInstance: "tickets", PackKind: "workflow",
		ObservedStatus: "ancient", Property: "status", ObservedAt: 1,
	}); appendErr != nil {
		test.Fatalf("seed Append: %v", appendErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "ticket.md"), []byte(`---
type: ticket
status: pending
---
`), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	engine := buildWorkflowEngineForReindexTest(test)

	if _, runErr := reindex.Run(reindex.Config{
		Root:      root,
		Repo:      index.NewNodeRepo(store),
		Behaviors: engine,
		DriftLog:  driftRepo,
	}); runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	rows, _ := driftRepo.ListAll()

	if len(rows) != 0 {
		test.Errorf("drift after clean reindex = %+v, want empty", rows)
	}
}

func buildWorkflowEngineForReindexTest(test *testing.T) *behavior.Engine {
	test.Helper()

	const sample = `
[behaviors.workflow.tickets]
applies-to = ["ticket"]
states = [
  { name = "pending", initial = true },
  { name = "active" },
  { name = "completed", terminal = true, done = true },
]
transitions = [
  { from = "pending", to = "active" },
  { from = "active", to = "completed" },
]
`

	var decoded struct {
		Behaviors map[string]map[string]toml.Primitive `toml:"behaviors"`
	}

	meta, decodeErr := toml.Decode(sample, &decoded)

	if decodeErr != nil {
		test.Fatalf("toml decode: %v", decodeErr)
	}

	primitive := decoded.Behaviors["workflow"]["tickets"]

	instance, newErr := workflow.Kind{}.NewInstance("tickets", primitive, &meta)

	if newErr != nil {
		test.Fatalf("workflow.NewInstance: %v", newErr)
	}

	engine, _ := behavior.NewEngine([]behavior.Instance{instance})

	return engine
}
```

Add imports: `github.com/BurntSushi/toml`, `github.com/germanamz/tusk/internal/behavior`, `github.com/germanamz/tusk/internal/behavior/workflow`, plus existing test imports.

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/reindex/... -run "TestRun_OffSchemaStatusProducesDriftRow|TestRun_CleanPassClearsDrift"
```

Expected: FAIL — `Config.Behaviors`, `Config.DriftLog`, `Report.WorkflowViolations` undefined.

- [ ] **Step 3: Modify `internal/reindex/reindex.go`**

Grow `Config` and `Report`, then wire the dispatch into the per-file walk.

In `Config`:
```go
type Config struct {
	// ... existing fields ...
	Behaviors *behavior.Engine          // optional; nil = no validation
	DriftLog  *index.WorkflowDriftRepo  // optional; nil = no drift persistence
}
```

In `Report`:
```go
type Report struct {
	// ... existing fields ...
	WorkflowViolations int
}
```

Add imports: `"github.com/germanamz/tusk/internal/behavior"`, `"time"`.

In the per-file walk, after the existing `Repo.Upsert(...)` call (and `Edges.UpsertAll` if applicable), insert:

```go
		// + Plan 7: workflow validation in warn mode. Rejections become
		// drift rows; recoveries become drift rows; clean passes clear
		// any prior drift for this node.
		if config.Behaviors != nil {
			result, fireErr := config.Behaviors.FireNodeWriteValidateWithRecovery(nil, parsed)

			now := time.Now().UnixNano()

			switch {
			case fireErr != nil:
				report.WorkflowViolations++

				if config.DriftLog != nil {
					_ = config.DriftLog.Append(index.WorkflowDriftRow{
						NodeID:         parsed.ID,
						PackInstance:   instanceFromQualifier(result.Rejector),
						PackKind:       kindFromQualifier(result.Rejector),
						ObservedStatus: readStatusFromParsed(parsed),
						Property:       extractPropertyFromError(fireErr),
						ObservedAt:     now,
					})
				}

			case len(result.Recovered) > 0:
				report.WorkflowViolations += len(result.Recovered)

				if config.DriftLog != nil {
					for _, recovered := range result.Recovered {
						_ = config.DriftLog.Append(index.WorkflowDriftRow{
							NodeID:         parsed.ID,
							PackInstance:   recovered.PackInstance,
							PackKind:       recovered.PackKind,
							ObservedStatus: recovered.From,
							Property:       recovered.Property,
							ObservedAt:     now,
						})
					}
				}

			default:
				if config.DriftLog != nil {
					_ = config.DriftLog.ClearForNode(parsed.ID)
				}
			}
		}
```

Also append helpers at the bottom of reindex.go:

```go
func instanceFromQualifier(qualified string) string {
	for index := 0; index < len(qualified); index++ {
		if qualified[index] == '.' {
			return qualified[index+1:]
		}
	}

	return qualified
}

func kindFromQualifier(qualified string) string {
	for index := 0; index < len(qualified); index++ {
		if qualified[index] == '.' {
			return qualified[:index]
		}
	}

	return qualified
}

// readStatusFromParsed reads the "status" property as a string. The
// rejection-path drift row uses this since the rejection error doesn't
// carry the status-property name explicitly. (For v1 with one workflow
// configuration per workspace, "status" is the default; non-default
// status-property values surface in the recovery path which carries
// the property explicitly.)
func readStatusFromParsed(parsed *node.Node) string {
	if parsed == nil || parsed.Properties == nil {
		return ""
	}

	value, found := parsed.Properties["status"]

	if !found {
		return ""
	}

	stringValue, ok := value.(string)

	if !ok {
		return ""
	}

	return stringValue
}

// extractPropertyFromError pulls Property off a *workflow.Error if the
// rejection came from the workflow pack; otherwise returns "status".
func extractPropertyFromError(err error) string {
	var workflowErr *workflow.Error

	if errors.As(err, &workflowErr) {
		return workflowErr.Property
	}

	return "status"
}
```

Add imports: `"errors"`, `"github.com/germanamz/tusk/internal/behavior/workflow"`.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/reindex/... -v
```

Expected: every existing test PASS plus the two new ones.

- [ ] **Step 5: Commit**

```bash
git add internal/reindex/
git commit -m "feat(reindex): warn-mode workflow validation + drift writes + violations count"
```

---

## Task 15: Doctor — `workflow-violation` Issue from drift log

**Files:** Modify `internal/doctor/doctor.go`, `internal/doctor/doctor_test.go`.

Doctor adds a new Issue kind constant and reads the drift table when `Config.WorkflowDrift` is supplied. Each drift row materializes one Issue.

- [ ] **Step 1: Append failing test to `internal/doctor/doctor_test.go`**

```go
func TestRun_SurfacesWorkflowViolation(test *testing.T) {
	store, closer := newTempIndex(test)
	defer closer()

	driftRepo := index.NewWorkflowDriftRepo(store)

	if appendErr := driftRepo.Append(index.WorkflowDriftRow{
		NodeID: "tickets/foo", PackInstance: "tickets", PackKind: "workflow",
		ObservedStatus: "blocked", Property: "status", ObservedAt: 100,
	}); appendErr != nil {
		test.Fatalf("Append: %v", appendErr)
	}

	report, runErr := doctor.Run(doctor.Config{
		Nodes:         index.NewNodeRepo(store),
		WorkflowDrift: driftRepo,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	var found bool

	for _, issue := range report.Issues {
		if issue.Kind == doctor.IssueWorkflowViolation && issue.NodeID == "tickets/foo" {
			found = true

			if !strings.Contains(issue.Message, "blocked") {
				test.Errorf("Issue.Message = %q, want mention of 'blocked'", issue.Message)
			}
		}
	}

	if !found {
		test.Errorf("workflow-violation Issue not found in %+v", report.Issues)
	}
}

func newTempIndex(test *testing.T) (*index.Index, func()) {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return store, func() { store.Close() }
}
```

Add imports as needed.

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/doctor/... -run TestRun_SurfacesWorkflowViolation
```

Expected: FAIL — `doctor.IssueWorkflowViolation`, `Config.WorkflowDrift` undefined.

- [ ] **Step 3: Modify `internal/doctor/doctor.go`**

Add the constant and grow `Config`:

```go
const (
	IssueDanglingEdge      = "dangling-edge"
	IssueEmbedRetry        = "embed-retry"
	IssueWorkflowViolation = "workflow-violation"
)

type Config struct {
	Nodes         *index.NodeRepo
	Edges         *index.EdgeRepo
	EmbedQueue    *index.EmbedQueueRepo
	WorkflowDrift *index.WorkflowDriftRepo  // optional; nil = no workflow checks
}
```

In `Run`, add:

```go
	if config.WorkflowDrift != nil {
		drift, listErr := config.WorkflowDrift.ListAll()

		if listErr != nil {
			return nil, listErr
		}

		for _, row := range drift {
			report.Issues = append(report.Issues, Issue{
				Kind:    IssueWorkflowViolation,
				NodeID:  row.NodeID,
				Message: fmt.Sprintf("workflow %q: status %q is not a declared state for property %q",
					row.PackInstance, row.ObservedStatus, row.Property),
			})
		}
	}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/doctor/... -v
```

Expected: every test PASS, including the new one.

- [ ] **Step 5: Commit**

```bash
git add internal/doctor/
git commit -m "feat(doctor): workflow-violation Issue kind sourced from drift log"
```

---

## Task 16: cmd/tusk — `behavior_registry.go` helper

**Files:** Create `cmd/tusk/behavior_registry.go`.

A single helper builds a `*behavior.Engine` for every CLI command. The MCP runtime calls it too (Task 20). Centralizing the kind list here means adding a new pack later touches one place, not nine.

- [ ] **Step 1: Write `cmd/tusk/behavior_registry.go`**

```go
package main

import (
	"fmt"

	"github.com/germanamz/tusk/internal/behavior"
	"github.com/germanamz/tusk/internal/behavior/workflow"
	"github.com/germanamz/tusk/internal/manifest"
)

// newBehaviorEngine constructs a *behavior.Engine from loaded by
// registering every built-in pack kind and resolving the manifest's
// [behaviors] section. v1 registers exactly one kind: workflow.
func newBehaviorEngine(loaded *manifest.Manifest) (*behavior.Engine, error) {
	registry := behavior.NewRegistry()

	if registerErr := registry.Register(workflow.Kind{}); registerErr != nil {
		return nil, fmt.Errorf("behavior_registry: register workflow: %w", registerErr)
	}

	engine, buildErr := registry.BuildEngine(loaded)

	if buildErr != nil {
		return nil, buildErr
	}

	return engine, nil
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./cmd/tusk/...
```

Expected: success.

- [ ] **Step 3: Commit**

```bash
git add cmd/tusk/behavior_registry.go
git commit -m "feat(cli): newBehaviorEngine helper for cmd/tusk"
```

---

## Task 17: `tusk node modify` — wire engine + warnings

**Files:** Modify `cmd/tusk/cmd_node_modify.go`, `cmd/tusk/cmd_node_modify_test.go`.

The cobra command builds the engine via `newBehaviorEngine`, constructs `NewServiceWithBehaviors`, and passes `cmd.ErrOrStderr()` as the warnings writer.

- [ ] **Step 1: Append failing tests to `internal/cmd_node_modify_test.go` (existing file)**

Add tests covering: legal transition → success on stdout; illegal transition → error message on stderr; orphan recovery → warning on stderr + drift visible in subsequent doctor invocation; unset → rejected.

```go
func TestNodeModify_WorkflowLegalTransition(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{"status": "pending"})

	stdout, stderr, exit := runCLI(test, root, "node", "modify", "tickets/foo", "--prop", "status=active")

	if exit != 0 {
		test.Errorf("exit = %d, want 0; stderr = %s", exit, stderr.String())
	}

	if !strings.Contains(stdout.String(), "Modified tickets/foo") {
		test.Errorf("stdout = %q, want 'Modified tickets/foo'", stdout.String())
	}
}

func TestNodeModify_WorkflowIllegalTransitionRejected(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{"status": "pending"})

	_, stderr, exit := runCLI(test, root, "node", "modify", "tickets/foo", "--prop", "status=completed")

	if exit == 0 {
		test.Errorf("exit = 0, want non-zero")
	}

	if !strings.Contains(stderr.String(), "illegal-transition") {
		test.Errorf("stderr = %q, want mention of illegal-transition", stderr.String())
	}
}

func TestNodeModify_WorkflowRecoveryWarnsAndPersistsDrift(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{"status": "blocked"}) // off-schema

	stdout, stderr, exit := runCLI(test, root, "node", "modify", "tickets/foo", "--prop", "status=active")

	if exit != 0 {
		test.Errorf("exit = %d, want 0", exit)
	}

	if !strings.Contains(stderr.String(), "recovered from unknown status") {
		test.Errorf("stderr = %q, want recovery warning", stderr.String())
	}

	if !strings.Contains(stdout.String(), "Modified tickets/foo") {
		test.Errorf("stdout = %q, want success line", stdout.String())
	}

	// Drift should now be visible to `tusk doctor`.
	doctorStdout, _, doctorExit := runCLI(test, root, "doctor")

	if doctorExit != 0 {
		test.Errorf("doctor exit = %d, want 0", doctorExit)
	}

	if !strings.Contains(doctorStdout.String(), "workflow-violation") {
		test.Errorf("doctor stdout = %q, want workflow-violation", doctorStdout.String())
	}
}

func TestNodeModify_WorkflowUnsetRejected(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{"status": "active"})

	_, stderr, exit := runCLI(test, root, "node", "modify", "tickets/foo", "--unset", "status")

	if exit == 0 {
		test.Errorf("exit = 0, want non-zero")
	}

	if !strings.Contains(stderr.String(), "cannot-unset-status") {
		test.Errorf("stderr = %q, want mention of cannot-unset-status", stderr.String())
	}
}

// newWorkspaceWithWorkflow seeds a workspace under test.TempDir() with a
// tusk.toml that activates the workflow pack on tickets. Returns the
// workspace root.
func newWorkspaceWithWorkflow(test *testing.T) string {
	test.Helper()

	root := test.TempDir()

	manifestBody := `
[workspace]
name = "test"

[behaviors.workflow.tickets]
applies-to = ["ticket"]
states = [
  { name = "pending", initial = true },
  { name = "active" },
  { name = "completed", terminal = true, done = true },
]
transitions = [
  { from = "pending", to = "active" },
  { from = "active", to = "completed" },
]
`

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	return root
}

// mustCreateNode writes a node file directly to disk under root with the
// given frontmatter properties, then runs `tusk reindex` to populate the
// index. status starts off-schema where useful for tests.
func mustCreateNode(test *testing.T, root, id, typ string, props map[string]string) {
	test.Helper()

	relPath := filepath.Join(root, id+".md")

	if mkErr := os.MkdirAll(filepath.Dir(relPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("type: %s\n", typ))

	for key, value := range props {
		sb.WriteString(fmt.Sprintf("%s: %s\n", key, value))
	}

	sb.WriteString("---\n\nbody\n")

	if writeErr := os.WriteFile(relPath, []byte(sb.String()), 0o644); writeErr != nil {
		test.Fatalf("write node: %v", writeErr)
	}

	if _, _, exit := runCLI(test, root, "reindex"); exit != 0 {
		test.Fatalf("seed reindex: exit %d", exit)
	}
}

// runCLI is the existing test helper used by cmd_node_*_test.go. If the
// repo doesn't already have a generic version, copy from a sibling test
// file (each cmd_*_test file in this repo defines its own thin wrapper
// around root.go's command tree). Pattern:
//
//   func runCLI(test *testing.T, root string, args ...string) (stdout, stderr *bytes.Buffer, exit int)
//
// The wrapper sets cwd to `root`, builds a fresh root command via
// newRootCmd(), captures stdout/stderr buffers, and returns the exit code
// (0 for nil err; 1 for non-nil).
```

The `runCLI` helper already exists in the package's existing tests (e.g. `cmd_doctor_test.go`); reuse it. If it isn't visible across files, alias it.

- [ ] **Step 2: Run, verify fail**

```bash
go test ./cmd/tusk/... -run "TestNodeModify_Workflow"
```

Expected: FAIL — `cmd_node_modify` doesn't yet wire the behavior engine.

- [ ] **Step 3: Migrate `cmd/tusk/cmd_node_modify.go`**

Replace the body of `RunE`:

```go
RunE: func(cmd *cobra.Command, args []string) error {
	cwd, cwdErr := os.Getwd()

	if cwdErr != nil {
		return cwdErr
	}

	ws, findErr := workspace.Find(cwd)

	if findErr != nil {
		return fmt.Errorf("workspace: %w", findErr)
	}

	loaded, loadErr := manifest.Load(ws.ManifestPath)

	if loadErr != nil {
		return loadErr
	}

	setProps, setErr := parseSetFlags(setFlags)

	if setErr != nil {
		return setErr
	}

	engine, buildErr := newBehaviorEngine(loaded)

	if buildErr != nil {
		return buildErr
	}

	return withWorkspaceLock(ws, func() error {
		store, openErr := index.Open(ws.IndexPath)

		if openErr != nil {
			return openErr
		}

		defer store.Close()

		service := node.NewServiceWithBehaviors(
			ws.Root,
			index.NewNodeRepo(store),
			index.NewEdgeRepo(store),
			loaded.EdgeTypes,
			index.NewEmbedQueueRepo(store),
			engine,
			index.NewWorkflowDriftRepo(store),
			cmd.ErrOrStderr(),
		)

		modified, modifyErr := service.Modify(node.ModifyInput{
			ID:        args[0],
			SetProps:  setProps,
			UnsetKeys: unsetFlags,
		})

		if modifyErr != nil {
			return modifyErr
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Modified %s\n", modified.ID)

		return nil
	})
},
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./cmd/tusk/... -run "TestNodeModify" -v
```

Expected: every pre-existing TestNodeModify test still PASS, plus the four new workflow tests.

- [ ] **Step 5: Commit**

```bash
git add cmd/tusk/cmd_node_modify.go cmd/tusk/cmd_node_modify_test.go
git commit -m "feat(cli): tusk node modify wires workflow engine + warnings"
```

---

## Task 18: `tusk node create` — wire engine + initial-state validation

**Files:** Modify `cmd/tusk/cmd_node_create.go`, `cmd/tusk/cmd_node_create_test.go`.

- [ ] **Step 1: Append failing tests to `cmd_node_create_test.go`**

```go
func TestNodeCreate_WorkflowNonInitialRejected(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	_, stderr, exit := runCLI(test, root, "node", "create", "--type", "ticket", "--prop", "status=active", "--", "tickets/foo")

	if exit == 0 {
		test.Errorf("exit = 0, want non-zero")
	}

	if !strings.Contains(stderr.String(), "non-initial-on-create") {
		test.Errorf("stderr = %q, want mention of non-initial-on-create", stderr.String())
	}
}

func TestNodeCreate_WorkflowInitialAccepted(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	stdout, _, exit := runCLI(test, root, "node", "create", "--type", "ticket", "--prop", "status=pending", "--", "tickets/foo")

	if exit != 0 {
		test.Errorf("exit = %d, want 0", exit)
	}

	if !strings.Contains(stdout.String(), "tickets/foo") {
		test.Errorf("stdout = %q, want path mention", stdout.String())
	}
}
```

(The exact form of the `node create` CLI invocation may differ — adjust to match the existing `cmd_node_create.go` flag conventions; the imperative path is to assert that workflow validation runs at create time.)

- [ ] **Step 2: Run, verify fail**

```bash
go test ./cmd/tusk/... -run "TestNodeCreate_Workflow"
```

Expected: FAIL — `cmd_node_create` doesn't yet wire the behavior engine.

- [ ] **Step 3: Migrate `cmd/tusk/cmd_node_create.go`**

Replace the `node.NewService(...)` call site with `node.NewServiceWithBehaviors(...)` using `newBehaviorEngine(loaded)` and the drift repo + warnings writer (same pattern as Task 17). The diff is mechanical: load manifest, build engine, swap the constructor.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./cmd/tusk/... -run "TestNodeCreate" -v
```

Expected: every existing TestNodeCreate PASS plus the two new workflow tests.

- [ ] **Step 5: Commit**

```bash
git add cmd/tusk/cmd_node_create.go cmd/tusk/cmd_node_create_test.go
git commit -m "feat(cli): tusk node create wires workflow engine"
```

---

## Task 19: `tusk reindex` and `tusk doctor` — wire engine + drift surface

**Files:** Modify `cmd/tusk/cmd_reindex.go`, `cmd/tusk/cmd_reindex_test.go`, `cmd/tusk/cmd_doctor.go`, `cmd/tusk/cmd_doctor_test.go`.

- [ ] **Step 1: Append failing tests to `cmd_reindex_test.go`**

```go
func TestReindex_OffSchemaStatusReportedInSummary(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	if writeErr := os.WriteFile(filepath.Join(root, "tickets/foo.md"), []byte(`---
type: ticket
status: bogus
---
`), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(root, "tickets"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	stdout, _, exit := runCLI(test, root, "reindex")

	if exit != 0 {
		test.Errorf("exit = %d, want 0", exit)
	}

	if !strings.Contains(stdout.String(), "workflow-violation") {
		test.Errorf("stdout = %q, want mention of workflow-violation", stdout.String())
	}
}
```

- [ ] **Step 2: Append failing tests to `cmd_doctor_test.go`**

```go
func TestDoctor_RendersWorkflowViolation(test *testing.T) {
	root := newWorkspaceWithWorkflow(test)

	// Use existing helper: create a node, then drift it.
	mustCreateNode(test, root, "tickets/foo", "ticket", map[string]string{"status": "blocked"})

	stdout, _, exit := runCLI(test, root, "doctor")

	if exit != 0 {
		test.Errorf("exit = %d, want 0", exit)
	}

	if !strings.Contains(stdout.String(), "workflow-violation") {
		test.Errorf("stdout = %q, want mention of workflow-violation", stdout.String())
	}

	if !strings.Contains(stdout.String(), "tickets/foo") {
		test.Errorf("stdout = %q, want mention of tickets/foo", stdout.String())
	}
}
```

- [ ] **Step 3: Run, verify fail**

```bash
go test ./cmd/tusk/... -run "TestReindex_OffSchemaStatusReportedInSummary|TestDoctor_RendersWorkflowViolation"
```

Expected: FAIL — `cmd_reindex` and `cmd_doctor` don't yet wire the engine/drift.

- [ ] **Step 4: Modify `cmd/tusk/cmd_reindex.go`**

Build the behavior engine and pass it to `reindex.Run`:

```go
engine, buildErr := newBehaviorEngine(loaded)

if buildErr != nil {
	return buildErr
}

driftRepo := index.NewWorkflowDriftRepo(store)

report, runErr := reindex.Run(reindex.Config{
	Root:      ws.Root,
	Repo:      index.NewNodeRepo(store),
	Edges:     index.NewEdgeRepo(store),
	EdgeTypes: loaded.EdgeTypes,
	Behaviors: engine,
	DriftLog:  driftRepo,
	// ... existing fields ...
})

// Update summary line:
if report.WorkflowViolations > 0 {
	fmt.Fprintf(cmd.OutOrStdout(),
		"Indexed %d nodes (%d workflow-violation%s) in %s\nRun `tusk doctor` to inspect violations\n",
		report.NodeCount, report.WorkflowViolations, plural(report.WorkflowViolations), report.Duration)
} else {
	fmt.Fprintf(cmd.OutOrStdout(), "Indexed %d nodes in %s\n", report.NodeCount, report.Duration)
}
```

(Adjust to the existing summary format; the key requirement is that the violation count surfaces in stdout.)

Add a `plural` helper if not present:

```go
func plural(n int) string {
	if n == 1 {
		return ""
	}

	return "s"
}
```

- [ ] **Step 5: Modify `cmd/tusk/cmd_doctor.go`**

Pass `index.NewWorkflowDriftRepo(store)` to `doctor.Run`:

```go
report, runErr := doctor.Run(doctor.Config{
	Nodes:         index.NewNodeRepo(store),
	Edges:         index.NewEdgeRepo(store),
	EmbedQueue:    index.NewEmbedQueueRepo(store),
	WorkflowDrift: index.NewWorkflowDriftRepo(store),
})
```

The existing rendering loop already prints `Issue.Kind | Issue.NodeID | Issue.Message` — the new `workflow-violation` Kind surfaces automatically.

- [ ] **Step 6: Run, verify pass**

```bash
go test ./cmd/tusk/... -run "TestReindex|TestDoctor" -v
```

Expected: every existing test PASS plus the two new ones.

- [ ] **Step 7: Commit**

```bash
git add cmd/tusk/cmd_reindex.go cmd/tusk/cmd_reindex_test.go cmd/tusk/cmd_doctor.go cmd/tusk/cmd_doctor_test.go
git commit -m "feat(cli): tusk reindex + doctor surface workflow violations"
```

---

## Task 20: MCP runtime — `BehaviorEngine` + drift repo + ReloadManifest rebuilds engine

**Files:** Modify `internal/mcp/runtime.go`, `internal/mcp/runtime_test.go`.

`Runtime` grows two fields: `BehaviorEngine *behavior.Engine` and `WorkflowDrift *index.WorkflowDriftRepo`. `Open` and `ReloadManifest` build the engine through the same helper used by the CLI. `NodeService` migrates to `NewServiceWithBehaviors`.

- [ ] **Step 1: Append failing test to `internal/mcp/runtime_test.go`**

```go
func TestRuntime_ReloadManifestRebuildsBehaviorEngine(test *testing.T) {
	root := test.TempDir()

	manifestBody := []byte(`
[workspace]
name = "test"

[behaviors.workflow.tickets]
applies-to = ["ticket"]
states = [
  { name = "pending", initial = true },
]
`)

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), manifestBody, 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	if rt.BehaviorEngine == nil {
		test.Fatalf("BehaviorEngine is nil after Open")
	}

	// Modify the manifest: add a transitions block.
	updated := []byte(`
[workspace]
name = "test"

[behaviors.workflow.tickets]
applies-to = ["ticket"]
states = [
  { name = "pending", initial = true },
  { name = "active" },
]
transitions = [
  { from = "pending", to = "active" },
]
`)

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), updated, 0o644); writeErr != nil {
		test.Fatalf("rewrite: %v", writeErr)
	}

	if reloadErr := rt.ReloadManifest(); reloadErr != nil {
		test.Fatalf("ReloadManifest: %v", reloadErr)
	}

	if rt.BehaviorEngine == nil {
		test.Errorf("BehaviorEngine is nil after ReloadManifest")
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/mcp/... -run TestRuntime_ReloadManifestRebuildsBehaviorEngine
```

Expected: FAIL — `Runtime.BehaviorEngine` undefined.

- [ ] **Step 3: Modify `internal/mcp/runtime.go`**

Grow `Runtime`:

```go
type Runtime struct {
	// ... existing fields ...

	BehaviorEngine *behavior.Engine
	WorkflowDrift  *index.WorkflowDriftRepo
}
```

Add imports: `"github.com/germanamz/tusk/internal/behavior"`, `"github.com/germanamz/tusk/internal/behavior/workflow"`.

Add a `buildBehaviorEngine` helper near the bottom (mirroring `cmd/tusk/behavior_registry.go`'s `newBehaviorEngine`):

```go
func buildBehaviorEngine(loaded *manifest.Manifest) (*behavior.Engine, error) {
	registry := behavior.NewRegistry()

	if registerErr := registry.Register(workflow.Kind{}); registerErr != nil {
		return nil, fmt.Errorf("mcp: register workflow: %w", registerErr)
	}

	return registry.BuildEngine(loaded)
}
```

In `Open` (after the manifest loads and the index opens), build the engine and the drift repo:

```go
engine, buildErr := buildBehaviorEngine(loaded)

if buildErr != nil {
	store.Close()
	return nil, fmt.Errorf("mcp: behavior engine: %w", buildErr)
}

driftRepo := index.NewWorkflowDriftRepo(store)

rt := &Runtime{
	// ... existing field assignments ...
	BehaviorEngine: engine,
	WorkflowDrift:  driftRepo,
}
```

Replace the existing `node.NewServiceWithEmbedQueue(...)` call with the behaviors-aware constructor. The MCP runtime has no per-call cobra `cmd`, so warnings go to `os.Stderr`:

```go
rt.NodeService = node.NewServiceWithBehaviors(
	rt.Root,
	rt.Nodes,
	rt.Edges,
	loaded.EdgeTypes,
	rt.EmbedQueue,
	engine,
	driftRepo,
	os.Stderr,
)
```

In `ReloadManifest`, mirror the same construction:

```go
func (rt *Runtime) ReloadManifest() error {
	loaded, loadErr := manifest.Load(rt.ManifestPath)

	if loadErr != nil {
		return fmt.Errorf("mcp: reload manifest: %w", loadErr)
	}

	engine, buildErr := buildBehaviorEngine(loaded)

	if buildErr != nil {
		return fmt.Errorf("mcp: rebuild behavior engine: %w", buildErr)
	}

	rt.Manifest = loaded
	rt.BehaviorEngine = engine
	rt.NodeService = node.NewServiceWithBehaviors(
		rt.Root,
		rt.Nodes,
		rt.Edges,
		loaded.EdgeTypes,
		rt.EmbedQueue,
		engine,
		rt.WorkflowDrift,
		os.Stderr,
	)

	return nil
}
```

Add `"os"` import if missing.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/mcp/... -run TestRuntime -v
```

Expected: every existing TestRuntime PASS plus the new one.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/runtime.go internal/mcp/runtime_test.go
git commit -m "feat(mcp): runtime owns BehaviorEngine + WorkflowDrift; ReloadManifest rebuilds"
```

---

## Task 21: MCP tools — structured workflow rejection + recovery warnings + doctor drift

**Files:** Modify `internal/mcp/tools.go`, `internal/mcp/tools_test.go`.

`tusk_node_modify` and `tusk_node_create` return a structured error result when the workflow pack rejects, and grow a `warnings` field on the success payload when recovery fires. `tusk_doctor` already exists in Plan 6's surface — Plan 7 wires the drift repo into its config.

- [ ] **Step 1: Append failing test to `internal/mcp/tools_test.go` covering structured rejection**

```go
func TestTools_NodeModify_StructuredWorkflowRejection(test *testing.T) {
	rt, harness := newRuntimeWithWorkflow(test)
	defer harness.Close()

	// Seed a node with status=pending.
	mustCreateNodeViaRuntime(test, rt, "tickets/foo", "ticket", map[string]any{"status": "pending"})

	result := callTool(test, harness, "tusk_node_modify", map[string]any{
		"id":  "tickets/foo",
		"set": map[string]any{"status": "completed"}, // illegal: pending → completed not in transition table
	})

	if !result.IsError {
		test.Errorf("expected IsError=true")
	}

	body := decodeJSONContent(test, result)

	if body["code"] != "illegal-transition" {
		test.Errorf("body.code = %v, want illegal-transition", body["code"])
	}

	if body["pack_instance"] != "tickets" {
		test.Errorf("body.pack_instance = %v, want tickets", body["pack_instance"])
	}

	if body["from"] != "pending" || body["to"] != "completed" {
		test.Errorf("body.from/to = %v/%v", body["from"], body["to"])
	}
}

func TestTools_NodeModify_RecoveryWarnsOnSuccess(test *testing.T) {
	rt, harness := newRuntimeWithWorkflow(test)
	defer harness.Close()

	// Seed a node with off-schema status.
	mustCreateNodeViaRuntime(test, rt, "tickets/foo", "ticket", map[string]any{"status": "blocked"})

	result := callTool(test, harness, "tusk_node_modify", map[string]any{
		"id":  "tickets/foo",
		"set": map[string]any{"status": "active"},
	})

	if result.IsError {
		test.Errorf("expected success result for recovery, got error: %v", result)
	}

	body := decodeJSONContent(test, result)

	warnings, ok := body["warnings"].([]any)

	if !ok || len(warnings) == 0 {
		test.Fatalf("warnings absent; body = %v", body)
	}

	first, ok := warnings[0].(map[string]any)

	if !ok {
		test.Fatalf("warnings[0] is not an object: %T", warnings[0])
	}

	if first["kind"] != "workflow-recovered" {
		test.Errorf("warnings[0].kind = %v, want workflow-recovered", first["kind"])
	}

	if first["from"] != "blocked" || first["to"] != "active" {
		test.Errorf("warnings[0] from/to = %v/%v", first["from"], first["to"])
	}
}

// newRuntimeWithWorkflow + harness + callTool + decodeJSONContent helpers
// follow the patterns established by Plan 6's tools_test.go. Reuse those
// helpers; the workflow-specific addition is the manifest seeding.
```

The `newRuntimeWithWorkflow`, `callTool`, `decodeJSONContent`, `mustCreateNodeViaRuntime` helpers either exist in `tools_test.go` from Plan 6 or are thin wrappers around mcp-go's `CallTool`. Reuse what's there; if a helper is missing, copy from `cmd_node_modify_test.go` (the workspace-fixture pattern).

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/mcp/... -run TestTools_NodeModify
```

Expected: FAIL — tools.go doesn't yet emit structured workflow rejections or recovery warnings.

- [ ] **Step 3: Modify `internal/mcp/tools.go` — structured rejection helper**

Add a small helper:

```go
// toolJSONError builds a CallToolResult with IsError=true and a JSON-
// encoded body. Mirrors toolJSON's success path.
func toolJSONError(payload map[string]any) *mcpgo.CallToolResult {
	body, marshalErr := json.Marshal(payload)

	if marshalErr != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("toolJSONError: %v", marshalErr))
	}

	return &mcpgo.CallToolResult{
		IsError: true,
		Content: []mcpgo.Content{mcpgo.NewTextContent(string(body))},
	}
}
```

- [ ] **Step 4: Modify `registerNodeModifyTool` handler to emit structured workflow errors + recovery warnings**

Replace the rejection-handling block. Where the existing handler does `return toolError(modifyErr), nil`, change to:

```go
if modifyErr != nil {
	var workflowErr *workflow.Error

	if errors.As(modifyErr, &workflowErr) {
		return toolJSONError(map[string]any{
			"error":         "workflow-rejection",
			"code":          string(workflowErr.Code),
			"message":       workflowErr.Error(),
			"property":      workflowErr.Property,
			"from":          workflowErr.From,
			"to":            workflowErr.To,
			"valid_targets": stringSliceOrNil(workflowErr.ValidTargets),
			"known_states":  stringSliceOrNil(workflowErr.KnownStates),
			"pack_instance": workflowErr.PackInstance,
		}), nil
	}

	return toolError(modifyErr), nil
}
```

Recovery surface — capture stderr warnings into a buffer per-call so the success payload can include them. Replace the `Service` construction inside the handler so that warnings flow into a per-call buffer:

```go
var warningsBuf bytes.Buffer

// We need a Service whose warnings go to warningsBuf, not the runtime's
// shared os.Stderr. Construct a per-call Service that points at the
// runtime's repos and engine but with our local writer:
service := node.NewServiceWithBehaviors(
	srv.runtime.Root,
	srv.runtime.Nodes,
	srv.runtime.Edges,
	srv.runtime.Manifest.EdgeTypes,
	srv.runtime.EmbedQueue,
	srv.runtime.BehaviorEngine,
	srv.runtime.WorkflowDrift,
	&warningsBuf,
)

modified, modifyErr := service.Modify(input)
```

After `Modify` succeeds, build the success payload:

```go
result := map[string]any{
	"id":         modified.ID,
	"type":       modified.Type,
	"path":       modified.Path,
	"title":      modified.Title,
	"properties": modified.Properties,
}

if warningsBuf.Len() > 0 {
	result["warnings"] = parseRecoveryWarnings(warningsBuf.String(), modified.ID)
}

return toolJSON(result)
```

Add the parser:

```go
// parseRecoveryWarnings turns the Service's stderr warning lines into a
// structured slice. Format produced by node.Service:
//   warning: workflow "tickets" recovered from unknown status "blocked" → "active" on tickets/foo; transition not validated
func parseRecoveryWarnings(buf, nodeID string) []map[string]any {
	var warnings []map[string]any

	for _, line := range strings.Split(strings.TrimSpace(buf), "\n") {
		if !strings.HasPrefix(line, "warning: workflow ") {
			continue
		}

		// Extract instance name between the first pair of quotes.
		instance := extractQuoted(line, 0)
		from := extractQuoted(line, 1)
		to := extractQuoted(line, 2)

		warnings = append(warnings, map[string]any{
			"kind":          "workflow-recovered",
			"pack_instance": instance,
			"from":          from,
			"to":            to,
			"property":      "status",  // v1: only "status" is the recoverable property; richer carry comes with a structured warning channel later
			"message":       line,
		})
	}

	return warnings
}

func extractQuoted(line string, occurrence int) string {
	count := 0
	start := -1

	for index, char := range line {
		if char != '"' {
			continue
		}

		if start == -1 {
			start = index + 1
			continue
		}

		if count == occurrence {
			return line[start:index]
		}

		count++
		start = -1
	}

	return ""
}

func stringSliceOrNil(values []string) any {
	if len(values) == 0 {
		return nil
	}

	return values
}
```

(The string-parsing approach is a v1 expediency — the Service emits warnings as text. A future Plan can replace this with a structured warning channel; the spec's §10 ledger #11 already reserves space for richer surfaces.)

Add imports: `"bytes"`, `"errors"`, `"strings"`, `"github.com/germanamz/tusk/internal/behavior/workflow"`.

- [ ] **Step 5: Apply the same pattern to `registerNodeCreateTool`**

The Create path also needs the per-call warnings buffer + structured workflow rejection. Replicate the rejection translation; recovery doesn't fire on Create (no `before`), so the success path doesn't need a warnings parser — but for symmetry, emit an empty `warnings` field is acceptable. Simplest form:

```go
if createErr != nil {
	var workflowErr *workflow.Error

	if errors.As(createErr, &workflowErr) {
		return toolJSONError(map[string]any{ /* same shape as Modify */ }), nil
	}

	return toolError(createErr), nil
}
```

- [ ] **Step 6: Wire the drift repo into `registerDoctorTool` (or whatever the existing doctor tool is named in Plan 6's tools.go)**

Find the doctor handler and add `WorkflowDrift: srv.runtime.WorkflowDrift` to its `doctor.Run(doctor.Config{...})` config. The existing renderer surfaces every Issue regardless of Kind, so this is one line of plumbing.

- [ ] **Step 7: Run, verify pass**

```bash
go test ./internal/mcp/... -v
```

Expected: every existing test PASS plus the two new ones.

- [ ] **Step 8: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/tools_test.go
git commit -m "feat(mcp): tools.go structured workflow rejection + recovery warnings"
```

---

## Task 22: End-to-end smoke + plan doc commit

**Files:** none new.

- [ ] **Step 1: Run the full unit + race test suite**

```bash
make test
make test-race
```

Expected: every test PASS in both runs. If anything fails, debug + fix before claiming Plan 7 done.

- [ ] **Step 2: Run lint + vet**

```bash
make lint
make vet
```

Expected: clean.

- [ ] **Step 3: Run formatter**

```bash
make fmt
```

Expected: no diff.

- [ ] **Step 4: End-to-end CLI smoke**

```bash
mkdir -p /tmp/plan-7-smoke
cd /tmp/plan-7-smoke
cat > tusk.toml <<'EOF'
[workspace]
name = "smoke"

[behaviors.workflow.tickets]
applies-to = ["ticket"]
states = [
  { name = "pending", initial = true },
  { name = "active" },
  { name = "completed", terminal = true, done = true },
]
transitions = [
  { from = "pending", to = "active" },
  { from = "active", to = "completed" },
]
EOF

# Create node with initial status
/workspaces/tusk/bin/tusk node create --type ticket --prop status=pending tickets/foo
# Legal transition
/workspaces/tusk/bin/tusk node modify tickets/foo --prop status=active
# Illegal transition (should fail)
/workspaces/tusk/bin/tusk node modify tickets/foo --prop status=pending && echo UNEXPECTED
# Already valid; doctor should be empty.
/workspaces/tusk/bin/tusk doctor
```

Expected: create + first modify succeed; second modify (active → pending) is legal per our manifest, so it succeeds. Use the smoke output to sanity-check the rendered messages.

For an off-schema smoke:

```bash
cat > tickets/bar.md <<'EOF'
---
type: ticket
status: bogus
---
body
EOF
/workspaces/tusk/bin/tusk reindex
/workspaces/tusk/bin/tusk doctor
```

Expected: reindex summary mentions a workflow-violation; doctor renders one row.

- [ ] **Step 5: Commit the plan doc**

The plan doc itself lands as its own commit alongside the implementation commits, per the established stack convention:

```bash
git add docs/superpowers/plans/2026-05-06-tusk-v1-7-behavior-packs.md
git commit -m "docs(plan): plan 7 — behavior packs"
```

Note: this commit may have already happened earlier in the workflow (the plan doc is typically created on the branch before any implementation begins). If so, this step is a no-op.

- [ ] **Step 6: Verify the branch is clean and ready for PR**

```bash
git status
git log --oneline v1..feat/plan-7
```

Expected: empty `git status`; the log shows the spec, the plan, and ~20 implementation commits, in order.

- [ ] **Step 7: Open a draft PR**

```bash
gh pr create --base v1 --head feat/plan-7 --draft \
  --title "feat(v1): plan 7 — behavior packs" \
  --body "$(cat <<'EOF'
## Summary
- Behavior-pack engine + workflow pack (v1's reference implementation).
- Manifest grows `[behaviors.<kind>.<instance>]` with deferred-decode.
- `node.Service.Create` / `Modify` fire hooks; `reindex` runs the validator in warn mode.
- `tusk doctor` surfaces a new `workflow-violation` Issue from a persisted drift table.
- MCP `tusk_node_modify` / `tusk_node_create` return structured workflow rejections; success payload grows a `warnings` field on recovery.

## Spec
- `docs/superpowers/specs/2026-05-06-tusk-v1-behavior-packs-design.md`

## Test plan
- [ ] `make test` and `make test-race` are green
- [ ] `tusk node modify` rejects illegal transitions; output matches the spec §7.1 examples
- [ ] `tusk node modify` from an off-schema status emits a stderr warning and persists drift
- [ ] `tusk doctor` shows the workflow-violation Issue
- [ ] `tusk reindex` summary includes the workflow-violation count
- [ ] MCP `tusk_node_modify` returns the structured rejection JSON shape from spec §7.2
EOF
)"
```

---

## Self-Review Notes

**1. Spec coverage.** Every section of the sub-spec maps to at least one Task:

| Spec §  | Task(s) |
|---|---|
| §1 Goal & Scope | All — Plan 7 implements the full in-scope list |
| §2 Hook surface | T1 (hooks.go) + T2 (Hooks/Pack/Recovery) + T3 (Engine + simple Fire) + T4 (recovery-aware Fire) |
| §3 Pack/Instance/Registry | T2 (Kind/Instance/ReservedKey) + T5 (Registry + collision) |
| §4 Manifest schema | T5 step 3 (Behaviors field) + T9 (loader meta + structural validation) |
| §5 Workflow validator | T6 (Error/RecoveredError) + T7 (Kind + decoder) + T8 (instance + algorithm) |
| §6.1 Create | T12 |
| §6.2 Modify | T13 |
| §6.3 Recovery-aware Fire | T4 + T13 (consumer side) |
| §6.4 Reindex warn-mode | T14 |
| §6.5 Drift table | T10 |
| §6.6 Edge ordering | T13 (diffEdgeSets + ordering: removed before added) |
| §7.1 CLI rendering | T17 (modify) + T18 (create) + T19 (reindex/doctor) |
| §7.2 MCP rendering | T20 (runtime) + T21 (tools structured rejection + warnings) |
| §7.3 Doctor rendering | T15 + T19 |
| §7.4 Reindex summary | T19 |
| §8 Testing strategy | tests in every Task |
| §9 Open questions | covered as documentation, not tasks |
| §10 Plan 7.b ledger | covered as documentation, not tasks |

**2. Placeholder scan.** No `TBD` / `TODO` / `FIXME` markers. Every Step shows complete code or an explicit instruction to mirror an existing helper. The MCP recovery-warning parser is a v1 expediency (§10 ledger #11 reserves space for a structured warning channel) — flagged inline in Task 21.

**3. Type / method consistency:**
- `behavior.Hooks` struct field names match across T2 declaration and T8 usage in `instance.Hooks()`.
- `behavior.RecoveredEvent` field names match across T2 declaration, T4 usage in `FireResult.Recovered`, and T13 usage in Modify.
- `workflow.Error` field names match across T6 declaration and T8 (instance constructs them) and T21 (tools.go consumes them).
- `index.WorkflowDriftRow` field names match across T10 declaration, T13 (Modify writes), T14 (reindex writes), T15 (doctor reads).
- `node.Service` constructor signature `NewServiceWithBehaviors` matches across T11 declaration and T12/T13 usage and T17/T18/T20 callers.
- `behavior.Kind.NewInstance` signature `(instanceName string, raw toml.Primitive, meta *toml.MetaData) (Instance, error)` matches across T2 declaration, T7 implementation, and T5 registry usage.
- `manifest.Manifest.Behaviors` field type `map[string]map[string]toml.Primitive` matches across T5 (declaration) and T7/T9 (usage).

**4. Bundle assignment** (one implementer subagent per bundle):

| Bundle | Tasks | Theme |
|---|---|---|
| 1 | T1–T5 | `internal/behavior` skeleton: types, engine, registry |
| 2 | T6–T8 | `internal/behavior/workflow` pack |
| 3 | T9–T10 | manifest + drift table foundation |
| 4 | T11–T13 | `node.Service` integration |
| 5 | T14–T15 | reindex + doctor wiring |
| 6 | T16–T22 | CLI + MCP + e2e + final smoke |

Bundle 6 is wider than Bundles 1–5 because it touches more surface points; the implementer subagent for Bundle 6 should expect a larger PR-style review burden than other bundles.








