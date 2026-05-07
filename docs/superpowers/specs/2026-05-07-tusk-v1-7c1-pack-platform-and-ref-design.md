# Tusk v1 — Pack Platform and `ref` Property Type Design (Plan 7.c.1 sub-spec)

- **Status:** Draft
- **Date:** 2026-05-07
- **Author:** German Meza
- **Scope:** Implementation-shaped design for two foundations under Plan 7.c: (1) the `tusk pack add` CLI command for materializing community-shared TOML pack content into a workspace's `tusk.toml`, and (2) the `ref` property type — frontmatter references that desugar into auto-generated typed edges. This is the foundation plan for the 7.c series; subsequent plans (7.c.2 tags, 7.c.3 kanban, 7.c.4 vault) ship pack content built on these primitives.
- **Successor of:** the brainstorm dialogue captured during Plan 7.c.1 setup.

This document is a sub-spec of `2026-05-05-tusk-v1-rebuild-design.md` (the v1 rebuild design) and a follow-up to `2026-05-07-tusk-v1-7b-node-types-design.md` (Plan 7.b). It addresses ledger items #1 (`ref` property type + auto-edge-type generation) and #3 (built-in type packs) from Plan 7.b §10, with significant architectural refinements made during brainstorm: the `[workspace] type-packs = [...]` activation list and `[type-packs.<name>.<sub>]` override sections from v1 design spec §7.1 layer 1 are dropped in favor of pure-templating semantics.

---

## 1. Goal & Scope

Plan 7.c.1 lands two independent foundations:

1. A **pack platform** — a `tusk pack add` CLI command that fetches community-shared TOML pack content (by built-in name alias or arbitrary URL) and merges it into the user's workspace manifest under standard sections. The engine has no notion of "packs" as a runtime concept; packs are pure CLI/templating mechanism.
2. A **`ref` property type** — a manifest-declarable property that maps a frontmatter string or wikilink to a typed edge. Reference values resolve by node ID (path-without-extension) or by title within the target node-type scope. Each `ref` property auto-generates exactly one edge type whose attributes are configurable from the property declaration.

Plan 7.c.1 does not ship the actual content of the kanban, vault, or tags packs; those are 7.c.2/7.c.3/7.c.4 deliverables that consume the platform plus `ref`.

### 1.1 In scope

- `tusk pack add <name|url>` CLI command with built-in name aliases (`kanban`, `vault`, `tags`) resolving to canonical URLs in the tusk repo. Generic URL form supports HTTP/HTTPS plus `file://` (for tests, air-gapped install). No local cache; every invocation fetches fresh; network failure is a non-zero exit.
- Pack TOML format constraints: a fetched pack file may contain only standard manifest sections (`[node-types.X]`, `[edge-types.X]`, `[behaviors.X]`). All other top-level keys are rejected. Schema validation runs against the same rules as the manifest loader before any modification to `tusk.toml`.
- Collision detection: pack sections that overlap with existing user manifest sections are hard-error by default; a `--force` flag deletes the colliding sections from the user manifest before appending pack content.
- Atomic write: pack-add acquires the workspace write lock for the duration of validate/merge/write; the new manifest is written to a temp file and renamed over `tusk.toml`.
- `ref` property type: declarable as `{ name = "X", type = "ref", to = "<node-type>" }` and `{ name = "X", type = "list-of", item-type = "ref", to = "<node-type>" }`. Optional ref-only fields: `inverse`, `acyclic`, `ordered`. Manifest-load-time validation rejects malformed declarations.
- Auto-edge-type generation: each `ref` property declaration synthesizes exactly one `EdgeType` in the loaded manifest, with attributes derived from the property declaration. Collision with an explicit `[edge-types.<same-name>]` block is a manifest-load error.
- Multi-source merge for the same property name across node types: consistent attributes extend the synthesized edge type's `From` slice; conflicting attributes reject.
- Frontmatter ref resolution: a ref value is parsed as a wikilink (`[[X]]` → node-ID lookup) or as a bare title (lookup within the `to` node-type scope). Both shapes converge on a single edge representation in the index.
- Validator integration following v1 design spec §7.5 — dangling/ambiguous/type-mismatched/cyclic refs reject for tusk-owned writes (CLI/MCP), surface via `tusk doctor` for external edits.
- Doctor extension: four new issue kinds (`ref_dangling`, `ref_ambiguous`, `ref_type_mismatch`, `ref_cycle`) with the same row shape and CLI rendering pattern as Plan 7.b's `property_drift`.
- Service / reindex / watcher integration so that ref edges follow the full node lifecycle.

### 1.2 Out of scope (Plan 7.c+ ledger)

Each item below is captured in the consolidated ledger in §10. Briefly:

- Content of the kanban / vault / tags packs (each gets its own follow-up plan).
- Tag auto-creation hook (lives in 7.c.2 with the tags pack).
- Subcommand shortcuts (`tusk ticket open`, `tusk note new`) — deferred from v1.c entirely; revisited later as a manifest config rather than inference from materialized content.
- Filter-grammar shortcuts (`+tag` / `-tag`) — dropped from v1.c entirely.
- `[workspace] type-packs = [...]` activation list — dropped from the design entirely.
- `[type-packs.<name>.<sub>]` override sections — dropped (materialized content lives under standard sections).
- Bare path-string ref values (`parent: tickets/auth-cleanup` without wikilink brackets). Parsing is straightforward but Plan 2's rename rewrite pipeline would gain a property-value-rewrite path; deferred until usage shows the wikilink ceremony is a real friction.
- Pack discovery/listing (`tusk pack list`) — small, additive; revisit if needed.
- `tusk pack remove <name>` for un-merging — same.
- MCP surface for `tusk pack add` — workspace-configuration commands intentionally don't live in the MCP surface (consistent with `tusk_init`).

### 1.3 Backward compatibility

No existing v1 functionality changes shape. Workspaces with no `ref` properties and no calls to `tusk pack add` see zero behavior change. Plan 7.b's `[node-types]` schema gains four optional fields on `PropertyDecl` (`To`, `Inverse`, `Acyclic`, `Ordered`); manifests that don't use them parse identically. The `[edge-types]` table is unchanged.

---

## 2. Pack Platform — `tusk pack add` Command

### 2.1 Command shape

```
tusk pack add <name-or-url> [--force]
```

Positional argument:
- A built-in pack name (`kanban`, `vault`, `tags`) — distinguished by *not* containing `://`.
- Or a URL with scheme `http`, `https`, or `file`.

Flag:
- `--force` — sections in the user's manifest that collide with the fetched pack are removed before append. Without `--force`, any collision is a fatal error.

The `tusk pack` parent command exists for the namespace; in 7.c.1 it has only the `add` subcommand. Future subcommands (`list`, `remove`) are out of scope.

### 2.2 Name alias resolution

A small in-binary map in `internal/typepacks/aliases.go`:

```go
var BuiltinAliases = map[string]string{
    "kanban": "https://raw.githubusercontent.com/germanamz/tusk/main/packs/kanban.toml",
    "vault":  "https://raw.githubusercontent.com/germanamz/tusk/main/packs/vault.toml",
    "tags":   "https://raw.githubusercontent.com/germanamz/tusk/main/packs/tags.toml",
}
```

Resolution:
- Argument contains `://` → treat as URL. Pass through verbatim.
- Argument does not contain `://` and is a key in `BuiltinAliases` → use the mapped URL.
- Otherwise → error: `pack add: unknown pack name "<arg>"; supported names: kanban, tags, vault. To install from a URL, pass the full URL.`

The pack TOML files at the canonical URLs are not part of Plan 7.c.1's deliverable; they ship in 7.c.2/7.c.3/7.c.4. In 7.c.1, the alias map is committed but the URLs return 404 until the corresponding pack plan lands. This is intentional — 7.c.1 is reviewable end-to-end against `file://` test fixtures.

### 2.3 Fetch

Standard `net/http` GET with a `context.WithTimeout(ctx, 30 * time.Second)`. Up to 3 HTTP redirects via the default transport's `CheckRedirect` policy. Custom user-agent `tusk/<version>` (the binary's version string from `cmd/tusk/version.go`).

A single response-size cap of 1 MiB enforced via `io.LimitReader` — pack TOML files are tiny by construction; oversize indicates a misconfigured URL or a hostile target. The cap is intentionally conservative.

`file://` URLs use `os.ReadFile` against the path component. Symlinks resolve normally; permission errors propagate as fetch errors.

Failure modes (each surfaces as a `pack add: fetch <url>: <reason>` message and a non-zero exit):
- DNS resolution failure
- Connection refused / timeout
- HTTP non-2xx response
- Redirect loop (more than 3 hops)
- Response exceeds the size cap
- `file://` path doesn't exist or isn't readable

No retries. No cache.

### 2.4 Pre-merge validation

Fetched bytes pass through three sequential checks. Each rejection is a non-zero exit; the user's `tusk.toml` is untouched.

**(a) TOML well-formedness.** The standard `BurntSushi/toml` decoder runs against the fetched bytes. Decode failure → `pack add: invalid TOML at <url>: <decoder error>`.

**(b) Pack schema check — allowed top-level keys.** Pack TOML may contain only the top-level keys `node-types`, `edge-types`, `behaviors`. Any other top-level key is rejected:

```
pack add: pack at <url> contains disallowed top-level section "[workspace]"
(packs may only contain [node-types], [edge-types], [behaviors])
```

