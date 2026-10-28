---
type: package
title: internal/lexical — word-match scores
import-path: github.com/germanamz/tusk/internal/lexical
status: stable
---

# internal/lexical

Scores how well a set of texts match a query's words, so semantic search can blend a word match into the cosine rank when `[query] lexical-weight` is set. Embeddings blur exact wording: a clause buried in a long passage barely moves the passage's vector, and identifier-heavy passages score moderately against almost any query. A word-overlap score catches what the vectors miss.

## Public surface

- `Scores(query, docs) []float64` — Okapi BM25 (k1 1.2, b 0.75) of each doc against the query, divided by the best doc's score, so the best match scores 1 and a doc sharing no word scores 0. Term rarity and average length come from `docs` alone, so it needs no index. A word repeated in the query counts once. Returns nil when no doc shares a word with the query.
- `Tokens(text) []string` — the words `Scores` compares: lowercase runs of Unicode letters and digits, minus a short list of English stopwords, each with one common suffix stripped (`ingly`, `edly`, `ing`, `ers`, `er`, `ed`, `ly`, `es`, `s`) and then a final `e`, each step keeping at least three bytes. The suffix list is deliberately cruder than a real stemmer; it only has to make "dropped" and "dropping" meet, and the final `e` makes "node" meet "nodes" and "consume" meet "consumes" (#849).
- `QueryTerms(query) Terms` — the query's distinct words, stemmed as `Tokens` stems them.
- `(Terms).Coverage(text) float64` — the share of `text`'s distinct words that are among the terms: 1 when the query names every word of `text`, 0 when it names none. `internal/query` uses it for the title match (`[query] title-weight`, #849), where `text` is a file's title.

## Notes

`internal/query` calls `Scores` once per semantic query over the candidate chunks it already loaded (sub-unit leaves, or file chunks when sub-units are off) and hands the result to `filter.SemanticRank`, which raises each cosine by `weight × lexical × (1 − cosine)`. At the default weight of 0 it isn't called.

The cost is a scan of every candidate's text per query. A word is stemmed and looked up only when it starts with a query term, since a stem is always a prefix of its word, and only a word with an uppercase letter is copied. On 10,000 leaves of about 600 bytes a query takes about 35 ms on an M1 Max.

How the weight was chosen, and what it changed on tusk's own docs, is in `docs/guide/benchmarks.md#lexical-fusion` (#812).
