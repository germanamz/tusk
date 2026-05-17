---
type: note
title: Tusk v1.1.0 — bugs and rough edges
---

# Tusk v1.1.0 — bugs and rough edges found while bootstrapping the Superhuman workspace

A working list of issues found while initializing the Superhuman repo as a Tusk
workspace and bringing up the WBS pack. Each entry includes a concrete
reproduction, the workaround used, and a suggested upstream fix.

The list is meant to be turned into individual issues on
`github.com/germanamz/tusk` once we have appetite to file them; the format here
is "issue draft" rather than "design note."

Tusk version: `tusk version v1.1.0` (binary at `~/.local/bin/tusk`, built from
`github.com/germanamz/tusk`).
Workspace: `/Users/germanamz/projects/superhuman` with `superhuman-wbs` pack at
`plugins/superhuman/packs/wbs.toml`.

---

## 1. `:` in a string property silently breaks the file on write

**Severity:** High. Silent corruption — the next read of the file fails with a
YAML decode error, and `tusk node modify` / `tusk node get` start refusing to
operate on the node until it's repaired by hand.

**Repro:**

```sh
tusk node create --type wbs-note \
  --path wbs/example/spec.md \
  --title "Placeholder" \
  --prop kind=spec
tusk node modify wbs/example/spec --prop title="Spec: Example"
# Modified wbs/example/spec
tusk node modify wbs/example/spec --prop archived=false
# node: decode frontmatter wbs/example/spec.md: yaml: line 2:
# mapping values are not allowed in this context
```

The first `modify` writes a frontmatter line `title: Spec: Example` (unquoted).
The second `modify` parses the file as YAML, hits the inner `:`, and refuses.

**Expected:** Tusk quotes (or escapes) frontmatter string values containing
YAML-significant characters (`:`, `#`, `?`, leading `[`, leading `{`, `&`, `*`,
`|`, `>`, `!`, `%`, etc.).

**Workaround:** Avoid colons in any string property value. Rename
`"Spec: Superhuman ↔ Tusk v1 migration"` to
`"Migration spec — Superhuman ↔ Tusk v1"`.

**Suggested fix:** in the frontmatter writer, run each string value through the
YAML emitter's string-style picker — anything containing a YAML-reserved
character gets emitted with single or double quoting.

**Related risk:** the user can also produce this state by writing the file in
an editor without quoting. Detecting it during `tusk reindex` and surfacing the
offending file in `tusk doctor` would be a defense-in-depth measure even after
the writer is fixed.

---

## 2. `tusk node move` drops the file extension when the target has none

**Severity:** Medium. Doesn't lose data, but desyncs the index from disk and
breaks subsequent ops on the node until manually repaired.

**Repro:**

```sh
tusk node create --type wbs-note --path wbs/example/foo.md --title "Foo" --prop kind=spec
tusk node move wbs/example/foo wbs/example/bar
# Renamed wbs/example/foo → wbs/example/bar (rewrote 0 referring file(s))
ls wbs/example/
# bar    ← no .md extension
tusk node modify wbs/example/bar --prop archived=false
# node: decode frontmatter wbs/example/bar: yaml: line 2: ...
```

The CLI accepts both `wbs/example/bar` and `wbs/example/bar.md` as a node ID
for the source (workspace-relative path *without* extension is the documented
form). When the move target is given the same way, the renamer takes the
literal string as the destination filename — including the missing extension —
and writes a file with no `.md`.

**Expected:** the move target inherits the source file's extension. Either
always (since the index keys on the path-without-extension and the extension is
just a file-format marker), or with an explicit error if the target string has
no extension and the source did.

**Workaround:** always pass `--path`-style paths with `.md` to both source and
target, or run `mv` + `tusk reindex` if you already lost the extension.

**Suggested fix:** in `cmd/tusk/node/move.go` (or wherever the move handler
lives), if the source path has an extension and the target doesn't, append the
source's extension to the target before renaming. Alternatively, error out and
require the caller to specify.

---

## 3. Filter docs say `key:value`, actual parser requires `key=value`

**Severity:** Low. Documentation drift, but a high-frequency speed bump for
anyone reading the help and copying examples.

**Repro:**

```sh
tusk node list 'type:wbs-node'
# filter parse: {4 expected comparison operator (= != < <= > >=)}

tusk node list 'type=wbs-node'
# (works)
```

The help text for `tusk query` and `tusk node list` both describe the syntax as
`key:value for exact match`:

```
The filter argument uses Tusk's TaskWarrior-flavored grammar —
key:value for exact match, key.contains:foo for substring,
+tag/-tag for presence, …
```

The parser only accepts `=`, `!=`, `<`, `<=`, `>`, `>=`.

**Expected:** either the help matches the parser, or the parser accepts both
`:` and `=`. TaskWarrior's grammar (which the help name-drops) uses
`key:value` — so the user-facing expectation matches the help, not the parser.

**Workaround:** mentally translate `key:value` → `key=value` when reading
examples. Updated all internal docs to use `=`.

**Suggested fix:** prefer making the parser accept both forms — `:` is more
ergonomic and matches the cited prior art. If that's undesirable for
parsing-precedence reasons (the wikilink `[[foo:bar]]` collides), fix the docs
to use `=` consistently and drop the TaskWarrior reference.

