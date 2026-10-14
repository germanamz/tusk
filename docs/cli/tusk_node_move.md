---
title: tusk node move
---

## tusk node move

Atomically rename a node and rewrite all referring edges

### Synopsis

Atomically rename a node file and rewrite every edge that points at it.

The new path is workspace-relative. If you omit the file extension on the
target, the source file's extension (e.g. ".md") is inherited so the renamed
file keeps its on-disk extension. All other node files that reference the old
id are rewritten in the same transaction, so the index never observes a broken
state.

Every wikilink keeps naming the note it named. A relative link
([[../old-id]]) is re-rendered from where its file lives after the move, the
moved note's own links included. A file-name link ([[old-name]]) stays as
written while the name still finds the note, becomes [[new-name]] when the
move renames the file, and becomes the full id when another note already has
that name. Links to that other note by name are rewritten to its full id too,
and their files count as referring files.

```
tusk node move <old-id> <new-rel-path> [flags]
```

### Examples

```
  # Move a note into a subdirectory
  tusk node move notes/hello notes/intros/hello.md

  # Same move using the id-style target (extension inherited from source)
  tusk node move notes/hello notes/intros/hello

  # After move, confirm references were rewritten
  tusk edge list --to notes/intros/hello
```

### Options

```
  -h, --help   help for move
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk node](tusk_node.md)	 - Manage individual nodes (create, get, render, list, modify, move, delete)

