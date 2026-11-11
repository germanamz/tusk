---
type: ticket
title: Parse a Claude Code session file into turns
blocks:
  - docs/tickets/memory-sleep/03c-transcripts-cli
order: 1
parent: docs/tickets/memory-sleep/03-transcripts-command
points: 3
priority: medium
status: pending
---

One package knows Claude Code's session file format, and nothing else does. Input is a session's JSONL file. Output is its turns in order, each with a timestamp, a role and text.

## Scope

- Keep what the person wrote, the assistant's text replies, and the summaries Claude Code writes when a session runs out of context. Mark a summary as a summary.
- Drop tool calls, tool results, subagent lines and meta lines.
- The format is undocumented and can change, so skip any line the parser does not recognize and never fail on one.
- Fixtures shaped like real sessions, with invented content. Never commit a real session.

What the spike learned about the format, as a starting point and not a contract: each line is a JSON object; `type` is `user` or `assistant` for turns; `isSidechain` marks subagent lines and `isMeta` marks meta lines; `message.content` is either a string or a list of blocks, where a block with `type` `text` carries prose; `timestamp` is ISO 8601; a summary arrives as a user turn that begins "This session is being continued from a previous conversation".

## Done when

Tests cover a plain session, a session with a context summary, a session with tool calls and subagent lines, an empty file, a file with a truncated last line, and a line of an unknown type.
