# Node & Edge Source Namespace — Design

**Status:** Draft
**Date:** 2026-05-25
**Author:** German Meza (with Claude Code)

## Summary

Replace the overloaded `nodes.type` / `edges.type` columns with a
layered `(kind, source, type)` shape on both tables. `kind` makes
row-class explicit on nodes (`file` | `subunit`) and edge origin
explicit on edges (`direct` | `derived` | `structural`). `source`
scopes built-in type-pack reservations to their owning pack. Together
they create a clean extension point for future AST families (Go,
Python, TS, …) that will contribute sub-units and their own reserved
kinds.

Today's only AST sub-unit source is `markdown`; this spec ships the
schema and reservation model so adding the next AST pack is purely
additive.

This is an incompatible index schema change. Tusk's semantic version
stays in `1.x.x` because file format, manifest grammar, CLI surface,
and MCP wire format are unaffected — but indexes written by any
prior 1.x release are no longer readable. On first run after upgrade
the index is dropped and rebuilt from source files via the existing
`reindex` pipeline. The cost is a one-time full reindex.

## Background

Tusk indexes a markdown vault as a typed graph. Files (notes, people,
projects) become "file" rows in `nodes`; markdown sub-document units
(sections, paragraphs, list items, …) become "sub-unit" rows attached
to their parent file via `parent_id`. Edges connect any two nodes and
carry a type name that may be user-declared, ref-derived from a
node-type's `references` property, or owned by a built-in pack.

The current schema has a single `type` column on each of `nodes` and
`edges`. The `subdocument` type-pack reserves a fixed set of names
globally to prevent user declarations from colliding with sub-unit kinds
and built-in edges:

- Nodes: `section`, `paragraph`, `list-item`, `code-block`,
  `blockquote`, `table-cell`
  (`internal/typepacks/subdocument/pack.go:26`).
- Edges: `contains`, `contained-by`
  (`internal/typepacks/subdocument/pack.go:39`).

Row-class on `nodes` is encoded implicitly as `parent_id IS NOT NULL`
(sub-unit) vs `parent_id IS NULL` (file). Code that needs to
distinguish the two reads `parent_id`'s nullity directly in several
places (`internal/index/node_repo.go`,
`internal/manifest/subunits.go`, `internal/doctor/doctor.go`, the
partial UNIQUE index on `nodes.path`).

## Problems

Three distinct problems live inside the single `type` column today.

**Problem A — Row-class is implicit.** The `parent_id IS NOT NULL`
convention is correct but scattered. Query writers must remember it.
The `type` value alone does not tell a reader whether they are looking
at a file or a sub-unit.

**Problem B — User types and pack types share one namespace.** Because
`nodes.type` and `edges.type` are global string namespaces, the
`subdocument` pack permanently steals six node-type names and two
edge-type names from the user's vocabulary. A user cannot declare a
node-type called `section` or an edge-type called `contains` even when
the semantics differ entirely from the pack's.

**Problem C — Future AST families would collide with each other.** A
Go AST pack would want sub-unit kinds like `function`, `struct`,
`interface` and edge-types like `calls`, `imports`. A Python pack
would also want `function` and `imports`. Within the current
single-namespace model they would compete with each other and with the
existing markdown reservations.

**Problem D — Edge origin is unrecoverable.** Edges today carry no
record of how they were produced. A `tagged-with` edge could have
come from a user writing it directly in frontmatter, from
`synthesizeRefEdgeTypes` derived from a node-type's `references`
declaration, or — in future — from an AST pack. The three have
different lifecycles (user edges die when the property value is
removed; derived edges die when the declaration is removed;
structural edges are rewritten whenever their producing pack
re-runs), but doctor diagnostics, rebuild operations, and
lifecycle-aware tooling have no way to tell them apart. Recovering
this information after the fact requires re-parsing the manifest
that was active when the edge was written, which is not durable.

## Goals

- Add an explicit row-class column (`kind`) on `nodes` so the
  `parent_id IS NOT NULL` convention is replaced by direct column
  reads.
- Add an explicit edge-origin column (`kind`) on `edges` so the
  lifecycle of a row (`direct` | `derived` | `structural`) is
  recoverable from the row itself, not from reconstructing the
  manifest active at write time.
