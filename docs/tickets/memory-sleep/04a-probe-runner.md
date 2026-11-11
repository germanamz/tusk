---
type: ticket
title: Run probes and report misses
blocks:
  - docs/tickets/memory-sleep/04b-probe-check-surface
order: 1
parent: docs/tickets/memory-sleep/04-probe-check
points: 5
priority: medium
status: pending
---

The core of the probe check described in [[docs/tickets/memory-sleep/04-probe-check]], with no command-line surface yet.

## Scope

- Take every node that has a `probes` list, whatever its type. The memory pack's ideas are the first user.
- Run each probe as a semantic query with the workspace's own query settings and find where the node's file ranks.
- A probe passes when its node is among the first three files. A miss is a warning that names the probe, the node, the rank it reached and the file that came first.
- Rank decides, never a score threshold. In the vault the spike used, nearly every score fell between about 0.5 and 0.75, so no threshold separates a good answer from a poor one.
- Keep no baseline and no list of accepted misses.
- The embedder sits behind a port, so tests use a stub and the boundary check passes.

## Done when

Tests with a stubbed embedder cover:

- a pass at rank 1 and at rank 3
- a miss at rank 4, and a node that does not appear in the results at all
- two nodes that share the same probe
- a node with an empty `probes` list
- a vault with no probes, which reports nothing
