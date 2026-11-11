---
type: ticket
title: Find a workspace's Claude Code sessions
blocks:
  - docs/tickets/memory-sleep/03c-transcripts-cli
order: 2
parent: docs/tickets/memory-sleep/03-transcripts-command
points: 3
priority: medium
status: pending
---

Given a workspace, find the folder where Claude Code keeps its session files and describe each session.

## Scope

- Resolve the project folder under `~/.claude/projects/` for a workspace root. Honor `CLAUDE_CONFIG_DIR` and `CLAUDE_CODE_PROJECT_DIR_NAME`.
- Find out how Claude Code names the folder for a session started in a subdirectory or a git worktree of the workspace, and cover what it does.
- Describe each session: id, first and last timestamp, turn count, and size as turns.
- File, environment and clock access go through ports, per the repository's boundary rules.

Claude Code deletes session files after `cleanupPeriodDays`, about 30 days by default, so the list only ever shows what is still on disk.

## Done when

- Against a fixture folder, sessions come back oldest first with the right time span and counts.
- A workspace with no project folder returns an empty list and a clear message, not an error.
- An empty session file and one with a truncated last line are listed without failing.
- The boundary check passes.
