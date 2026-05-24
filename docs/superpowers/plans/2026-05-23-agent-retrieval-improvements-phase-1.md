# Agent Retrieval Improvements — Phase 1 (Rich Query + Memory Entry Point) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Code blocks in this plan are **data-structure proofs only** — schemas, struct shapes, manifest examples, response envelopes. Implementation bodies are deliberately omitted; describe them in prose during execution.

**Spec:** `docs/superpowers/specs/2026-05-23-agent-retrieval-improvements-design.md` §3, §4.

**Goal:** Collapse the agent ↔ tusk round-trip loop without changing storage or embeddings: rich opt-in result shapes on read tools, manifest-declared aliases reusable across CLI/MCP, and a single warm-context tool composed from those aliases.

**Architecture:** All additive. The Cobra command tree gains a service-layer split so the same Go function backs CLI `RunE`, MCP tool handlers, and the new alias dispatcher. A new positional-name registry maps each read-only verb to its named positionals. The filter grammar gets one new predicate (`modified-since:`). New optional arguments and new tools/CLI verbs ride on top of the existing surfaces.

**Tech Stack:** Go, Cobra (CLI), mcp-go (MCP), SQLite, BurntSushi/toml (manifest), existing `internal/filter` grammar.

**Prerequisites:** None beyond current `main`.

---

## Task 1: Service-layer extraction and positional-name registry

**Why this task exists:** Today, every Cobra `RunE` body reads flags, calls into the engine, and renders output inline. The MCP tool handlers duplicate the engine-call logic with their own argument decoding. Phase 1 introduces a third caller — the alias dispatcher (Task 4) — which would triple the duplication. This task collapses the three paths to one Go function per verb. No behavior change.

**Files:**

- Create: `internal/cliregistry/registry.go` — the positional-name registry and read-only verb set.
- Create: `internal/cliregistry/registry_test.go`.
- Modify (one file each, refactor only): the existing service implementations behind these read-only verbs:
  - `node list` → `internal/node/list_service.go` (extract from `cmd/tusk/node_list.go`).
  - `node get` → `internal/node/get_service.go`.
  - `query` → `internal/mcp/tools.go` query handler is the most evolved; extract from there into `internal/filter/query_service.go` (or a new package `internal/query/`).
  - `edge list` → `internal/index/edge_list_service.go` (or similar local home).
  - `doctor` → `internal/doctor/run_service.go` (extract from current doctor implementation).
  - `status` → `internal/status/run_service.go`.
- Modify: the corresponding Cobra `RunE` files under `cmd/tusk/` so they build the request struct from flags, call the service, and render the result.
- Modify: `internal/mcp/tools.go` handlers for `tusk_node_list`, `tusk_node_get`, `tusk_query`, `tusk_edge_list`, `tusk_doctor`, `tusk_status` so they call the service functions instead of repeating logic.

**Steps:**

- [ ] **Map the existing call paths.** For each of the six read-only verbs, locate the Cobra `RunE` and the matching MCP handler in `internal/mcp/tools.go`. Note where flags/args are read, where the index is touched, where rendering happens. The render step stays at the call site (Cobra prints to stdout, MCP wraps in `CallToolResult`); the engine-touch step is what moves into the service.

- [ ] **Define request and result types per verb.** Each verb gets a typed `<Verb>Request` and `<Verb>Result` struct. The fields must cover every flag and positional that exists today. The result must carry every piece of data the existing renderers consume.

  Data-structure proof (one verb shown — extend the pattern to all six):

  ```go
  // internal/node/list_service.go
  type ListRequest struct {
      Filter string
      Sort   []string
      Take   int
      Skip   int
      // Include, Fields, Format are added in Task 3 — leave them as TODO comments
      // here so the struct field set is stable.
  }

  type ListResult struct {
      Rows  []NodeRow      // existing NodeRow shape from internal/node
      Count int
  }

  func ListRun(ctx context.Context, rt *mcp.Runtime, req ListRequest) (*ListResult, error)
  ```

- [ ] **Move the engine-call body into each service function.** Cut from the Cobra `RunE`, paste into the new `<Verb>Run` function, parameterize on the request fields. The function returns the result struct; it does not print or marshal.

