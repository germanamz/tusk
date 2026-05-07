# Tusk v1 — Node-Types Design (Plan 7.b sub-spec)

- **Status:** Draft
- **Date:** 2026-05-07
- **Author:** German Meza
- **Scope:** Implementation-shaped design for the `[node-types]` manifest section and property-type validation, plus the engine-build-time collision detector extension. This is the first focused follow-up to Plan 7's behavior-pack work, addressing ledger items #1 and #7 from `2026-05-06-tusk-v1-behavior-packs-design.md` §10.
- **Successor of:** the brainstorm dialogue captured during Plan 7.b setup.

This document is a sub-spec of `2026-05-05-tusk-v1-rebuild-design.md` (the v1 rebuild design). It refines §7.1 layer 2 (inline custom node types) and §7.3 (property types) into a concrete implementation plan: manifest schema, validator contracts, write-path integration, drift surface, error and warning rendering, and testing strategy. The plan doc that follows (`2026-05-07-tusk-v1-7b-node-types.md`) implements this sub-spec.

---

## 1. Goal & Scope

Plan 7.b lands the **`[node-types]` manifest section** and **property-type validation** as the structural-schema layer described in v1 design spec §7.1 layer 2, and extends the behavior-pack collision detector from Plan 7 to cover declared properties versus behavior-reserved keys (v1 spec §13.2 → Plan 7 ledger #7). Plan 7.b does not ship the built-in type packs (`kanban`, `vault`, `tags`) that will consume this layer; those land in Plan 7.c alongside the `ref` property type.

### 1.1 In scope

- A new `[node-types.<t>]` top-level manifest section. Each declared type carries an optional free-form `description` and a list of `properties` declarations.
- A property type set spanning eight scalar primitives (`string`, `int`, `float`, `bool`, `date`, `datetime`, `enum`, `markdown`) plus a single composite (`list-of`). Composite element types are scalars only — no nesting.
- A new validator in `internal/node` exposing a pure `ValidateProperties(parsed, declarations)` that returns hard errors and informational drift entries separately. Pure means no I/O, no graph reads — same discipline Plan 7's workflow validator follows.
- Integration of property validation into `node.Service.Create` and `node.Service.Modify`: hard errors abort before file write; drift entries surface a stderr warning + persist drift rows after the index commits, alongside Plan 7's recovery handling. Same insertion-point pattern.
- A new `property_drift` SQLite table and repository, parallel to Plan 7's `workflow_drift`. Modify and reindex write to it; clean validation passes clear it for the node.
- Reindex runs the same validator in warn mode: hard errors are recorded as drift rows but never abort indexing.
- Doctor reads `property_drift` and surfaces four new Issue kinds (`undeclared-property`, `type-mismatch`, `required-missing`, `enum-violation`).
- The behavior-pack collision detector grows to also reject overlap between declared properties and behavior reservations on the same `(node-type, property)` pair. Detection is engine-build-time; clear collision message names both sources.
- CLI/MCP integration plumbed through existing surfaces: structured rejection envelopes on hard errors; `warnings` array on success when undeclared-property drift fires; reindex summary line gains a property-violation count.

### 1.2 Out of scope (Plan 7.c+ ledger)

Each item below is captured in the consolidated ledger in §10 with a one-line rationale. Briefly:

- The `ref` property type and its auto-generated edge-type declarations.
- Nested `list-of(list-of(...))`.
- Built-in type packs (`kanban`, `vault`, `tags`).
- `OnNodeRead*` firing site (still no v1 consumer).
- `tusk_doctor` MCP tool surfacing the new property Issue kinds (carries over from Plan 7).
- Typed property accessors that parse on read.
- Schema-evolution polish (deprecated-property markers, `tusk node-types show`, etc.).

### 1.3 Backward compatibility

Workspaces with no `[node-types]` section behave identically to pre-Plan-7.b. Untyped nodes (those whose `type:` does not match any declared `[node-types.<t>]`) pass through validation unchanged. The schema is opt-in; existing v1 tests need only ensure the empty-section default still parses.

## 2. Manifest Schema

### 2.1 Section shape

The `[node-types]` table holds one entry per declared type. Each entry carries an optional `description` and a `properties` array of inline tables.

```toml
[node-types.ticket]
description = "A unit of trackable work"
properties = [
    { name = "title",       type = "string", required = true, description = "Short summary" },
    { name = "priority",    type = "int" },
    { name = "due",         type = "date" },
    { name = "labels",      type = "list-of", item-type = "string" },
    { name = "stages",      type = "list-of", item-type = "enum", values = ["draft", "review", "shipped"] },
    { name = "stage",       type = "enum", values = ["pending", "active", "completed"] },
]

[node-types.note]
description = "Free-form note or memo"
properties = [
    { name = "title", type = "string", required = true },
    { name = "tags",  type = "list-of", item-type = "string" },
]
```

Two types declared, with mixed scalar / list-of / enum / list-of-enum properties.

### 2.2 Reserved property names

The engine reserves two names universally; they cannot appear in any `properties` declaration:

- **`type`** — the node-type discriminator itself; declaring it as a property is a category error.
- **`title`** — already an implicit field on the Node struct; redeclaring would conflict with the existing parse pipeline.

Behavior-pack reservations (e.g., `status` when a workflow pack is active on this type) are detected at engine-build time and produce the collision error described in §6.

### 2.3 Manifest-load-time validation rules

The manifest loader rejects any of the following at decode time, before the engine starts:

- A node-type's name (the table key) is empty.
- A property's `name` is empty or duplicates another property's name within the same type.
- A property's `name` is one of the reserved names (`type`, `title`).
- A property's `type` is not in the supported set: `string`, `int`, `float`, `bool`, `date`, `datetime`, `enum`, `markdown`, `list-of`.
- For `enum`: `values` is missing, empty, contains an empty string, or contains duplicate entries.
- For `list-of`: `item-type` is missing.
- For `list-of`: `item-type` is `list-of` (no nesting).
- For `list-of` with `item-type = "enum"`: `values` is missing or fails the enum-values rules above.
- Any constraint key appears on a type that doesn't accept it (e.g., `values` on a `string` property; `item-type` on anything other than `list-of`).

Spec-conformant manifests pass; manifests with any of the above are rejected with a message that names the offending type and property.

### 2.4 Type sketches

The manifest package grows two new types — one for the per-type declaration, one for the per-property declaration. These mirror the TOML shape and are decoded directly by the existing `BurntSushi/toml` loader. A `NodeTypes` field is added to the `Manifest` struct.

```go
type NodeType struct {
    Description string
    Properties  []PropertyDecl
}

type PropertyDecl struct {
    Name        string
    Type        string
    ItemType    string
    Values      []string
    Required    bool
    Description string
}
```

`Description` is stored verbatim and not consumed by any v1.b surface (see §9 open questions / §10 ledger #4).

### 2.5 Untyped nodes

A node whose `type:` frontmatter doesn't match any declared `[node-types.<t>]` is *untyped*. The validator treats untyped nodes as a pass-through — no property validation runs. This is the v1.b semantics: `[node-types]` is opt-in, and a workspace with no declarations validates nothing. Adding declarations later catches drift retroactively at the next reindex.

## 3. Validator

### 3.1 File location and shape

The validator lives in `internal/node/types.go`, co-located with `internal/node/edges.go`. It mirrors the structural-validation pattern that edge-type validation already established: a small file in the node package, called directly by `Service.Create` / `Modify` / `reindex.Run`, no behavior-pack wrapping.

The validator is **pure**: no I/O, no graph reads, no goroutines. It receives the parsed node and the manifest's decoded node-type map and returns a result-with-two-lists.

### 3.2 Public surface

```go
type PropertyValidationResult struct {
    HardErrors []PropertyError
    Drift      []PropertyDrift
}

type PropertyError struct {
    Kind     PropertyErrorKind
    Property string
    Type     string
    Value    any
    Reason   string
}

type PropertyErrorKind int

const (
    ErrTypeMismatch PropertyErrorKind = iota
    ErrRequiredMissing
    ErrEnumViolation
    ErrCannotUnsetRequired
)

type PropertyDrift struct {
    Property string
    Reason   string
}
```

A non-empty `HardErrors` means the caller must reject the write. A non-empty `Drift` is informational — the caller proceeds with the write, emits a stderr warning per entry, and persists drift rows.

### 3.3 Algorithm

The validator runs through the following decision tree on every call:

1. **Untyped node.** If the manifest has no declaration for `parsed.Type`, return an empty result. Pass-through.
2. **Required-property check.** For each `PropertyDecl` with `Required = true` whose `Name` is absent from `parsed.Properties`, append a `PropertyError{Kind: ErrRequiredMissing}` carrying the property name and the declared type rendering.
3. **Per-property loop.** For each entry in `parsed.Properties`:
   - If the property name isn't declared on the node-type, append a `PropertyDrift` entry with reason `"not declared on type \"<t>\""`. Continue.
   - Otherwise, validate the value against the declared type using the rules in §3.4.
4. **Return** the accumulated result. The caller decides disposition.

Order of returned errors: required-missing entries first (in declaration order), then per-property entries (in node-property iteration order). Drift entries follow the same per-property iteration order. This makes test assertions stable.

### 3.4 Per-type acceptance and rejection rules

| Declared type | Accepted Go shape | Rejected examples | Notes |
|---|---|---|---|
| `string` | Go `string` | int, bool, list, map | Empty string is allowed (it's still a string). |
| `markdown` | Go `string` | int, bool, list, map | Same as `string` for validation; the `markdown` distinction matters for downstream rendering, not for v1.b validation. |
| `int` | Go `int` or `int64` | float, string, bool | Floats with zero fractional part still rejected — the YAML loader produces typed values, and a value declared as `int` must arrive as an integer literal. |
| `float` | Go `float64` or `int` / `int64` | string, bool | Integer values auto-promote to float. |
| `bool` | Go `bool` | string `"true"`, int 1 | YAML's true/false produce Go bools; quoted-string `"true"` is a string and is rejected. |
| `date` | Go `string` parseable as `time.DateOnly` (YYYY-MM-DD) | non-string; non-parseable string | Empty string rejected. |
| `datetime` | Go `string` parseable as `time.RFC3339` | non-string; non-parseable string | Empty string rejected. |
| `enum` | Go `string` whose value is in the declared `values` slice | non-string; string not in `values` | `ErrEnumViolation`, not `ErrTypeMismatch`. |
| `list-of` | Go `[]any` (YAML's array shape) | scalar; map | Each element validated against `item-type`. |

For `list-of` with `item-type = "enum"`, every element must be a string in the declared `values` slice. Mixed-element lists (e.g., a string and an int in the same array) produce one `ErrTypeMismatch` per offending element.

### 3.5 Modify-only branch — required-property unset

`Service.Modify` may call a small companion helper alongside `ValidateProperties`:

```go
func WhichRequiredWereUnset(before, after *Node, decls map[string]NodeType) []string
```

The helper walks the after-node's missing properties and returns the names of those that were marked `required = true` in the declarations and had a value on `before`. Each returned name is converted by `Service.Modify` into a `PropertyError{Kind: ErrCannotUnsetRequired}` and joined into the rejection. This branch does not fire on `Create` (there's no `before`).

### 3.6 Caller contract

Callers (`Service.Create`, `Service.Modify`, `reindex.Run`) translate the result into action:

- **`HardErrors` non-empty**:
  - Tusk-owned writes (Create, Modify): join all hard errors into a single error and return to the caller. No file write, no index update.
  - Reindex (warn mode): convert each hard error to a `property_drift` row keyed by the matching `Kind`. Indexing proceeds.
- **`Drift` non-empty**:
  - Tusk-owned writes: emit a stderr warning to the Service's `warnings` writer per entry. Persist a `property_drift` row with `Kind: "undeclared-property"`. Continue with the write.
  - Reindex: persist the drift row without a stderr warning.
- **Both empty (clean pass)**:
  - Caller calls `propertyDrift.ClearForNode(parsed.ID)` so doctor's view is current.

This is the same shape Plan 7's recovery handling follows: the validator is pure and decision-free; the orchestrator decides what's fatal vs. informational.

## 4. `node.Service` Integration

### 4.1 Constructor change

The Plan 7 production constructor `NewServiceWithBehaviors` grows two parameters:

- `nodeTypes map[string]manifest.NodeType` — the declarations the validator consults.
- `propertyDrift *index.PropertyDriftRepo` — the drift sink.

Existing constructors (`NewService`, `NewServiceWithManifest`, `NewServiceWithEmbedQueue`) stay untouched; their callers (test-only paths now) leave the new fields as zero values, which the validator treats as "no node-types declared" → pass-through.

The Service struct gains two fields beyond Plan 7's `behaviors` / `drift` / `warnings`:

```go
nodeTypes      map[string]manifest.NodeType
propertyDrift  *index.PropertyDriftRepo
```

### 4.2 `Service.Create` order of operations

The existing pipeline runs:

1. Stat target absPath; reject if exists.
2. Render properties + body.
3. Parse the rendered file.
4. ResolveEdges; materialize wikilinks; ValidateEdges; cycle check.
5. **+ 7.b**: `result := ValidateProperties(parsed, service.nodeTypes)`. If `len(result.HardErrors) > 0`, return the joined error before any file write.
6. Plan 7's hook validate-phase (`OnNodeWriteValidate`, then `OnEdgeAddValidate` per derived edge row).
7. MkdirAll + WriteFile.
8. Stat + node row Upsert + edge rows Upsert + embed enqueue.
9. Plan 7's hook after-phase (`OnNodeWriteAfter`, then `OnEdgeAddAfter`).
10. **+ 7.b**: For each `result.Drift` entry, emit a stderr warning to `service.warnings` and append a `PropertyDriftRow{Kind: "undeclared-property"}`. On clean pass (no hard errors, no drift), call `service.propertyDrift.ClearForNode(parsed.ID)`.

Step 5 sits after edge validation and before any hook firing, so property-type errors are caught alongside structural errors that already abort early. Step 10 mirrors Plan 7's recovery-handling tail.

### 4.3 `Service.Modify` order of operations

The Modify pipeline grows symmetrically:

1. Existing: get index row; read file; parse `before`; resolve before's edges.
2. Apply `SetProps` / `UnsetKeys` / `Body` to produce `after`.
3. Render + reparse + ResolveEdges + ValidateEdges + cycle check on the after-node.
4. **+ 7.b**: `result := ValidateProperties(after, service.nodeTypes)`. Compute `unsetRequired := WhichRequiredWereUnset(before, after, service.nodeTypes)`. For each name in `unsetRequired`, append a `PropertyError{Kind: ErrCannotUnsetRequired}` to `result.HardErrors`. If `len(result.HardErrors) > 0`, return joined error.
5. Plan 7's recovery-aware Validate fire on `OnNodeWriteValidate`; then `OnEdgeRemoveValidate` for each removed edge row, `OnEdgeAddValidate` for each added row.
6. atomicWrite + index row Upsert + edge rows Upsert + embed enqueue.
7. Plan 7's after-phase fires + recovered-event drift writes (workflow_drift).
8. **+ 7.b**: For each `result.Drift` entry, emit stderr warning + persist `PropertyDriftRow{Kind: "undeclared-property"}`. On clean pass (no hard errors, no drift), call `service.propertyDrift.ClearForNode(after.ID)`.

The required-unset check runs only in Modify; Create has no `before`.

### 4.4 Hard-error rendering at the Service boundary

The Service joins multiple hard errors into a single error value with a stable shape:

```
node-types: rejected create: ticket "tickets/foo" has 2 errors:
  - property "title" is required (declared in [node-types.ticket])
  - property "priority" expected type "int" but value "high" is not an integer
```

The CLI passes through; MCP returns a structured payload (see §7).

## 5. Reindex Integration

### 5.1 Config and Report extensions

`reindex.Config` already carries the Plan 7 fields `Behaviors node.Behaviors` and `DriftLog *index.WorkflowDriftRepo`. Plan 7.b adds two parallel fields:

```go
NodeTypes     map[string]manifest.NodeType
PropertyDrift *index.PropertyDriftRepo
```

`reindex.Report` adds `PropertyViolations int` alongside Plan 7's `WorkflowViolations int`.

### 5.2 Per-file walk extension

After the existing per-file `Repo.Upsert` + `Edges.UpsertAll` block and Plan 7's workflow validation block, the walk runs property validation in warn mode:

- If `parsed.Type` is not in `Config.NodeTypes`, skip the block (untyped node = pass-through).
- Otherwise, call the validator. For each `HardError`, increment `report.PropertyViolations` and append a `PropertyDriftRow` with the matching `Kind` and `Details` populated from the error's structured fields.
- For each `Drift` entry, increment `report.PropertyViolations` and append a row with `Kind: "undeclared-property"`.
- If both lists are empty, call `config.PropertyDrift.ClearForNode(parsed.ID)`.

Reindex never aborts indexing on property issues. Every node still upserts. This matches the v1 design spec §4.2 / §14 "off-schema content is warned, not rejected" discipline.

### 5.3 Summary line

The reindex summary grows the property-violation count when non-zero:

```
Indexed 142 nodes (3 workflow-violations, 5 property-violations) in 240ms
Run `tusk doctor` to inspect violations
```

When both counts are zero, the parenthetical is omitted (Plan 7 behavior). Exit code stays 0 even with non-zero counts.

### 5.4 Reindex pre-image is always nil

Like Plan 7, reindex sees only on-disk state — `before = nil` is irrelevant for property validation since the validator doesn't take a `before`. Required-property-unset cannot fire from reindex (it's a Modify-only branch). Type, enum, required-missing, and undeclared-property checks all run as expected.

## 6. Engine Collision Detection Extension

### 6.1 Scope

Plan 7's `behavior.NewEngine` collision detector rejects two behavior instances reserving the same `(NodeType, Property)` pair. Plan 7.b extends the same detector to also reject overlap between behavior reservations and node-type declared properties.

### 6.2 Detection rule

A collision exists when:

- Behavior instance A reserves `(NodeType: T, Property: P)`.
- A node-type declaration `[node-types.T]` declares a property named `P`.

The engine refuses to start with a clear message that names both colliding sources:

```
behavior: behaviors.workflow.tickets reserves property "status" on type
"ticket" but it is also declared in node-types.ticket.properties[stage]
```

The user can fix either side: remove the property from the manifest, change the workflow pack's `status-property`, or change the property's name.

### 6.3 Surface change

`behavior.NewEngine` grows a second parameter alongside the existing `[]Instance`:

```go
type DeclaredKey struct {
    NodeType string
    Property string
    Source   string  // e.g. "node-types.ticket.properties[priority]"
}

func NewEngine(instances []Instance, declaredKeys []DeclaredKey) (*Engine, error)
```

`DeclaredKey` lives in the `behavior` package directly. The wiring layer (CLI's `newBehaviorEngine` helper and MCP runtime's `buildBehaviorEngine` helper) constructs the slice from the manifest's `loaded.NodeTypes` and passes it in. Putting the type in `behavior` keeps the manifest package free of behavior-engine concerns.

A new method `Registry.BuildEngineWithDeclaredKeys(loaded, declaredKeys)` is the production path; existing `BuildEngine(loaded)` (instances-only) stays for tests.

### 6.4 Detector-internal logic

The detector (informally):

1. Walk `declaredKeys`; build `(NodeType, Property) → Source` owner map.
2. Walk `instances`. For each instance's `ReservedKey`, check the map. On collision, error with both source names.
3. Continue Plan 7's instance-vs-instance collision check unchanged (treats two instances reserving the same pair as the existing collision).

Same property name on a different type passes; different property names on the same type pass. The detector's uniqueness key is `(NodeType, Property)` — same as Plan 7.

### 6.5 Wiring layer responsibility

The CLI wiring helper builds the declared-keys slice from the manifest's node-types section by iterating each declared type and each declared property. The MCP runtime's helper does the same. Both call the same `Registry.BuildEngineWithDeclaredKeys` path.

## 7. Error and Warning Surfaces

### 7.1 CLI rendering

Hard errors render as a multi-line message with one indented line per offender:

```
$ tusk node create --type ticket --prop priority=high tickets/foo
Error: node-types: rejected create: ticket "tickets/foo" has 2 errors:
  - property "title" is required (declared in [node-types.ticket])
  - property "priority" expected type "int" but value "high" is not an integer
```

```
$ tusk node modify tickets/foo --unset title
Error: node-types: rejected modify: cannot unset required property "title" on type "ticket"
  edit the file directly to remove the field, then reindex
```

Drift surfaces a stderr warning and continues:

```
$ tusk node modify tickets/foo --prop assignee=bob
warning: node-types: property "assignee" is not declared on type "ticket"; surfaces as a property-drift in tusk doctor
Modified tickets/foo
```

Stderr for warnings; stdout for success. The Service's `warnings` writer is the same `io.Writer` Plan 7 plumbed through (`os.Stderr` in production; injectable for tests).

### 7.2 MCP rendering

The MCP `tusk_node_modify` and `tusk_node_create` tools return a structured tool error when property validation produces hard errors:

```json
{
  "isError": true,
  "error": "node-types-rejection",
  "node_id": "tickets/foo",
  "node_type": "ticket",
  "op": "create",
  "errors": [
    {
      "kind": "required-missing",
      "property": "title",
      "type": "string",
      "value": null,
      "reason": "property \"title\" is required (declared in [node-types.ticket])"
    },
    {
      "kind": "type-mismatch",
      "property": "priority",
      "type": "int",
      "value": "high",
      "reason": "expected type \"int\" but value \"high\" is not an integer"
    }
  ]
}
```

The `errors` array carries one entry per `PropertyError` from the validator — multiple errors are reported together rather than collapsed.

Drift surfaces as a `warnings` array on the success payload, in the same envelope shape Plan 7 introduced for workflow recovery:

```json
{
  "id": "tickets/foo",
  "type": "ticket",
  "path": "tickets/foo.md",
  "title": "Fix login bug",
  "properties": {"...": "..."},
  "warnings": [
    {
      "kind": "property-drift",
      "node_type": "ticket",
      "property": "assignee",
      "reason": "not declared on type \"ticket\"",
      "message": "node-types: property \"assignee\" is not declared on type \"ticket\""
    }
  ]
}
```

The warning entries are constructed directly from the validator's `PropertyDrift` slice, not by re-parsing stderr text. (Plan 7's recovery-warning parser used text re-parsing as a v1 expediency; Plan 7.b takes the cleaner direct path because the structured drift slice is already available in the same call frame.)

### 7.3 Doctor rendering

`internal/doctor` adds four Issue kind constants:

- `IssueUndeclaredProperty = "undeclared-property"`
- `IssueTypeMismatch       = "type-mismatch"`
- `IssueRequiredMissing    = "required-missing"`
- `IssueEnumViolation      = "enum-violation"`

`Run` reads `config.PropertyDrift.ListAll()` when configured and emits one Issue per row, with a kind-specific message:

| Issue kind | Rendered message |
|---|---|
| `undeclared-property` | `node-types: property "<P>" not declared on type "<T>"` |
| `type-mismatch` | `node-types: property "<P>" expected type "<type>" but value "<value>" is not <expected description>` |
| `required-missing` | `node-types: required property "<P>" missing on type "<T>"` |
| `enum-violation` | `node-types: property "<P>" value "<value>" not in declared enum [<values>]` |

Doctor's existing text and JSON renderings already handle arbitrary `Issue.Kind` values; no further changes needed.

### 7.4 Reindex summary

Per §5.3: parenthetical grows to include `<N> property-violations` alongside Plan 7's `<N> workflow-violations`. Both omitted when zero. Exit code 0.

## 8. Testing Strategy

The plan doc names a per-package test surface. The design here defines what must be exercised; harness specifics are an implementer concern.

**`internal/manifest`** — extend the loader test suite:

- Well-formed multi-type manifest with mixed property declarations (scalar, list-of, enum, list-of-enum).
- Per-type-and-property structural rejections, one test per rule in §2.3: empty type name, empty property name, reserved name (`type` / `title`), unknown type, enum without `values`, list-of without `item-type`, list-of with nested list-of `item-type`, list-of-enum without `values`, duplicate property names within a type, misplaced constraint keys.

**`internal/node`** — new test suite for `ValidateProperties`:

- **Pass-through:** untyped node (no declaration) returns empty result.
- **Required:** missing required emits `ErrRequiredMissing`; present required passes.
- **Per-type acceptance and rejection:** one positive and one negative per scalar type — `string`, `int`, `float`, `bool`, `date`, `datetime`, `enum`, `markdown`. Float accepts integers (auto-promotion); int rejects floats.
- **List-of:** flat list of valid scalars passes; mixed-element list rejected; list-of-enum with element outside `values` rejected.
- **Undeclared property:** appears as a Drift entry, not a HardError.
- **`WhichRequiredWereUnset`:** before-has-required and after-missing-required returns the property name; non-required unset returns empty; required-unchanged returns empty.

**`internal/index/property_drift_repo`** — mirror of `workflow_drift_repo` tests:

- Append + ListAll happy path with deterministic ordering.
- Idempotent on the primary key — repeated appends collapse to one row with the most recent `observed_at`.
- ClearForNode removes only that node's rows.
- CountAll returns expected total.

**`internal/doctor`** — extend with one fixture per new Issue kind:

- Seeded `property_drift` rows for each of the four kinds surface in `Run`'s output.
- Text and JSON renderings include the rendered messages.

**`internal/node` (Service tests)** — extend with workspace fixtures:

- **Create rejection (required missing):** declared-required `title` not provided → joined hard-error message; no file written.
- **Create rejection (type mismatch):** `priority` declared `int`, supplied `"high"` → rejected; no file written.
- **Create with undeclared property:** drift warning written to the configured writer; file written; index row upserted; drift row visible.
- **Modify with --unset of required:** rejected with `ErrCannotUnsetRequired`; file unchanged.
- **Modify clean pass:** previously-drifted node with no drift now has its drift rows cleared.

**`internal/reindex`** — extend the existing fixture-style tests:

- Reindex of a workspace with mixed legal / drifted / violating typed nodes produces the expected `report.PropertyViolations`; rows persist; index still upserts every node; exit semantics unchanged.
- Clean reindex pass clears prior property drift for nodes that no longer drift.

**`cmd/tusk`** — end-to-end through the CLI:

- `tusk node create` with mismatched type rejects (stderr message; exit 1; no file).
- `tusk node modify` with undeclared property succeeds, emits stderr warning, drift visible to a follow-up `tusk doctor`.
- `tusk node modify --unset` of a required property rejects.
- `tusk reindex` summary line includes property-violation count when non-zero; omits parenthetical when zero.
- `tusk doctor` renders each new Issue kind with the right text.

**`internal/mcp`** — tools.go test extensions:

- `tusk_node_modify` returns the structured `node-types-rejection` envelope for property hard errors; envelope contains the `errors` array with per-entry kind/property/type/value/reason.
- `tusk_node_modify` success payload grows the `warnings` array on undeclared-property drift; entry has `kind: "property-drift"`.
- `tusk_node_create` returns the same structured envelope on rejection.

**Behavior collision detection (`internal/behavior`)** — extend the existing `BuildEngine` collision test:

- `[node-types.ticket].properties` declaring `status` plus an active workflow pack reserving `status` on `ticket` produces a clear collision error at engine-build time. Error names both colliding sources.
- Same property declared on different types passes.
- Different properties declared on the same type pass.

**Race detector** — `make test-race` runs the same set; property validation is purely synchronous.

## 9. Open Questions / Residuals

Items the design accepts as known and either scoped-in or scoped-out, that future contributors should know about.

1. **Type-coerced reads.** `date` and `datetime` are stored as strings on disk and validated on write. Code that reads node properties (e.g., the filter compiler comparing dates) does string comparison today; this works for `YYYY-MM-DD` and RFC3339 because both are lexicographically ordered. A future plan can introduce typed property accessors that parse on read.

2. **Behavior reservations vs. node-type declarations.** Plan 7.b rejects collisions outright. There's a softer reading where a workflow pack's `states` could populate a node-type's `enum` `values` for the same property — i.e., the workflow pack's state list as the source of truth. Appealing but conflates two declaration sources; v1.b keeps them strictly separate.

3. **Schema evolution / property removal.** Removing a property from `[node-types.<t>]` after nodes have used it: those nodes now have an undeclared property → drift warnings on every reindex. This is the intended behavior (visible drift) but may be noisy; v1.x can introduce a "deprecate" marker that suppresses drift warnings during a transition window.

4. **`description` field unused at runtime.** Plan 7.b accepts and stores `description` on both node-types and properties, but no v1.b surface reads it. Future doctor enhancements (`tusk doctor explain <node-id>`) and CLI introspection (`tusk node-types show ticket`) will consume it. Acceptable to ship the field-only schema now.

5. **Cross-type uniqueness of property names.** Two different node-types can declare the same property name with different types (e.g., `status` as enum on `ticket`, `status` as string on `decision`). v1.b allows this — `(NodeType, Property)` is the uniqueness key. Future work could surface an opinion but v1.b doesn't take a stance.

6. **`property_drift` rows persist across manifest reload.** A drift row carries `node_type` from the time of observation. If the manifest's `[node-types.<t>]` is removed, the drift rows for type `<t>` remain visible until the affected nodes are re-touched (Modify or reindex re-runs property validation; if the type is now untyped, validation is a no-op and the clean-pass branch clears the rows). Acceptable v1.b semantics.

## 10. Plan 7.c Ledger (Deferred Items)

| # | Deferred item | Rationale |
|---|---|---|
| 1 | `ref` property type + auto-edge-type generation | Needs its own design loop; intersects existing edge-type validation. Lands alongside built-in type packs. |
| 2 | Nested `list-of(list-of(...))` | Flat lists of scalars cover all observed v1 needs. |
| 3 | Built-in type packs (`kanban`, `vault`, `tags`) consuming `[node-types]` | Land in 7.c with `ref`. |
| 4 | `OnNodeRead*` firing site (Service.Get / List) | Still no v1 consumer. Carries over from Plan 7's ledger. |
| 5 | `tusk_doctor` MCP tool surfacing the new property Issue kinds | Doctor is CLI-only today; MCP doctor was already deferred from Plan 7. |
| 6 | Typed property accessors (parse-on-read) | See open question #1. |
| 7 | Workflow `states` populating node-type `enum` `values` automatically | See open question #2. |
| 8 | "Deprecated property" marker to suppress drift warnings during schema migration | See open question #3. |
| 9 | `tusk node-types show <type>` CLI introspection | See open question #4. |
| 10 | `tusk doctor explain <node-id>` showing schema documentation | See open question #4. |

Plan 7's residual ledger items not picked up by 7.b stay open for 7.d+:

- **Cascading behaviors:** `auto-complete-parent` / `auto-revert-parent` runtime activation, re-entrant write support, journaled write log.
- **Drift surface dedup / most-recent-only rendering:** polish; can move whenever.
