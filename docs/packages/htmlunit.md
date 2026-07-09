---
type: package
title: internal/htmlunit — HTML sub-unit parsing & plain-text normalization
import-path: github.com/germanamz/tusk/internal/htmlunit
status: stable
---

# internal/htmlunit

Parses standalone HTML content into the same flat `[]subunit.Unit`
representation the markdown sub-unit parser emits, and renders HTML to
deterministic plain prose. A pure transformation layer over
`golang.org/x/net/html`: it never touches the index, the embed queue,
or any repository. The reserved `Kind` names and the structural address
grammar are shared with `internal/subunit`; the namespace is
distinguished downstream by the `source = "html"` column, not by this
package.

## Public surface

- `Parse(source []byte) ([]subunit.Unit, error)` — walks the DOM and
  produces a deterministic, document-order `[]subunit.Unit`. Sectioning
  is driven ONLY by `<h1>`–`<h6>`; wrappers (`section`/`article`/`div`)
  contribute no structure. `x/net/html` never errors on malformed
  input. Reuses `subunit.Unit`, `subunit.Kind`, and
  `subunit.ComputeHash` so HTML and markdown sub-units are
  interchangeable downstream.
- `NormalizeText(source []byte) string` — HTML → clean prose: tags
  stripped, block elements separated by a blank line, inline elements
  joined, entities decoded, intra-block whitespace collapsed,
  `head`/`script`/`style`/comments excluded. Deterministic.

## Structural address grammar

Mirrors `internal/subunit` verbatim. A sub-unit id suffix is the
structural address:

- **Section path** follows the heading outline by nesting depth, not
  the raw tag number: `<h1>` then a skipped `<h3>` yields `S1` then
  `S1.1` (no empty intermediate section). A heading of outline depth
  *N* closes all open sections of depth ≥ *N*.
- **Leaves** reset per enclosing section (or document root), per kind,
  1-based in document order: `<p>` → `P`, `<pre>`/block `<code>` → `B`,
  `<blockquote>` → `Q`, `<li>` → `L`, table cell `T<k>R<row>C<col>`
  (header row is `R0`). Body cells embed as `"<column-header>: <cell>"`.
- A heading inside `<pre>`/`<code>`/`<table>` is block content, not a
  section boundary. `<head>`/`<script>`/`<style>` produce no sub-units.

## Notes

- Content-addressing matches `subunit` for leaves: leaf `ContentHash` is
  `sha256(EmbedPayload)`. Byte-identical payloads reuse vectors; positional
  shifts keep the hash. Section `ContentHash` is
  `sha256("section\x00<level>\x00<heading-text>")` here — heading-only,
  because the HTML walker sets a section's `Text` to the heading element's
  text (body blocks are DOM siblings). The markdown walker hashes the full
  subtree instead; sections are never embedded, so the divergence carries no
  vector cost, and HTML section edges derive from the same heading-only text,
  keeping hash and edge set in lockstep on this path too.
- Dead code as of Phase 1 — wired into the reindex pipeline in later
  phases. No pipeline, no index, no flag dependency here.