- [ ] **Rewire Cobra `RunE` to call the service.** `RunE` is now: build `<Verb>Request` from `cmd.Flags()` and `args`; call `<Verb>Run`; render the result the way it was rendered before (text to stdout, or JSON if `--json`).

- [ ] **Rewire the MCP handlers to call the same service.** Each handler in `internal/mcp/tools.go` builds the request from `mcpgo.CallToolRequest`, calls the service, and uses `toolJSON` (or whatever renderer applies) to wrap the result. The MCP-side rendering layer stays where it is.

- [ ] **Build the positional-name registry.** A single Go map declares, for each read-only verb, its positional argument names in CLI order, plus a back-pointer to its MCP tool name.

  Data-structure proof:

  ```go
  // internal/cliregistry/registry.go
  type VerbSpec struct {
      Positionals []string // ordered names; "" for unnamed (verb has none)
      Tool        string   // MCP tool name, e.g. "tusk_node_list"
      Verb        string   // CLI sub-command path, e.g. "node list"
      ReadOnly    bool     // all six P1 entries are true; write verbs are added with false
  }

  var ReadOnly = map[string]VerbSpec{
      "node list": {Positionals: nil,              Tool: "tusk_node_list", Verb: "node list", ReadOnly: true},
      "node get":  {Positionals: []string{"id"},   Tool: "tusk_node_get",  Verb: "node get",  ReadOnly: true},
      "query":     {Positionals: []string{"filter"}, Tool: "tusk_query",   Verb: "query",     ReadOnly: true},
      "edge list": {Positionals: nil,              Tool: "tusk_edge_list", Verb: "edge list", ReadOnly: true},
      "doctor":    {Positionals: nil,              Tool: "tusk_doctor",    Verb: "doctor",    ReadOnly: true},
      "status":    {Positionals: nil,              Tool: "tusk_status",    Verb: "status",    ReadOnly: true},
  }

  // Write verbs are added so the alias loader (Task 4) can reject them by name without
  // needing to know what they do. ReadOnly=false marks them as off-limits to aliases.
  var Write = map[string]VerbSpec{
      "node create": {Verb: "node create", ReadOnly: false},
      "node modify": {Verb: "node modify", ReadOnly: false},
      "node move":   {Verb: "node move",   ReadOnly: false},
      "node delete": {Verb: "node delete", ReadOnly: false},
      "edge add":    {Verb: "edge add",    ReadOnly: false},
      "edge remove": {Verb: "edge remove", ReadOnly: false},
  }
  ```

  Test the registry by asserting that every CLI sub-command path Cobra knows about appears either in `ReadOnly` or `Write` (no orphans), and that `Tool` names match the MCP registrations.

- [ ] **Verification.** Run the full Go test suite — every existing test must still pass without modification. Run `tusk node list <filter>` and `tusk node get <id>` against a sample workspace and confirm the output is byte-identical to current `main`. Run an MCP smoke test (manual or scripted) confirming `tusk_query` and friends respond as before.

- [ ] **Commit.** `refactor(read-verbs): extract service layer + positional registry`. The whole task is one commit; the refactor must land atomically so the codebase doesn't have a mixed state where some verbs are extracted and others aren't.

**Pitfalls:**

- Renderers sometimes read derived fields directly off the DB row scan. Move those derivations into the service (returning a populated result) rather than letting renderers re-query.
- The MCP `tusk_query` handler is the most evolved — it does semantic ranking inline. Keep the semantic-ranking logic inside the service (it is shared, not a render concern).
- Lefthook pre-commit runs the full Go test suite. Do not commit while tests fail.

---

## Task 2: Filter grammar — `modified-since:` predicate

**Why this task exists:** `tusk_context.recent` and many useful aliases want to filter on "modified in the last N days." The grammar needs one new predicate; the SQL compiler already has `last_mtime` in scope so the codegen is straightforward.

**Files:**

- Modify: `internal/filter/lexer.go` (or wherever tokens are defined) — add a `modified-since` token.
- Modify: `internal/filter/parse.go` — recognize `modified-since:<value>` as a predicate.
- Modify: `internal/filter/compile.go` — emit `last_mtime >= ?` against the index.
- Modify: `internal/filter/validate.go` — accept the new predicate.
- Tests: add cases in the existing `*_test.go` files for each layer.

**Steps:**

