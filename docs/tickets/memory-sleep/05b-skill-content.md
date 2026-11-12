---
type: ticket
title: Write the sleep skill
blocks:
  - docs/tickets/memory-sleep/06-memory-guide
  - docs/tickets/memory-sleep/07-live-sleep-run
order: 2
parent: docs/tickets/memory-sleep/05-sleep-skill
points: 5
priority: medium
status: pending
---

The skill text is what turns a Claude Code session into a sleep. It carries the five stages and every rule a sleep has to keep.

## The five stages

1. Gather. List the sessions the last sleep did not cover, or covered only in part, with the command from [[docs/tickets/memory-sleep/03c-transcripts-cli]], and read them oldest first.
2. Capture. Write one capture per attempt: what was tried, what happened, and a takeaway of a sentence or two. Quote the person's own words where they state an idea or a decision. Write through tusk's node tools so the schema is checked.
3. Consolidate. Group captures that say the same thing and ask tusk what the vault already says, using the takeaway or the person's sentence as the search text. Then do one of four things: add the captures to an existing idea's `distilled-from` list, write a new idea, write a new idea that `supersedes` an existing one and leave the old one as it is, or set the captures aside.
4. Dream. For each new idea, write questions a future session would ask. List some on the page under "Questions this answers" and hold others out in `probes`. Run the probe check from [[docs/tickets/memory-sleep/04b-probe-check-surface]]. When a new idea's probe misses, rework that idea's title, prose and listed questions, twice at most. Then run the check over every idea and report any older idea whose probe stopped landing, without touching it.
5. Report. Write the sleep entry: the sessions covered and how far into each, the pages written, the probe results and anything flagged for a person. Run `tusk doctor`. Show what changed. Do not commit.

## Page shapes

- A capture's body has three short paragraphs labelled Tried, Result and Takeaway.
- An idea's body opens with the statement in three or four sentences, then uses whichever of these sections apply: what is alike and what differs, how it maps to the code, why it is built this way, how we know, not confirmed, approaches already tried, questions this answers. The spike's ideas ran 250 to 330 words.
- A new idea starts at `theory`. Only a person marks one `verified`.

## First run and budget

- On a vault's first sleep, say what is uncovered (how many sessions, their dates and size) and ask where to start and where memory should live. Suggest `memory/` beside the vault's notes, with `captures/`, `ideas/` and `sleeps/` under it. Later runs follow the newest sleep entry and write where the existing pages are.
- Read about 300 KB of turns per sleep. Stop at a session boundary, record how far the sleep got, and say how many sessions remain, so the next run continues from there.
- Do not hand sessions to subagents unless the person asks.

## Guards

- Never copy credentials, tokens or environment values, whatever the session shows.
- Never commit or push.
- Never edit a probe to make it pass.
- The only change to a page that existed before the sleep is a `distilled-from` link added to an existing idea. Check that against git at the end and report anything else as a failure.

Keep the text terse, since a model reads it, and measure its tokens.

## Done when

- The skill covers every stage, page shape and guard above.
- Its token count is written into this ticket.
- The live run in [[docs/tickets/memory-sleep/07-live-sleep-run]] passes with it.