This guards against a remote URL rewriting workspace configuration. Implementation: decode the raw TOML into a `map[string]toml.Primitive` and assert the key set is a subset of the allowed three.

**(c) Manifest schema validation.** The decoded pack content is wrapped in a synthetic `manifest.Manifest` value (with empty `Workspace`, `Embeddings`, etc.) and passed through the same validation path the manifest loader uses for user manifests:
- `PropertyDecl` shape (Plan 7.b §2.3 rules — reserved name check, type vocabulary, etc.).
- `EdgeType.Cardinality` value membership.
- `ref`-specific validation from §3 below — including the requirement that any `ref` property declared *inside* the pack has its `to` target referencing a node type also declared *inside* the same pack file. Pack files must be self-contained for ref resolution. This does *not* preclude a user from later declaring an inline `[node-types.X]` block in `tusk.toml` with a ref property pointing at a pack-defined type; that case is validated at engine load against the post-merge manifest (§3.2). What is rejected here is a pack file whose own ref properties depend on types defined outside that pack.

### 2.5 Collision detection

The user's existing `tusk.toml` is parsed (read-only) and the set of populated keys at depth 2 is computed:

```
node-types.<X>      → {ticket, project, ...}
edge-types.<X>      → {parent, blocks, ...}
behaviors.<kind>.<instance> → {workflow.kanban, ...}
```

The pack's same-shaped key set is intersected. A non-empty intersection is a collision.

Behavior:
- No collisions → proceed to write.
- Collisions, no `--force` → exit non-zero. Error message lists every overlapping section, prefixed by the pack source identifier:

```
pack add: cannot apply pack from <url>: 3 colliding sections in tusk.toml:
  - [node-types.ticket]
  - [edge-types.parent]
  - [behaviors.workflow.kanban]
re-run with --force to overwrite, or remove the colliding sections by hand
```

- Collisions, `--force` → the colliding sections are removed from the in-memory representation of the user manifest. Pack content is appended (see §2.6). Other sections in the user manifest are untouched. The collision report is still printed to stderr for transparency (one line per removed section).

`--force` only deletes sections explicitly present in the *fetched pack*. Sections in the user's manifest that don't overlap are preserved verbatim.

### 2.6 Atomic write

`tusk pack add` acquires the workspace write lock (Plan 3) before touching anything. Sequence:

1. Acquire workspace write lock.
2. Read existing `tusk.toml`.
3. Fetch pack content.
4. Run pre-merge validation (§2.4).
5. Run collision detection (§2.5); on `--force`, mutate the in-memory user manifest representation to remove colliding sections.
6. Compose the new manifest body (§2.7).
7. Write to a temp file in the workspace root (`.tusk.toml.tmp.<pid>`).
8. `fsync` the temp file.
9. `os.Rename` the temp file over `tusk.toml`.
10. Release the workspace write lock.

If any step from 2–9 fails, the temp file is removed (best-effort) and the original `tusk.toml` is untouched.

### 2.7 Append style

Pack content is appended to the existing `tusk.toml` as a separate text block prefixed by a header comment:

```
<existing tusk.toml content>

# Added by `tusk pack add kanban` on 2026-05-07
[node-types.ticket]
description = "A unit of trackable work"
properties = [ ... ]

[edge-types.parent]
...
```

Pure text concatenation. The pack body (the verbatim fetched bytes, post-validation) is appended below a blank line and the header comment. We do *not* re-emit the user's manifest through the TOML encoder — that would reorder keys, normalize whitespace, and strip comments. The user's authored `tusk.toml` stays bit-identical to before, with new content appended.

The `--force` collision-removal path requires editing the user's portion. When sections are removed from the user manifest, we identify the byte ranges of the colliding sections in the original file (via the TOML AST positions or a regex over `[<key>]` headers — the latter is simpler and adequate for our two-level depth), splice them out, and concatenate. Comments adjacent to the removed sections are removed with them. The pack content is then appended as in the no-`--force` path, with the standard header comment.

### 2.8 Exit codes

