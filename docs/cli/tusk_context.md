---
title: tusk context
---

## tusk context

Compose a warm-context digest from the manifest [context] block

### Synopsis

Print the composed warm-context digest for this workspace.

The digest is the single entry point an agent calls at session start instead
of issuing three to five exploratory list/get/query calls. It returns:

  * Pinned nodes ([context.pinned]) with body + edges expanded by default.
  * Recent activity ([context.recent] or recent = "<alias>"): one alias
    result, typically a node list filtered with modified-since:<N>d.
  * Aliases ([context.include]): a fan-out over named, manifest-declared
    aliases; each result is folded under its alias name.

Use --include to override the per-node expansion set for the pinned and
recent sections (default: body,edges). Use --format / --json to override
the output format (default: compact at TTY, JSON when piped).

```
tusk context [flags]
```

### Examples

```
  # Pull the digest at session start
  tusk context

  # Trim per-node payload to edges only
  tusk context --include edges

  # Pipe the digest into another tool
  tusk context --json
```

### Options

```
      --format string     output format: compact|json (default: compact at TTY, JSON when piped)
  -h, --help              help for context
      --include strings   per-node include set for pinned + recent (default body,edges)
      --json              emit structured JSON (sugar for --format json)
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown vault into a graph

