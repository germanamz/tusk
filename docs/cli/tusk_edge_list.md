---
title: tusk edge list
---

## tusk edge list

List edges, optionally filtered by source, target, or kind

### Synopsis

List edges in the index.

Filter with any combination of --from, --to, and --type. Output is a
tab-aligned table of source, type, target, attributed source-path. Use
--format compact|json (or --json) to opt into structured output; the
default mirrors prior CLI behavior (tab-aligned table for TTY).

A link written in a paragraph is stored for the file and again for the
paragraph and each section around it. The listing shows it once, from
the file, with UNITS naming the paragraphs, list items or sections the
link is written in, so you can open the passage with tusk node get. It
also leaves out each file's contains edges to its own sections and
paragraphs. --sub-units lists the stored rows as they are. A --from that
names a sub-unit (notes/x#S1.2P1) lists that sub-unit's own edges.

A file's targets of one type are listed in the order the file writes
them: its frontmatter list first, then body links in reading order.

```
tusk edge list [flags]
```

### Examples

```
  # All edges that touch a node (either direction)
  tusk edge list --from tickets/T-001
  tusk edge list --to   tickets/T-001

  # Every "blocks" edge in the workspace
  tusk edge list --type blocks

  # One row per stored edge, including each section and paragraph's copy
  tusk edge list --to tickets/T-001 --sub-units

  # JSON for piping into jq
  tusk edge list --from tickets/T-001 --json
```

### Options

```
      --format string   output format: compact|json (default: legacy table)
      --from string     filter to edges originating from this source id
  -h, --help            help for list
      --json            emit structured JSON (sugar for --format json)
      --sub-units       list the stored rows: one per section or paragraph that holds a link, plus each file's contains edges
      --to string       filter to edges targeting this id
      --type string     filter by edge type
```

### Options inherited from parent commands

```
  -v, --verbose         emit debug-level logs to stderr
  -C, --workspace dir   run as if started in dir: find tusk.toml from there instead of the working directory (default $TUSK_WORKSPACE)
```

### SEE ALSO

* [tusk edge](tusk_edge.md)	 - Manage edges between nodes (add, remove, list)

