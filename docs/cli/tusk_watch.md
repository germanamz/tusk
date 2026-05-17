---
title: tusk watch
---

## tusk watch

Watch the workspace for external edits and keep the index in sync

### Synopsis

Watch the workspace for filesystem changes and update the index in
real time.

The watcher debounces rapid edits, follows file moves via fsnotify, and
drains the embedding queue in the background. It uses the same internal
service as "tusk node create" / "modify", so files edited in vim, Obsidian,
or piped from an LLM produce identical index state.

Runs until interrupted (Ctrl-C). Pair with "tusk status" or "tusk doctor"
in another shell to observe progress.

```
tusk watch [flags]
```

### Examples

```
  # Foreground: keep the index live while you author in any editor
  tusk watch

  # Background with a status pulse every 5 seconds
  tusk watch &
  watch -n 5 tusk status
```

### Options

```
  -h, --help   help for watch
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first agent brain: index a markdown vault into a graph

