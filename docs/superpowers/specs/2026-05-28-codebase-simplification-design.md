# Codebase Simplification — Framing

**Status:** Draft **Date:** 2026-05-28 **Author:** German Meza (with Claude
Code)

## Goal

Tusk works well and ships a complete CLI and MCP surface. The code behind that
surface, however, has accumulated repetition and a number of functions with high
cyclomatic complexity. This work stream improves the **internal quality** of the
codebase — readability, maintainability, and consistency — without changing
observable behavior.

The single guarantee for this whole effort: **the tool's behavior does not
change.** Files remain the source of truth, the CLI and MCP wire formats stay
stable, and the index schema is untouched. Every refactor is validated against a
behavioral baseline rather than against intuition.

The work splits into two initiatives that run in sequence: the first establishes
the safety net, the second does the actual simplification.

## Initiative 1 — Robust E2E testing (the golden status)

Before touching internals, capture the tool's current, correct behavior in a
robust end-to-end test suite covering **both the CLI and the MCP server**. This
suite becomes the golden status: the authoritative definition of "tusk behaves
correctly." Any refactor in Initiative 2 is correct if and only if these tests
stay green.

Scope:

- Exercise real user-facing flows end to end (init, indexing, query, node/edge
lifecycle, MCP tool calls), not isolated units.
- Cover both entrypoints — the `tusk` CLI and the MCP server — so each surface
is independently pinned.
- Be deterministic and fast enough to run on every refactor commit, given the
pre-commit hook already runs the full Go test suite.

## Initiative 2 — Package-by-package refactor

With the golden status in place, simplify the codebase one `internal/*` package
at a time:

- Simplify functions and reduce cyclomatic complexity.
- Consolidate repeated logic into reusable functions.
- Flatten and clarify conditional logic (simpler `if` statements, early returns,
fewer nested branches).
- Improve readability and naming in line with `STYLE.md`.

Each package is refactored, validated against the Initiative 1 suite, and landed
before moving to the next — keeping changes reviewable and the tree green
throughout.

## Sequencing

Initiative 1 is a hard prerequisite for Initiative 2: there is no safe way to
refactor for behavior preservation without a behavioral baseline to refactor
against.
