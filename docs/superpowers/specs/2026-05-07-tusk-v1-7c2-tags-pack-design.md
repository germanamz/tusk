---
type: spec
title: Plan 7.c.2 — Tags Pack Spec
---

# Tusk v1 — Tags Pack Design (Plan 7.c.2 sub-spec)

- **Status:** Draft
- **Date:** 2026-05-07
- **Author:** German Meza
- **Scope:** Ship the built-in `tags` pack content as data-only — a TOML file at `packs/tags.toml` in the tusk repo containing the universal `tag` node type and the `tagged` edge type. No engine code; no frontmatter shorthand; no auto-creation; no path templating; no manifest flag. Establishes the `packs/` directory convention used by 7.c.3 (kanban) and 7.c.4 (vault).
- **Successor of:** the brainstorm dialogue captured during Plan 7.c.2 setup.

This document is a sub-spec of `2026-05-05-tusk-v1-rebuild-design.md` (the v1 rebuild design) and a follow-up to `2026-05-07-tusk-v1-7c1-pack-platform-and-ref-design.md` (Plan 7.c.1, which built the pack platform). It addresses ledger item #1 from 7.c.1 §10 (tags pack content) and **explicitly drops** v1 design spec §6's `tags:` frontmatter shorthand, auto-creation behavior, path templating, and manifest gate flag. The v1 spec's §6 paragraph about `tags:` shorthand is historical; the actual v1.c implementation has tags as simple nodes.

---

## 1. Goal & Scope

Plan 7.c.2 ships the `tags` pack as **data only**. The user runs `tusk pack add tags` (or `tusk pack add file://./packs/tags.toml` for testing), the pack platform from 7.c.1 fetches the file, validates it, and merges its two sections into the user's `tusk.toml`. From that point on, tags are just nodes — created, modified, queried, and edge-connected through the same mechanisms every other node uses.

### 1.1 In scope

- A new file `packs/tags.toml` at the tusk repo root containing exactly two sections: `[node-types.tag]` and `[edge-types.tagged]`.
- Repository convention: pack files live at `packs/<name>.toml`. The 7.c.1 alias map already points `kanban`, `vault`, and `tags` at canonical URLs `https://raw.githubusercontent.com/germanamz/tusk/main/packs/<name>.toml`; this plan creates the directory and ships the first file.
- One end-to-end smoke test that runs `tusk pack add file://./packs/tags.toml` against the actual pack file and verifies the resulting workspace can create tag nodes and `tagged` edges.

### 1.2 Out of scope (deferred or dropped from v1.c)

The following features from v1 design spec §6 are **dropped from v1.c entirely**. Users who want this UX must implement it themselves via the ref-pattern opt-in described in §3 below; revisit only if usage shows demand.

- The `tags: [auth, security]` frontmatter shorthand.
- Auto-creation of missing tag nodes when a tags reference doesn't resolve.
- Path templating (`tags/<name>` default, configurable per pack).
- The manifest flag that gates auto-creation versus strict resolution.
- The "empty tag bodies" surface in `tusk doctor`.

The dropped features traded UX convenience for engine complexity (a special-case `tags:` resolver, recursive tag-node auto-creation, transactional cleanup on failure, a new doctor issue kind, a new manifest config section). Plan 7.c.1's ref property type already gives users the bare-string shorthand at the cost of declaring `{ name = "tags", type = "list-of", item-type = "ref", to = "tag" }` per node type that wants tags. That's a small constant cost paid by the user (a one-line declaration) in exchange for no special-case engine logic.

### 1.3 Backward compatibility

Workspaces with no `tag` node type and no `tagged` edge type are unaffected. Workspaces that previously used custom-declared `tag`/`tagged` schema may collide with the pack on `tusk pack add tags`; the 7.c.1 collision-detection path surfaces this and rejects without `--force`.

---

## 2. Pack File

`packs/tags.toml`:

```toml
# Tusk built-in pack: tags
#
# Adds a universal `tag` node type and a `tagged` edge type.
# Tags are simple nodes; nodes can carry multiple tags via the
# many-to-many `tagged` edge.
#
# Usage patterns:
#   - Wikilinks in body: `[[tag/auth]]` materializes a `tagged` edge.
#   - Wikilinks in frontmatter: `tagged: ["[[tag/auth]]"]`.
#   - Ref property opt-in (per node type):
#       properties = [{ name = "tags", type = "list-of", item-type = "ref", to = "tag" }]
#     Then write: `tags: [auth, security]` in frontmatter.
#   - Direct edge: `tusk edge add tagged <node-id> tag/<tag-name>`.

[node-types.tag]
description = "A label that can be applied to any node"
properties = []

[edge-types.tagged]
description = "Marks a node as tagged with a tag"
from = ["*"]
to = ["tag"]
cardinality = "many-to-many"
ordered = false
```

That is the entire pack — about thirty lines including the explanatory comment block.

**Notes on the schema choices:**

- `properties = []` — only the implicit `title` field. The markdown body of a tag node is the natural place to write descriptive context about what the tag means; declaring no properties keeps the tag node maximally flexible.
- `from = ["*"]` — any node type may carry a `tagged` edge. This is the universal-tag affordance.
- `to = ["tag"]` — `tagged` edges always target tag nodes. The 7.c.1 manifest validator's edge-type rules ensure this.
- `cardinality = "many-to-many"` — a node can carry many tags; a tag can apply to many nodes.
- `ordered = false` — tag order on a node has no meaning. (Compare with `list-of(ref) ordered = true` for cases where order matters.)

---

## 3. Repository Layout

A new directory at the tusk repo root:

```
packs/
  tags.toml          # this plan (7.c.2)
  kanban.toml        # added in 7.c.3
  vault.toml         # added in 7.c.4
```

The 7.c.1 alias map (`internal/typepacks/aliases.go`):

```go
var BuiltinAliases = map[string]string{
    "kanban": "https://raw.githubusercontent.com/germanamz/tusk/main/packs/kanban.toml",
    "vault":  "https://raw.githubusercontent.com/germanamz/tusk/main/packs/vault.toml",
    "tags":   "https://raw.githubusercontent.com/germanamz/tusk/main/packs/tags.toml",
}
```

The `tags` URL becomes live as soon as `packs/tags.toml` lands on `main` (after the `v1 → main` cascade). During 7.c.2's PR review, the URL returns 404; the smoke test uses `file://` against the repo-local path.

No `README.md` or other documentation file lives inside `packs/` in v1.c. The pack file's leading comment block is the per-pack documentation; the alias map and this design doc are the global reference. A future plan may add a `packs/README.md` if the directory grows enough to merit it.

---

## 4. User UX Patterns

After `tusk pack add tags`, four supported ways to apply tags exist:

### 4.1 Markdown body wikilink

```markdown
---
type: ticket
title: Migrate the auth service
---

This depends on [[tag/auth]] work that's been parked since Q4.
```

The Plan 2 wikilink resolver picks up `[[tag/auth]]` and emits a `tagged` edge from the ticket node to `tag/auth`. No additional config; works as long as the `tagged` edge type is declared.

### 4.2 Frontmatter wikilink array

```yaml
---
type: ticket
title: Migrate the auth service
tagged: ["[[tag/auth]]", "[[tag/security]]"]
---
```

Plan 2's frontmatter-edge mechanism: the `tagged` key holds an array whose entries are wikilinks; each becomes a `tagged` edge.

### 4.3 Per-node-type ref opt-in

```toml
[node-types.note]
properties = [
    { name = "summary", type = "string", required = true },
    { name = "tags", type = "list-of", item-type = "ref", to = "tag" },
]
```

```yaml
---
type: note
title: Q1 retro
summary: Themes from the team retro
tags: [auth, retro, q1]
---
```

The 7.c.1 ref resolver looks each string up by title within the `tag` node-type scope (or by node ID if the value is a wikilink) and emits one edge per resolved tag.

**Edge-type-name caveat.** The 7.c.1 auto-edge synthesizer (§3.3) names the synthesized edge type after the *property name*. A property named `tags` produces edge type `tags`; a property named `tagged` would attempt to synthesize edge type `tagged` and collide with the pack's explicit `[edge-types.tagged]` (rejected at manifest load per 7.c.1 §3.4). Users must therefore pick a property name other than `tagged`. The conventional name is `tags`, which produces edges under edge type `tags` rather than `tagged`.

