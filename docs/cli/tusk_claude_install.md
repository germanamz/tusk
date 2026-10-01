---
title: tusk claude install
---

## tusk claude install

Install the Claude Code plugin into this workspace

### Synopsis

Write the tusk plugin to <workspace>/.claude/skills/tusk/.

Install also approves the plugin's MCP server for you: Claude Code holds a
project plugin's MCP server until each user approves it, so install adds
"plugin:tusk:tusk" to enabledMcpjsonServers in .claude/settings.local.json
(your per-user, uncommitted settings).

If .mcp.json runs its own "tusk mcp" server, install removes that entry
(and the file, when it held nothing else) so the tools are not registered
twice. Pass --keep-mcp-json to leave it.

Re-running install is safe: it rewrites the files it owns. It refuses a
.claude/skills/tusk folder it did not write unless you pass --force.

The plugin runs the binary named by --bin, "tusk" on PATH by default. A
relative path resolves against the workspace root, where sessions start.

```
tusk claude install [flags]
```

### Examples

```
  # Install for this workspace
  tusk claude install

  # Point the plugin at a locally built binary
  tusk claude install --bin ./bin/tusk
```

### Options

```
      --bin string      tusk binary the plugin runs (default "tusk")
      --force           replace a .claude/skills/tusk folder tusk did not write
  -h, --help            help for install
      --keep-mcp-json   leave a .mcp.json tusk server in place
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk claude](tusk_claude.md)	 - Install and inspect the Claude Code plugin for this workspace

