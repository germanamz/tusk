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

# != and NOT also match nodes without the property; * means "has a value"
tusk query 'type=ticket status!=done'           # includes tickets with no status
tusk query 'type=service owner!=*'              # services with no owner

# Lists: ~= for one item, [a, b] for all of them in any order
tusk query 'type=ticket labels~=bug'
tusk query 'type=ticket labels=[bug, urgent]'

# Relative dates on date and datetime properties: h, d, w, m (months), y
tusk query 'type=service reviewed<today-90d'
tusk query 'type=ticket due<=today+2w'

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

Each result carries `matched_units`, the passages that matched, best first. A passage folds into its innermost section, so one finding is one row: the section's id, `heading`, and `lines: [start, end]`, plus the passage's score and snippet. Lines are 1-based and counted from the top of the file, frontmatter included, so an agent can open the file at the passage instead of reading all of it (HTML units carry no lines). `--max-units N` keeps the best N per file, MCP keeps 2 unless told otherwise, and `units_total` says how many there were. MCP also returns 5 files unless `take` says otherwise; the CLI returns every ranked file.

The JSON stays small because an agent pays for every byte of a result again on each later turn. A unit carries only `id`, `heading`, `lines`, `score` and `snippet`, and a result has no snippet of its own unless it has no units. Scores are rounded to three decimals.

The top result's best unit carries more: its passage's markdown as `text`, in place of the snippet, with `text_lines` saying which lines of the file the text covers. The text is the section's own content, read from the file: its paragraphs, list items, tables and code, without the heading line or any subsections. A section over 2 KB comes as a window of whole paragraphs and items around the one that matched, so `text_lines` is where to read on. An agent can usually answer from the text and skip reading the file. `--include body` gives every result's best unit its text. A file edited since tusk last indexed it gets no text, because its lines may have moved. The structural `--include units` outline is the one place units still carry `type`, `parent_id`, `heading_level` and `ordinal`. `[workspace] line-numbering` decides what ends a line: `lf` (the default, matching sed and grep -n), `universal` (adds a lone CR, like most editors), or `unicode` (adds the Unicode line separators).

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

Each prefix ends with a space. `{title}` in `document-prefix` becomes the note's title (`none` for sub-units). The other keys are `document-header` (`full`, `title`, or `none`: how much frontmatter leads each file chunk), `chunk-target-bytes` / `chunk-max-bytes` / `chunk-overlap-bytes` (defaults 1600 / 4000 / 200, sized for a 2K window; for a 512-token model set `chunk-max-bytes = 1000` and a smaller target such as `chunk-target-bytes = 800`, and consider `document-header = "title"` since the header doesn't count toward the cap), and `num-ctx` (passed to Ollama as `num_ctx`). `num-ctx` can't take a model past the context it was trained on: `nomic-embed-text` stops at 2048 tokens whatever you set.

The default cap assumes about 2 bytes per token, which holds for prose and most code. Dense text runs denser (JSON at about 1.8 bytes per token, hex at 1.5, base64 at 1.4), so a chunk of it near the cap can still be too long for the model. When that happens tusk embeds the part that fits and logs a warning. A sub-unit longer than `chunk-max-bytes`, such as a big code block or table, is split the way a file is, and its best chunk ranks it. An input the model refuses for some other reason is retried twice and then set aside: `tusk doctor` lists it as `embed-failed` with the error, and it is tried again when the note changes, on `tusk reindex --force`, or after an `[embeddings]` change.

How much the prefixes matter was measured before they shipped ([#758](https://github.com/germanamz/tusk/issues/758)): a real vault of 79 markdown files, 44 hand-written queries with known target notes, note-level ranks scored from `tusk query --semantic --json`.

| Config | MRR@10 | hit@1 | hit@5 |
| --- | --- | --- | --- |
| `nomic-embed-text`, no prefixes | 0.533 | 0.34 | 0.77 |
| `nomic-embed-text`, prefixes | **0.650** | **0.50** | **0.84** |
| `embeddinggemma-2`, no prefixes | 0.576 | 0.39 | 0.84 |
| `embeddinggemma-2`, prefixes | 0.532 | 0.34 | 0.80 |

The prefixes raised `nomic-embed-text` by +0.117 MRR@10 (paired bootstrap 95% CI +0.019 to +0.216), the only difference in the table that clears the noise. For `embeddinggemma-2` the recommended document prefix didn't help on that corpus.

A second measurement on tusk's own package docs ([#811](https://github.com/germanamz/tusk/issues/811)) found a smaller gain for `nomic-embed-text`. The section that answers a question ranked a little higher with the prefixes (MRR@10 0.67 against 0.62, within the noise), and an agent answering 40 questions spent about the same, 3% more tokens with them than without ([Benchmarks](benchmarks.md#embedding-prefixes) has the numbers). That's why the prefixes are settings and not defaults. They also raise every score: on that corpus 99% of query-to-file scores reached 0.5 with the prefixes and 80% without, so a `min_score` you picked before turning them on lets more through after.

Which settings work best depends on the model and on your notes, so try a few semantic queries before and after a change. Changing `query-prefix` takes effect on the next query. Changing any other key re-embeds every note. Apply the change with `tusk reload` (or `tusk_reload` from an agent) so a running MCP server picks up the new settings too; `tusk reindex` also applies it, but a server that hasn't reloaded stops embedding until it does, rather than embed with the old settings. `tusk doctor` suggests the prefixes when it recognizes one of the models above with them unset, and names the missing one when only one of a two-sided pair is set. If you've tried a prefix and decided against it, set it explicitly to `""` (for example `document-prefix = ""`) and doctor stops suggesting it.

### Matching the query's words

Embeddings match meaning and blur exact wording. A clause buried in a long passage barely moves that passage's vector, and passages dense with identifiers score moderately against almost any query, so the passage that uses your exact words can rank below ones that only sound related. `[query] lexical-weight` blends a word-match score into the ranking:

```toml
[query]
lexical-weight = 0.5
```

Each passage gets a BM25 score for the query's words, scaled so the best match among the candidates scores 1, and its similarity rises by that share of the gap to 1: `score = cosine + weight × lexical × (1 − cosine)`. A score never drops and never passes 1, so whatever cleared `min_score` before still clears it, and a passage just below it can clear it on its words. `--explain` (`explain` on `tusk_query`) shows `cosine_score` and `lexical_score` for each row and passage. The weight is read when a query runs, so changing it re-embeds nothing; run `tusk reload` (or `tusk_reload`) so a running MCP server picks it up.

It's off by default. On tusk's own package docs, replaying the first query an agent sent for each of 40 questions, a weight of 0.5 put the answering section first for 66% of questions instead of 55%, and among the passages an agent sees (five files of three passages each) for 93% instead of 86%. Using each question's full text as the query instead made no measurable difference. Short queries that name identifiers, flags or phrases gain the most. [Benchmarks](benchmarks.md#lexical-fusion) has the numbers and what it did for an agent.

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
