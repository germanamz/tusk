---
type: spec
title: Agent Retrieval Improvements — Reducing Round-Trips
---

# Agent Retrieval Improvements

- **Status:** Draft
- **Date:** 2026-05-23
- **Author:** German Meza
- **Builds on:** `2026-05-05-tusk-v1-rebuild-design.md`

---

## 1. Summary

Tusk v1 is increasingly used as an agent's medium-term memory: notes, summaries,
decisions, documentation, books, and anything else relevant to a project. Agents
reach for tusk in the same role users reach for Claude Code memories or Obsidian
— a place to put things now so that future invocations of the agent can find
them.

The agent ↔ tusk loop today, however, burns more round-trips than it should. A
typical "find context for what I'm about to work on" run looks like:

```
tusk_status → tusk_query → tusk_node_get × N → tusk_edge_list × N
```

Each call costs a tool call, a JSON parse, and a tail of agent context. The goal
of this spec is to collapse that loop.

Three phased changes, in order of dependency and shipping order:

- **P1 — Rich query + memory entry point.** `tusk_query` learns to return
bodies, edges, and properties on demand. A new `tusk_context` tool gives agents
a single warm-context call at session start, composed from named **aliases**
(manifest-declared CLI invocations that any tool can wrap). A `format=compact`
mode halves token usage.
- **P2 — Sub-document AST.** Markdown is parsed into a deterministic AST.
Sections, paragraphs, list-items (with checkbox state), code blocks, and
blockquotes become first-class nodes in the graph. Identity is a content hash
scoped to the parent file. Embeddings move from file-level to sub-unit-level.
- **P3 — Graph-aware semantic ranking.** Edges contribute to nearest-neighbor
scoring. An agent searching for "auth" finds the OAuth-flow paragraph because it
links to nodes the auth keyword matches, not because the paragraph happens to
contain the word "auth".

P3 is technically independent of P2; it operates on whatever graph exists. It
becomes substantially more useful after P2 lands because the graph is
finer-grained.

---

## 2. Motivation

Concrete round-trips this spec eliminates:

1. **Find → read loop.** `tusk_query` returns `{id, type, path, title}` per hit.
An agent that needs to actually use the content calls `tusk_node_get` per hit.
(P1.A)
2. **Edge inspection.** Knowing which nodes a hit relates to requires a separate
`tusk_edge_list` call per node. (P1.A)
3. **Cold start.** Each new agent session re-discovers the workspace shape, the
pinned context, and recent activity through 3–5 exploratory calls. There is no
single "give me the warm context" tool. (P1.D)
4. **Whole-file retrieval for a one-paragraph hit.** A semantic query for "OAuth
decision" returns `notes/auth-rfc` (the whole file). The agent reads thousands
of tokens of unrelated content to use the one paragraph that matched. (P2)
5. **Word-match-only semantic retrieval.** A query about "session storage"
misses a paragraph about "cookie persistence" because the embedding model never
connects them — even though the paragraph lives one wikilink away from a node
titled "session storage decision". (P3)
6. **JSON overhead.** Even thin result sets pay JSON's quoting and bracket cost.
For result lists rendered for agent consumption, a tabular text form is 30–50%
smaller. (P1.E)

---

## 3. Out of Scope

- **Structured edit operations (`tusk_node_patch`).** Tusk is a retrieval
engine; it does not own the filesystem write path beyond the small set of
operations it already exposes (`tusk_node_create`, `tusk_node_modify`,
`tusk_node_move`, `tusk_node_delete`). Adding section-level / bullet-level edit
ops would be mission creep — agents edit markdown with the filesystem, and the
watcher catches the change.
- **Auto-generated section summaries.** Would require a generation provider
beyond Ollama embeddings. Out of scope; not revisited until tusk gains a
generation dependency for another reason.
- **Phrase / sentence indexing.** Too noisy and explodes the index without
proportional retrieval gain.
- **Sub-unit → sub-unit edges (inbound to sub-units).** Requires durable
structural identity which conflicts with the hash-as-identity model adopted in
§5.2. Wikilinks always target file nodes, never sub-units (§5.4). Promote a
paragraph to its own file when it needs to be a cross-document reference target.
- **Server-side response truncation (`max_tokens`).** Agents are responsible for
their own context budgets in v1.
- **Aliases with parameters / templating.** v1 aliases are parameter-free
strings. Argument substitution (`$1`, `${ticket-id}`) is future work; revisit
only with a concrete use case.

