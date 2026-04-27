# Phase 2 — MCPEnv + MCP Test Migration

> Initiative: Repo-Root Tusk Workspace
> Spec: `docs/superpowers/specs/2026-04-27-repo-root-tusk-workspace-design.md`

## Prerequisites

Phase 1 (Harness Foundation) complete. This phase relies on:

- `newCmd(t, binPath, args ...string) *exec.Cmd` exists in
  `tests/e2e/harness.go`. `MCPEnv`'s constructor uses it directly to
  build the `tusk mcp serve` subprocess command.
- `Env.workDir` defaults to `t.TempDir()` (general suite invariant —
  not directly consumed by `MCPEnv`, but Phase 1's other extensions
  must be in place so the broader suite still passes after this
  phase's deletions).

`MCPEnv` defines its own `WithHome` and `WithEnv` methods (mirroring
`Env`'s pattern but unrelated as code). It does not consume `Env`'s
opt-out methods.

## Inherits From

The codebase at the start of this phase has Phase 1's harness
extensions in place. The pre-existing `mcpEnv` type, `newMCPEnv`,
`newMCPEnvWithHome` helpers, and all current MCP tests still work
unchanged — Phase 1 did not touch them.

The implementer can rely on:

- `tests/e2e/harness.go` exporting Layer 2 primitives (`Env`,
  `newCmd`, the new opt-outs).
- Existing `mcpEnv` in `tests/e2e/mcp_test.go` lines 14-68 — to be
  replaced.
- Existing `newMCPEnv` and `newMCPEnvWithHome` in `mcp_test.go` — to
  be deleted at end of phase.
- Existing local `newMCPEnv` clone in
  `tests/e2e/mcp_urgency_overrides_test.go` (defined inside that
  file, around line 23-40) — to be deleted.
- `config_test.go::TestMCP_DisabledTools` (line 52) calls
  `newMCPEnvWithHome(t, binPath, homeDir)` — also migrated in this
  phase so the helper can be deleted cleanly.
- `config_test.go::envWithHome` is **untouched** in this phase. It
  still has consumers in `config_test.go` that migrate in Phase 4.

## Goal

Introduce a unified `MCPEnv` type built on `newCmd`, migrate every
MCP-using test to it, and delete the legacy `mcpEnv` /
`newMCPEnv` / `newMCPEnvWithHome` helpers.

## Tasks

### 1. Create `tests/e2e/harness_mcp.go`

New file. Defines:

- `MCPEnv` struct with fields `t`, `cmd`, `stdin`, `stdout`, `nextID`,
  `homeDir string`, `extraEnv []string`, `started bool`, `dbPath
  string`, `configDir string`.
- `NewMCPEnv(t *testing.T, binPath string) *MCPEnv` — creates a temp
  DB path under `t.TempDir()`, creates a temp config dir under
  `t.TempDir()`, stores them on the struct, and returns the (not yet
  started) `MCPEnv`.
- `(*MCPEnv) WithHome(dir string) *MCPEnv` — chainable; mutates
  `homeDir`. Calls `t.Fatalf` if `e.started`.
- `(*MCPEnv) WithEnv(key, value string) *MCPEnv` — chainable; appends
  to `extraEnv`. Calls `t.Fatalf` if `e.started`.
- `(*MCPEnv) Send(method string, params any) jsonRPCResponse` — lazy-
  starts the subprocess on first call (sets `e.started = true`,
  registers `t.Cleanup`, sends `initialize`), then sends the
  requested method.
- `(*MCPEnv) callTool(name string, args map[string]any) map[string]any`
- `(*MCPEnv) callToolRaw(name string, args map[string]any) string`
- `(*MCPEnv) callToolExpectError(name string, args map[string]any) string`

The three `callTool*` helpers stay unexported (lowercase) — same
package, still accessible from test files. They wrap `Send` with the
`tools/call` envelope, exactly as today's `mcpEnv.callTool` family
does. No behavior change.

Construction inside `Send`'s lazy-start path:

```go
cmd := newCmd(t, binPath, "--db", e.dbPath, "mcp", "serve")
if e.homeDir != "" {
    cmd.Env = append(cmd.Env, "HOME="+e.homeDir, "USERPROFILE="+e.homeDir)
}
cmd.Env = append(cmd.Env, "TUSK_CONFIG_DIR="+e.configDir)
cmd.Env = append(cmd.Env, e.extraEnv...)
// stdin/stdout pipes attached as in current mcpEnv constructor
```

Reuse the existing `jsonRPCRequest` / `jsonRPCResponse` types from
`mcp_test.go` — they remain in `mcp_test.go` (unmoved). The new
`MCPEnv.Send` references them via the package.

**Source for `MCPEnv` method bodies:** the existing `mcpEnv` methods
in `mcp_test.go`:

- `mcpEnv.send` (line 87) → `MCPEnv.Send`
- `mcpEnv.initialize` (line 121) → `MCPEnv.initialize`
- `mcpEnv.callTool` (line 147) → `MCPEnv.callTool`
- `mcpEnv.callToolRaw` (line 179) → `MCPEnv.callToolRaw`
- `mcpEnv.callToolExpectError` (line 203) → `MCPEnv.callToolExpectError`

Before deleting `mcpEnv` in Task 2, capture each method body and
re-implement on `MCPEnv` in `harness_mcp.go` against the new field
set (reading from `e.stdout`, writing to `e.stdin`, incrementing
`e.nextID`). Behavior must be identical — same request envelopes,
same response decoding, same error handling. The receiver type
changes from `*mcpEnv` to `*MCPEnv`; otherwise the bodies are
unchanged.

`MCPEnv.initialize` is called automatically inside `Send`'s lazy-
start path on first call; tests do not invoke it directly.

### 2. Migrate `tests/e2e/mcp_test.go`

- Delete the `mcpEnv` struct (lines ~14-21), `newMCPEnv` (lines
  ~24-68), `newMCPEnvWithHome` (later in the file), and **all five
  methods on `mcpEnv`**: `send` (line 87), `initialize` (line 121),
  `callTool` (line 147), `callToolRaw` (line 179),
  `callToolExpectError` (line 203). Their bodies were captured in
  Task 1 and re-implemented on `MCPEnv`.
- Keep `jsonRPCRequest` and `jsonRPCResponse` types (lines ~71-86) —
  they remain in `mcp_test.go`, referenced by `harness_mcp.go` via
  the package.
- Update every call site in the file:
  - `newMCPEnv(t, binPath)` → `NewMCPEnv(t, binPath)`
  - `newMCPEnvWithHome(t, binPath, homeDir)` → `NewMCPEnv(t, binPath).WithHome(homeDir)`
  - Method calls on the returned value (`.send(...)`, `.callTool(...)`,
    `.callToolRaw(...)`, `.callToolExpectError(...)`) become `.Send(...)`,
    `.callTool(...)`, `.callToolRaw(...)`, `.callToolExpectError(...)`
    respectively. Only `Send` is renamed (capitalized); the other three
    keep their lowercase names.
- Test functions to update (call sites only — no test logic changes):
  - `TestMCPTaskLifecycle` (line 229)
  - `TestMCPTaskModify` (line 301)
  - `TestMCPRelations` (line 328)
  - `TestMCPProjects` (line 372)
  - `TestMCPErrorHandling` (line 398)
  - `TestMCPAnnotations` (line 441)
  - `TestMCPTaskDelete` (line 466)
  - `TestMCPResources` (line 521)
  - `TestMCPTree` (line 630)

### 3. Migrate `tests/e2e/mcp_urgency_overrides_test.go`

- Delete the local `newMCPEnv` clone (around lines 23-40) and any
  associated helpers.
- Update every call site in the file to use `NewMCPEnv` from
  `harness_mcp.go`.
- Test functions to update (10 in this file).

### 4. Migrate `TestMCP_DisabledTools` in `tests/e2e/config_test.go`

This is the only MCP-using test in `config_test.go`. Update it so:

- The current `newMCPEnvWithHome(t, binPath, homeDir)` call (line 69)
  becomes `NewMCPEnv(t, binPath).WithHome(homeDir)`.
- All other test logic is unchanged.

`config_test.go::envWithHome` is **not** touched in this phase. It is
not used by `TestMCP_DisabledTools` (which uses
`newMCPEnvWithHome`, deleted in this phase).

### 5. Run `make test-e2e`

All MCP tests must pass with `MCPEnv`. If a test fails, debug before
declaring the phase complete. Common pitfalls:

- Forgetting to call the lazy-start in the new constructor.
- HOME-override env var not being set when `WithHome` is followed by
  `Send`.
- Race condition if `WithHome` / `WithEnv` is called after `Send`.

## User-visible behaviors that must still work

- Every MCP test in `mcp_test.go`, `mcp_urgency_overrides_test.go`,
  and `TestMCP_DisabledTools` in `config_test.go` continues to pass.
- The MCP server subprocess still spawns with `--db <tmp>` and
  isolated `TUSK_CONFIG_DIR`.
- HOME-faking tests still resolve config from the synthetic
  homeDir (now via `WithHome`).
- The MCP server's own walk-up config resolver remains scoped to
  `cmd.Dir = t.TempDir()` (inherited from `newCmd`).
- Cleanup on test completion still closes stdin and waits for the
  subprocess.

## Bridge code

None introduced in this phase. The legacy helpers (`mcpEnv`,
`newMCPEnv`, `newMCPEnvWithHome`, the local clone in
`mcp_urgency_overrides_test.go`) are **deleted** within this phase, not
kept as bridge.

`config_test.go::envWithHome` is preserved (not bridge code — it's
existing helper code with consumers that migrate in Phase 4).

## Changes Introduced

**New files:**
- `tests/e2e/harness_mcp.go` — `MCPEnv` type, constructor, `Send`,
  `WithHome`, `WithEnv`, `initialize`.

**Deleted code:**
- `tests/e2e/mcp_test.go::mcpEnv` struct.
- `tests/e2e/mcp_test.go::newMCPEnv`.
- `tests/e2e/mcp_test.go::newMCPEnvWithHome`.
- Local `newMCPEnv` clone in `tests/e2e/mcp_urgency_overrides_test.go`.

**Modified files:**
- `tests/e2e/mcp_test.go` — call-site migration (~50+ sites across
  9 test functions).
- `tests/e2e/mcp_urgency_overrides_test.go` — call-site migration
  (~10+ sites across 10 test functions).
- `tests/e2e/config_test.go` — one call site in
  `TestMCP_DisabledTools` migrated.

**No new env vars, no schema migrations, no new dependencies.**
