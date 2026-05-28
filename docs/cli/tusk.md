---
title: tusk
---

## tusk

Local-first memory for agents: index a markdown vault into a graph

### Synopsis

Tusk is a local-first memory system for coding agents. It turns a directory
of markdown files into a schema-validated, semantically-indexed graph and
serves that graph over MCP, so an agent recalls context with a single query
instead of many filesystem round trips.

Files are the source of truth (markdown + a tusk.toml manifest), git is the
history, and tusk is the indexer and retrieval engine. Every node is a plain
markdown file you can read, edit, and commit by hand — nothing is locked away
in a proprietary store.

WHY GRAPHRAG
  An agent that greps and reads files pays one round trip per file and still
  misses how they relate. Tusk indexes nodes, their typed edges, and vector
  embeddings up front, so a single "tusk query" (or the tusk_query MCP tool)
  returns the matching nodes plus their graph-expanded neighbours ranked by
  semantic similarity — the context an agent needs, gathered in one call.

HOW TO USE
  1. tusk init --name my-brain   Create tusk.toml and the .tusk/ index.
  2. edit tusk.toml              Declare your node types and edge types.
  3. tusk node create …          Add content (or write .md files directly).
  4. tusk reindex / tusk watch   Bring the index up to date / keep it live.
  5. tusk query / tusk context   Retrieve structurally, semantically, or both.
  6. tusk mcp                    Expose the same surface to an MCP agent.

  Most read/write verbs are also MCP tools (tusk_query, tusk_node_create, …),
  so an agent uses the same surface without a shell. Run "tusk doctor" any
  time to surface schema warnings, dangling edges, and index-health issues.

CONFIGURATION
  All configuration lives in tusk.toml at the workspace root:

    [workspace]              name, ignore globs, sub-units (default true)
    [node-types.<name>]      typed properties a node may/must set
    [edge-types.<name>]      from / to / cardinality / inverse / acyclic /
                             hierarchy / wikilinks for typed relationships
    [embeddings]             provider = "ollama", model, endpoint, dim,
                             api-key, workers, timeout-seconds — enables
                             semantic search
    [query.graph-expansion]  enabled, hops (1|2), edge-types, weight,
                             candidate-multiplier — tunes graphrag retrieval
    [context]                pinned, recent, include — shape the warm-context
                             digest produced by "tusk context"
    [lease]                  ttl_seconds (default 60) for multi-instance
                             indexing
    [behaviors.<kind>]       manifest-declared behaviors
    [alias.<name>]           saved queries runnable via "tusk run <name>"

  Environment overrides (read once at startup, highest precedence):
    TUSK_EMBED_WORKERS       embed/reindex pool size; 0 = read-only instance.
                             Falls back to [embeddings] workers, then
                             max(1, NumCPU/2).
    TUSK_LEASE_TTL_SECONDS   lease window in seconds. Falls back to
                             [lease] ttl_seconds, then 60.

  See "tusk <command> --help" for per-command flags and detail.

### Examples

```
  # Bootstrap a fresh memory vault and verify it
  tusk init --name my-brain
  tusk doctor

  # Index markdown already on disk, then keep it live in the background
  tusk reindex
  tusk watch

  # Add a node (or just write the .md file yourself and reindex)
  tusk node create --type note --path notes/auth-design.md \
      --title "Auth design" --prop tag=architecture

  # Retrieve: structural, then semantic, then graph-expanded hybrid
  tusk query 'type=note AND tag=architecture'
  tusk query 'type=note' --semantic 'how does login work' --min-score 0.5
  tusk query 'type=note' --semantic login --graph-expand --hops 2 --include edges

  # Compose a warm-context digest for an agent session
  tusk context

  # Serve the graph to an MCP agent (Claude Code, Cursor, Zed)
  tusk mcp

  # Run a read-only MCP instance that never drains the index
  TUSK_EMBED_WORKERS=0 tusk mcp
```

### Options

```
  -h, --help      help for tusk
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk context](tusk_context.md)	 - Compose a warm-context digest from the manifest [context] block
* [tusk doctor](tusk_doctor.md)	 - Surface validation warnings, dangling edges, and index health issues
* [tusk edge](tusk_edge.md)	 - Manage edges between nodes (add, remove, list)
* [tusk init](tusk_init.md)	 - Initialize a Tusk workspace in the current directory
* [tusk mcp](tusk_mcp.md)	 - Run the long-running MCP server (stdio or SSE)
* [tusk node](tusk_node.md)	 - Manage individual nodes (create, get, list, modify, move, delete)
* [tusk pack](tusk_pack.md)	 - Install and manage built-in type packs
* [tusk query](tusk_query.md)	 - Run a structural, semantic, or hybrid query against the index
* [tusk reindex](tusk_reindex.md)	 - Walk the workspace and bring the index up to date with disk
* [tusk run](tusk_run.md)	 - Run a manifest-declared alias by name
* [tusk status](tusk_status.md)	 - Print a one-screen workspace summary
* [tusk watch](tusk_watch.md)	 - Watch the workspace for external edits and keep the index in sync

