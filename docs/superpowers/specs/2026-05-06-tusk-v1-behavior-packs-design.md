# Tusk v1 — Behavior Packs Design (Plan 7 sub-spec)

- **Status:** Draft
- **Date:** 2026-05-06
- **Author:** German Meza
- **Scope:** Implementation-shaped design for the behavior-pack engine and the workflow pack referenced by §8 and punted to a separate pass by §13.2 of the v1 rebuild design.
- **Successor of:** the brainstorm dialogue captured during Plan 7 setup.

This document is a sub-spec of `2026-05-05-tusk-v1-rebuild-design.md`. It refines §8 (behavior packs) and §13.2 (open question on the hook surface) into a concrete implementation plan: hook primitives, dispatch semantics, pack and registry contracts, manifest schema, workflow validator behavior, write-path integration, error and warning surfaces, and testing strategy. The plan doc that follows (`2026-05-06-tusk-v1-7-behavior-packs.md`) implements this sub-spec.

---

## 1. Goal & Scope

Plan 7 lands the **behavior-pack engine** — the long-term composition primitive for per-write effects — plus the **workflow pack** as v1's reference implementation. The engine is shipped as a fixed surface of eight primitive hook slots; future behaviors (inside the binary in v1, plugin-loaded in v2+) compose against these primitives without changing the engine.

### 1.1 In scope

- A new `internal/behavior/` package: hook primitive types, the `Hooks` struct, the `Pack` / `Instance` / `Registry` / `Engine` contracts, and the dispatch loop. Eight registration slots — `OnNodeWriteValidate`, `OnNodeWriteAfter`, `OnNodeReadValidate`, `OnNodeReadAfter`, `OnEdgeAddValidate`, `OnEdgeAddAfter`, `OnEdgeRemoveValidate`, `OnEdgeRemoveAfter`. Validate phases short-circuit on first rejection; After phases fan out unconditionally.
- A new `internal/behavior/workflow/` package: a declarative state-machine pack registered as the only built-in `Kind` in v1. Manifest decoder, validator, and `Hooks()` filling the `OnNodeWriteValidate` slot.
- Manifest grows a top-level `[behaviors]` section with two-level subkeys: `[behaviors.<kind>.<instance>]`. Multi-instance is the v1 schema (§4); single-instance manifests are the natural one-entry case.
- Engine integration in `node.Service.Create` and `node.Service.Modify` — firing both node and edge hooks; rejecting writes on Validate-phase errors before any file or index write.
- Engine integration in `reindex.Run` — firing `OnNodeWriteValidate` in **warn mode**: rejections are captured into a persisted drift surface rather than aborting the indexing.
- Status-property reservation: a workflow Instance reserves `(node-type, status-property)` pairs. The engine refuses to start when two Instances reserve the same pair. Detected at manifest-load time.
- Orphan-status recovery: when the previous status isn't a declared state, Modify allows a transition into any declared state, emits a stderr warning, and records a drift entry. `tusk doctor` and `tusk reindex` see the same drift entries through a shared SQLite table.
- `OnNodeRead*` slots are **defined but not fired** in v1. Reservation only — the firing site lands when v1.x has a consumer.
- No new CLI commands: workflow enforcement is plumbed through the existing `tusk node modify --prop status=...` and the MCP `tusk_node_modify` tool.

### 1.2 Out of scope (Plan 7.b ledger)

Each item below is captured in the consolidated ledger in §10 with a one-line rationale. Briefly:

- `[node-types.<name>]` declarations (and the property-type validator that comes with them).
- Built-in type packs (`kanban`, `vault`, `tags`) and the ergonomic shortcuts they ship.
- Runtime activation of `auto-complete-parent` / `auto-revert-parent` (Plan 7 reserves the schema only).
- `+tag` / `-tag` filter shorthand (originally deferred from Plan 4).
- User-defined / out-of-tree behavior packs.
- Re-entrant write support for after-phase cascades.

## 2. Hook Primitives and Dispatch

### 2.1 The eight slots

The hook surface is **fixed**. v1 freezes four primitives (`NodeWrite`, `NodeRead`, `EdgeAdd`, `EdgeRemove`) each with two phases (`Validate`, `After`), totaling eight registration slots. Future behaviors — including cascading ones such as `auto-complete-parent` — compose by registering handlers on these primitives; they do not introduce new primitives.