**Touched copy:** `cmd/tusk/query.go` help string, `cmd/tusk/node/list.go` help
string, and any README sections that show filter examples.

---

## 4. `ordered=true` edges all get ordinal 0 when added via `tusk edge add`

**Severity:** Low / design-clarity. Doesn't break anything, but the type-pack
author can't rely on edge ordering without an additional explicit step that
doesn't exist in the CLI.

**Repro:**

`plugins/superhuman/packs/wbs.toml`:

```toml
[edge-types.wbs-parent]
from        = ["wbs-node"]
to          = ["wbs-node"]
ordered     = true
```

Then:

```sh
for slug in s1 s2 s3 s4 s5 s6 s7; do
  tusk node create --type wbs-node \
    --path wbs/proj/$slug.md --title "$slug" \
    --prop level=story --prop status=drafted
  tusk edge add --type wbs-parent \
    --source wbs/proj/$slug --target wbs/proj
done

tusk edge list --type wbs-parent
# TYPE        SOURCE        TARGET    ORDINAL  SOURCE_PATH
# wbs-parent  wbs/proj/s1   wbs/proj  0        __cli__
# wbs-parent  wbs/proj/s2   wbs/proj  0        __cli__
# wbs-parent  wbs/proj/s3   wbs/proj  0        __cli__
# … all zero
```

Every child gets ordinal `0`. No way to set the ordinal from the CLI —
`tusk edge add` has no `--ordinal` flag.

**Expected:** at least one of:

- `tusk edge add --ordinal N` to set the ordinal explicitly.
- Auto-assign the next free ordinal per `(source, target, type)` group when
  `ordered = true` is declared on the edge type.
- Document the expectation that ordering on CLI-added edges is the user's
  responsibility and is encoded somewhere else (frontmatter `[[edges]]` blocks?).

**Workaround:** rely on title prefixes (`S1 —`, `S2 —`) for ordering. Adequate
for a 7-Story migration project, doesn't scale.

**Suggested fix:** the auto-assign behavior is the most ergonomic. When
`ordered = true`, on edge add:
`SELECT COALESCE(MAX(ordinal), -1) + 1 FROM edges WHERE source=? AND target=? AND type=?`,
then insert at that ordinal. Add `--ordinal` for explicit overrides.

---

## 5. Workflow-owned `status` triggers a permanent `undeclared-property` warning

**Severity:** Low / cosmetic, but it affects the built-in `kanban` pack too —
so anyone running `tusk doctor` after `tusk pack add kanban` sees the same
noise.

**Repro:**

```sh
tusk init --name demo
tusk pack add kanban
tusk node create --type ticket --path tickets/T-1.md \
  --title "X" --prop status=pending --prop priority=high
tusk doctor
# [undeclared-property] tickets/T-1: node-types: property "status" not declared on type "ticket"
```

The `kanban` pack explicitly comments that `status` is governed by
`[behaviors.workflow.kanban]` and *must not* be redeclared in
`[node-types.ticket].properties`. Tusk's writer respects that constraint, then
Tusk's doctor flags every ticket because `status` isn't in the type's
properties list.

**Expected:** when a `[behaviors.workflow.<name>]` block declares
`applies-to = [<type>]` with `status-property = "<prop>"`, that property is
implicitly considered "declared" on the type for `undeclared-property` checks.

**Workaround:** none — accept the warning. Document it in the Superhuman
bootstrap output so first-time users aren't alarmed.

**Suggested fix:** in the property-drift validator, build the
declared-property set as
`union(type.properties, all workflow.status-property for workflows applying to this type)`.
Add `enum`-style enforcement against `states[].name` while you're there, so
`--prop status=typo` is caught as a workflow violation rather than slipping
through as a property-drift.

**Touched code:** wherever `doctor` materializes the schema view for each node.
Probably the same place that validates enum values today.

---

## Composite request — already on the Superhuman migration spec

These two were already filed as feature requests in
`docs/superpowers/specs/2026-05-17-superhuman-tusk-v1-migration.md` (and the
Tusk-managed copy at `wbs/superhuman-tusk-v1-migration/spec.md`) in the
Superhuman workspace, but worth pointing at from here:

- **FR #1 — composite `tusk_node_attach`** (create node + add edge atomically).
- **FR #4 — depth-N descendants in one query** (the binary already has
  `descendants_%d` strings).

These aren't bugs, but they're the two ergonomic asks most felt during the
Superhuman bootstrap session. Move them into this file if/when we file the bugs
upstream and want a single "open Tusk asks" doc.

---

## Filing checklist (when we're ready)

For each issue above, the upstream filing should include:

- [ ] Tusk version (`tusk --version`)
- [ ] OS / arch (`uname -smr`)
- [ ] Minimal repro from this doc
- [ ] Expected vs. actual
- [ ] Workaround if known
- [ ] Suggested code location (if we've identified one)

Filing order, lowest-coupling first: **#3 (docs)** → **#5 (validator change)**
→ **#4 (edge ordinal)** → **#2 (move extension)** → **#1 (frontmatter
quoting)**. #1 is the highest-impact but also the most-likely-to-have-a-design-debate
(which YAML library, do we re-emit existing files with quotes, etc.), so it
benefits from filing after the easier ones have established a pattern.
