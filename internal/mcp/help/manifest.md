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
url      = "http://localhost:11434"

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