| Primitive | Validate phase | After phase |
|---|---|---|
| `NodeWrite` | Pre-write check; non-nil error rejects. Receives `before` (nil on create) and `after` (nil on delete). | Post-commit reactor; return value ignored for control flow. |
| `NodeRead` | Reserved in v1; no firing site. | Reserved in v1; no firing site. |
| `EdgeAdd` | Pre-commit check; runs once per edge row produced by a write. | Post-commit reactor; runs once per edge row added. |
| `EdgeRemove` | Pre-commit check; runs once per edge row removed by a write. | Post-commit reactor; runs once per edge row removed. |

`Validate` handlers are pure: no I/O, no graph reads. `After` handlers may read the index but **must not write** in v1; the engine fires them inside the workspace lock and the lock is non-reentrant. This constraint is documented in §10 and is the precondition for activating cascading behaviors in v1.x.

### 2.2 The `Hooks` struct

A pack instance describes its registered handlers by returning a `Hooks` struct:

```go
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
```

A `nil` slot means the instance does not register on that hook. The workflow pack instance fills only `OnNodeWriteValidate`; the other seven slots stay nil.

Handler signatures are slot-specific but uniform in shape: each takes a small `HookContext` (carrying at least the pack-instance name and pack-kind name) plus the data relevant to the slot — `before` and `after` Node values for node writes, a single Node snapshot for node reads, an EdgeRow for edge add/remove. Validate handlers return an `error`; After handlers also return an `error` but its value affects only telemetry (no write rejection from the After phase).

### 2.3 The `Engine` and dispatch

The `Engine` owns eight handler chains, one per slot, populated at manifest-load time and immutable thereafter. `runtime.ReloadManifest` rebuilds the Engine from scratch.

For each slot the engine exposes a `Fire*` method. The semantics differ by phase:

- **Validate-phase fire**: walks the chain in registration order. The first non-nil error short-circuits and is returned to the caller along with the registering instance's qualified name (`<kind>.<instance>`, e.g. `workflow.tickets`). Tests can introspect which instance rejected.
- **After-phase fire**: walks the chain in registration order, runs every handler regardless of return values, and aggregates non-nil errors into a multi-error suitable for telemetry. Control flow is unaffected by After-phase errors.

The workflow pack's orphan-state recovery path needs a richer Validate-phase variant — see §6.3.

### 2.4 Why fire edge hooks for derived edges

Edges enter the system from two paths: implicitly, via `flattenEdges` during a node write; or explicitly, via `tusk edge add` and its MCP equivalent. From a pack's perspective, "an edge was added" is one event. The engine fires `OnEdgeAdd*` in both paths so that future packs (e.g., a hypothetical cycle-detector behavior) need only register once. The workflow pack registers nothing on edge slots, so v1 pays no overhead.

## 3. Pack Interface, Configuration, and Registration

### 3.1 Kind and Instance

A pack **kind** is a Go type that knows how to interpret its TOML config and produce instances. A pack **instance** is one configured form of a kind — produced by parsing one TOML subtable like `[behaviors.workflow.tickets]`.

```go
type Kind interface {
    Name() string                                              // "workflow"
    NewInstance(instanceName string, raw toml.Primitive) (Instance, error)
}

type Instance interface {
    Name() string                  // instance name, e.g. "tickets"
    Kind() string                  // delegate to its Kind.Name()
    Hooks() Hooks                  // handler slots
    ReservedKeys() []ReservedKey   // (node-type, property) pairs this instance owns
}

type ReservedKey struct {
    NodeType string  // e.g. "ticket"; "*" wildcard not supported in v1
    Property string  // e.g. "status"
}
```

`NewInstance` performs all kind-specific validation (state-machine well-formedness, transition references, etc.). Errors at this stage abort engine construction and surface as manifest-load errors.

### 3.2 Registry

A `Registry` maps kind names to constructors. v1 exposes one method to register a kind, one to look it up by name, and one to consume a fully-loaded `Manifest` and produce a ready-to-dispatch `Engine`.

Registration is **explicit**: there are no `init()` side effects. Both the CLI commands (`cmd/tusk/`) and the MCP runtime (`internal/mcp/`) register the workflow kind through the same helper, so tests can construct a Registry from scratch and exercise the engine in isolation.

