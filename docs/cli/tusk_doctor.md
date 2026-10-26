---
title: tusk doctor
---

## tusk doctor

Check workspace and index health; exits 1 when an error is present

### Synopsis

Run health checks against the workspace and index.

Every finding has a severity, and the report lists errors first:
  * error: the vault or the index is wrong (a dangling link, a property
    that breaks its declaration, a file reindex could not parse).
  * warning: correct but degraded (a node missing from semantic results,
    an undeclared type, a no-op setting).
  * advice: a hint that changes nothing about correctness.

Doctor exits 1 when an error is present, so "tusk doctor" works as a check
after every edit. --fail-on=warning also fails on warnings; --fail-on=never
always exits 0. Advice never fails the run.

Doctor checks the index, not the files. Pass --reindex to bring the index up
to date with disk first: the same pass as "tusk reindex", minus embedding, so
it needs no Ollama. A CI runner starts without an index (.tusk/ is
gitignored), so "tusk doctor --reindex" is the whole CI check.

Doctor reports:
  * Index not built: no reindex pass has ever written this index, so files on
    disk may be unchecked. A warning; --reindex builds the index.
  * Skipped files: files reindex could not index because their frontmatter
    does not decode, an edge value has the wrong shape, or the path is a
    reserved id. Plain markdown with no frontmatter or no type is not a node
    and is never reported.
  * Undeclared types: node types used by indexed files but not declared in
    tusk.toml, whose properties go unvalidated. Skipped when tusk.toml
    declares no node types at all.
  * Property drift (frontmatter values whose type does not match the
    manifest declaration). Drift for a deleted or renamed node is not
    reported — it is an orphan with no repair path.
  * Dangling edges (edges whose target node no longer exists), once per
    file, with the sub-units that carry the same link.
  * Edges their edge type forbids: a source or target type outside the
    type's from/to, or more than one target on a one-to-one or many-to-one
    type. "tusk edge add" refuses these, but reindex indexes a hand-edited
    file as written, so the edge stays queryable until it is fixed.
  * Invalid [alias] and [context] declarations, and [context.pinned] ids
    that no longer resolve.
  * Rules: every node a [rule.<name>] filter matches, reported as
    rule:<name> at the rule's severity (error unless it sets one). A rule
    matches file nodes only unless its filter selects a sub-unit type. A rule
    that does not parse, or names a type, property or enum value tusk.toml
    does not declare, is a rule-invalid error.
  * Embedding queue depth, embed-retry rows (an embed that failed and is
    being retried) and embed-failed rows (one that failed 3 times and stopped
    retrying until the node changes, reindex --force, or an [embeddings]
    change), each with its attempt count and last error.
  * Sub-unit pane: per-kind counts, deduped sub-units, oversize payloads.
  * Graph-expansion pane: the resolved [query.graph-expansion] settings,
    unknown edge types (valid but undeclared — the walker skips them),
    invalid edge types (malformed refs that break every --semantic query),
    and no-op warnings when the feature is enabled with weight=0 or an
    empty edge-types list.

Doctor also auto-migrates any legacy "__cli__" / "__mcp__" edge rows in the
index back into the source node's markdown frontmatter — pass --no-migrate
for a diagnostic-only run. A legacy row whose edge type is no longer declared
in tusk.toml cannot be migrated; it is reported as skipped and left in place.

Sub-unit addresses: sub-units are indexed under structural addresses appended
to the file id, e.g. notes/doc#S1.2P3. The "deduped sub-units" count is the
number of content groups shared by two or more sub-units (embedded once, then
shared). Sections are aggregated from their descendants, never embedded, so
they are not flagged as missing embeddings.

```
tusk doctor [flags]
```

### Examples

```
  # Health snapshot after a manifest change
  tusk pack add kanban
  tusk doctor

  # Quick check before starting an MCP session
  tusk doctor && tusk mcp

  # CI check on a fresh checkout: build the index, then check it
  tusk doctor --reindex

  # Strict check: fail on warnings too
  tusk doctor --reindex --fail-on=warning

  # Report only; always exit 0
  tusk doctor --fail-on=never

  # Diagnostic-only run; do not migrate legacy edge rows
  tusk doctor --no-migrate
```

### Options

```
      --fail-on string   exit 1 when an issue at this severity or worse is present: error, warning, or never (default "error")
  -h, --help             help for doctor
      --no-migrate       skip auto-migration of legacy __cli__/__mcp__ edge rows (diagnostic-only run)
      --reindex          bring the index up to date with disk before checking (structural pass, no embedding)
```

### Options inherited from parent commands

```
  -v, --verbose         emit debug-level logs to stderr
  -C, --workspace dir   run as if started in dir: find tusk.toml from there instead of the working directory (default $TUSK_WORKSPACE)
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown + HTML vault into a graph

