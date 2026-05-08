# Tusk v1 — Vault Pack Design (Plan 7.c.4 sub-spec)

- **Status:** Draft
- **Date:** 2026-05-08
- **Author:** German Meza
- **Scope:** Ship the built-in `vault` pack content as data-only — a TOML file at `packs/vault.toml` in the tusk repo containing three categorical node types (`note`, `meeting`, `decision` — all with empty properties) and two edge types (`references` for wikilink materialization, `relates-to` for user-declared "see also"). No engine code. No platform extensions. No behaviors. Closes the v1.c built-in pack trilogy alongside 7.c.2 (tags) and 7.c.3 (kanban).
- **Successor of:** the brainstorm dialogue captured during Plan 7.c.4 setup.

This document is a sub-spec of `2026-05-05-tusk-v1-rebuild-design.md` (the v1 rebuild design) and a follow-up to `2026-05-07-tusk-v1-7c1-pack-platform-and-ref-design.md` (Plan 7.c.1, which built the pack platform), `2026-05-07-tusk-v1-7c2-tags-pack-design.md` (Plan 7.c.2, which established the `packs/<name>.toml` repository convention), and `2026-05-07-tusk-v1-7c3-kanban-pack-design.md` (Plan 7.c.3, which shipped the kanban pack alongside the "trim master-spec opinions" pattern). It addresses ledger item #3 from 7.c.1 §10 (vault pack content). It **explicitly diverges** from v1 design spec §7.1 / §7.2 in two ways enumerated below; the divergences follow the same "v1 spec is older thinking" pattern that 7.c.2 and 7.c.3 already established.

---

## 1. Goal & Scope

Plan 7.c.4 ships the `vault` pack as **data only** (no behaviors, no engine code). The user runs `tusk pack add vault` (or `tusk pack add file://./packs/vault.toml` for testing); the pack platform from 7.c.1 fetches the file, validates it, and merges its sections into the user's `tusk.toml`. From that point forward, notes/meetings/decisions are validated through the existing property validator (Plan 7.b — trivially, since none of the three node types declare any properties), wikilinks materialize as `references` edges (Plan 2 — gated on the pack's `[edge-types.references]` declaration), and `relates-to` edges work via frontmatter or `tusk edge add`.

### 1.1 In scope

- A new file `packs/vault.toml` at the tusk repo root containing five sections: `[node-types.note]`, `[node-types.meeting]`, `[node-types.decision]`, `[edge-types.references]`, `[edge-types.relates-to]`.
- One end-to-end smoke test that runs `tusk pack add file://./packs/vault.toml` against the actual pack file and verifies the full feature surface end-to-end: pack add, three node types creatable, body wikilink materializing as `references` edge after reindex, manual `relates-to` edge add/list roundtrip.

### 1.2 Out of scope (deferred or dropped from v1.c)

The following were either drawn in master spec §7.1 / §7.2 or could plausibly belong to a "vault" pack but are explicitly **not** in v1.c vault. Each is a deliberate cut, documented here so future readers don't reintroduce them by accident.

- **Structured properties on any node type.** Master spec §7.1 sketched `[node-types.decision]` with `title` (now reserved by Plan 7.b — would fail to load), `decided-at` (date), `status` enum, and `rationale` (markdown). v1.c vault drops all of them. Decision content (status, date, rationale) lives in the markdown body; users wanting structured fields declare them inline in their workspace manifest. Same "categorical pack" framing the user reaffirmed during the brainstorm.
- **Ergonomic shortcuts (`tusk note new`, `tusk meeting open`, etc.).** Master spec §7.2 mentioned per-pack ergonomic shortcuts. Already deferred from v1.c by the 7.c.1 ledger; vault inherits that policy.
- **`relates-to` inverse name.** A symmetric edge type — naming the inverse `relates-to` (self-inverse) is redundant; naming it `related-from` is awkward. Vault declares no inverse and relies on the `<-` operator per master spec §7.4.
- **A dedicated minimal "wiki-core" pack split.** Could split `[edge-types.references]` (the wikilink-edge activation switch) out of vault into its own pack so users wanting wiki-style backlinks without note/meeting/decision types don't have to take the whole vault. v1.c keeps it bundled in vault for simplicity; flagged as residual in §7 for revisit.

### 1.3 Backward compatibility

Workspaces with no `note` / `meeting` / `decision` node types and no `[edge-types.references]` / `[edge-types.relates-to]` are unaffected. Workspaces that previously declared any of these names may collide with the pack on `tusk pack add vault`; the 7.c.1 collision-detection path surfaces this and rejects without `--force`.

