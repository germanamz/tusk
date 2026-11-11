---
type: ticket
title: The transcripts list and show commands
blocks:
  - docs/tickets/memory-sleep/05b-skill-content
order: 3
parent: docs/tickets/memory-sleep/03-transcripts-command
points: 3
priority: medium
status: pending
---

The command a sleep calls. It joins the parser from [[docs/tickets/memory-sleep/03a-transcript-reader]] and the discovery from [[docs/tickets/memory-sleep/03b-session-discovery]] under `tusk claude transcripts`.

## Scope

- `list` prints the workspace's sessions oldest first: id, first and last timestamp, turn count and size as turns. It also has a JSON form.
- `show <id>` prints one session's turns: what the person wrote, the assistant's text replies and the context summaries, with no tool calls or tool output.
- A cap on long turns, and a flag to start after a given timestamp, so a session resumed after a sleep is read from where that sleep stopped.
- Output a model reads cheaply: plain and terse.
- Help text, `make docs`, and the command's place in `docs/packages/claudeplugin.md` or its own package doc.

The exact names and the output format are the builder's choice.

## Done when

- In this repository, `list` shows the project's sessions and `show` prints one of them without tool output.
- `show` with the start flag leaves out the earlier turns.
- A workspace with no Claude Code sessions prints an empty list and exits 0.
- `make docs` leaves nothing changed.
