---
type: ticket
title: Memory guide and docs wiring
order: 6
parent: docs/tickets/memory-sleep/epic
points: 3
priority: medium
status: pending
---

A user who has never seen the design should be able to add memory to a vault and run a sleep from the guide alone.

## Scope

- A new guide page, `docs/guide/memory.md`: what captures, ideas and the sleep log are, adding the pack, running a sleep, reviewing what it wrote before committing, probes and the probe check.
- Say that Claude Code deletes transcripts after `cleanupPeriodDays`, so a vault that sleeps less often than that loses sessions.
- Give the page its place in `llmsGuideOrder` (`cmd/tusk/docgen_llms.go`), in `llms.txt` and in the README's documentation list.
- Run `make docs` and keep what it regenerates.
- Write it in the same plain voice as the other guides.

Each code ticket updates its own package doc. This ticket is the guide and the wiring only.

## Done when

- Every command in the guide was run as written against a scratch vault.
- `make docs` leaves nothing changed, and the pre-push docs check passes.