| Code | Cause |
|---|---|
| 0 | Success |
| 1 | Invalid arguments / unknown pack name / collision without `--force` |
| 2 | Fetch failure (network, HTTP non-2xx, oversize, redirect loop, file:// not found) |
| 3 | Pack content failed validation (bad TOML, disallowed sections, schema-invalid) |

---

## 3. `ref` Property Type — Manifest Schema

### 3.1 Schema extension

`manifest.PropertyDecl` (introduced in Plan 7.b) gains four optional fields. They are meaningful only when `Type == "ref"` or (`Type == "list-of"` and `ItemType == "ref"`):

```go
type PropertyDecl struct {
    Name        string
    Type        string
    ItemType    string
    Values      []string
    Required    bool
    Description string

    // ref-only:
    To      string  // target node-type name; "*" means any. Required for ref properties.
    Inverse string  // optional; name of the auto-derived inverse edge.
    Acyclic bool    // optional; if true, the engine validates no cycle on edge add.
    Ordered bool    // optional; only meaningful for list-of(ref); defaults to true.
}
```

TOML form:

```toml
[node-types.ticket]
properties = [
    { name = "assignee", type = "ref", to = "person" },
    { name = "watchers", type = "list-of", item-type = "ref", to = "person", inverse = "watching" },
    { name = "parent",   type = "ref", to = "ticket", acyclic = true },
]
```

### 3.2 Manifest-load-time validation

The loader rejects, before the engine starts:

- A property with `Type == "ref"` (or `Type == "list-of" && ItemType == "ref"`) where `To` is empty:
  ```
  manifest: node-type "ticket" property "assignee": ref property requires `to`
  ```
- A `ref` property where `Values`, `ItemType` (when `Type == "ref"`), or other inapplicable fields are populated:
  ```
  manifest: node-type "ticket" property "assignee": ref property cannot declare `values`
  ```
- A `ref` property where `To` is not `*` and is not the name of any declared `[node-types.<X>]` in the *post-merge* manifest (this includes packs the user has already merged into `tusk.toml`):
  ```
  manifest: node-type "ticket" property "assignee": ref target "person" is not a declared node type
  ```
  Forward references are not allowed. Pack ordering matters during materialization: a pack adding ref properties whose `to` targets are defined in another pack must be added after that other pack. If the user has not yet added the prerequisite pack, the manifest fails to load.
- `To == "*"` is allowed and produces an edge type with `to = ["*"]`.

Plan 7.b's reserved property names (`type`, `title`) still reject — `ref` doesn't change that.

### 3.3 Auto-edge-type generation

After the manifest decodes, the loader walks every `PropertyDecl` with a ref-shaped type and synthesizes an `EdgeType`:

| EdgeType field | Source |
|---|---|
| `Description` | `"auto-generated from <owning-node-type>.<property-name>"` |
| `From` | `[<owning-node-type>]` (single-element slice) |
| `To` | `[<property.To>]` (single-element slice; `["*"]` for `*`) |
| `Cardinality` | `"many-to-one"` if `Type == "ref"`; `"many-to-many"` if `list-of(ref)` |
| `Ordered` | `false` for plain ref; `property.Ordered` (default `true`) for list-of(ref) |
| `Inverse` | `property.Inverse` (empty if not specified) |
| `Acyclic` | `property.Acyclic` |

The synthesized `EdgeType` is added to the in-memory `Manifest.EdgeTypes` map under key `<property-name>`.

### 3.4 Collision rule

If `Manifest.EdgeTypes[<property-name>]` already exists from an explicit `[edge-types.<property-name>]` block in the user's TOML, the loader rejects:

```
manifest: edge type "assignee" is auto-generated by ref property
"ticket.assignee"; remove the explicit [edge-types.assignee] declaration
or rename the property
```

Single source of truth — attribute customization happens via the optional ref-only fields (`Inverse`, `Acyclic`, `Ordered`).

### 3.5 Multi-source merge

If two different node types both declare `{ name = "assignee", type = "ref", to = "person" }`, the second auto-derivation merges into the first by extending the `From` slice. The merge is consistent only if `To`, `Cardinality`, `Ordered`, `Inverse`, `Acyclic` agree. Mismatches reject:

```
manifest: ref property "assignee" is declared on both "ticket" and "story"
with conflicting attributes (cardinality: many-to-one vs many-to-many);
align the declarations or use distinct property names
```

The `From` slice's element order is the source-declaration order in the manifest (TOML iteration order of `[node-types.<X>]` blocks).

### 3.6 Type sketches

```go
// (No new struct types beyond the PropertyDecl extension above.)

// Manifest loader signature is unchanged. The auto-edge synthesis is a
// new internal pass invoked after PropertyDecl validation:
func synthesizeRefEdgeTypes(manifestRef *Manifest) error
```

The pass mutates `manifestRef.EdgeTypes` in place and returns the first validation error encountered.

---

## 4. `ref` Property Type — Frontmatter Resolution

### 4.1 Authoring shapes

A `ref` property is a single string in YAML (or TOML) frontmatter:

```yaml
---
type: ticket
title: Migrate the auth service
assignee: alice                     # bare title
parent: "[[tickets/auth-cleanup]]"  # wikilink → node ID
---
```

A `list-of(ref)` is an array of the same shapes (mixing wikilinks and bare titles in one list is allowed):

```yaml
watchers:
  - alice
  - "[[people/bob]]"
```

### 4.2 Parse-and-resolve algorithm

For each ref-typed property value declared on the node, resolution runs at every `Service.Create` / `Service.Modify` and at every reindex pass that touches the node. Algorithm:

1. Trim whitespace.
2. If the value is the empty string (or YAML `null`) → omit. The property contributes no edge.
3. If the value matches the wikilink shape `[[X]]` (regex: `^\[\[(.+?)\]\]$`) → strip the brackets and resolve `X` as a **node-ID lookup** (path-without-extension; same machinery as Plan 2 wikilink edges).
4. Otherwise → **title lookup** within the property's `to` node-type scope. SQL: `SELECT id FROM nodes WHERE type = ?to AND title = ?value`. The `to == "*"` case skips the type filter.

Outcomes (both lookup branches):
- Exactly one match → emit edge to that node ID.
- Zero matches → dangling-ref outcome (§4.4).
- Multiple matches → ambiguous-ref outcome (§4.4). Multiple matches are rare for ID lookup (paths are unique by construction unless there's a workspace-level corruption) but common for title lookup with `to == "*"`.

### 4.3 Edge representation

Each successfully resolved ref value emits one row into the existing `edges` table (Plan 2 schema). Edge type = property name. `Ordinal` is set to the value's array index for `list-of(ref)` (preserving authored order); ordinal is `0` for plain ref.

The frontmatter property *value* is **not** stored as a property column on the node. The persistent representation of a ref value is the edge alone. Round-tripping back to the user (e.g., `tusk node get` rendering frontmatter) reads the file from disk verbatim — the index is for query, the file is the source of truth for the authoring shape.

### 4.4 Validation outcomes

Following v1 design spec §7.5 and Plan 7.b's pattern:

| Outcome | Tusk-owned write (CLI/MCP) | External edit (file watcher / reindex) |
|---|---|---|
| Dangling ref (target not found) | Reject the write before touching the file. Error names the property and the unresolved value. | Index the node; surface in `tusk doctor` as `ref_dangling`. |
| Ambiguous match (multiple candidates) | Reject the write. Error lists every candidate node ID. | Index the node; emit no edge for this value; surface as `ref_ambiguous` listing candidates. |
| Type mismatch (target's type doesn't match `to`, when `to != "*"`) | Reject. | `tusk doctor` issue `ref_type_mismatch`. |
| Cycle (when `acyclic = true`) | Reject. | `tusk doctor` issue `ref_cycle`. |

Doctor row shapes mirror Plan 7.b's `property_drift` (node ID, property name, kind, additional fields per kind).

### 4.5 Required-property interaction

A `required = true` ref property whose value is *missing* is a `property_required_missing` issue (Plan 7.b kind), not a ref-specific kind. A `required = true` ref property whose value is *present but unresolved* is a `ref_dangling` issue.

### 4.6 Modify-only edge case

Per Plan 7.b §3.5: if a Modify call sets a ref property and the resolution fails, the Modify is rejected before any file write — no half-state. If a Modify *removes* a ref property, the corresponding edge is deleted from the same per-write transaction.

### 4.7 Cycle detection (when `acyclic = true`)

Before emitting the edge, the engine performs a cycle check via a recursive CTE on the `edges` table over the same edge type (mechanism reused from Plan 2's edge invariants and Plan 4's filter grammar). If the proposed edge would create a cycle, the resolution fails with `ref_cycle`.

### 4.8 Self-reference

Allowed by default. Combined with `acyclic = true` it gives the natural "tree" constraint (e.g., a ticket's `parent` can target another ticket but cannot create a cycle).

### 4.9 Watcher / reindex specifics

When the watcher detects a file change, the affected node is re-resolved end-to-end. Old ref edges (queried via `source = nodeID AND edgeType IN (refEdgeTypes for nodeType)`) are deleted; new ones are emitted from the latest frontmatter. This piggybacks on the existing edge-rewrite pipeline from Plan 2. Reindex performs the same per file. Reindex pre-image is always nil (per Plan 7.b §5.4); resolution always runs from the file.

---

## 5. Service / Reindex / Watcher / Doctor Integration

### 5.1 `node.Service.Create` order of operations

Plan 7.b's order, with one new pass appended:

1. Behavior `Validate` hooks (Plan 7).
2. Property validation (Plan 7.b).
3. **Ref resolution (new — §4.2).** Returns either a list of `(edgeType, targetID, ordinal)` tuples or a structured rejection.
4. Behavior `After` hooks (Plan 7).
5. Persist (file write + index commit) inside one transaction. Edge inserts from step 3 commit alongside the property writes.

Rejection at step 3 surfaces as the same per-property error structure Plan 7.b already renders. The error is annotated with the issue kind (`ref_dangling`, `ref_ambiguous`, etc.) for MCP structured output.

### 5.2 `node.Service.Modify` order of operations

Plan 7.b's order, with ref resolution appended after property validation. The pre-image fetch (Plan 7.b §4.3) provides the *previous* set of ref edges; ref resolution computes the *new* set; the per-write transaction inserts new edges, updates changed edges, and deletes removed edges.

### 5.3 Reindex integration

Per-file walk in `internal/reindex` invokes the ref resolver after property validation. Old ref edges for the node are deleted; new edges are inserted. No pre-image is consulted (Plan 7.b §5.4 invariant preserved). The reindex `Report` gains four counters parallel to Plan 7.b's existing `PropertyDrift` family: `RefDangling`, `RefAmbiguous`, `RefTypeMismatch`, `RefCycle`. Reindex summary line gets a corresponding row when any counter is non-zero.

### 5.4 Watcher integration

Watcher invokes the ref resolver via the same per-file path as reindex. No new watcher-specific code beyond plumbing the resolver into the existing dispatch.

### 5.5 Doctor extension

`internal/doctor` gains four issue kinds with the same row shape as Plan 7.b's `property_drift`:

```go
const (
    IssueRefDangling      = "ref_dangling"
    IssueRefAmbiguous     = "ref_ambiguous"
    IssueRefTypeMismatch  = "ref_type_mismatch"
    IssueRefCycle         = "ref_cycle"
)
```

Storage table mirrors `property_drift` (node ID, property name, issue kind, JSON-encoded details). CLI rendering follows the existing pattern; per-kind detail format:

```
ref_dangling     <node-id>  property=assignee value="alice" to=person
ref_ambiguous    <node-id>  property=assignee value="alice" to=person candidates=[people/alice-1, people/alice-2]
ref_type_mismatch <node-id> property=assignee value="people/bob" to=person actual_type=user
ref_cycle        <node-id>  property=parent path=[a,b,c,a]
```

Plan 7.b's MCP-doctor-tool deferral (ledger #5) carries forward; ref doctor issues are CLI-only in 7.c.1.

### 5.6 Lock + atomicity

`tusk pack add` acquires the workspace write lock for the duration of validate/merge/write (§2.6). Service ref resolution runs inside the existing per-write transaction so that property writes and edge inserts commit atomically.

---

## 6. MCP Surface

No new MCP tools in 7.c.1.

- Existing `tusk_node_create`, `tusk_node_modify`, `tusk_node_get`, `tusk_node_list` go through `Service`, which now handles refs transparently. Ref rejections surface in the existing MCP error envelope; the `kind` field carries the new issue kinds.
- `tusk_doctor` remains CLI-only per Plan 7.b ledger #5; ref doctor issues are CLI-only.
- `tusk pack add` does **not** get an MCP tool. Workspace-configuration commands intentionally don't live in the MCP surface (consistent with `tusk_init`).

---

## 7. File Layout

```
cmd/tusk/
  cmd_pack.go              # `tusk pack` parent command
  cmd_pack_add.go          # `tusk pack add` subcommand
  cmd_pack_add_test.go
internal/typepacks/
  aliases.go               # built-in name-to-URL map
  fetch.go                 # HTTP/file fetch with timeout, redirect cap, size cap
  validate.go              # disallowed-section + manifest-schema check
  merge.go                 # collision detection + --force splice
  pack.go                  # AddPack(ctx, source, force, workspaceDir) orchestrator
  pack_test.go
  testdata/
    sample.toml
    invalid-section.toml
    bad-toml.toml
internal/manifest/
  loader.go (modified)     # ref schema validation + auto-edge synthesis pass
  loader_test.go (extended)
internal/node/
  refs.go                  # parse-and-resolve algorithm; edge tuple synthesis
  refs_test.go
  service.go (modified)    # invoke resolver in Create/Modify ordering
internal/reindex/
  reindex.go (modified)    # invoke resolver in per-file walk
internal/doctor/
  doctor.go (modified)     # new issue kinds + CLI rendering
```

No new files in `internal/index/`; refs piggyback on the Plan 2 `edges` table.

---

## 8. Testing Strategy

### 8.1 Pack platform

`internal/typepacks/pack_test.go`:

- Name resolution: `"kanban"` → built-in URL; `"https://example.com/x.toml"` → passthrough; `"random"` (no `://`, not in alias map) → unknown-pack error.
- Fetch — `file://` from testdata: success; missing file → error.
- Fetch — HTTP via `httptest.NewServer`: 200 OK; 404; 500; redirect chain (3 hops OK; 4+ → error); response > 1 MiB → error.
- Pre-merge validation:
  - Bad TOML → reject.
  - `[workspace]` at top level → reject.
  - `[embeddings]` at top level → reject.
  - PropertyDecl errors propagated from manifest validator (e.g., `to` missing on ref property).
- Collision detection:
  - Pack with `[node-types.ticket]` against manifest containing `[node-types.ticket]`, no `--force` → reject; error names every overlapping section.
  - Same input with `--force` → success; original section deleted from in-memory manifest; pack appended.
  - Pack with no overlap → success.
- Atomic write (`cmd/tusk/cmd_pack_add_test.go`):
  - Successful run: temp file gone after `os.Rename`; `tusk.toml` contains both old content and pack body.
  - Simulated rename failure (mock filesystem): temp file cleaned up; original `tusk.toml` byte-identical to before.
- Lock behavior (extends Plan 3 lock-test pattern): `tusk pack add` blocks if the workspace lock is held by another process.
- Append style: comments and whitespace in the user's pre-existing `tusk.toml` are byte-preserved when no `--force` is used.
- `--force` splice: a colliding section in the middle of a user manifest with comments above and below is removed cleanly; non-colliding sections retain their comments.
- Exit codes: each documented exit code is reachable through a corresponding test scenario.

### 8.2 Ref schema

`internal/manifest/loader_test.go` extensions:

- `{ name = "assignee", type = "ref" }` (no `to`) → reject.
- `{ name = "assignee", type = "ref", to = "person" }` when `[node-types.person]` is not declared → reject.
- `{ name = "assignee", type = "ref", to = "*" }` → accepted.
- `{ name = "assignee", type = "ref", to = "person", values = ["a","b"] }` → reject (values inapplicable).
- Auto-edge generation: `{ name = "assignee", type = "ref", to = "person" }` declared on `[node-types.ticket]` → loaded `Manifest.EdgeTypes["assignee"]` exists with `From: ["ticket"]`, `To: ["person"]`, `Cardinality: "many-to-one"`.
- list-of(ref) with `ordered = false` overrides default-true.
- Inverse: `{ name = "watchers", ..., inverse = "watching" }` → synthesized edge type's `Inverse` field set.
- Acyclic: `{ name = "parent", ..., acyclic = true }` → synthesized edge type's `Acyclic` field set.
- Multi-source merge:
  - Same `assignee` declaration on `ticket` and `story` (consistent attributes) → `From: ["ticket", "story"]`.
  - Same name with different cardinality → reject with the §3.5 error message.
- Auto-edge collision with explicit `[edge-types.assignee]` → reject.

### 8.3 Ref resolution

`internal/node/refs_test.go`:

- `Service.Create` with `assignee: alice`, alice exists → edge inserted, file written.
- `Service.Create` with `assignee: alice`, alice doesn't exist → write rejected, file not created, error envelope includes `ref_dangling`.
- `Service.Create` with `assignee: alice`, two `person` nodes titled "alice" → write rejected, error envelope includes `ref_ambiguous` with both candidate IDs.
- `Service.Create` with `assignee: "[[people/alice]]"`, alice exists → edge inserted (path branch).
- `Service.Create` with `assignee: "[[people/missing]]"` → reject.
- `Service.Modify` removes `assignee` → edge deleted in same transaction.
- `Service.Modify` changes `assignee: alice` → `assignee: bob` → old edge deleted, new edge inserted in same transaction.
- `parent: ticket-id` with `acyclic = true`, where the assignment would create a cycle → reject with `ref_cycle`.
- `list-of(ref)` `watchers: [alice, bob]` → two edges, ordinal 0 and 1.
- `list-of(ref)` re-ordering via Modify preserves the new ordinals.
- Empty string and YAML `null` → no edge.
- Type mismatch: `assignee: "[[people/alice]]"` resolves to a node whose type is `user` (not `person`) → reject with `ref_type_mismatch`.

### 8.4 Reindex

`internal/reindex/reindex_test.go` extensions:

- Reindex of a workspace where a previously dangling ref now resolves (target was added since last index) → edge appears, `ref_dangling` doctor row cleared.
- Reverse: target removed externally → edge gone, `ref_dangling` row appears.
- Reindex `Report` carries non-zero counters for each issue kind and the summary line renders them.

### 8.5 Doctor

`internal/doctor/doctor_test.go` extensions: each new issue kind round-trips through SQLite storage and CLI rendering with the documented detail format.

### 8.6 End-to-end CLI smoke test

`cmd/tusk/cmd_pack_add_test.go`:

```
tusk init --name test-workspace
tusk pack add file://testdata/packs/sample.toml
# sample.toml declares [node-types.task] with a ref property to person
tusk node create --type person --title alice
tusk node create --type task --title "ship the thing" --prop assignee=alice
tusk query 'edge.assignee'  # exercises the auto-generated edge type via filter grammar
```

The smoke test verifies that pack add → reload manifest → ref resolution → query all compose end-to-end.

---

## 9. Open Questions / Residuals

1. **Pack URL stability.** The built-in alias map points at `main` branch URLs. When a v1.c plan ships a pack, the URL becomes live; when v1.d revises a pack, users running an older binary continue to fetch the latest pack at `main`. This is a deliberate v1.c choice — pack content evolves with the repo, not the binary. If pack-vs-binary version skew becomes a problem, we can pin to a release tag (or to the binary's git SHA) in a future revision. Carry forward.

2. **Multi-pack-add ordering during init.** Since `tusk init --type-pack <names>` is dropped (Q11a), users wanting multiple packs run `tusk pack add` per pack. If they shell-script a sequence and one fails, partial state is left in `tusk.toml` (the successful packs are merged, the failed one is not). This is acceptable: each command is atomic in isolation, and the user can `tusk pack add` again after resolving the failure. Document in the command help text.

3. **Pack TOML versioning.** Packs do not declare a schema version. If the manifest schema evolves (e.g., new required fields on `EdgeType`), older pack TOML on the live URLs becomes invalid. Acceptable v1.c semantics; we revisit if it bites.

4. **`ref` to a node type that gets removed later.** If the user `tusk pack add`s a pack defining `[node-types.person]` and `ticket.assignee → person`, then later removes the `[node-types.person]` block by hand-editing, the manifest fails to load (per §3.2 forward-reference rule). The user must remove or rewrite the dependent ref properties first. Consistent with Plan 7.b's "off-schema is warned, not rejected" framing — except here it's a manifest validation error, not a node-validation error, because the *schema itself* is inconsistent. Document.

5. **Title collision under `to == "*"`.** Title lookup with `to == "*"` is more likely to be ambiguous (any node-type qualifies). Doctor surfaces it via `ref_ambiguous`. If usage shows this is too noisy, we add a secondary disambiguation rule (e.g., prefer the node whose type is most-recently-modified) in a follow-up plan. Defer.

6. **Bare path-string values.** `parent: tickets/auth-cleanup` (without wikilink brackets) is treated by §4.2 as a title lookup against `tickets/auth-cleanup`. By convention titles don't contain slashes, so the lookup typically dangles and surfaces as `ref_dangling`. Users wanting path lookups must use wikilink syntax `[[tickets/auth-cleanup]]`. Bare-path-string-as-ID parsing is in the §10 deferred ledger.

---

## 10. Plan 7.c+ Ledger (Deferred Items)

| # | Deferred item | Rationale |
|---|---|---|
| 1 | Tags pack content (`tag` node type, `tagged` edge type, auto-creation hook, frontmatter `tags:` shorthand) | Plan 7.c.2. Built on `ref` from this plan. |
| 2 | Kanban pack content (`ticket`, `project`, `parent`, `blocks`, `tagged`; `[behaviors.workflow.kanban]`) | Plan 7.c.3. |
| 3 | Vault pack content (`note`, `meeting`, `decision`; `references`, `relates-to`) | Plan 7.c.4. |
| 4 | Subcommand shortcut machinery (`tusk ticket open`, `tusk note new`, etc.) | Deferred from v1.c entirely. Future plan ships shortcuts as a manifest config rather than inference. |
| 5 | Filter-grammar shortcuts (`+tag` / `-tag`) | Dropped from v1.c entirely. May revisit much later. |
| 6 | `tusk pack list` for discovery | Small, additive; revisit if usage shows demand. |
| 7 | `tusk pack remove <name>` for un-merging | Same. Implies tracking which sections came from which pack — requires either a per-pack tag in the manifest or heuristic matching. |
| 8 | Pack URL pinning to a release tag or SHA | If pack-vs-binary version skew causes problems, ship a `--version` flag on `tusk pack add` or a per-pack `version` annotation. |
| 9 | Bare path-string ref values (e.g., `parent: tickets/auth-cleanup` without `[[]]`) | Plan 2's rename rewrite pipeline would gain a property-value-rewrite path. Defer until usage shows the wikilink ceremony is a real friction. |
| 10 | MCP `tusk_pack_add` tool | Workspace-configuration commands intentionally don't live in the MCP surface. Revisit only if a strong agent-driven use case emerges. |
| 11 | Cross-pack `ref` references (a pack's ref property targeting a type defined in another pack) | Out of scope; would require a multi-pack composition layer Plan 7.c.1 explicitly avoids. Workaround: declare both types in the same pack, or document the install-order requirement. |
| 12 | Pack TOML schema version field | Defer until manifest schema evolution makes this concrete. |

Plan 7.b's residual ledger items not picked up by 7.c.1 stay open for 7.c.2+/7.d+:

- All Plan 7.b §10 items #4-#10 (read hooks, MCP doctor surface, typed property accessors, workflow-states-to-enum-values, deprecated-property markers, `tusk node-types show`, `tusk doctor explain <node-id>`) carry forward unchanged.
- Plan 7's residuals (cascading behaviors, drift surface dedup) carry forward unchanged.
