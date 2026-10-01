---
title: tusk claude uninstall
---

## tusk claude uninstall

Remove the Claude Code plugin from this workspace

### Synopsis

Remove <workspace>/.claude/skills/tusk/ and the plugin's MCP approval from
.claude/settings.local.json. A .mcp.json entry install migrated is not
restored; re-add it with:

  claude mcp add --scope project tusk -- tusk mcp

```
tusk claude uninstall [flags]
```

### Options

```
  -h, --help   help for uninstall
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk claude](tusk_claude.md)	 - Install and inspect the Claude Code plugin for this workspace