---

## 4. Phase 1 — Rich Query and Memory Entry Point

### 4.1 `include` and `fields` knobs on read tools

`tusk_query`, `tusk_node_get`, and `tusk_node_list` gain two optional arguments:

- `include = [body, edges, properties]` — opt-in expansion. Default remains
`[id, type, path, title]` for back-compat.
  - `body` — full markdown body. For semantic `tusk_query`, the body shown is
  the best-matching unit's body (already computed in `filter.SemanticRank`), not
  the entire file.
  - `edges` — 1-hop outgoing and incoming, as `{type, direction, target_id,
  target_title}` pairs. No further traversal.
  - `properties` — frontmatter properties as a flat string-keyed map.
- `fields = [...]` — projection. When set, replaces the default field set
entirely. Implies the matching `include` entries; `fields = [id, title, body]`
is equivalent to `include = [body], fields = [id, title, body]`.

These are additive: nothing changes for callers that pass neither.

### 4.2 Aliases — named CLI sub-command invocations

Top-level manifest blocks declare named aliases. Each alias binds a name to a
tusk CLI sub-command plus a typed argument map. The CLI command tree (Cobra) is
the registry; no new parser is introduced. Aliases are read-only by convention:
write-side verbs (`node create`, `node modify`, `edge add`, `edge remove`, `node
move`, `node delete`) are rejected as alias targets at manifest load.

```toml
[alias.open-tickets]
command = "node list"
description = "Active tickets, by priority" # optional; surfaces in `tusk run --list`
args.filter = "type=ticket status=active"
args.sort = "priority-desc"
args.top = 10

[alias.charter]
command = "node get"
args.id = "docs/agent-charter"   # positional, named
args.include = ["body", "edges"]

[alias.recent-decisions]
command = "node list"
args.filter = "type=decision modified-since:7d"
args.sort = "modified-desc"
args.top = 5

[alias.health]
command = "doctor"
```

The shape: `command` is a CLI sub-command path (`node list`, `query`, `doctor`,
...) resolved against the Cobra command tree. `args` is a TOML table whose keys
map either to flag names on the resolved command (e.g., `--top` → `args.top`) or
to named positionals (e.g., `tusk node get <id>` → `args.id`). Values are
TOML-typed; no shell tokenization, no flag-string parsing, no quoting concerns
for filter expressions.

Aliases are parameter-free in v1 — see §3 out-of-scope and §8.2. Each alias
wraps one tusk verb; composition lives in `tusk_context` (§4.3).

#### Dispatch

1. Resolve `command` against the Cobra command tree → `*cobra.Command`.
2. For each `args.X`, set the corresponding flag (or named positional) on the
   resolved command with its typed TOML value.
3. Call the service function the Cobra `RunE` would delegate to (existing
   read-side verbs are refactored as a P1.B prerequisite so the service layer is
reachable independently of the Cobra `RunE`).

The MCP `tusk_<noun>_<verb>` tools become thin shells over the same service
layer. CLI invocation, `tusk run`, `tusk_run`, and the per-verb MCP tools all
share one dispatch path.

#### Positional-name registry

Some CLI verbs take positionals (`tusk node get <id>`, `tusk query <filter>`).
Cobra does not name positionals on its own. P1.B introduces a small in-engine
registry (one entry per read-only verb) declaring positional names so `args.id`
and `args.filter` resolve unambiguously. The registry doubles as the schema
`tusk_run` exposes for introspection.

#### Invocation

- CLI: `tusk run <alias-name>` — dispatches to the resolved verb. Output shaped
by `--format` (defaults to compact for TTY, JSON for `--json`).
- CLI listing: `tusk run --list` — prints alias names, descriptions, and the
resolved `command` + `args` for each.
- MCP: `tusk_run(alias)` — returns the structured result of the underlying verb,
tagged with `{alias, command, kind}` so the agent knows which service's response
shape applies.

#### Validation at manifest load

Every alias is validated when the manifest loads:

- `command` resolves to a known sub-command. Else: alias marked invalid.
- The resolved command is in the read-only verb set. Else: invalid.
- Every `args` key matches a flag name or a registered positional name for that
command. Else: invalid.
- Each `args` value matches the flag's declared type. Else: invalid.

A bad alias surfaces in `tusk doctor` as a manifest warning; the engine does not
refuse to start over a bad alias, and other valid aliases keep working.

### 4.3 `tusk_context` — single warm-context call

A new MCP tool and matching CLI verb (`tusk context`) that returns a structured
digest of "what this agent should know at session start." Composed from a
manifest section. The `recent` slot and the `include` list both reference the
alias mechanism declared in §4.2:

```toml
[context]
pinned = ["docs/agent-charter", "docs/style"]
recent = "recent-activity"                    # alias name (string form)
include = ["open-tickets", "health"]

[alias.recent-activity]
command = "node list"
args.filter = "modified-since:7d type=note OR type=decision"
args.sort = "modified-desc"
args.top = 20
```

Or, when the recent query is one-off and doesn't deserve a top-level alias,
inline it:

```toml
[context]
pinned = ["docs/agent-charter", "docs/style"]
include = ["open-tickets", "health"]

[context.recent] # inline alias for recent
command = "node list"
args.filter = "modified-since:7d type=note OR type=decision"
args.sort = "modified-desc"
args.top = 20
```

`pinned` is the load-bearing piece. It is the explicit "always include this"
surface that replaces ad-hoc CLAUDE.md-style memories. The manifest is
committed; the pinned set is the workspace's curated memory. Under the hood,
`pinned` is sugar for a batched `node get <each> --include body,edges` — kept as
a typed list of IDs rather than an alias because the semantics (always include
full body) are universal enough to deserve a named slot.

`recent` accepts either form:

- **String form** (`recent = "<alias-name>"`) — references an alias declared
with `[alias.<alias-name>]`. Useful when the same query is also invocable via
`tusk run`.
- **Inline form** (`[context.recent]` block) — declares the alias body in place,
scoped to context. Useful for one-off queries that don't need a top-level name.

Setting both forms is a manifest error and surfaces in doctor. If neither is
set, the `recent` slot is omitted from the response.

`include` is a list of alias names (string form only — inline definitions inside
a list aren't a TOML shape we want to invent). Each named alias is invoked and
its output appears in the response under that alias name. Because aliases can
wrap any read-only tusk verb (query, doctor, edge list, status), this is the
generic extension point — `include` is not restricted to query-shaped results.

Response shape (JSON):

```json
{
  "pinned":  [<node>, ...],
  "recent":  [<node>, ...],
  "aliases": {
    "open-tickets": { "kind": "node-list", "results": [<node>, ...] },
    "health":       { "kind": "doctor",    "warnings": [...] }
  }
}
```

Each `<node>` in `pinned` and `recent` honors a tool-level `include` argument
exactly like `tusk_query` does. The default `include` for `tusk_context` is
`[body, edges]` because the purpose of the tool is to *be* the context the agent
reads — not to send the agent on follow-up `node_get` calls. Aliases under
`aliases.*` carry whatever shape their underlying verb returned; `kind`
discriminates.

### 4.4 Compact wire format

A `format = "compact"` argument on `tusk_query`, `tusk_node_list`,
`tusk_edge_list`, `tusk_run`, and `tusk_context`. Output is column-aligned text
rows, one record per line:

```
notes/auth-rfc        note     Auth RFC
tickets/fix-login     ticket   Fix login bug         status=active priority=3
tickets/refactor-db   ticket   Refactor storage      status=pending priority=2
```

When `include=body` is set, the body follows on subsequent indented lines until
the next record. When `include=edges` is set, edges follow as `  → <type>
<target_id> <target_title>` indented lines.

Default remains JSON; `format=compact` is per-call opt-in. The CLI defaults to
compact when stdout is a TTY, JSON when `--json` is set (status quo).

### 4.5 Grammar additions

The filter grammar gains a small set of predicates to support `tusk_context` and
aliases:

- `modified-since:<duration>` — e.g., `modified-since:7d`, `modified-since:48h`.
Compiles to `last_mtime >= ?` against the index.
- `modified-since:<date>` — ISO 8601 date or datetime.

These are additive; existing filter expressions are unchanged.

---

## 5. Phase 2 — Sub-Document AST

Markdown is parsed into a deterministic AST. Each AST node above the inline
level becomes a node in the graph, alongside the file node it descends from.

### 5.1 Unit types

The sub-document type pack ships with the engine and is global on/off via
`[workspace] sub-units = true|false` (default: `true`). The pack declares:

| Type         | AST origin                                | Notes                                                       |
| ------------ | ----------------------------------------- | ----------------------------------------------------------- |
| `section`    | heading + descendants until next ≤heading | nested by heading level; H1 is the file body itself         |
| `paragraph`  | paragraph block                           | the typical leaf                                            |
| `list-item`  | list item                                 | property `checkbox: true \| false \| null`                  |
| `code-block` | fenced code block                         | property `lang`                                             |
| `blockquote` | blockquote                                | nested blockquotes flatten to one unit                      |
| `table-cell` | one cell of a table (header or body)      | properties `header`, `row`, `column`, `column-header`       |

Inline-level constructs (links, emphasis, code spans) are not units. They are
body content of their containing leaf.

The containing **table** is not itself a unit — only its cells are indexed.
Each `table-cell` carries `row` and `column` as integers (0-indexed; row 0 is
the header row when present), `header: bool` to distinguish header cells from
body cells, and `column-header` — the text of the cell at `(row=0, column=this)`
when the table has a header row, empty otherwise. The `column-header` property
lets queries hit "Name: John" semantics even when the literal cell text is just
"John" (see §5.6 for how it factors into the embedded payload).

Footnotes and horizontal rules remain non-units in v1. They render as their
containing paragraph or section's body content.

### 5.2 Identity

A sub-unit's canonical id is `<parent-file-id>#<content-hash>`, where the
content hash is a stable hash (SHA-256, truncated to 12 hex chars in display)
over the unit's serialized AST form (text + structural metadata, excluding
ordinal).

