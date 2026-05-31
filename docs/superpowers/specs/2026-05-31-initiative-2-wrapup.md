# Initiative 2 — Codebase Simplification: Wrap-Up

**Date:** 2026-05-31 · **Status:** ✅ COMPLETE (PRs #492–#505 all merged to `main`)

This is the closing record for **Initiative 2 — the per-package, behavior-preserving
refactor** of the Tusk v1 codebase. It followed Initiative 1 (the golden E2E suite,
PRs #484–491), which pinned the CLI + MCP surfaces byte-for-byte and was the
behavioral gate for everything below.

---

## TL;DR

Thirteen `internal/*` + `cmd/tusk` packages were each refactored in a single PR,
behavior-preserving and validated against the Initiative-1 golden suite. The work
removed genuine duplication while changing **zero** observable behavior — every
byte of CLI stdout/stderr, every MCP JSON payload, every error string, every wire
hash, and every golden-pinned count was preserved.

- **−869 lines of non-test source** (1242 inserted, 2111 deleted across 44 files).
- **13 PRs**, one per package, all squash-merged (`refactor:` → non-release-bumping).
- **1 new shared package** introduced (`internal/argval`).
- **0 behavior changes** — the full golden suite (incl. the binary-spawning wire
  tier) stayed green on every PR.

---

## By the numbers

| # | Package | PR | Net LOC | Headline dedup |
|---|---------|----|--------:|----------------|
| 1 | `internal/behavior` | #492 | −54 | collapse 8 dispatch chains onto a generic entry |
| 2 | `internal/render` | #493 | −15 | `maxWidth[T]` helper for the compact column passes |
| 3 | `internal/index` | #494 | +7 | edge column list + IN-clause placeholders |
| 4 | `internal/manifest` | #495 | +8 | reuse `isRefProperty`; graph-expansion decode helper |
| 5 | `internal/filter` | #496 | −24 | binary-join SQL + recursive CTE + error appends |
| 6 | `internal/node` | #498 | −73 | type-mismatch msg, edge write-back, Create/Modify persist |
| 7 | `internal/query` | #499 | −88 | `expandAndBlend`+`blendedTrace`, `applyMinScore`, `window[E]`, `compileAndQuery`, `unmarshalProperties` |
| 8 | `internal/subunit` | #500 | +7 | `stringProperty` twin; merge identical code-block cases |
| 9 | `internal/reindex`+`internal/embed` | #501 | −18 | orphan-reap `release` closure; embed `retryOrDrop` |
| 10 | `internal/doctor` | #502 | −15 | `edgeRowLess`, `isLegacyEdge`, `newDanglingEdgeIssue`, unified `formatRef` |
| 11 | `internal/aliasdispatch` | #503 | +160¹ | new `internal/argval` pkg + error-accumulating `argReader` |
| 12 | `internal/mcp` | #504 | −36 | `classifyNodeWriteError`, `aliasErrorsPayload`, `requireStrings`, `listRowToCompact` |
| 13 | `cmd/tusk` | #505 | −539 | `openStore`/`resolveWorkspace` preamble (×17/×14) + `newNodeService`/`newAliasDeps`/`buildEmbedder`/`listRowToCompactBasic` |

¹ #503 is net-positive because it **adds** a new shared package (`internal/argval`,
~123 LOC) plus two regression tests; `dispatch.go` itself shrank 638→486.

The two biggest wins bookend the dependency graph: `internal/query` (the scoring
core) and `cmd/tusk` (which sits atop every package — the ~30-LOC workspace-open
preamble was repeated across 17 commands).

---

## Method: survey workflow + adversarial byte-identity critic

Every package followed the same rhythm:

1. **Sync + branch** (`refactor/<kebab>`), confirm a green baseline.
2. **Survey workflow** — a read-only multi-agent fan-out, one agent per file-group
   or candidate, each returning structured `{opportunities, doNotTouch}` grounded
   in the real source (the agent-authored plan was repeatedly *wrong* about
   specifics; grounding caught that).
3. **Adversarial critic** — a single agent that re-reads the source at each cited
   location, **traces concrete inputs through both the original and the proposed
   helper**, and refutes anything not provably byte-behavior-identical. It returns
   an ordered, lowest-risk-first vetted set.
4. **Implement only what the critic vetted**, verify incrementally, run the full
   gate (`go test` pkg + golden + `vet` + `gofmt` + `golangci-lint` + `go test
   ./...`), commit, push, one PR.

This was the load-bearing decision. **The critic caught a real, ship-blocking
trap on nearly every package** (see below). Trusting its refusals — rather than
the optimistic plan — is what kept all 13 PRs behavior-preserving.

---

## What the critic caught (the traps)

The negative findings were as valuable as the dedup. A sampling:

- **query (#499)** — the plan's proposed generic `applyMinScore[E interface{
  GetScore() float64 }]` constrains on a method that **exists nowhere in the
  repo**; it would not compile. Shipped the monomorphic form instead.
- **subunit (#500)** — `ComputeHash` is a **stable wire format**; a single byte
  shift in its sha256 input rebuilds every workspace index. Only 2 of the surveyed
  changes were provably byte-identical; the rest were left alone. The
  `ApplyFile→diffUnits` decomposition was rejected (no dedup, highest-stakes path).
- **filter (#496)** earlier in the series — unifying `compileProperty` /
  `compilePropertyOnAlias` was refused because one uses a **checked** type
  assertion (error) and the other **unchecked** (panic).
- **doctor (#502)** — the unified `formatRef` had to preserve a non-obvious
  arg-order trap (`Value, ActualType, To` — ActualType *before* To) and the
  `" → "` (U+2192) cycle separator vs. the ASCII `" -> "` of the dangling-edge
  message. The generic `appendIssues[T]` over the emission loops was rejected:
  the Issues are heterogeneous (different element types; some set no NodeID).
- **mcp (#504)** — the plan's §4.2 wish to have `internal/mcp` **consume**
  `internal/argval` proved **infeasible**: mcp's `CallToolRequest` coercers have
  different optional/required + silent-skip semantics and golden-pinned error text
  (`"missing or non-string argument %q"`, etc.) that differ from argval on every
  edge case. Delegating would have broken the golden suite.
- **cmd/tusk (#505)** — the `listRowToCompactBasic(query.ListRow)` converter
  could not be applied to the visually-identical `query.Row`/`query.ScoredRow`
  sites: different input types, and those sites deliberately *omit* `MatchedUnits`
  / explain-trace fields that the alias paths must not emit.

---

## What was deliberately NOT done

A consistent boundary emerged and held across all 13 packages:

- **Pure decomposition was rejected.** Splitting a long single-occurrence function
  into sub-functions that removes no duplication ("churn without dedup on a
  high-stakes path") was refused every time it was proposed: `DrainQueue` and
  `processReindexJob` (reindex/embed), `ApplyFile→diffUnits` (subunit),
  `validateEdgeAddition` and `renderDoctorReport` (mcp/cmd).
- **Generics/helpers that would shift a golden byte were rejected** — even when
  the plan listed them (`appendErrorIssues[T]`, `applyCommonListArgs`, the
  cross-package `retryOrDrop`, argval-in-mcp).
- **Genuine dedup = repeated literals / blocks / comparators / converters, or
  high-volume repetition** (the per-field arg pattern in aliasdispatch; the ~30-LOC
  preamble × 17 in cmd/tusk). That is where the real LOC came out.

---

## New shared surface

- **`internal/argval`** (new in #503): the five argument coercers
  (`String`/`Int`/`Float`/`Bool`/`StringSlice` over `map[string]any`), moved
  verbatim from `aliasdispatch` with byte-identical error text. Intended as a
  cross-cutting helper for both `aliasdispatch` and `mcp` — but mcp's coercers
  turned out semantically incompatible (above), so today it is consumed by
  `aliasdispatch` only. It remains a clean home for the contract and a candidate
  for future reuse if a new caller matches its map-based, error-on-wrong-type shape.

---

## Validation strategy

Each PR passed the same gate (sandbox disabled — `httptest` loopback + Go module
stat-cache writes fail otherwise):

- `go test ./internal/<pkg>/...`
- `go test ./cmd/tusk/ ./internal/mcp/ -run TestGolden` — the byte-for-byte surface
- `go vet`, `gofmt -l` (empty), `golangci-lint run` (0 issues)
- `go test ./...` (no FAIL/panic)

For `cmd/tusk` specifically, the **wire tier** (`golden_wire_test.go`, gated behind
`!testing.Short()`) spawns the *built binary* and asserts real exit codes — so the
full `go test ./cmd/tusk/` (not `-short`) was the definitive check that the
17-site preamble extraction preserved every CLI byte and exit code.

---

## Lessons for future initiatives

- **Survey before you cut.** The agent-authored plan was directionally right but
  wrong on specifics (names, types, line ranges, feasibility) on almost every
  package. A read-only survey grounded in the real source is cheap insurance.
- **An adversarial critic that must *refute* is worth more than one that
  confirms.** Framing the verification agent to trace inputs and try to break each
  candidate caught compile errors, panics, wire-format shifts, and infeasible
  cross-cutting plans before any code changed.
- **A thin vetted set is the correct outcome for wire-format-bearing packages**
  (`subunit`, `reindex`/`embed`, `doctor`, `mcp`). Don't pad it.
- **For tedious byte-identical multi-file substitutions** (the cmd/tusk preamble),
  an exact-string `.replace()` script keyed off a *known-good* block, followed by
  `goimports -w` for the orphaned imports, was far more reliable than dozens of
  hand edits — with the golden+wire suite as the safety net.
- **The golden suite (Initiative 1) made all of this possible.** Without a
  byte-for-byte behavioral gate, a "behavior-preserving refactor" is an assertion;
  with it, it's a test result.

---

## Status

Initiative 2 is **complete**. `main` is green (`go test ./...` exit 0). All merges
are `refactor:` (non-release-bumping), so there is no version bump or release
artifact — consistent with the milestone-completion convention, no `docs/status/`
or `docs/releases/` doc is warranted. This document is the record.
