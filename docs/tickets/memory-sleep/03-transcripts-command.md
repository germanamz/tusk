---
type: ticket
title: Read Claude Code transcripts with tusk claude transcripts
order: 3
parent: docs/tickets/memory-sleep/epic
points: 8
priority: medium
status: pending
---

A sleep reads finished sessions through this command and never parses Claude Code's files itself. The command lists a workspace's sessions and prints one session as turns: what the person wrote, what the assistant replied in text, and the summaries Claude Code writes when a session runs out of context. It leaves out tool calls and their output, which is most of a session file. In the spike, five sessions were about 19 MB on disk and 270 KB as turns, and the person's messages plus the summaries carried most of what mattered.

## Why it is split

Estimated at 8 points, so it has three child tickets:

- [[docs/tickets/memory-sleep/03a-transcript-reader]] parses one session file into turns.
- [[docs/tickets/memory-sleep/03b-session-discovery]] finds a workspace's sessions.
- [[docs/tickets/memory-sleep/03c-transcripts-cli]] is the command that prints them.

The parser and the discovery do not depend on each other. The command needs both.

## Done when

The three children are done, and in this repository `tusk claude transcripts` lists the project's sessions and prints one of them as turns with no tool output.