- [ ] **Define the value grammar.** The predicate accepts either a duration (`7d`, `48h`, `30m`) or an ISO 8601 date or datetime. The lexer treats the right-hand side as an opaque string; the validator parses and rejects malformed values.

- [ ] **Lexer.** Add `modified-since` as a recognized predicate name. Whatever scheme the existing lexer uses for tag-like predicates (`tagged`, `tree`, `parent`, `root`) is the pattern — follow it. Write a failing lexer test asserting that `modified-since:7d` tokenizes as `[ident:modified-since][colon][value:7d]` (or whatever the existing token shape is), then implement.

- [ ] **AST.** Add a `ModifiedSinceNode` (or extend an existing predicate node with a discriminator) carrying the parsed value as either a `time.Duration` or a `time.Time`. Decide which at parse time and keep both fields with one set, the other zero.

  Data-structure proof:

  ```go
  // internal/filter/ast.go
  type ModifiedSincePredicate struct {
      Duration time.Duration // set iff value parsed as a duration
      Since    time.Time     // set iff value parsed as a date/datetime
  }
  ```

- [ ] **Validator.** Reject `modified-since:` with empty value, malformed duration, or unparseable date. Surface a friendly error pointing at the offending token. Test both happy and unhappy paths.

- [ ] **Compiler.** When emitting SQL, translate `modified-since:7d` to `last_mtime >= unix_now() - 7*86400`. For absolute dates, `last_mtime >= <unix-of-date>`. Use parameterized SQL — never inline values. Add a compile test asserting the generated SQL and bound parameters.

- [ ] **End-to-end test.** Add a filter-package integration test: parse `modified-since:7d type=note`, validate, compile, run against a small fixture index, assert the right rows return.

- [ ] **Verification.** Run `go test ./internal/filter/...` — all passing.

- [ ] **Commit.** `feat(filter): add modified-since predicate`.

**Pitfalls:**

- Date strings vs duration strings: `2026-05-23` parses as a date; `7d` as a duration. `1d` is a duration, not "the first of December." Always try duration parsing first if there are no separators, date parsing if there is a `-` followed by digits.
- The filter grammar is documented in `docs/cli/` and `man/` (generated). After this task lands, regenerate docs via `make docs` and include the regenerated artifacts in the commit. The pre-push docs-drift hook will reject the push otherwise (see CLAUDE.md memory `project_make_docs_regenerates_man_and_cli`).

---

## Task 3: `include`, `fields`, `format` on read tools, plus compact renderer

**Why this task exists:** This is the round-trip-killer feature for "find → read" loops. After this task, agents can request body / edges / properties in one call instead of fetching nodes individually.

**Files:**

- Modify: every `<Verb>Request` struct created in Task 1 to add `Include []string` and `Fields []string`. (The `Format` choice lives at the renderer, not in the service request — see below.)
- Modify: each `<Verb>Run` service function to expand the result with the requested fields. For `query` and `node list`, this means an additional SELECT projection. For `node get`, it means optionally loading the file body and edges. For `edge list`, no change (edges are the whole result).
- Modify: each Cobra `RunE` to accept `--include`, `--fields`, `--format` flags.
- Modify: each MCP tool registration to declare `include`, `fields`, `format` as optional arguments.
- Create: `internal/render/compact.go` — the compact-format renderer for query results, node-list results, and edge-list results.
- Tests: per-service tests for include expansion, plus a render-test fixture comparing compact output line-by-line.

**Steps:**

- [ ] **Define the include vocabulary and defaults.** Allowed values: `body`, `edges`, `properties`. Default include set when neither `include` nor `fields` is provided: empty (back-compat — returns only `id, type, path, title` as today). When `fields` is set, derive the include set automatically from which expandable fields are listed.

- [ ] **Extend the result structs.** Each `<Verb>Result.Rows[i]` (or equivalent) gains optional fields: `Body string`, `Properties map[string]any`, `Edges []EdgeRef`. The fields are populated only when the request asked for them; the renderers and MCP marshallers must omit empty fields from JSON output.

  Data-structure proof:

  ```go
  // internal/node/types.go (or wherever NodeRow lives)
  type NodeRow struct {
      ID    string
      Type  string
      Path  string
      Title string

      // Populated only when Include contains the matching value.
      Body       string         `json:"body,omitempty"`
      Properties map[string]any `json:"properties,omitempty"`
      Edges      []EdgeRef      `json:"edges,omitempty"`
  }

  type EdgeRef struct {
      Type        string `json:"type"`
      Direction   string `json:"direction"`   // "out" | "in"
      TargetID    string `json:"target_id"`
      TargetTitle string `json:"target_title,omitempty"`
  }
  ```

