# Manifest (tusk.toml)

The manifest at `./tusk.toml` is the schema source of truth. There is
no MCP tool to edit it — open the file directly, then call
`tusk_reindex` to pick up the changes.

## Top-level sections

```toml
[workspace]
name      = "my-brain"
sub-units = true             # parse markdown headings into sub-unit nodes

[node-types.<name>]
properties = [
    { name = "priority", type = "int" },
    { name = "tags",     type = "list-of", item-type = "ref", to = "tag" },
]
# See tusk_help(topic: "node-types") for property types.

[edge-types.<name>]
from        = ["ticket"]
to          = ["ticket"]
cardinality = "many-to-many" # one-to-one | one-to-many | many-to-one | many-to-many
ordered     = "order"        # optional; sort siblings by this property
hierarchy   = "wbs"          # optional; enables tree=/parent=/root= shortcuts
wikilinks   = true           # optional; [[wikilinks]] in body produce this edge
# See tusk_help(topic: "edge-types").

[embeddings]                  # required for semantic queries
provider = "ollama"
model    = "nomic-embed-text"
endpoint = "http://localhost:11434"
dim      = 768
query-prefix    = "search_query: "     # optional; see "Embedding model settings"
document-prefix = "search_document: "  # optional; may contain {title}

[query.graph-expansion]       # default knobs for tusk_query
enabled    = true
hops       = 2                # 1 or 2
weight     = 0.5              # per-hop blend weight, [0, 1]
edge-types = ["mentions", "tags"]

[context]                     # composed by tusk_context
pinned  = ["notes/north-star"]
aliases = ["recent-tickets"]

[alias.<name>]                # invoked via tusk_run(alias)
command = "query"             # query | node list | node get | edge list | doctor | status
args    = ["type=ticket AND modified-since:7d"]
```

## Embedding model settings

Optional `[embeddings]` keys that match tusk to the embedding model. All
default to tusk's original behavior.

- `query-prefix` / `document-prefix`: instruction prefixes the model
  was trained with. `{title}` in `document-prefix` becomes the node's
  title (`none` for sub-units).
- `document-header`: `full` (default), `title`, or `none`; how much
  frontmatter leads each file chunk.
- `chunk-target-bytes` / `chunk-max-bytes` / `chunk-overlap-bytes`:
  file chunking, default 1600 / 4000 / 200 (sized for a 2K window).
- `num-ctx`: Ollama context window (`num_ctx`).

Known prefixes (each ends with a space) and context windows:

- nomic-embed-text: query-prefix "search_query: ", document-prefix
  "search_document: "; 2K window in Ollama.
- embeddinggemma: query-prefix "task: search result | query: ",
  document-prefix "title: {title} | text: "; 2K window.
- mxbai-embed-large and snowflake-arctic-embed (v1): query-prefix
  "Represent this sentence for searching relevant passages: ", no
  document prefix; 512-token window (set chunk-max-bytes = 1000,
  chunk-target-bytes = 800, and consider document-header = "title").
- snowflake-arctic-embed2: query-prefix "query: ", no document
  prefix; 8K window.

Results vary by vault, so compare a few semantic queries before and
after. Changing `query-prefix` re-embeds nothing. Changing any other
key re-embeds every node: call `tusk_reload` after editing so this
server embeds under the new settings (the re-embed runs in the
background; `tusk_status` shows the queue).
`tusk_doctor` suggests prefixes for the models above when they're unset.

## Packs

A "type pack" is a bundle of node-type + edge-type declarations
(e.g. the `kanban` pack with a ticket workflow). Built-in names
(kanban, tags, vault) are fetched over the network. Install via the
CLI: `tusk pack add kanban`. See `tusk_help(topic: "packs")`.

## After editing

1. Save `tusk.toml`.
2. `tusk_reindex` — re-parses files against the new schema.
3. `tusk_doctor` — surfaces newly-detected drift (or confirms clean).

If `tusk_doctor` reports off-schema nodes after an edit, either change
the offending node's frontmatter or extend the manifest further.
