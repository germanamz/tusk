---
type: ticket
title: Run a live sleep end to end
order: 7
parent: docs/tickets/memory-sleep/epic
points: 5
priority: medium
status: pending
---

Green tests are not the proof for this epic. This ticket is: a real sleep, in a real Claude Code session, over real sessions.

## Scope

- Build the binary and copy a real vault to a scratch folder outside the repository.
- Add the memory pack and install the plugin there.
- Before the sleep, pick four control questions about pages the sessions did not touch and record the first five results of each.
- Run the sleep in an interactive session started at the scratch vault's root. The plugin does not load under `claude -p`.
- Run the sleep a second time straight after.

The best input is the set of sessions the spike ran on, because the spike's numbers give something to compare against. Those are kept outside this repository (see [[docs/tickets/memory-sleep/08-keep-spike-material]]). Without them, use recent sessions from any real vault.

## Done when

- The sleep wrote captures and ideas, and git shows no change to any page that existed before it ran.
- `tusk doctor` reports no errors.
- The probe check ran, and its counts of hits and misses are written into this ticket.
- Each control question returns the same first five pages as before.
- The second sleep found nothing new to read.
- Every failure found on the way is fixed or filed.

Write only counts and tusk findings into this ticket. The vault's own content stays out of the repository.