- [ ] **Service-layer expansion logic.** A shared helper (in `internal/node` or a new `internal/expand` package) takes a slice of `NodeRow` plus an include set and decorates each row. `body` reads the file from disk (use the existing workspace abstraction). `edges` runs a single SQL query with `source_id IN (...)` OR `target_id IN (...)` and groups by node. `properties` parses each row's `properties_json` column.

- [ ] **`fields` projection.** When `fields` is set on the request, the renderers and MCP marshallers emit only those fields. The service still populates the full result; projection is a render-time concern so the cache and downstream consumers see a stable shape.

- [ ] **Compact renderer.** Output format per the spec §4.4: tab-aligned columns; one record per line; when `body` is included, indented body lines follow until the next record; when `edges` is included, `  → <type> <target_id> <target_title>` lines follow. The renderer is shared across `query`, `node list`, `edge list`, and `tusk run`. Width is the maximum across rows for each column.

  Data-structure proof of the compact-form contract:

  ```
  notes/auth-rfc        note     Auth RFC
    body line 1
    body line 2
    → references  notes/oauth-rfc    OAuth RFC
  tickets/fix-login     ticket   Fix login bug    status=active priority=3
  ```

  Fixture test: take a fixed result slice, render, compare byte-for-byte.

- [ ] **CLI flag wiring.** Add `--include` (comma-separated), `--fields` (comma-separated), `--format` (`compact` | `json`) to each read-verb command. Default for `--format` is compact when stdout is a TTY, JSON when `--json` is set (the existing `--json` flag stays as an alias).

- [ ] **MCP argument wiring.** Add `include` (array), `fields` (array), `format` (string) to each read-verb tool's MCP schema. Test by calling the MCP tool with `include: ["body"]` and asserting the JSON response carries the body field.

- [ ] **Verification.** Run the full suite; run `tusk query type=ticket --include body,edges` against a fixture vault and inspect the output. Run the same query via MCP smoke test.

- [ ] **Commit.** `feat(read-verbs): add include/fields/format to query, node get, node list, edge list`.

**Pitfalls:**

- `tusk_query` already returns snippets in semantic mode. When `include=body` is also set, prefer the snippet (best-matching chunk body) over the full file body — the spec is explicit on this (§4.1).
- The MCP `format` argument is unusual — MCP tools normally return JSON. When `format=compact`, return the compact text inside a single `text` content block. Add a test asserting the wrapper shape.
- `--json` is an existing CLI flag. Keep it working; `--format compact` and `--format json` are the new spelling. `--json` is sugar for `--format json`.

---

## Task 4: Alias mechanism (`[alias.<name>]`, `tusk run`, `tusk_run`)

**Why this task exists:** Manifest-declared, reusable, read-only aliases that any caller (CLI, MCP, `tusk_context`) can invoke by name. This is the composition primitive Task 5 depends on.

**Files:**

- Create: `internal/manifest/alias.go` — the `Alias` struct and the parse/validate logic for `[alias.<name>]` blocks.
- Create: `internal/manifest/alias_test.go`.
- Create: `internal/aliasdispatch/dispatch.go` — the dispatcher that takes an `Alias` and runs the corresponding service function.
- Create: `internal/aliasdispatch/dispatch_test.go`.
- Modify: `internal/manifest/manifest.go` to load `[alias.<name>]` blocks alongside existing config.
- Modify: `cmd/tusk/run.go` (new file): the `tusk run` Cobra command and `tusk run --list`.
- Modify: `internal/mcp/tools.go`: register the `tusk_run` MCP tool.
- Modify: `internal/doctor/...`: report invalid aliases.
- Tests: manifest-load tests for valid and invalid alias declarations; dispatcher tests; end-to-end CLI and MCP tests.

**Steps:**