A subtle behavioral change: workspaces adding the vault pack gain wikilink-as-edge materialization workspace-wide, because `[edge-types.references]` activates the conditional in `internal/node/service.go:203`. The engine looks for the literal edge type name `references`; users for whom the wikilink-edge behavior is undesirable can remove the section after merge (renaming has no effect, since the engine only acts on the canonical name).

---

## 2. Pack File

`packs/vault.toml`:

```toml
# Tusk built-in pack: vault
#
# Adds three categorical node types — `note`, `meeting`, `decision` —
# and two edge types — `references` (auto-materialized from body
# wikilinks) and `relates-to` (user-declared "see also").
#
# Pack is purely categorical: no declared properties on any node type.
# The implicit `title` is always available; the markdown body is the
# natural place for content (meeting agendas, decision rationale,
# note prose). Users wanting structured fields on a node type can
# declare them inline in their workspace manifest.
#
# Note: declaring [edge-types.references] is what activates body
# wikilink → edge materialization in the engine. A workspace with
# this pack added gets `[[some/node]]` → references-edge behavior;
# a workspace without it gets wikilinks as plain text only.
#
# `relates-to` has no declared inverse — the relation is symmetric
# in meaning, so the <- operator handles backward traversal.

[node-types.note]
description = "A free-form markdown note"
properties = []

[node-types.meeting]
description = "A meeting record (agenda, attendees, discussion in body)"
properties = []

[node-types.decision]
description = "A captured decision (rationale, status, date in body)"
properties = []

[edge-types.references]
description = "Implicit edge materialized from body wikilinks"
from        = ["*"]
to          = ["*"]
cardinality = "many-to-many"
ordered     = false
acyclic     = false
inverse     = "referenced-by"

[edge-types.relates-to]
description = "User-declared 'see also' relationship between nodes"
from        = ["*"]
to          = ["*"]
cardinality = "many-to-many"
ordered     = false
acyclic     = false
```

About 50 lines including the comment block.

**Notes on the schema choices:**

- All three node types declare `properties = []`. Pure categorical — the implicit `title` is the only structured field, and the markdown body holds everything else. Mirrors the tag node type from 7.c.2 and the user's reaffirmed framing during the brainstorm: "this pack is merely categorical."
- `references` is the edge type the engine hardcodes in `internal/node/service.go:203` for wikilink materialization. Declaring it here activates the behavior; omitting it leaves wikilinks as plain text. The leading comment block explains this so future readers don't accidentally rename or remove the declaration.
- `references.from = ["*"]` and `references.to = ["*"]` — any node can wikilink to any other node. Tighter scoping (e.g., `from = ["note", "meeting", "decision"]`) would mean tickets/tags can't have wikilink-edges, breaking the "every node is a wiki page" model.
- `references.acyclic = false` — notes commonly cross-reference each other (A mentions B mentions A); forcing acyclic would reject natural patterns.
- `references.inverse = "referenced-by"` — the classic backlink query handle.
- `relates-to.acyclic = false` and `cardinality = "many-to-many"` — same rationale as `references`. Symmetric semantics.
- `relates-to` has no declared inverse — the relation is semantically symmetric ("relates to" reads naturally in either direction). Naming the inverse the same name would be redundant; naming it `related-from` would imply asymmetry that doesn't exist. The `<-` operator covers backward traversal per master spec §7.4.

---

## 3. Repository Layout

After 7.c.4 lands, the `packs/` directory holds the full v1.c trilogy:

```
packs/
  tags.toml          # 7.c.2 (shipped)
  kanban.toml        # 7.c.3 (shipped)
  vault.toml         # 7.c.4 (this plan)
```

The 7.c.1 alias map (`internal/typepacks/aliases.go`) is unchanged — it already points `vault` at `https://raw.githubusercontent.com/germanamz/tusk/main/packs/vault.toml`. The URL becomes live as soon as `packs/vault.toml` lands on `main` (after the v1→main cascade). During 7.c.4's PR review, the URL returns 404; the smoke test uses `file://` against the repo-local path.

No `packs/README.md` ships in 7.c.4. With three packs in the directory after this plan, the threshold for a directory README is approaching — flagged in §7 residuals as a candidate for a future plan, but not blocking 7.c.4.

---

## 4. Divergences from Master Spec §7.1 / §7.2

Two explicit divergences from the master v1 rebuild design. Each is a deliberate "v1 spec is older thinking" cut following the same pattern as 7.c.2's drop of the `tags:` frontmatter shorthand and 7.c.3's drop of `[node-types.project]`.

### 4.1 No structured properties on any vault node type

Master spec §7.1 included an inline example for `[node-types.decision]`:

```toml
[node-types.decision]
description = "A captured decision with rationale and date"
properties = [
  { name = "title",       type = "string", required = true },
  { name = "decided-at",  type = "date",   required = true },
  { name = "status",      type = "enum",   values = ["proposed", "accepted", "rejected", "superseded"] },
  { name = "rationale",   type = "markdown" },
]
```

