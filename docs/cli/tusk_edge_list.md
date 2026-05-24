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

  # JSON for piping into jq
  tusk edge list --from tickets/T-001 --json
```

### Options

```
      --format string   output format: compact|json (default: legacy table)
      --from string     filter to edges originating from this source id
  -h, --help            help for list
      --json            emit structured JSON (sugar for --format json)
      --to string       filter to edges targeting this id
      --type string     filter by edge type
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk edge](tusk_edge.md)	 - Manage edges between nodes (add, remove, list)

