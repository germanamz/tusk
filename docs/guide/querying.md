# Querying

Three query shapes run over the same index: a structural filter, semantic similarity, and hybrid, which filters and then ranks. The full filter grammar is in [`docs/cli/tusk_query.md`](../cli/tusk_query.md), and agents can call `tusk_help(topic: "filter")`.

## Structural filter

A compact filter grammar that compiles to parameterized SQL. Property
comparisons accept `=` or `:` interchangeably; the rest of the grammar uses
the operators below.

```bash
# Property predicates (`=` and `:` are equivalent)
tusk query 'type=ticket status=active priority=high'
tusk query 'type:plan shipped-at>=2026-04-01'   # date ordering, chronological

# Path patterns on path and id: * stays in one folder, ** crosses folders
tusk query 'type=note path=docs/product/*'      # notes directly in docs/product/
tusk query 'path=docs/** path!=docs/archive/**' # all of docs/ except the archive

# Edge traversal: -> outgoing, <- incoming
tusk query 'type=ticket blocks->type=ticket'        # tickets that block other tickets
tusk query 'type=note references<- type=spec'      # notes referenced by specs

# The one term after the arrow constrains the linked node; group for more
tusk query 'type=note references->(domain=technical OR domain=wip)'
tusk query 'domain=product references-> NOT domain=product'   # product notes linking outside product

# Multi-hop
tusk query 'type=ticket parent->parent->title="auth-epic"'

# Sort + pagination
tusk query 'type=ticket status=active' --sort '+priority,-due' --take 10
```

## Semantic search

Requires `[embeddings]` configured (Ollama by default). Embedding runs asynchronously after writes; until a node is embedded, it's invisible to semantic queries (and surfaces in `tusk doctor`).

```bash
tusk query '' --semantic "auth bug in password reset flow" --take 5
tusk query 'path=docs/**' --semantic "billing totals"   # scoped to docs/ and below
```

Each result carries `matched_units`, the passages that matched, best first. A passage folds into its innermost section, so one finding is one row: the section's id, `heading`, and `lines: [start, end]`, plus the passage's score and snippet. Lines are 1-based and counted from the top of the file, frontmatter included, so an agent can open the file at the passage instead of reading all of it (HTML units carry no lines). `--max-units N` keeps the best N per file, MCP keeps 3 unless told otherwise, and `units_total` says how many there were. `[workspace] line-numbering` decides what ends a line: `lf` (the default, matching sed and grep -n), `universal` (adds a lone CR, like most editors), or `unicode` (adds the Unicode line separators).

### Matching the embedding model

Many embedding models are trained with an instruction prefix on each side, one for queries and another for documents. Ollama adds neither, so tusk lets you set them under `[embeddings]`, along with the chunk sizes and context window that suit the model:

```toml
[embeddings]
provider        = "ollama"
endpoint        = "http://localhost:11434"
model           = "nomic-embed-text"
dim             = 768
query-prefix    = "search_query: "
document-prefix = "search_document: "
```

| Model | `query-prefix` | `document-prefix` | Context window |
| --- | --- | --- | --- |
| `nomic-embed-text` | `search_query: ` | `search_document: ` | 2K in Ollama |
| `embeddinggemma` | `task: search result \| query: ` | `title: {title} \| text: ` | 2K |
| `mxbai-embed-large` | `Represent this sentence for searching relevant passages: ` | none | 512 |
| `snowflake-arctic-embed` (v1) | `Represent this sentence for searching relevant passages: ` | none | 512 |
| `snowflake-arctic-embed2` | `query: ` | none | 8K |

Each prefix ends with a space. `{title}` in `document-prefix` becomes the note's title (`none` for sub-units). The other keys are `document-header` (`full`, `title`, or `none`: how much frontmatter leads each file chunk), `chunk-target-bytes` / `chunk-max-bytes` / `chunk-overlap-bytes` (defaults 1600 / 4000 / 200, sized for a 2K window; for a 512-token model set `chunk-max-bytes = 1000` and a smaller target such as `chunk-target-bytes = 800`, and consider `document-header = "title"` since the header doesn't count toward the cap), and `num-ctx` (passed to Ollama as `num_ctx`).

How much the prefixes matter was measured before they shipped ([#758](https://github.com/germanamz/tusk/issues/758)): a real vault of 79 markdown files, 44 hand-written queries with known target notes, note-level ranks scored from `tusk query --semantic --json`.

| Config | MRR@10 | hit@1 | hit@5 |
| --- | --- | --- | --- |
| `nomic-embed-text`, no prefixes | 0.533 | 0.34 | 0.77 |
| `nomic-embed-text`, prefixes | **0.650** | **0.50** | **0.84** |
| `embeddinggemma-2`, no prefixes | 0.576 | 0.39 | 0.84 |
| `embeddinggemma-2`, prefixes | 0.532 | 0.34 | 0.80 |

The prefixes raised `nomic-embed-text` by +0.117 MRR@10 (paired bootstrap 95% CI +0.019 to +0.216), the only difference in the table that clears the noise. For `embeddinggemma-2` the recommended document prefix didn't help on that corpus. One vault isn't a general result, which is why the prefixes are settings and not a hard-coded table.

Which settings work best depends on the model and on your notes, so try a few semantic queries before and after a change. Changing `query-prefix` takes effect on the next query. Changing any other key re-embeds every note. Apply the change with `tusk reload` (or `tusk_reload` from an agent) so a running MCP server picks up the new settings too; `tusk reindex` also applies it, but a server that hasn't reloaded stops embedding until it does, rather than embed with the old settings. `tusk doctor` suggests the prefixes when it recognizes one of the models above with them unset, and names the missing one when only one of a two-sided pair is set. If you've tried a prefix and decided against it, set it explicitly to `""` (for example `document-prefix = ""`) and doctor stops suggesting it.

## Hybrid (recommended for agents)

Structural filter narrows the candidate set; semantic similarity ranks within it.

```bash
tusk query 'type=ticket status=active' --semantic "login flow" --take 10
```

## JSON output

Add `--json` to any query for structured output that's easy to pipe into an LLM or a script.

```bash
tusk query 'type=decision' --semantic "storage backend" --take 3 --json
```