v1.c vault drops every property:

- `title` is reserved by Plan 7.b's manifest loader (`internal/manifest/loader.go:34` — `reservedPropertyNames` map). Declaring it would fail manifest load.
- `decided-at`, `status`, `rationale` could all ship cleanly, but each represents a structured opinion about how decisions should look. The user reaffirmed during the brainstorm that vault is "merely categorical" — a vehicle for adding semantic node-type names, not for prescribing their data shape.

Users who want a `status` enum on decisions, a `decided-at` date, or a `rationale` markdown field can declare them inline in their workspace manifest after `tusk pack add vault`. Same opt-in pattern as kanban's `assignee` deferral in 7.c.3 §4.3.

### 4.2 No ergonomic shortcuts

Master spec §7.2 mentioned per-pack ergonomic shortcuts (`tusk note new`, `tusk meeting open`, etc.). Already deferred from v1.c by the 7.c.1 ledger entry on type-pack ergonomic shortcuts; vault inherits that policy without further consideration.

---

## 5. User UX Patterns

After `tusk pack add vault`, the supported authoring shapes:

### 5.1 Create a note via CLI

```bash
tusk node create --type note --title "Auth RFC" --path notes/auth-rfc.md
```

The note exists with an implicit `title` and an empty body. Property validation runs (no properties to check). Markdown body holds the actual content — added via direct file edit or piped stdin via `tusk node create`.

### 5.2 Wikilinks materialize as `references` edges

```markdown
---
type: note
title: Q1 retro
---

Continuing the discussion from [[notes/standup-2026-04-30]]
about the [[tickets/auth-jwt]] effort.
```

Plan 2's wikilink resolver picks up both wikilinks and materializes them as `references` edges. **This only works because the vault pack declared `[edge-types.references]`.** Without the vault pack, the wikilinks render as text but no edges are created.

Backlink query: `tusk edge list --to notes/auth-rfc` shows everything that references this note (via the `referenced-by` inverse).

### 5.3 `relates-to` via frontmatter

```yaml
---
type: decision
title: Use JWT for session tokens
relates-to:
  - "[[notes/auth-rfc]]"
  - "[[tickets/auth-jwt]]"
---

Decided 2026-04-15. Considered cookie-based sessions but rejected
due to mobile compatibility issues.
```

Plan 2's frontmatter-edge mechanism: the `relates-to` key holds wikilinks; each becomes a `relates-to` edge. The `decision` body holds the date / rationale / context as prose since those aren't structured properties.

### 5.4 `relates-to` via direct edge

```bash
tusk edge add --type relates-to --source decisions/jwt --target notes/auth-rfc
```

Useful for scripting or for adding relations after a node already exists.

### 5.5 Composing with the other packs

```bash
tusk pack add tags    # adds [node-types.tag] + [edge-types.tagged]
tusk pack add kanban  # adds ticket + parent + blocks + workflow
tusk pack add vault   # adds note/meeting/decision + references + relates-to
```

After all three are added, the workspace has the full v1.c built-in vocabulary: tickets with workflow, notes/meetings/decisions for context, tags for cross-cutting categorization, wikilinks materialize as `references` edges across everything. Any pack can be added or omitted independently — they have no cross-pack collisions or dependencies in v1.c.

---

## 6. Testing Strategy

One end-to-end smoke test, added to `cmd/tusk/cmd_pack_add_test.go` as a new function `TestPackAdd_VaultPackEndToEnd`. The test mirrors `TestPackAdd_TagsPackEndToEnd` and `TestPackAdd_KanbanPackEndToEnd` (added in 7.c.2 / 7.c.3) and uses the existing `testSourceDir(test)` helper to resolve the pack file path.

Test sequence:

```
1. tusk init --name vault-smoke

2. tusk pack add file://<repo>/packs/vault.toml
   - verify tusk.toml gained:
       [node-types.note], [node-types.meeting], [node-types.decision],
       [edge-types.references], [edge-types.relates-to]

3. tusk node create --type note --title "Auth RFC" --path notes/auth-rfc.md
4. tusk node create --type meeting --title "Standup" --path meetings/standup.md
5. tusk node create --type decision --title "Use JWT" --path decisions/jwt.md
   - verify all three files exist with the expected `type:` and `title:` frontmatter

6. Externally write a second note containing a body wikilink:

   os.WriteFile("notes/refback.md", []byte(`---
   type: note
   title: Backreference
   ---

   This references [[notes/auth-rfc]] in the body.
   `), 0o644)

7. tusk reindex
   - picks up the wikilink and materializes a `references` edge

8. tusk edge list --from notes/refback
   - assert output contains "references" and "notes/auth-rfc"

9. tusk edge add --type relates-to --source decisions/jwt --target notes/auth-rfc

10. tusk edge list --from decisions/jwt
    - assert output contains "relates-to" and "notes/auth-rfc"
```

