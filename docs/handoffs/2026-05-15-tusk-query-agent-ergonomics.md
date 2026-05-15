---
type: handoff
title: Tusk semantic query — agent ergonomics (MCP path)
session-date: "2026-05-15"
---

# Tusk semantic query — agent ergonomics (MCP path)

Make `tusk_query --semantic` return fewer, more informative results so MCP-client agents need fewer follow-up tool calls. Single PR, scoped to the MCP path (CLI may inherit changes for free where it shares code).

## Why

Running `./bin/tusk query 'type!=""' --semantic "what is tusk?"` against the dogfood vault returns **60+ rows down to score 0.41**, with snippets like `rorf("tusk.toml not created: %v"...)` (mid-token fragments). An agent calling this tool then has to:

1. Skim 60 rows of mostly-noise.
2. Call `tusk_node_get` on several candidates to read actual content.
3. Possibly re-query with refined wording.

We can collapse most of that into one tool call by capping results, dropping the noise tail, returning useful snippets, and surfacing the title.

The deeper ranking work (title embedding, hybrid structural-semantic blend, model upgrade) is a separate milestone — out of scope here.

## Scope (ship together in one PR)

### 1. Default `take` in the MCP tool when `semantic` is set

- File: `internal/mcp/tools.go:282` (the `WithNumber("take", ...)` for `tusk_query`).
- Behavior: when `semantic` is non-empty and the caller does **not** pass `take`, default to `take = 10`. When `semantic` is empty, leave the existing `take=0` (unlimited) behavior alone — structural-only listings still want full pagination control.
- CLI (`cmd/tusk/cmd_query.go`) is **not** changed for now. Interactive CLI users can still ask for everything. Only the MCP tool gets the default.

### 2. Score floor (`min_score`) on the MCP tool

- File: same — `internal/mcp/tools.go:277-432`.
- Add a new optional `min_score` number parameter, default **`0.5`**. Drop ranked rows below the threshold *before* applying `take`.
- Document on the MCP tool description that callers can lower it explicitly when their first shot misses.
- Apply only when `semantic` is set. Ignore otherwise.
- Edge case: if filtering by `min_score` empties the result, return `{"results": [], "count": 0, "model": "...", "filtered_below_min_score": <int>}` so the agent knows it was a threshold prune, not a vault miss. The extra field is cheap and lets agents auto-retry with a lower floor.

### 3. Query-relevant snippet windowing

- File: `internal/filter/semantic.go:88-146` (`RenderSnippet` — currently just leading-runes-of-body with whitespace collapse).
- Replace with a query-aware windowing function. Proposed signature:

  ```go
  func RenderSnippetForQuery(body, query string, maxRunes int) string
  ```

- Algorithm (start simple, refine later):
  1. Tokenize the query into lowercase words (drop stopwords: `the`, `a`, `is`, `what`, `how`, etc. — keep the list small; ~30 entries).
  2. Find the earliest position in `body` (lowercase) where any query token appears.
  3. Window `maxRunes` around that position (e.g., 40 runes before, 160 after), respecting word boundaries.
  4. If no query token matches, fall back to current leading-runes behavior so we never return empty.
  5. Preserve the existing whitespace-collapse + ellipsis behavior at edges. Prepend `…` when the window doesn't start at body[0].
- Keep the existing `RenderSnippet` exported function as a thin wrapper that calls `RenderSnippetForQuery(body, "", maxRunes)` to preserve the doctor / non-semantic callers (`internal/doctor/...` may use it; check before refactoring).
- Update the MCP handler at `internal/mcp/tools.go:420` to pass `semanticQuery` into the new function.
- Update the CLI handler at `cmd/tusk/cmd_query.go:251` to do the same (free win — same code path).

### 4. Surface `title` in MCP semantic results (already there — verify)

- File: `internal/mcp/tools.go:413-422`. Looks like `title` is already in the response map. Confirm it's actually populated (the `byID` map is built from the *structural* result rows, which include title from the SQL scan at line 349). **Probably nothing to change**, but add a regression test if not covered: an MCP `tusk_query` semantic response must include a non-empty `title` for nodes that have one in frontmatter.

## Tests to add