When the Registry resolves the manifest, it iterates `[behaviors.<kind>.<instance>]` entries, calls the matching `Kind.NewInstance` for each, collects the resulting Instances, builds the `(node-type, property) → instance-name` reservation map, and rejects duplicates with a clear message naming both colliding instances.

### 3.3 The `NodeService` constructor

Plan 7 adds a new constructor `NewServiceWithBehaviors` that takes the existing dependencies plus a `*behavior.Engine`. The pre-Plan-7 constructors stay in place for the test code that predates behavior-pack work; production callers (`cmd/tusk/`, `internal/mcp/runtime.go`) migrate to the new constructor.

The Service uses the engine for dispatch only — it does not read the engine's reservation map or its registered packs directly. All workflow-specific logic lives inside the workflow pack; the Service is pack-agnostic.

### 3.4 Reservation collision detection

Two instances both reserving the same `(NodeType, Property)` pair is a hard error at manifest load:

```
manifest: behaviors.workflow.tickets and behaviors.workflow.alt-tickets both
reserve property "status" on type "ticket"; only one workflow may govern a
(type, property) pair
```

The same property on a different type is fine. A different property on the same type is fine. The constraint is *one workflow per (type, property)*.

## 4. Manifest Schema

### 4.1 Section shape

```toml
[behaviors.workflow.tickets]
applies-to       = ["ticket", "task"]
status-property  = "status"
states = [
    { name = "pending",   initial = true },
    { name = "active",    start = true },
    { name = "completed", terminal = true, done = true },
]
transitions = [
    { from = "pending",   to = "active" },
    { from = "active",    to = "completed" },
    { from = "active",    to = "pending" },
    { from = "completed", to = "pending" },
]
auto-complete-parent = false        # forward-compat; ignored in v1
auto-revert-parent   = false        # forward-compat; ignored in v1

[behaviors.workflow.bugs]
applies-to       = ["bug"]
status-property  = "stage"
states = [
    { name = "triage", initial = true },
    { name = "fixing" },
    { name = "verified", terminal = true, done = true },
]
transitions = [
    { from = "triage",  to = "fixing"   },
    { from = "fixing",  to = "verified" },
]
```

Two instances of the same kind, governing different types with different state machines, different status-property names, and different transition tables.

### 4.2 Manifest-side decode (deferred-decode contract)

The `manifest` package decodes `[behaviors.<kind>.<instance>]` only as far as a two-level map — outer key is the kind name, inner key is the instance name, value is the raw, undecoded TOML table. The kind-specific schema lives inside the pack package, which decodes the raw table when its `NewInstance` is called.

This is the **deferred-decode contract**. Its consequences:

- Adding a new pack kind never modifies `internal/manifest`.
- Manifest-load-time errors for a given pack become "bad behavior config" errors surfaced at `BuildEngine` time, not at `Load` time. A workspace with a syntactically valid TOML file but an invalid workflow config still passes `manifest.Load`; the failure surfaces when the Engine is built.
- Tests for the manifest package and for the workflow pack stay independent.

The manifest package validates only that `[behaviors.*.*]` is a table at the right nesting depth and that kind/instance names are non-empty. Anything else is the kind's responsibility.

### 4.3 Workflow-specific schema rules

The workflow pack's `NewInstance` rejects a configuration when:

- `applies-to` is missing or empty, or any element is not a non-empty string.
- `status-property` is the empty string. Absent → default `"status"`.
- `states` is missing or empty.
- Any `state.name` is empty or duplicates another state's name.
- More than one state has `initial = true`. Same for `start` (so the kanban-pack ergonomic `tusk ticket start` resolves to a single state).
- A state has `done = true` and `terminal = false`. (`done` without `terminal` has no v1 meaning; the spec calls them separate roles but enforces the implication for clarity.)
- `transitions` references an undeclared `from` or `to` state.
- Two transitions share the same `(from, to)` pair.

The decoder accepts `auto-complete-parent` and `auto-revert-parent` as booleans, stores them on the Instance, but the runtime validator never reads them. When either is `true`, the Engine's construction emits a single stderr notice — `workflow: auto-* directives accepted but not yet active` — so users don't silently rely on inactive cascades.

## 5. Workflow Validator

### 5.1 Internal model

After `NewInstance`, a workflow Instance holds:

- The set of types it governs (`applies-to` materialized as a hash set).
- The configured status-property name.
- A map from state name to its declared roles (`initial`, `start`, `terminal`, `done`).
- A set of declared `(from, to)` transitions.
- The two `auto-*` booleans (stored, unused in v1).