Properties of this scheme:

- **Stable across reordering.** Moving a paragraph keeps its hash; the index
updates the unit's `ordinal` field and nothing else.
- **Changes on every edit.** A one-word edit produces a new hash. The old row is
deleted; a new row is inserted. Outgoing edges are re-derived. This is the
"natural rebalancing" property: the index never accumulates stale sub-unit
state.
- **Self-contained.** The hash is derivable from the file alone; no inline
anchor IDs are written to the markdown.

Hash collisions within a single file are vanishingly unlikely but possible (two
identical paragraphs). On collision the engine appends a disambiguating ordinal
suffix (`#a1b2c3-1`, `#a1b2c3-2`).

### 5.3 Schema

Sub-units live in the same `nodes` table as files. A discriminator distinguishes
them:

```sql
nodes
  id              TEXT PRIMARY KEY    -- "notes/auth-rfc" or "notes/auth-rfc#a1b2c3"
  type            TEXT                -- "note" | "section" | "paragraph" | ...
  path            TEXT                -- file path for files; parent file path for sub-units
  title           TEXT                -- file title for files; first-line excerpt for sub-units
  properties_json TEXT
  parent_id       TEXT NULL           -- file id for sub-units; NULL for files
  ordinal         INTEGER NULL        -- position within parent; NULL for files
  last_mtime      INTEGER             -- parent file's mtime for sub-units
  last_size       INTEGER
  last_checksum   TEXT
```

`parent_id` is denormalized for cheap subtree queries. It is also represented
in the edge table as a single edge type, `contains` (from file to sub-unit,
cardinality one-to-many, `inverse = "contained-by"`). The grammar's inverse
derivation lets queries walk either direction — `contains->` from a file to its
sub-units, `contained-by->` from a sub-unit to its file, or `<-contains` /
`<-contained-by` for the reverse. No new edge type name is introduced for the
reverse direction, which avoids colliding with the kanban pack's existing
`parent` edge.

The `embeddings` table is keyed by `(node_id, chunk_idx)` today. With sub-units,
every embedded row is a sub-unit and `chunk_idx` becomes always-zero (the AST is
the chunking). A migration drops `chunk_idx` from the primary key and adds an
index on `node_id`.

### 5.4 Edges

