---
type: ticket
title: 'Epic: memory types and the sleep pass'
priority: medium
status: pending
---

Build the first of two memory designs: a `memory` type pack, the sleep pass an agent runs when work is finished, and the tusk commands that pass needs.

## Why

An agent keeps what it knows in its weights, which are expensive to change, and in its context window, which is small. A vault reached by meaning is a third place, and it is cheap to update. But embeddings only measure what someone has written down. An idea that is stated in a session and never written to the vault cannot be found later. A sleep is the step that writes it down: after the work is finished, an agent reads the finished sessions, compresses them into short pages, and checks that search finds those pages.

## The design in short

- Memory comes in two kinds. A capture is short-term: one attempt, with what was tried, what happened and a takeaway. An idea is long-term: one concrete idea, how we know it, and links to every capture and page it was compressed from. A sleep entry logs each run.
- A sleep has five stages: gather the sessions the last sleep did not cover, write captures, consolidate captures into ideas, dream, and report.
- Dreaming means the agent writes questions a future session would ask and tests them against search. Some questions are listed on the idea's page. Others are held out as probes, and the probe check reports any that stop landing on their idea.
- A sleep only adds. It never deletes or rewrites a page that existed before it ran, it never commits, and it never copies a credential out of a session. A person reviews what it wrote and commits.
- Tusk finds and checks. The agent does the thinking, and tusk never calls a language model.

A spike ran one sleep by hand on a copy of a private vault. Five sessions became eleven captures and three ideas. Eight questions about those ideas had no page to land on before the sleep, and five landed first on the first draft of the ideas. Four unrelated control questions kept their first five results in every pass.

## How the work is sized

Each ticket carries Fibonacci points: 1, 2, 3, 5 or 8. A ticket at 8 or more is too large to finish and verify in one sitting, so it gets child tickets of 5 or less and stays as their parent. Three tickets were split this way: the transcripts command, the probe check and the sleep skill. The leaves add up to 40 points.

## Order

1. [[docs/tickets/memory-sleep/01-packs-accept-rules]] (3), which the memory pack needs.
2. [[docs/tickets/memory-sleep/02-memory-pack]] (3).
3. [[docs/tickets/memory-sleep/03-transcripts-command]] (8, three children).
4. [[docs/tickets/memory-sleep/04-probe-check]] (8, two children).
5. [[docs/tickets/memory-sleep/05-sleep-skill]] (8, two children). Writing the skill waits on 2, 3 and 4.
6. [[docs/tickets/memory-sleep/06-memory-guide]] (3).
7. [[docs/tickets/memory-sleep/07-live-sleep-run]] (5), which is the proof.
8. [[docs/tickets/memory-sleep/08-keep-spike-material]] (1). It blocks nothing.

Tickets 1, 3 and 4 do not depend on each other and can run side by side.

## Not in this epic

The wake side (captures written during a session, the "have I tried this already" lookup, the drift check) is the second design. Two fixes the spike found are separate small changes: the title match lifts a word that every title in a vault shares, and tusk has no lookup for "what is near this node". Claude Code's own memory folder is untouched.