- [ ] **Define the in-memory Alias type.** It carries the alias name, the resolved verb spec (`cliregistry.VerbSpec` from Task 1), the `description`, and an args map.

  Data-structure proof:

  ```go
  // internal/manifest/alias.go
  type Alias struct {
      Name        string                  // "open-tickets"
      Command     string                  // "node list"
      Description string                  // optional
      Args        map[string]any          // TOML-typed values
      Verb        cliregistry.VerbSpec    // resolved at load time
  }
  ```

  And the on-disk TOML shape:

  ```toml
  [alias.open-tickets]
  command     = "node list"
  description = "Active tickets, by priority"
  args.filter = "type=ticket status=active"
  args.sort   = "priority-desc"
  args.top    = 10
  ```

- [ ] **Manifest loader.** Read `[alias.*]` tables from `tusk.toml`. For each, resolve `command` against `cliregistry.ReadOnly` ∪ `cliregistry.Write` (so the error message for a write verb is "alias targets a write verb, which is not permitted" instead of "unknown verb"). Validate that:
  - The verb exists.
  - The verb is read-only (`Write` entries fail loading).
  - Every `args.<key>` is either a registered positional name for the verb or a flag name registered on the corresponding Cobra command.
  - Each value's TOML type matches the destination type (string / int / bool / list-of-string).

  Invalid aliases do not fail manifest load; they are collected into a list of validation errors that doctor surfaces. Other aliases keep working. The engine never refuses to start over a bad alias.

- [ ] **Dispatcher.** Given an `Alias` plus a runtime, the dispatcher builds the request struct for the alias's verb (using reflection or a per-verb adapter — choose one and be consistent), sets each `args` key onto the right field, and calls the service function. The result is returned to the caller untouched.

  The dispatch table — a registry of `verb-name → (request-builder, service-call, kind)` — is the cleanest approach. Per-verb adapters are explicit, easy to test, no reflection.

  Data-structure proof:

  ```go
  // internal/aliasdispatch/dispatch.go
  type Dispatcher struct{ rt *mcp.Runtime }

  type DispatchResult struct {
      Alias   string
      Command string
      Kind    string   // "node-list" | "node-get" | "query" | "edge-list" | "doctor" | "status"
      Result  any      // the typed *<Verb>Result from the service
  }

  func (d *Dispatcher) Run(ctx context.Context, a manifest.Alias) (*DispatchResult, error)
  ```

  Per-verb adapter pattern: one function per verb that takes `(args map[string]any) (request, error)` and returns the typed request, plus a closure that invokes the corresponding service. Register these in a `map[string]VerbAdapter` keyed on the verb name (`"node list"` etc.).

- [ ] **CLI: `tusk run`.** New Cobra command. `tusk run <alias>` resolves the alias, dispatches, renders the result. Supports the same `--format` flag as the underlying verb (sourced from Task 3). `tusk run --list` prints all defined aliases with name, description, and the resolved command + args.

- [ ] **MCP: `tusk_run`.** A new tool registered in `internal/mcp/tools.go`. Argument: `alias` (string, required). Returns the dispatched result wrapped with `{alias, command, kind}`.

  Data-structure proof of the response envelope:

  ```json
  {
    "alias":   "open-tickets",
    "command": "node list",
    "kind":    "node-list",
    "result": {
      "rows":  [<NodeRow>, ...],
      "count": 7
    }
  }
  ```

- [ ] **Doctor.** Add a "Aliases" pane to `tusk doctor` listing each invalid alias with its validation error. Valid aliases need not be reported.

- [ ] **Verification.** Define a sample alias in a fixture workspace's `tusk.toml`. Run `tusk run open-tickets` and confirm the output matches an equivalent direct CLI call. Run `tusk_run alias=open-tickets` via MCP and confirm the JSON envelope shape. Run `tusk doctor` on a workspace with one bad alias and confirm the surface.

- [ ] **Commit.** `feat(alias): manifest-declared aliases and tusk run / tusk_run dispatchers`.

**Pitfalls:**

- TOML quirks: `args.sort` as a string vs a list. If the verb's `--sort` flag accepts comma-separated strings on the CLI but the alias offers a list of strings, prefer the list form for clarity. Either both should work, or the adapter should normalize.
- Reflection-based dispatch is tempting and shorter, but per-verb adapters are easier to reason about when something fails. Go with adapters even if it's more code; this is the registry's seam.
- Doctor must not crash on bad aliases. The validator should accumulate errors, never panic.

---

## Task 5: `tusk_context` — the warm-context entry point