This means **pattern 4.3 emits edges under edge type `tags`**, distinct from patterns 4.1 / 4.2 which emit under `tagged`. Mixed workspaces — some nodes using body wikilinks, some using ref properties — end up with two parallel edge types both pointing at tag nodes. Queries asking "what tags does this node have?" must cover both. This is captured as a residual in §6 and may motivate a follow-up "ref property aliases an existing edge type" feature; for v1.c the cost is accepted.

### 4.4 Direct edge command

```bash
tusk edge add tagged tickets/auth tag/auth
```

Always works regardless of frontmatter shape; useful for scripting and bulk operations.

---

## 5. Testing Strategy

One end-to-end smoke test, added to `cmd/tusk/` as a new test file `cmd_pack_tags_smoke_test.go` (or as an additional case in the existing `cmd_pack_add_test.go`):

```
tusk init --name test
tusk pack add file://<repo>/packs/tags.toml
# verify tusk.toml has [node-types.tag] and [edge-types.tagged]

tusk node create --type tag --title auth
tusk node create --type tag --title security
# verify tag/auth.md and tag/security.md exist

tusk edge add tagged tag/auth tag/security
# verify the edge exists via direct repo lookup
```

The test uses the actual `packs/tags.toml` file from the repo (resolved via `file://` against the test's CWD or absolute path). This serves dual duty: validates the pack-platform end-to-end against a real fixture, and validates that the pack content itself decodes and merges cleanly under the manifest validator.

No new unit tests beyond this. The pack file has no logic to unit-test; it's data. The existing `internal/typepacks` test suite already covers the fetch/validate/merge/atomic-write paths against synthetic fixtures.

---

## 6. Open Questions / Residuals

1. **Spec drift with v1 rebuild design §6.** The v1 design spec describes `tags:` shorthand, auto-creation, and path templating that this plan explicitly drops. The divergence is documented in §1.2 above and in §10's ledger. The v1 design spec itself is not edited by this plan; readers cross-reference. If the divergence proves confusing, a future `docs(spec): supersede v1 §6 tags paragraph` commit can land separately.
2. **Pattern 4.3 edge-type naming.** The ref-property opt-in (§4.3) produces edges under whatever edge type the property's name synthesizes — typically `tags`, not `tagged`. Mixed workspaces (some patterns using `tagged`, some using `tags`) end up with two parallel edge types both pointing at tag nodes. Querying "all tag relationships" then needs to cover both. Acceptable v1.c semantics; revisit if real usage shows pain.
3. **`packs/README.md`.** Not shipped in 7.c.2; the per-pack TOML comment block plus this design doc plus the alias map serve as documentation. Add a directory README when 7.c.3 / 7.c.4 / future packs grow the directory enough to warrant it.
4. **No MCP surface for `tusk pack add`.** Carries forward unchanged from 7.c.1 §10 ledger #10. Tags pack inherits that policy: workspace-config commands stay CLI-only.

---

## 7. Plan 7.c+ Ledger Updates

Plan 7.c.1's §10 ledger items are updated:

| # | Item | Status |
|---|---|---|
| 1 | Tags pack content (`tag` node type, `tagged` edge type) | **Shipped in 7.c.2** |
| (new) | v1 design spec §6 tag features — `tags:` shorthand, auto-creation, path templating, manifest gate flag, empty-tag-body doctor surface | Dropped from v1.c. Users opt in per node type via ref pattern (§4.3). May revisit if usage shows demand. |

All other 7.c.1 ledger items carry forward unchanged.

The 7.c series remaining:

- **7.c.3** — kanban pack content (`ticket`, `project` node types; `parent`, `blocks`, `tagged` edges; `[behaviors.workflow.kanban]`). The kanban pack DEPENDS on the tags pack having been added, OR re-declares `tagged` itself. The dependency / re-declaration choice is a 7.c.3 design decision.
- **7.c.4** — vault pack content (`note`, `meeting`, `decision` node types; `references`, `relates-to` edges).