**Outbound from sub-units only.** Sub-units appear as edge sources, never as
edge targets.

Outbound edges from a sub-unit are derived from its body content via the
existing wikilink and ref-resolution machinery (§6.4 and §6.3 of the v1 spec).
When a paragraph contains `[[notes/auth-rfc]]`, an edge from that paragraph (the
sub-unit) to `notes/auth-rfc` (the file) materializes for every edge type the
manifest declares with `wikilinks = true`.

**Wikilink targets are always file nodes.** A wikilink resolves to the file
identified by its path, never to a sub-unit within that file. This holds whether
the wikilink appears in a file body, in a paragraph sub-unit, or in any other
indexed body content. The invariant follows directly from §5.4's "outbound-only
from sub-units, file nodes are the durable reference targets" rule — there is no
syntactic way to address a sub-unit from outside its own file in v1.

Frontmatter-declared edges remain on the file, not on sub-units. Sub-units have
no frontmatter of their own.

On a content edit, the changed sub-unit's row is dropped (cascade-deleting its
outgoing edges via foreign key), a new row is inserted, and outgoing edges are
re-derived from the new content. Nothing inbound breaks because there is nothing
inbound by construction.

If a sub-unit needs to be a *target* of an edge — e.g., a decision document
wants to link to a specific paragraph in a reference document — the user
promotes that paragraph to its own file. The constraint is intentional and not
revisited in this spec.

### 5.5 Parse and reindex

On each parse of a file (initial reindex, watcher event, or `tusk node modify`):

1. Parse the file's body into the AST.
2. Compute each unit's content hash.
3. Diff against the existing sub-unit rows for this file:
   - Hashes present in the file but missing from the index → INSERT.
   - Hashes present in the index but missing from the file → DELETE (cascades
   edges and embeddings).
   - Hashes present in both → UPDATE only if `ordinal` changed; otherwise no-op.
4. Re-derive outgoing edges for inserted sub-units.
5. Enqueue inserted sub-units in `embed_queue`.

The file's own node row is updated as today (mtime, size, checksum, properties).

### 5.6 Embedding

The existing `MarkdownRecursive` chunker in `internal/embed/chunking.go` is
replaced for the sub-units case by AST-driven chunking. Each leaf unit
(paragraph, list-item, code-block, blockquote, table-cell) is embedded as a
single vector. Section units are not embedded — they are query-time aggregates
over their descendants' embeddings.

**Table-cell payload.** Because a cell's literal text in isolation often lacks
semantic context ("John"), the embedded payload for a `table-cell` is
synthesized as `"<column-header>: <cell-text>"` when the cell has a non-empty
`column-header` property, and the bare cell text otherwise. The synthesized form
is what the embedder sees; the cell's stored body in the index remains the
literal text so queries and display behave as expected. Header cells (where
`header = true`) embed their own text without synthesis.

File nodes themselves remain embedded as today when `[workspace] sub-units =
false`, or as a back-compat path. When sub-units are enabled, file-level
embeddings are dropped — the file's "meaning" is the union of its sub-units'
meanings, recoverable by aggregation.

The `WholeDocument` strategy stays available for very short documents (a file
whose entire body fits in one paragraph produces exactly one paragraph sub-unit)
and for users who explicitly disable sub-unit indexing.

### 5.7 Surfacing in `tusk_query`

When `tusk_query` performs a semantic search with sub-units enabled, the
underlying ranking is over sub-unit embeddings. The result presentation,
however, groups by parent file.

**Section weighting by heading level.** Sections are not embedded themselves
(§5.6); their score is an aggregate of their descendant leaves' cosine scores,
modulated by the section's heading level. Broader headings carry more semantic
weight than narrower ones — an H2 is a primary theme of the document; an H5 is a
side note. Fixed weights:

| Heading level | Weight |
| ------------- | ------ |
| H1            | 1.00   |
| H2            | 0.85   |
| H3            | 0.70   |
| H4            | 0.55   |
| H5            | 0.40   |
| H6            | 0.25   |

Aggregation: `section.score = heading-weight × max(descendant-leaf.score)`. Max
(rather than mean) keeps a single strong leaf hit from being washed out by its
weaker siblings. The chosen scale is workspace-fixed; per-workspace tuning is
§8.5 future work.