**Why this task exists:** Replaces the cold-start "3–5 exploratory calls per session" pattern with a single tool call. Composes pinned nodes, recent activity, and named aliases per the manifest.

**Files:**

- Create: `internal/manifest/context.go` — the `[context]` block parser, including the `recent`-as-string vs `[context.recent]`-as-block discriminator.
- Create: `internal/manifest/context_test.go`.
- Create: `internal/contextcompose/compose.go` — the digest composer.
- Create: `internal/contextcompose/compose_test.go`.
- Modify: `cmd/tusk/context.go` (new file): the `tusk context` CLI verb.
- Modify: `internal/mcp/tools.go`: register the `tusk_context` MCP tool.
- Tests: manifest-load tests for `[context]` + `[context.recent]`; composer tests; end-to-end CLI/MCP tests.

**Steps:**

- [ ] **Define the in-memory Context type.**

  ```go
  // internal/manifest/context.go
  type Context struct {
      Pinned  []string             // node IDs
      Recent  *Alias               // resolved alias (either ref by name or [context.recent] block)
      Include []string             // alias names (looked up at compose time)
  }
  ```

  And on-disk TOML — both forms accepted, mutual exclusion enforced:

  ```toml
  # Reference form:
  [context]
  pinned  = ["docs/agent-charter", "docs/style"]
  recent  = "recent-activity"
  include = ["open-tickets", "health"]

  [alias.recent-activity]
  command     = "node list"
  args.filter = "modified-since:7d type=note OR type=decision"
  args.sort   = "modified-desc"
  args.top    = 20
  ```

  ```toml
  # Inline form:
  [context]
  pinned  = ["docs/agent-charter", "docs/style"]
  include = ["open-tickets", "health"]

  [context.recent]
  command     = "node list"
  args.filter = "modified-since:7d type=note OR type=decision"
  args.sort   = "modified-desc"
  args.top    = 20
  ```

- [ ] **Discriminate the two `recent` forms at load.** If `recent` is a string in `[context]`, look it up in the alias registry and bind. If `[context.recent]` block is present, parse it as an inline alias (use the same parser as Task 4, with a synthetic name like `__context_recent__`). If both are present, this is a manifest error — surface in doctor; the engine still starts but `recent` is treated as unset.

- [ ] **Composer.** Given a runtime and a parsed `Context`, produce the digest:
  - `pinned` → batched `node get` for each ID with `include = [body, edges]` (use the service from Task 1 + the include expansion from Task 3).
  - `recent` → dispatch the resolved alias via Task 4's dispatcher.
  - `include` → for each alias name, dispatch via the dispatcher.

  Compose all into the response envelope.

  Data-structure proof:

  ```json
  {
    "pinned": [<NodeRow with body+edges>, ...],
    "recent": [<NodeRow>, ...],
    "aliases": {
      "open-tickets": { "kind": "node-list", "result": { "rows": [...], "count": 10 } },
      "health":       { "kind": "doctor",    "result": { "warnings": [...] } }
    }
  }
  ```

  The `pinned` and `recent` fields are omitted when empty.

- [ ] **CLI: `tusk context`.** New Cobra command. No required args. Honors `--format` (compact or JSON; default per Task 3 rules). Compact form for `tusk_context` is the JSON pretty-printed as nested sections (`# Pinned`, `# Recent`, `# Aliases / <name>`); this is the one place the compact form is hierarchical rather than tabular because the response is composite.

- [ ] **MCP: `tusk_context`.** New tool registered in `internal/mcp/tools.go`. No required arguments. Optional: `format` (string), `include` (array — overrides the per-node include default `[body, edges]`).

- [ ] **Default include for nodes.** Unless the caller overrides, every `<NodeRow>` in `pinned` and `recent` is returned with `include = [body, edges]`. The aliases under `aliases.*` are not modified — they carry whatever shape their underlying verb returned.

- [ ] **Doctor.** Extend the "Aliases" pane from Task 4 to also report invalid `[context]` configuration: an unknown alias name in `recent` or `include`, the both-forms-set error for `recent`, an unparseable inline alias.

- [ ] **Verification.** Define a fixture workspace with all three forms (`pinned`, `[context.recent]` inline, `include`). Run `tusk context` and confirm the output. Run `tusk_context` via MCP and confirm the JSON envelope. Switch the fixture to use `recent = "alias-name"` reference form and confirm equivalent output.

