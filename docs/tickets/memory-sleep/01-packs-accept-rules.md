---
type: ticket
title: Let type packs carry doctor rules
blocks:
  - docs/tickets/memory-sleep/02-memory-pack
order: 1
parent: docs/tickets/memory-sleep/epic
points: 3
priority: medium
status: completed
---

A pack file may hold only `[node-types]`, `[edge-types]` and `[behaviors]` today (`internal/typepacks/validate.go`). The memory pack in [[docs/tickets/memory-sleep/02-memory-pack]] ships three doctor rules, so `tusk pack add` has to accept `[rule]` sections and write them into `tusk.toml`.

## Scope

- Allow `[rule]` as a top-level section of a pack.
- Treat a rule whose name already exists in the workspace manifest as a collision, the way a node type or an edge type is, and let `--force` override it the same way.
- Keep `[check]` out of packs. A check is shell text, and a pack fetched from a URL must not be able to add one.
- Update `docs/packages/typepacks.md` and the packs help topic.

## Done when

- A pack with a rule adds it, and `tusk doctor` runs the rule.
- A colliding rule name fails without `--force` and writes nothing.
- A pack with a `[check]` section is still refused.
- The existing built-in packs still load unchanged.

## Result

Done. A pack may now declare `[rule]` beside node types, edge types and behaviors.

- A rule block has to decode, or the add is refused with exit code 3: a value of the wrong type, a key a rule does not take, or a `rule.<name>` that is not a table.
- Whether a rule's filter names declared types is not checked at add time, because it depends on the workspace the pack lands in. Doctor reports a rule like that as `rule-invalid` on its next run, as it does for a hand-written rule.
- `manifest.DecodeRules` is the new exported decoder the pack code reads rules with.
