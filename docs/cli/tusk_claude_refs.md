---
title: tusk claude refs
---

## tusk claude refs

Print the reminder the Claude Code plugin adds after an edit

### Synopsis

Print the reminder the plugin adds after Claude Code edits a file: the
vault pages that name the file, or a directory above it, in inline code, with
the lines and sections they name it in. The pages that name the file itself
come first, then those naming a nearer directory. At most --max pages are
listed; a last line says how to see the rest.

Pages name paths through edge types that set paths = true in tusk.toml. The
path may be absolute or relative to the current directory; one outside the
workspace prints nothing.

The plugin calls this once per file a session edits. It never fails an edit:
when nothing names the path, the workspace declares no paths edge type, or the
index is missing, it prints nothing. It always exits 0.

```
tusk claude refs <path> [flags]
```

### Examples

```
  # Preview the reminder an edit to this file would trigger
  tusk claude refs server/ledger/core/service.go
```

### Options

```
  -h, --help      help for refs
      --max int   list at most N pages (default 5)
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk claude](tusk_claude.md)	 - Install and inspect the Claude Code plugin for this workspace

