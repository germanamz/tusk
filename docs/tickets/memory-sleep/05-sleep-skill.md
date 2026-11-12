---
type: ticket
title: The sleep skill in the Claude Code plugin
order: 5
parent: docs/tickets/memory-sleep/epic
points: 8
priority: medium
status: pending
---

`tusk claude install` gains a sleep skill. A person runs it by hand in a Claude Code session at the vault root when the work is finished, and it walks the agent through the five stages of a sleep. Nothing starts a sleep automatically.

## Why it is split

Estimated at 8 points, so it has two child tickets:

- [[docs/tickets/memory-sleep/05a-skill-packaging]] gets a skill to ship with the plugin.
- [[docs/tickets/memory-sleep/05b-skill-content]] writes the skill itself.

Packaging comes first because it settles where the text lives and how it is invoked.

## Done when

Both children are done, and the live run in [[docs/tickets/memory-sleep/07-live-sleep-run]] passes with the shipped skill.
