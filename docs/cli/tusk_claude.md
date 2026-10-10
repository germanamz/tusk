---
title: tusk claude
---

## tusk claude

Install and inspect the Claude Code plugin for this workspace

### Synopsis

Manage the Claude Code plugin tusk ships for this workspace.

The plugin lives at <workspace>/.claude/skills/tusk/ and Claude Code loads it
for any session started in the workspace root. It carries two things:

  * The tusk MCP server ("tusk mcp"), so the agent has the tusk_* tools.
  * A hook that runs "tusk claude context" when a conversation starts and adds
    its output (a short orientation plus the [context] digest) to the first
    message, the way CLAUDE.md is added. /clear and compaction re-read it.
  * A hook that runs "tusk claude refs" after the agent edits a file and,
    when vault pages name that file in inline code (edge types with
    paths = true), adds a short reminder listing them after the edit's
    result. Each file is mentioned once per session.

Run "tusk claude install" once per workspace (or "tusk init --claude"), and
again after upgrading tusk to refresh the plugin.

### Options

```
  -h, --help   help for claude
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown + HTML vault into a graph
* [tusk claude context](tusk_claude_context.md)	 - Print the context block the Claude Code plugin adds to a session
* [tusk claude install](tusk_claude_install.md)	 - Install the Claude Code plugin into this workspace
* [tusk claude refs](tusk_claude_refs.md)	 - Print the reminder the Claude Code plugin adds after an edit
* [tusk claude status](tusk_claude_status.md)	 - Report the Claude Code plugin installed in this workspace
* [tusk claude uninstall](tusk_claude_uninstall.md)	 - Remove the Claude Code plugin from this workspace