`Hooks()` returns a `Hooks` value with `OnNodeWriteValidate` filled and the other seven slots `nil`.

### 5.2 Validation algorithm (informally)

The validator runs on every node write the engine sends to its `OnNodeWriteValidate` slot. The decision tree:

1. **Type filter.** If the node's `type` is not in `applies-to`, return success — this Instance has nothing to say about this write.

2. **Read both sides.** Read the value at `status-property` on `before` and `after`. Coerce to string; absent or empty → empty string.

3. **Both sides empty.** No status involvement — return success.

4. **Setting status for the first time** (`before` empty, `after` non-empty). The target must be a declared state. If at least one state has `initial = true`, the target must be one of those; if no state has `initial`, any declared state is allowed. Otherwise return an `Error` with code `non-initial-on-create` or `unknown-target-state`.

5. **Unsetting status** (`before` non-empty, `after` empty). Reject with code `cannot-unset-status`. The workflow pack owns the property; the user must edit the file directly to recover (after which `tusk doctor` will surface the now-missing field as a separate concern).

6. **Orphan-state recovery** (`before` is non-empty but not in the declared states). If `after` is also not declared, return an `Error` with code `unknown-target-state`. If `after` is declared, return a `RecoveredError` — a sentinel that the engine treats as success-with-information rather than a rejection. The Modify path catches it and surfaces a stderr warning + a drift entry; reindex catches it and writes a drift entry.

7. **No-op transition** (`before == after`). Always allowed — no need for a self-transition row in the table.

8. **Normal transition** (`before` and `after` both non-empty, `before` is a declared state, `before != after`). First check that `after` is a declared state — if not, return an `Error` with code `unknown-target-state` and a `KnownStates` field. Otherwise, if `(before, after)` is in the transition table, return success; if not, return an `Error` with code `illegal-transition` and a `ValidTargets` field listing legal next states from the current state.

The validator is pure. It reads only the `before` and `after` Node values and the configuration captured at `NewInstance`. No graph reads, no I/O.

### 5.3 The two error types

The workflow pack returns one of two typed errors:

- **`workflow.Error`** — outright rejection. Carries an `ErrorCode` (one of `illegal-transition`, `unknown-target-state`, `non-initial-on-create`, `cannot-unset-status`), the property name, the `from` and `to` strings, and (where relevant) a `ValidTargets` slice or a `KnownStates` slice. The Modify path returns this to the caller; reindex captures it as a drift row.
- **`workflow.RecoveredError`** — orphan-state recovery, treated as success-with-information. Carries `from`, `to`, property, and the pack-instance name. The Modify path emits a stderr warning and writes a drift row; reindex writes a drift row.

The Engine's recovery-aware Validate variant accumulates `RecoveredError`s into a separate slice rather than returning them as errors — see §6.3.

### 5.4 Roles in v1

| Role | v1 effect | Plan 7.b activation |
|---|---|---|
| `initial` | Enforced: setting status for the first time must use an `initial` state when at least one is declared. | — |
| `start` | Stored, not read by the validator. | Read by ergonomic shortcuts (`tusk ticket start`) when the kanban pack lands. |
| `terminal` | Stored, not read by the validator. | Read by `auto-complete-parent` cascades and ergonomic shortcuts. |
| `done` | Stored, not read by the validator. Implies `terminal` at decode time. | Same as `terminal`. |

## 6. Engine Integration

### 6.1 `node.Service.Create`

After the existing parse, edge resolution, edge validation, and cycle-detection steps complete, the Service fires `OnNodeWriteValidate` with `before = nil` and `after = the parsed node`. A non-nil error aborts the create before the file is written.

The Service then computes the edge rows (`flattenEdges`) and fires `OnEdgeAddValidate` once per row. Any rejection aborts.

After the file write + node row upsert + edge row upsert + embed enqueue, the Service fires `OnNodeWriteAfter` and `OnEdgeAddAfter` for every new edge row. After-phase errors are aggregated into a telemetry multi-error and otherwise ignored.

`Create` is the simpler dispatch path: there is no `before`, so orphan-state recovery cannot apply.

### 6.2 `node.Service.Modify`

Modify is the workflow pack's primary path and the source of orphan-state recovery. The dispatch sequence:

1. Parse the on-disk node into `before`. Apply `SetProps` / `UnsetKeys` / `Body` to produce `after`.
2. Run **recovery-aware** Validate-phase fire on the node write (§6.3). A true error aborts; recovered events are kept on the side.
3. Diff the edge sets between `before` and `after`. Fire `OnEdgeRemoveValidate` for each removed row first, then `OnEdgeAddValidate` for each new row. Any rejection aborts before the file is written.
4. Atomic-write the file. Upsert the node row and edge rows. Enqueue the re-embed.
5. Fire `OnNodeWriteAfter`, `OnEdgeRemoveAfter`, and `OnEdgeAddAfter` (in that order, mirroring the Validate sequence).
6. For each recovered event captured in step 2: emit a stderr warning to the Service's configured `io.Writer` (defaults to `os.Stderr`; tests inject a buffer); append a row to the drift log.

### 6.3 Recovery-aware Validate fire

Recovery is a chain feature, not a per-handler concern. The Engine exposes two variants of the node-write Validate fire:

- **Simple variant.** Returns either success or a single rejection (handler name + error). Used by `Create`.
- **Recovery-aware variant.** Returns a result object carrying an optional rejection plus a slice of `RecoveredEvent`s accumulated across the chain. Used by `Modify` and by `reindex`.

In the recovery-aware variant the chain semantics are:

- A `*workflow.Error` (or any error other than `*workflow.RecoveredError`) short-circuits the chain.
- A `*workflow.RecoveredError` appends a `RecoveredEvent` and continues.
- Nil continues.

This keeps `Create`'s call site simple while giving `Modify` and `reindex` the information they need to surface drift without aborting.

### 6.4 `reindex.Run` (warn mode)

Reindex sees the on-disk state only — there is no `before`. The engine's recovery-aware Validate fire still runs with `before = nil`; the validator's "setting status for the first time" branch (step 4 in §5.2) is what naturally applies.

For each indexed file:

- If the Validate fire returns a `*workflow.Error`, **reindex does not abort indexing the file**. Instead, the reindex captures the error as a drift row and proceeds to upsert the node.
- If the Validate fire returns recovered events (only possible when the validator runs with a non-nil `before`, which never happens during reindex — but the surface is uniform), they are appended to the drift log.
- Reindex *clears* a node's drift rows on a clean Validate pass: status is in the declared states and no recovery happened.
- Reindex's summary line gains a count of workflow violations. Exit code stays 0; drift is informational, not a reindex failure.

> **Note.** Because reindex's `before` is always `nil`, reindex cannot validate transition legality — only state legality. The Plan 7.b ledger captures this as an item to revisit when v1.x adds a journaled write log.

### 6.5 Drift log surface

A new SQLite table `workflow_drift` carries the persisted observations:

```sql
CREATE TABLE workflow_drift (
    node_id          TEXT NOT NULL,
    pack_instance    TEXT NOT NULL,
    pack_kind        TEXT NOT NULL,
    observed_status  TEXT NOT NULL,
    property         TEXT NOT NULL,
    observed_at      INTEGER NOT NULL,
    PRIMARY KEY (node_id, pack_instance, observed_status)
);
```

A new repository in `internal/index` provides append (idempotent on the primary key), list, and clear-for-node operations. Both Modify (recovery path) and reindex (rejection or recovery path) write to it. Both clear a node's rows on a clean pass. `tusk doctor` reads the table and surfaces a `workflow-violation` Issue per row.

The reason this is a persisted table rather than an on-the-fly check inside `tusk doctor`: drift events happen at write time (Modify) or batch time (reindex), and `tusk doctor` is a read-only command. Persisting makes drift observable without requiring `tusk doctor` to re-run validation.

### 6.6 Edge ordering during Modify

When Modify replaces edges (e.g., a frontmatter `parent:` is changed), the engine fires `OnEdgeRemoveValidate` / `OnEdgeRemoveAfter` for the old edge before `OnEdgeAddValidate` / `OnEdgeAddAfter` for the new one. A behavior pack that needs ordering invariants gets a clean "tear down old, build new" sequence.

## 7. Error and Warning Surfaces

### 7.1 CLI rendering

The CLI commands wrap the underlying error with workspace context but otherwise pass through the workflow error's human-readable message. Examples:

```
$ tusk node modify tickets/foo --prop status=donee
Error: behavior "workflow.tickets" rejected modify: workflow "tickets": "donee" is not a declared state for property "status"
  declared states: pending, active, completed
```

```
$ tusk node modify tickets/foo --prop status=active     # before is "blocked" (orphan)
warning: workflow "tickets" recovered from unknown status "blocked" → "active" on tickets/foo
  transition not validated; surfaces as a workflow-violation in tusk doctor
Modified tickets/foo
```

The warning goes to **stderr**; the `Modified ...` success line stays on stdout. The Service's `io.Writer` for warnings defaults to `os.Stderr` and is injectable for tests.

### 7.2 MCP rendering

The MCP `tusk_node_modify` tool returns a structured tool error when the workflow pack rejects:

```json
{
  "isError": true,
  "error": "workflow-rejection",
  "code": "illegal-transition",
  "message": "workflow \"tickets\": cannot transition status \"active\" → \"pending\"",
  "property": "status",
  "from": "active",
  "to": "pending",
  "valid_targets": ["completed"],
  "pack_instance": "tickets"
}
```

`valid_targets` and `known_states` are omitted when empty. Other rejection codes follow the same shape.

Recovery is informational, not an error result. The success payload grows an optional `warnings` field:

```json
{
  "id": "tickets/foo",
  "type": "ticket",
  "path": "tickets/foo.md",
  "title": "Fix login bug",
  "properties": {"...": "..."},
  "warnings": [
    {
      "kind": "workflow-recovered",
      "pack_instance": "tickets",
      "from": "blocked",
      "to": "active",
      "property": "status",
      "message": "workflow \"tickets\" recovered from unknown status \"blocked\" → \"active\"; transition not validated"
    }
  ]
}
```

Empty/missing `warnings` field on a clean transition. Future warning kinds use the same envelope.

### 7.3 `tusk doctor` rendering

`internal/doctor` adds a new constant for the Issue kind and reads the `workflow_drift` table. Each drift row materializes one `Issue{Kind: "workflow-violation", NodeID, Message}`. The default text rendering keeps the existing `Kind | NodeID | Message` table; the JSON mode emits the same fields.

### 7.4 `tusk reindex` summary

The reindex summary line gains a violation count and a hint:

```
$ tusk reindex
Indexed 142 nodes (3 workflow-violations) in 240ms
Run `tusk doctor` to inspect violations
```

Exit code stays 0 even when drift is non-empty.

## 8. Testing Strategy

The plan doc names a per-package test surface. The design here defines what must be exercised; harness specifics are an implementer concern.

**`internal/behavior`.** Engine-level tests using a small in-test fake pack:

- Registry: duplicate kind names rejected; missing kind rejected at `BuildEngine`; reservation-collision rejection naming both colliding instances.
- Engine chain dispatch: registration order preserved; Validate short-circuits on first rejection; After fans out unconditionally; After-phase errors aggregated.
- Recovery-aware Validate variant: short-circuits on `*workflow.Error`-shaped errors; appends `RecoveredEvent`s on `*workflow.RecoveredError`-shaped sentinel and continues.

**`internal/behavior/workflow`.** Pack-level tests:

- Decoder coverage: every rule in §4.3, including the `done`-implies-`terminal` invariant and the auto-* notice path.
- Validator coverage: every branch in §5.2 — type-filter no-ops, both-sides-empty, set-from-empty (with and without `initial` declared), set-from-empty unknown target, unset rejected, orphan recovery (target declared and undeclared), no-op self-transition, illegal transition with `ValidTargets`, unknown target with `KnownStates`.

**`internal/manifest`.** Loader-level tests for the deferred-decode contract: well-formed multi-instance, missing kind name, missing instance name, non-table value at the wrong nesting depth. Verify that kind-specific decode is *not* attempted at manifest-load time.

**`internal/index/workflow_drift_repo`.** Idempotency on the primary key; deterministic `ListAll` ordering; `ClearForNode` semantics.

**`internal/doctor`.** Fixture seeds drift rows; assert the new `workflow-violation` Issue surfaces in the report.

**`internal/reindex`.** Fixture with mixed legal/orphan/illegal statuses on tickets; assert drift rows for off-schema cases while every node still upserts; summary count matches.

**`cmd/tusk/`.** End-to-end through workspace fixtures with a configured workflow pack:

- `cmd_node_modify_test.go`: legal transition (success on stdout); illegal transition (error to stderr; nothing committed); orphan recovery (warning to stderr + drift row visible to a follow-up `tusk doctor`); attempted unset rejected.
- `cmd_node_create_test.go`: non-initial-on-create rejection.
- `cmd_doctor_test.go`: seeded drift row appears in rendered output.
- `cmd_reindex_test.go`: off-schema status produces a drift row; summary count correct; exit code 0.

**`internal/mcp/`.** Tools-level tests: structured workflow-rejection tool error shape; success-with-`warnings` shape.

**Race detector.** `make test-race` covers the writes; behavior dispatch is purely synchronous, so no concurrency-specific tests are needed.

## 9. Open Questions / Residuals

Items the design accepts as known and either scoped-in or scoped-out, that future contributors should know about.

1. **Manifest reload during MCP runtime.** `runtime.ReloadManifest` rebuilds the engine from scratch. If a long-running session has registered drift rows under a since-removed instance name, those rows remain visible to `tusk doctor` until the affected node is re-touched. Acceptable for v1 — drift rows are observations of past state, and the user can run `tusk reindex` to recompute.

2. **Behavior dispatch and the workspace lock.** Hooks fire inside the existing per-write workspace lock. Validate-phase handlers must remain pure (no I/O, no graph reads); After-phase handlers may read the index but must not write — the lock is non-reentrant. When v1.x adds a journaled write log (Plan 7.b ledger #12), this constraint can be revisited.

3. **Implicit edge ordering during Modify.** Removes fire before adds. Documented in §6.6 so pack authors don't rely on the opposite ordering.

4. **Drift surfaces by `(node_id, pack_instance, observed_status)`.** A node that bounced between two off-schema statuses over time will have two drift rows. `tusk doctor` shows both — intended for v1, reflects observed history.

5. **No CLI command to clear drift manually.** Drift rows are cleared automatically on a clean Modify or reindex pass. A user who wants to suppress a drift row edits the file to a legal status and reindexes. No `tusk doctor --clear` or equivalent in v1.

6. **Reindex cannot validate transitions, only states.** Reindex sees only on-disk state; `before = nil`. Captured as Plan 7.b ledger #10 for the journaled-write-log activation.

## 10. Plan 7.b Ledger (Deferred Items)

| # | Deferred item | Rationale |
|---|---|---|
| 1 | `[node-types.<name>]` manifest section + property declarations | Coherent separate concern with its own validation rules and built-in pack composition; out of Plan 7's six-bundle rhythm. |
| 2 | Built-in type packs (`kanban`, `vault`, `tags`) and ergonomic shortcuts (`tusk ticket start`, `tusk note new`, etc.) | Land alongside the type-packs work in #1; the workflow pack provides the engine they compose against. |
| 3 | Runtime activation of `auto-complete-parent` / `auto-revert-parent` | Schema reserved in Plan 7 (§4.1); cascade implementation depends on #12. |
| 4 | `+tag` / `-tag` filter shorthand | Originally deferred from Plan 4; the tag pack owns the path template. |
| 5 | User-defined / out-of-tree behavior packs | v2+; the in-tree registration model and the on-disk pack format are separate concerns. |
| 6 | `OnNodeRead*` firing site (`Service.Get` / `List`) | Forward-compat surface only; no v1 consumer. |
| 7 | `[node-types]` reservation collision extension | Pairs with #1 — pack-reserved keys must not collide with declared property keys either. |
| 8 | Type-pack overrides merge logic (`[type-packs.kanban.workflow]` from rebuild design §7.1) | Pairs with #2 — composing pack-shipped defaults with workspace overrides. |
| 9 | `start` / `terminal` / `done` role activation in the validator | Read by ergonomic shortcuts (#2) and cascade reactors (#3). v1 stores them, doesn't read them. |
| 10 | Reindex transition replay via journaled write log | Reindex sees only on-disk state; can't validate transitions. |
| 11 | `tusk_doctor` MCP tool surfacing the `workflow-violation` Issue kind | Doctor is CLI-only today; MCP doctor is a separate item. |
| 12 | Re-entrant write support for after-phase cascades | Required before #3 is safe to wire — the workspace lock is non-reentrant in v1. |
| 13 | Drift surface dedup / most-recent-only rendering | Polish on doctor output noise when a node has bounced through multiple off-schema statuses. |
