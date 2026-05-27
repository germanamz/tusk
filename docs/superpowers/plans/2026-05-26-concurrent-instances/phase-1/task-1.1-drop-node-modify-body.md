# Task 1.1 — Drop `body` from `node_modify`

**Phase:** 1
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** none.
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.** Do not bundle unrelated changes.
2. **The PR must be independently shippable.** Build green, full Go test
   suite green (lefthook pre-commit runs it), `make lint` clean.
3. **No bridge code.** This is a pure removal; the rest of the work stream
   depends on the surface being narrowed.
4. **No documentation churn beyond what the change requires.** The CLI
   `node modify` help already describes frontmatter-only behavior; the MCP
   tool description needs a small update.

## Goal

Remove the optional `body` input from `tusk_node_modify`, the `Body` field
from `node.ModifyInput`, and the body-replacement branch in
`Service.Modify`. After this task, `node_modify` is a pure frontmatter
mutation tool. Body changes are out of scope for this tool entirely —
they happen through direct FS writes caught by `tusk watch`.

## Scope

### Files to modify

- `internal/node/service.go`
  - Remove the `Body *[]byte` field from `ModifyInput`
    (currently around line 37).
  - Remove the body-replacement block in `Service.Modify`
    (currently `body := parsed.Body` plus the `if input.Body != nil { ... }`
    branch around lines 387-392).
  - Use `parsed.Body` directly in the `renderMarkdown` call that follows.
  - Update the doc comment on `Service.Modify` to drop the `Body` mention.

- `internal/mcp/tools.go`
  - In `registerNodeModifyTool`, remove the `mcpgo.WithString("body", ...)`
    option from the tool declaration (currently around line 1032).
  - In the handler, remove the block that reads `body` from
    `request.GetArguments()` and assigns it to `input.Body` (currently
    around lines 1051-1054).
  - Update the tool description to clarify that body changes are not part
    of this tool's surface. Suggested phrasing: *"Modify a node's
    frontmatter properties (set or unset). Cannot change type. Body
    changes are made by writing to the file directly; the watcher reindexes."*

### Files to leave alone

- `cmd/tusk/cmd_node_modify.go` — the CLI command already exposes only
  `--prop` and `--unset`. No change required.
- All other callers of `node.ModifyInput` — verified by grep at
  planning time that no other call site sets `.Body`.

### Tests

A grep at planning time confirmed no test in `internal/node/service_test.go`
constructs a `ModifyInput` with the `Body` field set. Verify this once more
before submitting and remove any test that does, if one slipped in. Add no
new tests for this task — the removal is covered by existing modify tests
continuing to pass.

## Verification

Before opening the PR:

1. `make build` — confirms compilation.
2. `make test` — full unit test suite passes.
3. `make vet` — no new vet warnings.
4. `make lint` — `golangci-lint run ./...` is clean.
5. `make docs` — regenerates CLI and MCP docs. Inspect
   `docs/cli/tusk_node_modify.md` (no change expected) and any MCP
   tool-listing markdown for the description update.
6. Manual MCP smoke: send a `tusk_node_modify` call with a `body` field.
   The MCP framework should reject it as an unknown parameter.

If any of these fail, fix before submitting. Do not skip lefthook
(no `--no-verify`).

## Out of Scope

- The lease and concurrency work — that begins in Phase 2.
- Any change to `node_create`, `node_move`, or `node_delete`.
- Any change to how the watcher handles file body changes.

## Notes for the Implementer

- This task is small but load-bearing. It establishes the contract that
  the rest of the work stream relies on (specifically, the absence of an
  optimistic-concurrency token on writes — see spec § *Optimistic
  concurrency*). If you find yourself wanting to "just leave the body
  field in case someone needs it," stop and re-read the spec.
- If you discover an unexpected internal caller that relies on `.Body`,
  do not introduce a workaround in this task. Stop and surface the
  finding; the plan may need revision.