Steps 6–8 are the pack's most distinctive feature. Wikilink materialization is gated on the `[edge-types.references]` declaration, and using the existing `e2e_edges_test.go` pattern (`os.WriteFile` + `tusk reindex`) keeps the test simple — no need to wire stdin into a cobra command for a body. The `references` edge appearing in step 8's output proves end-to-end that the pack's most architecturally-important section is wired correctly.

Steps 9–10 verify the `relates-to` edge with the same direct-edge pattern as the tags pack's `tagged` test.

This is dual-duty validation: confirms pack platform end-to-end against a real fixture, confirms vault content decodes and merges cleanly under the manifest validator, exercises wikilink-as-references-edge materialization (the pack's unique architectural impact), and exercises the manual edge add/list roundtrip.

No new unit tests beyond this. Like the tags and kanban packs, the vault pack file has no logic to unit-test; it's pure data. The underlying mechanisms (wikilink resolution, edge materialization, frontmatter-edge resolution, reindex) are already covered by `internal/node/wikilinks_test.go`, `internal/node/edges_test.go`, `internal/node/refs_test.go`, and `cmd/tusk/e2e_edges_test.go`.

**CLI flag verification.** Per the 7.c.2 lesson (handoff entry #3, also reaffirmed in 7.c.3), the implementer should grep `cmd/tusk/cmd_node_create.go`, `cmd/tusk/cmd_edge_add.go`, `cmd/tusk/cmd_edge_list.go`, and `cmd/tusk/cmd_reindex.go` for the actual flag definitions before locking the smoke test code. The flag shapes shown in this design (`--type`/`--title`/`--path` for node create; `--type`/`--source`/`--target` for edge add; `--from`/`--to` for edge list) match the actual CLI as of v1 tip `8121dba`, but the implementer should re-verify rather than assume.

---

## 7. Open Questions / Residuals

1. **Wikilink-edge UX coupled to the vault pack.** `[edge-types.references]` is the activation switch for `[[wikilink]]` → edge materialization (`internal/node/service.go:203`). A workspace that wants wiki-style backlinks but doesn't want the note/meeting/decision node types has to either run `tusk pack add vault` and ignore the unused types, or inline-declare `[edge-types.references]` in their `tusk.toml`. A future plan could split `references` into a dedicated minimal "wiki-core" pack (or auto-include it whenever any pack uses wikilink-edges); for v1.c the simpler bundling in vault is acceptable. Revisit when usage shows demand.

2. **Master spec §7.1 `decision` example contradicts shipped pack.** The master spec showed `[node-types.decision]` with `title`, `decided-at`, `status` enum, and `rationale`. v1.c vault drops all of them in favor of a categorical-only declaration. Same handling pattern as the §7.2 `project` divergence from 7.c.3 — captured here and in §4.1; the master spec is not edited by this plan. If the divergence proves confusing, a future `docs(spec): supersede v1 §7.1 decision example` commit can land separately.

3. **No ergonomic shortcuts.** Carries forward unchanged from v1.c policy.

4. **`packs/README.md` still not shipped.** With three packs after 7.c.4, the directory has accumulated enough content that a small README explaining the alias map, the `tusk pack add <name>` UX, and the per-pack TOML comment-block convention would help future contributors. Flagged for a future plan; not blocking 7.c.4.

5. **No MCP surface for `tusk pack add`.** Carries forward unchanged from 7.c.1 §10 ledger #10.

6. **`relates-to` symmetric semantics encoded by absence of inverse.** Querying a `relates-to` edge backward currently requires the `<-` operator. Some users may find this less ergonomic than declaring an inverse name. v1.c keeps the absent-inverse encoding as the cleanest representation of symmetric semantics; revisit if the `<-` operator UX proves painful.

---

## 8. Plan 7.c+ Ledger Updates

Plan 7.c.1's §10 ledger items are updated:

| # | Item | Status |
|---|---|---|
| 3 | Vault pack content (`note`/`meeting`/`decision` node types, `references`/`relates-to` edges) | **Shipped in 7.c.4** |
| (new) | Master spec §7.1 inline `[node-types.decision]` example with structured properties (`title` reserved, `decided-at`, `status`, `rationale`) | Dropped from v1.c. Vault is purely categorical; users wanting structured decision fields declare them inline. |

After 7.c.4 lands, the entire 7.c series is closed. The `packs/` trilogy (tags + kanban + vault) constitutes the v1.c built-in pack roster. The remaining 7.c.1 ledger items (no MCP `pack add` surface, packs `README.md` deferral, future ref-edge-name-aliases work) carry forward into post-v1.c.
