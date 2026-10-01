---
title: tusk init
---

## tusk init

Initialize a Tusk workspace in the current directory

### Synopsis

Initialize a Tusk workspace in the current directory.

Creates tusk.toml (the manifest declaring node types and edge types) with a
minimal default schema, bootstraps the SQLite index under .tusk/, and appends
a .tusk/ ignore stanza to .gitignore if one is present.

With --claude it also installs the Claude Code plugin, as "tusk claude
install" does.

Safe to run only once per directory: it refuses to overwrite an existing
tusk.toml. After init, edit tusk.toml to declare your node/edge types, then
add content with "tusk node create" or by writing markdown files directly
and running "tusk reindex".

```
tusk init [flags]
```

### Examples

```
  # Create a workspace named "my-brain" in the current directory
  tusk init --name my-brain

  # Create it and set up Claude Code sessions for it
  tusk init --name my-brain --claude

  # Verify the workspace is healthy
  tusk doctor
```

### Options

```
      --claude        also install the Claude Code plugin (see tusk claude install)
  -h, --help          help for init
      --name string   workspace name written into tusk.toml (default "my-brain")
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown + HTML vault into a graph