- `internal/mcp/tools_test.go`:
  - `TestQueryTool_SemanticDefaultTakeIs10` — call with `semantic` and no `take`; assert <=10 results when vault has >10 embeddings above the floor.
  - `TestQueryTool_SemanticHonorsExplicitTake` — call with `semantic` and `take=3`; assert 3 results.
  - `TestQueryTool_SemanticAppliesMinScoreDefault` — stub embedder + candidates with known scores spanning 0.3-0.9; assert only ≥0.5 returned.
  - `TestQueryTool_SemanticHonorsExplicitMinScore` — pass `min_score=0.2`; assert lower-scored rows included.
  - `TestQueryTool_SemanticIncludesTitle` — assert response rows include populated `title`.
- `internal/filter/semantic_test.go`:
  - `TestRenderSnippetForQuery_WindowsAroundMatch` — body contains query token mid-string; assert snippet contains the matching window with leading `…`.
  - `TestRenderSnippetForQuery_FallsBackWhenNoMatch` — body contains none of the query tokens; assert behavior matches the old `RenderSnippet` output for the same body.
  - `TestRenderSnippetForQuery_StopwordsIgnored` — query is `"what is the tusk"`; only `tusk` should drive the window.

CLI tests likely won't need changes if you keep the CLI-side default `take` untouched. If you do touch CLI snippet rendering, add one e2e check in `cmd/tusk/cmd_query_semantic_test.go`.

## Out of scope (next milestone — write a follow-up handoff before starting)

- **Title-aware embedding** (concat title + body in the embed payload, or store a second small embedding for the title). Requires reindex; that's a one-line `make sure reindex re-embeds everything` migration but it's a real behavior change. Logged as the highest-value next step.
- **Hybrid structural rerank** (boost specs over plans for "what is X" questions, penalize implementation guides). Needs a query-intent signal we don't have yet.
- **Bigger embed model** (`mxbai-embed-large`, `bge-large-en-v1.5`). Wait until #5 above isn't enough.
- **Cross-encoder reranker.** Overkill.
- **CLI `--take` default.** Don't change interactive defaults. Only MCP needs the protection.

## Exit criteria

- All new tests pass; `make test-race` clean.
- `make vet` and `make lint` clean.
- Manual smoke: from the host, run
  ```
  ./bin/tusk mcp stdio < some-canned-tusk_query-request.json
  ```
  (or run the MCP server and exercise from an MCP client) and confirm:
  - `tusk_query` with `semantic` and no `take` returns ≤10 rows.
  - `tusk_query` with `semantic` and no `min_score` returns no rows below 0.5.
  - Snippets contain words from the query string when the body matches.
  - `title` is populated in every row that has one in frontmatter.
- The dogfood query `what is tusk?` returns ≤10 rows; the top row's snippet contains the word `tusk` (and ideally the actual one-liner from `docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md`).
- No reindex required (this PR doesn't touch embed payloads).

## Branch and commit shape

- Branch: `feat/tusk-query-agent-ergonomics` (per `feedback_branch_naming.md`: kebab-case, no dots, lowercase).
- Suggested commits:
  1. `feat(filter): query-aware snippet windowing`
  2. `feat(mcp): default take=10 and min_score=0.5 for tusk_query semantic`
  3. `test(mcp,filter): cover semantic query ergonomics defaults`
- Single PR. Don't split — these changes are co-validated by the same tests.

## Pointers (read first if you didn't write this)

- Current MCP query implementation: `internal/mcp/tools.go:277-432`
- Current CLI query implementation: `cmd/tusk/cmd_query.go:141-272`
- Ranking + snippet code: `internal/filter/semantic.go`
- Embedder interface: `internal/embed/embedder.go`
- Prior semantic-snippet design that landed best-chunk selection (this PR builds on it): `docs/superpowers/specs/2026-05-14-snippet-and-doctor-design.md` and commit `f53b29a`.
- Dogfood evidence that motivated this work: see the conversation that produced this handoff, specifically the 60-row output of `query 'type!=""' --semantic "what is tusk?"` against `.tusk/index.db` on 2026-05-15.

## What this is not

- Not a change to the embedding pipeline.
- Not a change to chunking.
- Not a change to the structural filter grammar.
- Not a change to the CLI `query` defaults — interactive users still get the full firehose if they ask for it. Only the agent-facing surface tightens up.
