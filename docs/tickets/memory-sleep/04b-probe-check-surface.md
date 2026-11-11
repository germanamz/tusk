---
type: ticket
title: Expose the probe check on doctor
blocks:
  - docs/tickets/memory-sleep/05b-skill-content
order: 2
parent: docs/tickets/memory-sleep/04-probe-check
points: 3
priority: medium
status: pending
---

Puts the runner from [[docs/tickets/memory-sleep/04a-probe-runner]] where a person, an agent and a CI job can reach it.

## Scope

- An opt-in flag on `tusk doctor`, because the check costs one embedding call per probe. The flag's name is the builder's choice.
- When the embedding backend cannot be reached, say the check did not run, the way reindex reports embeddings it could not make.
- The exit code follows doctor's existing rule: a miss is a warning, so a plain run exits 0 and `--fail-on=warning` exits 1.
- Decide whether `tusk_doctor` over MCP takes the option. If it does not, say so in the help text, since the sleep skill then calls the CLI.
- Help text, `make docs`, the rules-and-doctor guide and `docs/packages/doctor.md`.

## Done when

- On a scratch vault with Ollama running, a page with a fitting probe passes and a page with an unrelated probe is a warning that names the probe, the page, its rank and the file that came first.
- With Ollama stopped, the run says the check did not run and exits 0.
- `--fail-on=warning` exits 1 on a miss.
- `make docs` leaves nothing changed.
