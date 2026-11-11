---
type: ticket
title: Keep the spike material before it expires
due: "2026-11-17"
order: 8
parent: docs/tickets/memory-sleep/epic
points: 1
priority: high
status: completed
---

The spike behind this epic ran on a private vault. The session files it read sat in Claude Code's transcript store, which deletes files at about 30 days, and everything it produced sat in a session scratch folder that does not survive a restart. The live run in [[docs/tickets/memory-sleep/07-live-sleep-run]] is best compared against the same sessions, so they are worth keeping.

## Scope

- Copy the session files the spike read, their turn extracts, the captures and ideas it wrote, the question sets and the scripts to a place that survives.
- Keep all of it outside this repository. It quotes a private vault.

## Done when

The files exist outside the repository and the live run can read them.

## Result

Done on 2026-11-11. The material is kept on the maintainer's machine, outside any repository: 42 files, 18 MB.