- Add a namespace column (`source`) on both `nodes` and `edges` so
  type-name reservations are scoped to the source that owns them
  rather than to the global type space. `NULL` is the user
  namespace (user-declared types and edges). The only non-null
  source today is `'markdown'`, contributed by the built-in
  `subdocument` type-pack; future type-packs and user-configurable
  sources extend the column without further schema changes.
- Preserve `parent_id` on `nodes` for its FK role; the column stops
  doubling as the row-class discriminator but remains the structural
  parent pointer used for joins.
- Treat the change as an incompatible index schema bump: existing
  indexes are dropped and rebuilt from source files on first run
  after upgrade, rather than backfilled in place. The user-facing
  contract (file format, manifest grammar, CLI surface, MCP wire
  format) is unchanged, so tusk's semantic version stays in
  `1.x.x`.
- Preserve observable behavior: queries, graph-expansion, and MCP
  context returns match pre-upgrade results for the same workspace
  (bare-name union semantics preserve today's matches; the rebuilt
  index just carries additional `kind`/`source` columns).
- Define an unambiguous reference-resolution rule for type names in
  user-facing config (graph-expansion, queries) so the change is
  backward-compatible.

## Non-goals

- No new AST family is implemented in this spec. Go, Python, TS, and
  other future packs are validation points for the design, not
  deliverables.
- No change to sub-unit hash format, the embed pipeline, or the AST
  chunker.
- No change to how user manifests are authored beyond the new
  reservation scoping. Existing manifests continue to load unchanged.
- No change to the wikilink resolver, the property-drift recorder, or
  the alias dispatch.

## Design

### Nodes table

Add two columns:

- `kind TEXT NOT NULL` — structural row-class. Values: `file` |
  `subunit`.
- `source TEXT NULL` — namespace identifier for sub-unit type
  names. `NULL` for `kind='file'` rows. `'markdown'` for
  sub-units produced by the `subdocument` type-pack today. Future
  AST packs add new non-null values.

`parent_id` is retained for its existing FK role and stays NULL on
file rows. A CHECK constraint enforces that the two new columns and
`parent_id` agree:

```
CHECK (
  (kind = 'file'    AND source IS NULL     AND parent_id IS NULL) OR
  (kind = 'subunit' AND source IS NOT NULL AND parent_id IS NOT NULL)
)
```

Indexes:

- `nodes_kind_type_idx ON nodes(kind, type)` replaces
  `nodes_type_idx`. Most queries that grouped or filtered by type
  also implicitly restricted by row-class (sub-unit doctor counts,
  user-type lookups); composite covers both.
- The existing partial UNIQUE index on `path` (file rows only) is
  rewritten to predicate on `kind = 'file'` instead of
  `parent_id IS NULL`.

### Edges table

Add two columns:

- `kind TEXT NOT NULL` — edge origin. Values: `direct` |
  `derived` | `structural`.
  - `direct` — edge written from a user frontmatter property value
    (e.g., `mentions: [people/dana]`).
  - `derived` — edge synthesized from a node-type's `references`
    declaration (e.g., the `tagged-with` edge generated by
    `manifest.synthesizeRefEdgeTypes`).
  - `structural` — edge produced by an AST/structural pack
    (`contains`, `contained-by` from the `subdocument` pack today;
    `calls`, `imports` from future packs).
- `source TEXT NULL` — namespace identifier for edge type names.
  `NULL` for `direct` and `derived` edges (both live in the user
  namespace). `'markdown'` for `structural` edges produced by the
  `subdocument` type-pack. Future AST packs add new non-null
  values.

A CHECK constraint enforces source/kind agreement, mirroring the
nodes table:

```
CHECK (
  (kind IN ('direct', 'derived') AND source IS NULL) OR
  (kind = 'structural'           AND source IS NOT NULL)
)
```

UNIQUE constraint becomes:

```
UNIQUE(source, type, source_id, target_id, source_path)
```

SQLite treats NULL as distinct in UNIQUE constraints, but every
concrete row has a deterministic `source`, so this works as intended.

Indexes:

- Add `edges_source_type_idx ON edges(source, type)` to keep
  `(source=?, type=?)` lookups fast.
