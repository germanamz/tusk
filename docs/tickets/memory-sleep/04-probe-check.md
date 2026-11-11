---
type: ticket
title: The probe check
order: 4
parent: docs/tickets/memory-sleep/epic
points: 8
priority: medium
status: pending
---

Each idea holds a list of held-out questions, its probes. They are stored in a property, which tusk does not embed, so they test whether the page can be found by wordings it has never seen. The check runs every probe through tusk's search and reports the ones that do not land on their idea, which is how a sleep shows it broke nothing.

## The rules

- A probe passes when its node is among the first three files returned. A neighbouring page often outranks an idea for a good reason, and a session reads several results.
- Rank decides, never a score threshold.
- A miss is a warning, the way doctor already treats a node missing from semantic results: the vault is correct and retrieval is degraded.
- There is no baseline and no list of accepted misses. A person clears a miss by fixing the page, rewording the probe or removing it. A sleep reports misses and never clears one.

## Why it is split

Estimated at 8 points, so it has two child tickets:

- [[docs/tickets/memory-sleep/04a-probe-runner]] executes probes and produces findings.
- [[docs/tickets/memory-sleep/04b-probe-check-surface]] puts the check on doctor.

The runner comes first.

## Done when

Both children are done, and on a scratch vault with embeddings a page with a fitting probe passes while a page with an unrelated probe is reported as a warning.
