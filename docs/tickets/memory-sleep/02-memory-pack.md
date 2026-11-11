---
type: ticket
title: Add the built-in memory pack
blocks:
  - docs/tickets/memory-sleep/05b-skill-content
  - docs/tickets/memory-sleep/06-memory-guide
  - docs/tickets/memory-sleep/07-live-sleep-run
order: 2
parent: docs/tickets/memory-sleep/epic
points: 3
priority: medium
status: pending
---

`tusk pack add memory` gives a vault captures, ideas and the sleep log.

## The pack

```toml
[node-types.capture]
description = "Short-term memory: one attempt: what was tried, what happened, the takeaway"
properties = [
    { name = "session", type = "string", required = true },
    { name = "date",    type = "date",   required = true },
    { name = "outcome", type = "enum", values = ["favorable", "unfavorable", "open"], required = true },
    { name = "set-aside", type = "bool" },
]

[node-types.idea]
description = "Long-term memory: one concrete idea, what is true and how we know"
properties = [
    { name = "probes", type = "list-of", item-type = "string" },
]

[node-types.sleep]
description = "The log of one sleep: sessions covered, pages written, checks run"
properties = [
    { name = "date", type = "date", required = true },
]

[edge-types.distilled-from]
description = "This idea was compressed from that capture or page"
from        = ["idea"]
to          = ["*"]
cardinality = "many-to-many"
acyclic     = true
inverse     = "distilled-into"

[edge-types.supersedes]
description = "This idea contradicts or replaces that one"
from        = ["idea"]
to          = ["idea"]
cardinality = "many-to-many"
acyclic     = true
inverse     = "superseded-by"

[behaviors.workflow.memory-loop]
applies-to      = ["idea"]
status-property = "status"
states = [
    { name = "theory",   initial = true, start = true },
    { name = "verified", terminal = true, done = true },
]
transitions = [
    { from = "theory",   to = "verified" },
    { from = "verified", to = "theory" },
]
```

An idea starts at `theory`, and only a person moves it to `verified`. A capture that taught nothing new is written with `set-aside: true` and the reason in its body. A `tusk-ignore` entry cannot do that job: doctor reports an exemption as unused while the node does not break the rule, and a capture younger than a week never breaks the rule below.

## The rules

Three doctor rules ship with the pack, which is why it waits on [[docs/tickets/memory-sleep/01-packs-accept-rules]].

- An idea with no `distilled-from` link is an error.
- A capture more than a week old that no idea was distilled from, and that was not set aside, is a warning. The week of grace keeps doctor quiet about captures that arrive between sleeps.
- A `verified` idea that another idea supersedes is a warning, so a contradiction stays visible until a person settles it.

The spike tested the first two with these filters, the second without its `set-aside` term:

```toml
[rule.idea-names-its-sources]
filter = "type=idea AND NOT distilled-from-> (type=capture OR type=note)"

[rule.capture-is-consolidated]
filter   = "type=capture AND date<today-7d AND NOT set-aside=true AND NOT distilled-from<- type=idea"
severity = "warning"
```

## Scope

- `packs/memory.toml` with the types, edges, workflow and rules above.
- Write the sources rule so a link to any node counts, since page types differ from vault to vault. The filter above names `capture` and `note` only.
- Confirm that `probes` loads as a list of strings and that `set-aside` loads as a bool and filters the way the capture rule needs.
- Embed the pack as a built-in beside `kanban`, `tags` and `vault`, and add it wherever the built-ins are listed.

The pack does not choose a folder for memory. The sleep skill asks on its first run.

## Done when

- `tusk pack add memory` works offline in a fresh vault, and in a manifest that already has the `kanban` and `vault` packs, with no collision.
- In a scratch vault, an idea with no source is a doctor error. A capture dated eight days ago with no idea is a warning, the same capture with `set-aside: true` is quiet, and a capture dated today is quiet.
- A new idea starts at `theory`, and a status that is not a declared state is refused on write.
- `make docs` leaves nothing changed.