Sections carry a `heading_level` field in the result so agents can prefer
broader or narrower scopes when re-reading.

JSON shape:

```json
{
  "results": [
    {
      "id": "notes/auth-rfc",
      "type": "note",
      "title": "Auth RFC",
      "matched_units": [
        {
          "id": "notes/auth-rfc#a1b2c3",
          "type": "section",
          "heading_level": 2,
          "ordinal": 4,
          "score": 0.86,
          "snippet": "Decision: OAuth 2.1 with PKCE chosen for SSO migration..."
        },
        {
          "id": "notes/auth-rfc#b4d5e6",
          "type": "section",
          "heading_level": 3,
          "ordinal": 7,
          "score": 0.74,
          "snippet": "PKCE implementation: code-verifier generation..."
        },
        {
          "id": "notes/auth-rfc#d4e5f6",
          "type": "paragraph",
          "ordinal": 12,
          "score": 0.78,
          "snippet": "Users with SSO accounts hit the password reset flow when..."
        }
      ]
    }
  ]
}
```

Compact form (heading level shown alongside type for sections):

```
notes/auth-rfc                note         Auth RFC                                                       0.86
  → #a1b2c3                   section H2   "Decision: OAuth 2.1 with PKCE chosen for SSO migration..."    0.86
  → #b4d5e6                   section H3   "PKCE implementation: code-verifier generation..."             0.74
  → #d4e5f6                   paragraph    "Users with SSO accounts hit the password reset flow when..."  0.78
```

The hierarchical indentation is the "compact graph notation" — agents read it
the same way they read a markdown bullet list. No special parsing instructions
in the system prompt; the structure is self-evident.

