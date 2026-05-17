# Tusk workflows

Multi-command recipes. Each recipe states an intent, then lists the commands
in order with one comment line above each.

For per-command reference, see [the CLI index](README.md).

---

## 1. Bootstrap a new vault

You have an empty directory and want a working Tusk workspace with a
sensible schema.

```bash
# Create tusk.toml and .tusk/
tusk init --name my-brain

# Seed it with a built-in type pack (here: gtd)
tusk pack add gtd

# Confirm the manifest is valid and the index is healthy
tusk doctor
```

After this, write markdown into the directory by any means — Tusk's
`tusk node create` is one option, but vim/Obsidian/an LLM writing files
work just as well.

---

## 2. Author with an external editor

You prefer your editor; you just want Tusk to keep up.

In shell 1, leave the watcher running:

```bash
# Foreground watcher — debounces edits and refreshes embeddings
tusk watch
```

In shell 2, edit files freely:

```bash
# Create or edit any .md file under the workspace
$EDITOR notes/2026-05-16.md

# After saving, the watcher has already picked it up.
# Confirm it landed in the index:
tusk status
```

---

## 3. Structural → semantic → hybrid query

The same intent expressed three ways teaches the grammar by contrast.
Suppose you want: "design notes about SQLite write contention."

```bash
# Structural: only matches if frontmatter literally tags it
tusk query 'type:note AND +design AND title.contains:sqlite'

# Pure semantic: a permissive filter, ranked by similarity in embedding space.
# (The positional filter is required; use 'type:note' to scope or '' for everything.)
tusk query 'type:note' --semantic 'sqlite write contention'

# Hybrid (recommended for agents): tighter filter, then rank
tusk query 'type:note AND +design' --semantic 'sqlite write contention'
```

---

## 4. MCP wiring for Claude Code

You want Tusk's CLI surface available to an agent over MCP.

```bash
# Verify the workspace is healthy first
tusk doctor

# Start the MCP server (stdio is the default; Claude Code launches this for you)
tusk mcp
```

Then add this to your Claude Code MCP config (full instructions in the
project README):

```json
{
  "mcpServers": {
    "tusk": {
      "command": "tusk",
      "args": ["mcp"]
    }
  }
}
```

Most read/write CLI verbs have a 1:1 MCP tool of the same shape
(`tusk_node_create`, `tusk_query`, …). Workspace-bootstrap commands
(`tusk init`, `tusk pack`, `tusk watch`) are CLI-only.

---

## 5. Health check and recovery

Something feels off — drift, dangling edges, or stale embeddings.

```bash
# See what's wrong
tusk doctor

# Catch the index up with disk
tusk reindex

# Re-run doctor to confirm
tusk doctor
```

---

## 6. Install a type pack

You want to add a built-in schema bundle.

```bash
# Copy the pack's node/edge type declarations into tusk.toml
tusk pack add gtd

# Inspect the manifest diff in your editor
git diff tusk.toml

# Verify the schema is still consistent
tusk doctor
```

If the pack collides with existing declarations:

```bash
# Force-overwrite the colliding sections
tusk pack add gtd --force
```