- Add `edges_kind_idx ON edges(kind)` to support origin-filtered
  queries ("rebuild all derived edges," "show user-authored edges
  only," doctor lifecycle diagnostics).
- Existing `edges_type_idx` is retained for bare-name lookups.

`kind` is an internal/queryable column. It does not appear in the
`<source>:<type>` notation and is never user-typed in config or
queries — it is set by the writer (manifest loader, sub-unit sync,
frontmatter ingest) based on the edge's origin.

### The three-column model in practice

After the change, the rows look like:

| id | kind | source | type |
|---|---|---|---|
| `notes/standup-2026` | `file` | `NULL` | `note` |
| `people/dana` | `file` | `NULL` | `person` |
| `notes/standup-2026#a3f1` | `subunit` | `markdown` | `section` |
| `notes/standup-2026#b7e2` | `subunit` | `markdown` | `paragraph` |
| *(future)* `code/foo.go#fn-bar` | `subunit` | `go` | `function` |
| *(now possible)* `notes/section-overview` | `file` | `NULL` | `section` |

The last row demonstrates the payoff: a user can declare a node-type
called `section` because `(file, NULL, section)` and
`(subunit, markdown, section)` are distinct addresses.

Edges follow the same pattern with `(kind, source, type)`:

| kind | source | type | source_id | target_id |
|---|---|---|---|---|
| `direct` | `NULL` | `mentions` | `notes/standup-2026` | `people/dana` |
| `derived` | `NULL` | `tagged-with` | `notes/standup-2026` | `tags/retro` |
| `structural` | `markdown` | `contains` | `notes/standup-2026` | `notes/standup-2026#a3f1` |
| `structural` | `markdown` | `contained-by` | `notes/standup-2026#a3f1` | `notes/standup-2026` |
| *(future)* `structural` | `go` | `calls` | `code/foo.go#fn-a` | `code/foo.go#fn-b` |
| *(now possible)* `direct` | `NULL` | `contains` | `projects/x` | `projects/y` |

### Type-pack reservation model

The current global reservation in
`internal/typepacks/subdocument/pack.go` becomes scoped:

- `ReservedNodeTypes` is no longer a global list. It declares the
  names this pack owns *within `source='markdown'`*. The user
  namespace (`kind='file', source=NULL`) and any other pack's
  namespace are unaffected.
- `ReservedEdgeTypes` likewise becomes "names owned within
  `source='markdown'`".
- The manifest validator's `SubUnitConflict` check is rescoped: it
  fires only when two declarations collide *within the same
  `source`*. User declarations always live at `source=NULL` and
  pack declarations always live at `source!=NULL`, so the check
  never fires for the user-vs-pack case under this design. The
  validator stays present but becomes a guard against future
  manifest extensions (e.g., user-configurable sources) that could
  reintroduce within-source collisions.

Each future AST pack ships its own reservation list scoped to its
own source. Adding a Go pack is a new file in
`internal/typepacks/go/pack.go` declaring `ReservedNodeTypes`
within `source='go'`; no edits to other packs.

### Reference resolution rule

Type names appear as bare strings in user-facing surfaces:
graph-expansion config (`query.graph-expansion.edge-types`), query
filters, MCP arguments. Three forms are accepted, and a single rule
governs each:

| Form | Parsed scope | Matches rows where |
|---|---|---|
| `contains` | `ScopeAny` | `type = 'contains'`, any source |
| `markdown:contains` | `ScopeSource("markdown")` | `source = 'markdown' AND type = 'contains'` |
| `:contains` | `ScopeUser` | `source IS NULL AND type = 'contains'` |

Default semantics are **union** (bare name = any source). Rationale:

- For traversal and graph expansion, "follow all containment edges"
  is overwhelmingly the right default; pack origin is rarely the
  filter the user wants.
- Backward-compatible: today only the pack writes `contains`, so a
  bare `contains` reference matches exactly what it matched before.
- No shadowing surprises: a user adding their own `contains`
  declaration cannot silently subtract from what existing queries
  traverse; it can only add.

The qualified forms are available for the rarer cases where the user
wants to restrict to a single pack or to their own namespace.

### Naming conventions

`<source>:<type>` is the canonical notation for any type reference in
the system. The bare `<type>` form is a convenience shorthand whose
matching behavior is defined by the union rule above; the qualified
form is the unambiguous, stable address of a row in the graph.

Three forms, one grammar:

```
<source>:<type>     fully qualified — scoped to one source
:<type>             scoped to the user namespace (source is NULL)
<type>              shorthand — matches any source (union)
```

Implications:

- **Stability over convenience.** Configurations that need stable
  meaning across future user declarations — graph-expansion configs
  in shared workspaces, MCP arguments wired by automation, queries
  embedded in dashboards — should prefer the qualified form. The
  shorthand is fine for ad-hoc use and for the common case where
  there is only one source for a type.
- **One grammar across surfaces.** The same `<source>:<type>` parsing
  applies to manifest config, query filters, MCP arguments, and the
  CLI. Implementations live in `internal/manifest` (config parse),
  `internal/query` (query/filter parse), and the MCP boundary; they
  share a single parser.
- **No new reserved characters in type names.** Type names today are
  drawn from `[a-z0-9-]`; the `:` separator is unambiguous against
  that grammar. The bare form remains backward-compatible with every
  existing config.

This notation is the standard way to disambiguate the union-default
case in the [Risks](#risk--mitigations) section: a user concerned
that their config might drift in meaning as they add declarations
locks the qualifier (`markdown:contains` or `:contains`) and the
match is pinned to a single source forever.

### Reference resolution in code

The walker and query layers parse incoming type strings into an
internal ref:

```go
type EdgeRef struct {
    Scope EdgeRefScope // ScopeAny | ScopeUser | ScopeSource
    Source string      // populated only when Scope == ScopeSource
    Type string
}
```

`NeighborsByEdgeTypes` (`internal/index/edge_repo.go:167`) becomes
`NeighborsByEdgeRefs`, building a grouped OR clause:

```go
for _, ref := range refs {
    switch ref.Scope {
    case ScopeAny:
        clauses = append(clauses, "type = ?")
        args = append(args, ref.Type)
    case ScopeUser:
        clauses = append(clauses, "(source IS NULL AND type = ?)")
        args = append(args, ref.Type)
    case ScopeSource:
        clauses = append(clauses, "(source = ? AND type = ?)")
        args = append(args, ref.Source, ref.Type)
    }
}
where := "(" + strings.Join(clauses, " OR ") + ")"
```

Node-type references follow the same parsing rule and grouped-OR
construction, applied to `nodes(source, type)`.

### Index schema bump & rebuild

This change is an incompatible index schema change. Markdown files
and manifest grammar are unaffected; only the derived SQLite index
needs to be reshaped. Tusk's semantic version stays in `1.x.x`
because the user-facing contract — file format, manifest format,
CLI surface, MCP wire format — does not change. The breaking
boundary is purely internal: the on-disk index from any prior 1.x
release is no longer readable by the new binary.

Rather than backfilling existing rows in place, the new binary
drops and rebuilds the index from source files. This is the right
model for two reasons:

- **Source of truth.** The markdown files and manifest TOML are
  authoritative. The index is a derived cache. A schema-shape
  change should rebuild the cache, not transform it.
- **Correctness over cleverness.** A backfill would need to
  reconstruct the manifest-time origin of every edge to decide
  `direct` vs `derived` — fragile and version-coupled. A rebuild
  populates `kind` and `source` at write time, by the writer that
  knows the origin first-hand.

#### Schema-version contract

A `schema_version` key in the `meta` table records the schema
generation the on-disk index was last written by. The constant
lives in `internal/index` and is bumped to a new value as part of
this change.

On `index.Open`, the index reads `meta.schema_version`:

- Match → open normally.
- Mismatch (or missing, for legacy indexes that pre-date the key) →
  return a sentinel `ErrSchemaIncompatible` carrying the observed
  and expected versions.

Consumers decide how to react:

- **CLI commands** (`tusk reindex`, `tusk doctor`, `tusk query`,
  `tusk mcp serve`, watcher) catch `ErrSchemaIncompatible`, log a
  one-line message (`index schema changed in this version,
  rebuilding…`), delete the on-disk index file, re-`Open` at the
  same path to get a fresh empty database, and trigger
  `reindex.Run` over the workspace before continuing with the
  requested command.
- **MCP runtime** runs the same sequence silently behind a status
  notification so the calling agent sees a brief unavailability
  followed by the new index online.

To avoid duplicating the catch-rebuild-retry logic at every CLI
entry point, the wrapping lives in one helper (e.g.,
`index.OpenOrRebuild`) that takes the workspace root and the
reindex configuration; every consumer calls the helper instead of
`Open` directly.

The rebuild reuses the existing `reindex` pipeline; no new
ingestion code is required. Writers (manifest loader, frontmatter
ingest, sub-unit sync, AST packs) populate `kind` and `source`
correctly on every row they create, so the rebuilt index is
correct by construction.

#### What users see

- First `tusk` invocation after upgrade emits a one-line rebuild
  message and runs a full reindex. On a typical workspace this is
  the same cost as `tusk reindex --force` today.
- Subsequent invocations open instantly; the rebuild only runs
  once per schema bump.
- Release notes for the version that ships this change call out
  the one-time rebuild explicitly.
- Graph-expansion configs and queries continue to return the same
  results post-rebuild (bare-name union semantics preserve today's
  matches; only `kind`/`source` columns are added to the rows).
- User manifests load without modification; existing reservations
  relax (sub-document names are no longer in the user namespace's
  reserved set).

#### What the schema bump removes

No legacy migration code is added for this change. The existing
P2 sub-unit migrations and embeddings UNIQUE tightening
(`internal/index/index.go`) become dead code under the rebuild
model: any pre-existing index lacks the new `schema_version` key,
so `Open` returns `ErrSchemaIncompatible` and the file is dropped
before the old migration path could run.

Deleting the dead migration code is folded into Phase 6 of the
implementation plan, along with one correction the dead code's
absence makes possible: the embeddings table moves from
`UNIQUE(node_id)` (the shape `migrateEmbeddingsPrimaryKey`
ratcheted toward) to `UNIQUE(node_id, chunk_idx)`. The previous
shape silently collapsed every chunk of a multi-chunk node onto a
single row, which made the `embeddingsMatch` hash-skip in
`internal/embed/drain.go` permanently unable to short-circuit
re-embed for multi-chunk nodes — every reindex pass re-embedded
every chunk regardless of whether content changed. The corrected
DDL restores the hash-skip's intended behavior and is safe to
adopt only because the dead migration is being removed in the
same phase; otherwise the migration would try to ratchet new
indexes back to the wrong shape.

The embeddings DDL correction triggers its own `schema_version`
bump and therefore one additional transparent rebuild via
`OpenOrRebuild`. Users upgrading across the whole feature still
experience a single rebuild because mismatch detection rebuilds
to the current value regardless of how many bumps occurred.

### Testing

Each layer has its own coverage target. All tests live in the
package they exercise.

- **Fresh-DB schema shape** (`internal/index`): table
  introspection asserts the new columns, indexes, UNIQUE shape,
  and CHECK constraints exist after `Open` on a fresh DB. Asserts
  `meta.schema_version` is written to the expected constant.
- **Schema-version mismatch is detected** (`internal/index`): an
  index seeded with a different `schema_version` (or missing the
  key) returns `ErrSchemaIncompatible` from `Open`, with the
  observed and expected versions on the error.
- **Drop-and-rebuild helper rebuilds correctly** (`internal/index`
  or `cmd/tusk` depending on where `OpenOrRebuild` lives): given
  a fixture workspace and a stale index file, the helper deletes
  the file, opens a fresh index, runs `reindex.Run`, and the
  resulting index contains the expected `(kind, source, type)`
  rows for every fixture file.
- **Reservation scoping** (`internal/manifest`,
  `internal/typepacks/subdocument`): a manifest declaring a
  user-namespace `section` node-type loads cleanly (no longer
  raises `SubUnitConflict`); a synthetic within-source collision
  (two declarations both targeting `source='markdown'`,
  `type='section'`) still raises `SubUnitConflict` to confirm the
  rescoped validator's guard semantics.
- **Reference parsing** (`internal/query`,
  `internal/graphexpand`): the three forms parse to the correct
  scope; round-trip tests confirm bare names match union,
  qualified forms restrict correctly.
- **Edge walker** (`internal/graphexpand`): the three scenarios
  from the design discussion (bare-name from a file seed,
  bare-name from a subunit seed, mixed scoped list) return the
  expected edges.
- **Sub-unit sync** (`internal/subunit`): inserted sub-units carry
  `source='markdown'`; inserted `contains` edges carry
  `(kind='structural', source='markdown')`.
- **Edge kind on inserts** (`internal/manifest`,
  `internal/subunit`, frontmatter ingest): every edge writer
  populates `kind` correctly — `synthesizeRefEdgeTypes` writes
  `derived`, frontmatter property-value ingest writes `direct`,
  AST packs write `structural`. CHECK constraints accept every
  writer-produced row.

### Code touchpoints

Approximate impact, grouped by package:

- `internal/index` — schema DDL, `schema_version` constant and
  `meta` integration, `ErrSchemaIncompatible` sentinel returned by
  `Open`, repo writes/reads, composite indexes, CHECK constraints
  on both tables.
- A new small helper layer (likely `cmd/tusk/internal` or a sibling
  package — exact location chosen at plan time to avoid an
  `index` ↔ `reindex` import cycle) provides `OpenOrRebuild`,
  which calls `index.Open`, catches `ErrSchemaIncompatible`,
  deletes the on-disk file, re-`Open`s, and runs `reindex.Run`.
- `cmd/tusk` and `internal/mcp` — replace direct `index.Open`
  calls with the new `OpenOrRebuild`, so every entry point handles
  the schema bump uniformly.
- `internal/subunit/sync.go` — sub-unit inserts populate `source`;
  `contains` edge writes populate `(kind='structural', source='markdown')`.
- `internal/manifest` — loader's `synthesizeRefEdgeTypes` writes
  `(kind='derived', source=NULL)`; frontmatter property-value
  ingest writes `(kind='direct', source=NULL)`; subunit-conflict
  validator scopes its check.
- `internal/typepacks/subdocument` — reservation lists reframed as
  scoped to `source='markdown'`.
- `internal/graphexpand` — walker takes `[]EdgeRef`; builds grouped
  OR clauses.
- `internal/query` — semantic and matched-units queries parse
  incoming type names into refs and pass them through.
- `internal/doctor` — sub-unit pane counts read `kind='subunit'`
  instead of `parent_id IS NOT NULL`.
- `internal/node` — wikilink, edges, and types helpers stop
  reading `parent_id` as a row-class signal where they currently do.

### Risk & mitigations

- **One-time reindex cost on upgrade.** First `tusk` invocation
  after upgrading triggers a full rebuild from source files. Cost
  scales with workspace size and is identical to running
  `tusk reindex --force`. Mitigated by emitting a clear log line
  so the latency is attributable, and by documenting the rebuild
  in release notes. Repeat invocations are unaffected.
- **Stale `parent_id IS NOT NULL` reads in unreviewed code paths.**
  Mitigated by full grep and removal of the pattern as part of the
  implementation; the CHECK constraint guarantees the two columns
  agree, so any stale read is at most stylistic.
- **Bare-name union changing meaning if a user later adds a
  colliding declaration.** Acknowledged and accepted: union
  semantics mean adding a row can never *remove* matches from an
  existing query, only add. This is the intended trade vs. the
  shadowing hazard of first-match resolution. Users who need
  stability against future declarations can use the canonical
  `<source>:<type>` form (see [Naming conventions](#naming-conventions))
  to pin a reference to a single source.

## Future extensions

The `source` column is intentionally open-ended. This spec writes
only `NULL` (user) and `'markdown'` (the built-in pack), but nothing
in the schema, the parser, or the resolution rule restricts the set
of source values. The design anticipates two natural extensions
without requiring schema changes:

- **Additional AST sources.** A source is more than a declarations
  file: it contributes reserved node-types and edge-types, a parser
  that turns files into units, and a sync step that writes those
  units as `(subunit, <source-name>, …)` rows plus their
  `(structural, <source-name>, …)` edges. Today the markdown source
  is scattered across `internal/typepacks/subdocument/` (declarations)
  and `internal/subunit/` (parser + sync), with references woven
  through `manifest`, `index`, and `reindex`. A follow-up spec is
  expected to consolidate these into per-source packages — most
  likely `internal/sources/<name>/` — so adding a Go or Python AST
  becomes a self-contained module rather than a multi-package
  refactor. The schema in this spec already accommodates any number
  of named sources; only the code organization needs to catch up.
- **User-configurable sources.** The same notation that namespaces
  AST-source types can later namespace user-declared types. A
  manifest could declare `[sources.work]` and `[sources.personal]`,
  with `[node-types.work:meeting]` and `[edge-types.work:contains]`
  living inside them, letting a single workspace partition its own
  vocabulary the way built-in sources do. The grammar, storage, and
  resolution rule are already in place; only the manifest parser,
  the reservation validator, and a small UX layer would need to
  grow.

Both extensions are out of scope here but are explicitly enabled by
this spec. Future work should not need to re-shape the schema to
accommodate them; the next architectural question is code
organization (the source-package consolidation above), not schema.
