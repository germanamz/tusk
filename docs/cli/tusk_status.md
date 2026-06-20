---
title: tusk status
---

## tusk status

Print a one-screen workspace summary

### Synopsis

Print a one-screen summary of workspace state: node counts by
type, total edges, embedding-queue depth, and time of last reindex.

Use status as a fast pulse check; use "tusk doctor" for validation
warnings and drift detail.

```
tusk status [flags]
```

### Examples

```
  # Fast pulse check
  tusk status

  # Watch status in a loop while "tusk watch" is running
  watch -n 5 tusk status
```

### Options

```
  -h, --help   help for status
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown + HTML vault into a graph

