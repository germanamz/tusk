---
title: tusk run
---

## tusk run

Run a manifest-declared alias by name

### Synopsis

Invoke an alias declared under [alias.<name>] in tusk.toml.

Aliases are reusable, read-only verb invocations. They bind a command (one
of node list, node get, query, edge list, doctor, status) to a fixed set of
arguments so an agent or operator can dispatch a frequent query by name
rather than retyping the filter / sort / take flags.

Use --list to enumerate the aliases the loaded manifest declares. Use
--format / --json to override the output format (defaults match the
underlying verb: tab-aligned table for the legacy view, compact at TTY,
JSON when piped).

```
tusk run [alias] [flags]
```

### Examples

```
  # Run a pre-declared alias
  tusk run open-tickets

  # Enumerate every alias the manifest declares
  tusk run --list

  # Force JSON regardless of the alias's default
  tusk run open-tickets --json
```

### Options

```
      --format string   output format: compact|json (default: matches the verb's TTY behavior)
  -h, --help            help for run
      --json            emit structured JSON (sugar for --format json)
      --list            list every alias declared in tusk.toml
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown + HTML vault into a graph