- [ ] **Commit.** `feat(context): tusk_context warm-context entry point composed from aliases`.

**Pitfalls:**

- Pinned nodes that don't exist: emit a warning via doctor; the `pinned` array in the response omits missing IDs rather than emitting a half-populated row. Add a test for this case.
- The default per-node `include = [body, edges]` can be heavy on large pinned sets. Don't add a token budget knob in v1 (out of scope per §3); just document the behavior.
- Concurrent dispatch: each `include` alias dispatches independently. Run them in parallel using a `errgroup.WithContext` from `golang.org/x/sync/errgroup`. Bound concurrency by the number of `include` entries; the workspace lock and SQLite's WAL handle the actual contention.

---

## Self-Review

Before handing off, the planning agent confirms:

1. **Spec coverage.** Every §4 subsection has a task: §4.1 → Task 3; §4.2 → Task 4 (with Task 1's registry as prerequisite); §4.3 → Task 5; §4.4 → Task 3's compact renderer; §4.5 → Task 2. ✓
2. **Placeholder scan.** No TBDs, no "implement appropriate X," no "similar to Task N" without re-stating shape. ✓
3. **Type consistency.** `NodeRow` is consistent across Tasks 1, 3, 5. `Alias` is defined in Task 4 and used in Task 5. `cliregistry.VerbSpec` is defined in Task 1 and used in Tasks 4 and 5. ✓
4. **TDD spirit preserved.** Each task lists tests before describing the implementation prose. Implementation code is omitted per the user's directive; the prose describes the implementation in enough detail that the implementer agent can write it.

---

## Changes Introduced

**New files:**
- `internal/cliregistry/registry.go` + `registry_test.go`
- `internal/node/list_service.go`, `internal/node/get_service.go` (and analogous service files for the other read verbs)
- `internal/manifest/alias.go` + `alias_test.go`
- `internal/manifest/context.go` + `context_test.go`
- `internal/aliasdispatch/dispatch.go` + `dispatch_test.go`
- `internal/contextcompose/compose.go` + `compose_test.go`
- `internal/render/compact.go` + tests
- `cmd/tusk/run.go`, `cmd/tusk/context.go`

**Modified interfaces:**
- Every read-verb Cobra `RunE` and MCP tool handler now calls a service function.
- `tusk_query`, `tusk_node_get`, `tusk_node_list`, `tusk_edge_list`, `tusk_doctor`, `tusk_status` MCP tools gain `include`, `fields`, `format` arguments.
- New CLI verbs: `tusk run`, `tusk context`.
- New MCP tools: `tusk_run`, `tusk_context`.

**New environment variables:** none.

**Schema migrations:** none.

**Added dependencies:** `golang.org/x/sync/errgroup` (if not already present — verify at start of Task 5).

**Bridge code introduced:** none. P1 is fully additive.

**Manifest additions:**
- `[alias.<name>]` blocks.
- `[context]` block with `pinned`, `recent`, `include`.
- `[context.recent]` block (inline alias form).

**Filter grammar additions:**
- `modified-since:<duration|date>` predicate.

**Doctor surfaces added:**
- Invalid alias declarations (unknown verb, write verb, unknown flag/positional, type mismatch).
- Invalid `[context]` configuration.
- Pinned node IDs that don't resolve.

---

## User-Visible Behaviors That Must Still Work

This is the implementer agent's acceptance checklist for P1:

- Every existing CLI verb produces byte-identical output to current `main` when called with the same arguments and no new flags.
- Every existing MCP tool returns the same response shape when called with no new arguments.
- `tusk node list type=ticket --include body,edges` returns each ticket with its body and 1-hop edges inline.
- `tusk_query` MCP tool, called with `include: ["body", "edges"]`, returns hits with body and edges fields populated.
- `tusk run <alias-name>` invokes a manifest-declared alias and renders identically to the equivalent direct CLI call.
- `tusk_run` MCP tool returns `{alias, command, kind, result}` envelope.
- `tusk context` returns the composed digest with pinned, recent, and aliases sections.
- `tusk doctor` reports invalid aliases and invalid `[context]` configuration.
- `modified-since:7d type=note` is a valid filter expression that returns notes modified in the last 7 days.

If any of these fails, P1 is not done.