Structural-only queries (no `semantic` argument) return file-level results as
today. P2 adds one new `include` value, `units`, to the P1.A list: setting
`include = [units]` on a structural query attaches the file's sub-units as
`matched_units` (no scoring — the array is the file's full sub-unit list).
Sub-units also appear directly when the filter expression names a sub-unit type
(`type=section`, `type=list-item checkbox=false`); those return as top-level
result rows, not as `matched_units` of a parent.

### 5.8 Querying sub-units directly

The filter grammar treats sub-units as first-class nodes. All of these work:

```text
type=section heading-level<=2                       # only top-level sections (H1/H2)
type=list-item checkbox=false                       # all open todos across the vault
type=section contained-by->type=decision            # all sections that belong to a decision file
type=paragraph references->title="OAuth"            # all paragraphs that link to a node titled OAuth
type=table-cell column-header="Status" header=false # all data cells under a "Status" column
```

**Reserved names.** When `[workspace] sub-units = true`, the following names are
reserved by the sub-document pack and cannot be declared by user manifests:

- Node type names: `section`, `paragraph`, `list-item`, `code-block`,
  `blockquote`, `table-cell`.
- Edge type names: `contains` (with derived inverse `contained-by`).
- Property names on `section`: `heading-level` (1–6).
- Property names on `list-item`: `checkbox` (true / false / null).
- Property names on `code-block`: `lang`.
- Property names on `table-cell`: `header` (bool), `row` (int), `column` (int),
  `column-header` (string).

A user manifest that declares any of these surfaces in doctor as a reserved-name
conflict; the engine prefers the built-in declaration and ignores the user's
override (no silent shadowing).

### 5.9 Doctor implications

`tusk doctor` gains a sub-unit health pane:

- Sub-unit count per file (heuristic for parse correctness).
- Hash-collision count (should be ~0 in normal vaults).
- Orphaned sub-units (no parent file — should be impossible; surfaces parser
bugs).
- Embed-queue depth split by file vs sub-unit.

---

## 6. Phase 3 — Graph-Aware Semantic Ranking

### 6.1 Algorithm

`tusk_query --semantic` today does pure cosine over embeddings. With graph
expansion enabled:

1. **Embed the query.** Same as today.
2. **Initial candidates.** Top-K (default 50) by cosine similarity over the
`embeddings` table. K is a multiple of the requested `top` — sufficient
candidates for graph expansion to have something to work with.
3. **Edge expansion.** For each candidate, walk N hops (default 1, max 2) along
the configured edge types. Collected neighbors are added to the candidate pool
with `cosine_score = 0` if they were not in the initial set.
4. **Re-rank.** Each candidate's final score is `final = (1 - w) * cosine_score
+ w * graph_score`, where `graph_score` is the sum of cosine scores of the
candidate's neighbors that were in the initial top-K, normalized by the number
of such neighbors. Configurable `w` (default 0.2).
5. **Truncate.** Return the top-`take` results.

This is the GraphRAG pattern, implemented in pure Go over the existing index. No
external model, no generation step.

### 6.2 Configuration

```toml
[query.graph-expansion]
enabled    = false                                          # default off
hops       = 1                                              # 1 or 2
edge-types = ["references", "parent", "tagged", "contains"] # conceptual-relatedness edges
weight     = 0.2                                            # cosine/graph blend
candidate-multiplier = 5                                    # K = top * multiplier
```

The default `edge-types` list contains edges declared by the typical pack set
(`vault` ships `references`; `kanban` ships `parent`; `tags` ships `tagged`; the
sub-document pack ships `contains` — see §5.3, §5.8). Workspaces with a
different pack set should tune the list; missing edge type names are reported in
doctor but do not fail the query.

A per-call override is available on `tusk_query`:

```bash
tusk_query --semantic "auth" --graph-expand --hops 2 --graph-weight 0.3
```

### 6.3 Default behavior

**Off.** v1 has a stable retrieval contract; flipping this on by default changes
that contract for every existing workspace. Workspaces opt in by setting
`enabled = true` in the manifest.

### 6.4 Interaction with sub-units (P2)

P3 is fully independent of P2 — it operates over whatever node types are
embedded and whatever edges the workspace's active packs declare. Neither P2 nor
any specific pack set is a precondition for enabling graph expansion.

- Without P2: graph expansion walks edges between file nodes only. Useful but
  coarse.
- With P2: graph expansion walks the much richer sub-unit graph. A paragraph
  hit's neighbors include the parent file (via `contained-by`), the file's tags
  (via `tagged` traversed from the parent), and other paragraphs that link to
  the same files (via `references`). Re-ranking surfaces "conceptually nearby"
  sub-units that pure cosine missed.

The configured `edge-types` list determines which edges propagate semantic
signal. Hierarchy / containment edges (`parent`, `contains`) are usually wanted;
structural edges (`blocks`, `supersedes`) are usually not. The default in §6.2
assumes a typical workspace with `vault` + `kanban` + `tags` packs plus
sub-units; workspaces with a different pack set should adjust the list to
reference edge types their packs actually declare.

### 6.5 Performance

For a candidate set of K=50 and hops=1, expansion touches at most `K *
avg_degree` rows. On a vault with average degree 5, that's 250 edge lookups — a
single SQL query with `target_id IN (...)` returns them all. The cost is bounded
and additive to the existing cosine search.

Hops=2 fan-out grows as `K * avg_degree²`. Capped at hops=2 in v1; higher would
need a different algorithm (random walks, PageRank).

---

## 7. Surfaces — Summary of Changes

### CLI additions / changes

```bash
tusk context                                              # P1.D — single warm-context call
tusk run <alias>                                          # P1.B — invoke a named alias
tusk run --list                                           # P1.B — show defined aliases
tusk query --include body,edges,properties [...]          # P1.A — opt-in field expansion
tusk query --fields id,title,body [...]                   # P1.A — projection
tusk query --format compact|json                          # P1.E — wire format
tusk query --graph-expand [--hops N --graph-weight W]     # P3   — per-call opt-in
tusk node get <id-or-anchor> [--include ...]              # P1.A — same flags as query
tusk node list <filter> [--include ...]                   # P1.A
```

### MCP additions

- `tusk_context` (P1.D)
- `tusk_run` (P1.B)

All existing MCP tools (`tusk_query`, `tusk_node_get`, `tusk_node_list`,
`tusk_edge_list`) gain `include`, `fields`, and `format` arguments. No breaking
changes.

### Manifest additions

```toml
[workspace]
sub-units = true                                          # P2 toggle

[alias.<name>]                                            # P1.B
command     = "node list"                                 # CLI sub-command path
description = "..."
args.<flag-or-positional> = ...                           # typed TOML values

[context]                                                 # P1.D
pinned  = [...]                                           # list of node IDs
recent  = "<alias-name>"                                  # OR define [context.recent] block (same shape as [alias.<name>])
include = ["alias-name", ...]                             # list of alias references

[query.graph-expansion]                                   # P3
enabled              = false
hops                 = 1
edge-types           = [...]
weight               = 0.2
candidate-multiplier = 5
```

### Schema migrations

- P1: none.
- P2: add `parent_id`, `ordinal` columns to `nodes`. Add `sub-document` built-in
type pack registration. Migrate `embeddings` primary key from `(node_id,
chunk_idx)` to `(node_id)` (chunk_idx column retained for back-compat read,
ignored on write).
- P3: none.

---

## 8. Open Questions and Future Work

### 8.1 Named context profiles

`[context]` is currently singular. A future expansion is `[context.<name>]`, so
a workspace can declare `[context.boot]`, `[context.debugging]`,
`[context.planning]` and the agent calls `tusk_context --profile boot`. The
schema is forward-compatible — the v1 `[context]` block is sugar for
`[context.default]`.

### 8.2 Alias parameters

Aliases are parameter-free in v1. The structured `args` shape (§4.2) makes
parameterization straightforward when needed: declare which `args` keys are
parameters, and let invocations override them.

```toml
[alias.tickets-by-priority]
command = "node list"
args.filter = "type=ticket priority=${priority}"
args.top = 10
params = ["priority"]
```

`tusk run tickets-by-priority --param priority=3` / `tusk_run(alias, params:
{priority: 3})`. Out of scope until a concrete use case lands.

### 8.3 Bundled context for system-prompt rendering

A `--render-prompt` flag on `tusk context` that emits a single markdown blob
suitable for injection into an agent's system prompt (`tusk context
--render-prompt > .agent-prompt.md`). For harnesses that can't call MCP tools,
this is the integration point. Out of scope as a v1.x; trivial to add once
`tusk_context` is in.

### 8.4 Section-level summaries

If tusk ever gains a generation provider (for a different reason — e.g., a
future commit-message helper), section summaries become viable: each `section`
node carries a generated summary alongside its leaf children's full text. The
summary is the embedded payload at section level; leaves remain their own
embeddings. Until then, sections are aggregation-only.

### 8.5 Graph expansion heuristics

§6.1 specifies a simple weighted average. Alternatives — Personalized PageRank,
RWR (random walk with restart), or learned weights per edge type — are not in
scope for v1 but the configuration shape (`[query.graph-expansion]`) is
forward-compatible.

---

## 9. Phasing

Three phases, each shipping independently:

| Phase | Scope                                       | Schema change                                  | Default behavior change |
| ----- | ------------------------------------------- | ---------------------------------------------- | ----------------------- |
| P1    | Rich query + aliases + context + compact    | none                                           | none (all opt-in)       |
| P2    | Sub-document AST                            | `parent_id`, `ordinal` on `nodes`; embeddings PK | sub-units indexed by default; existing semantic queries return parent files with `matched_units` attached |
| P3    | Graph-aware ranking                         | none                                           | none (opt-in)           |

P3 can ship before P2 if priorities shift; the algorithm operates on whatever
graph exists.

---

## 10. References

- `2026-05-05-tusk-v1-rebuild-design.md` — base v1 design.
  - §6.4 wikilinks — sub-unit edges piggyback on this mechanism (§5.4).
  - §9.5 index schema — sub-units extend the `nodes` table (§5.3).
  - §10 retrieval — P3 ranking blends with §10.2 semantic and §10.3 hybrid.
  - §10.4 what gets embedded — replaced by §5.6 for sub-unit workspaces.
  - §10.7 chunking strategy — `WholeDocument` retained; `MarkdownRecursive`
  superseded by AST chunking when sub-units are enabled.
- `internal/embed/chunking.go` — current chunker; replaced for sub-unit
workspaces.
- `internal/mcp/tools.go` — current tool handlers; gain `include`, `fields`,
`format` arguments; gain `tusk_context` and `tusk_run` registrations.
- GraphRAG (Microsoft Research, 2024) — the graph-augmented retrieval pattern P3
implements.
