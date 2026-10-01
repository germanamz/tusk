---
title: tusk claude context
---

## tusk claude context

Print the context block the Claude Code plugin adds to a session

### Synopsis

Print the block the plugin adds to the first message of a Claude Code
conversation: a short orientation (the vault's declared node types with
counts, edge types, aliases, index state), then the [context] digest
("tusk context") when tusk.toml declares one.

The block stays within [context] max-bytes (default 16384). Over budget, it
drops alias sections first, then recent nodes, then pinned nodes, a whole node
at a time, and says so in its last line. The orientation is never cut.

It never fails a session: outside a workspace it prints nothing, a broken
tusk.toml prints a one-line notice, and a missing index prints the
orientation alone. It always exits 0.

```
tusk claude context [flags]
```

### Examples

```
  # Preview what a Claude Code session sees
  tusk claude context

  # Preview with a tighter budget
  tusk claude context --max-bytes 4096
```

### Options

```
  -h, --help            help for context
      --max-bytes int   override the block's byte budget ([context] max-bytes, default 16384)
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk claude](tusk_claude.md)	 - Install and inspect the Claude Code plugin for this workspace

