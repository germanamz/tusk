# v0.14 Task Tickets

One ticket per phase (epic) and one per phase-task (child). Each
ticket is self-contained — read it standalone and you have what you
need to claim and execute the work without reading the full plan
doc, while being able to fall back to the plan doc for the exact
implementation details.

## Index

| ID | Title | Type | Parent |
|----|-------|------|--------|
| [P1](P1.md) | Convention doc + linter scaffold + rule 1 | epic | — |
| [P1-T1](P1-T1.md) | Write STYLE.md | task | P1 |
| [P1-T2](P1-T2.md) | Cross-reference STYLE.md from CONTRIBUTING.md | task | P1 |
| [P1-T3](P1-T3.md) | Create cmd/tusk-lint multichecker shell | task | P1 |
| [P1-T4](P1-T4.md) | Wire lint-tusk into Makefile | task | P1 |
| [P1-T5](P1-T5.md) | Configure varnamelen with per-package exclusions | task | P1 |
| [P1-T6](P1-T6.md) | Verify Phase 1 CI green | task | P1 |
| [P2](P2.md) | Custom analyzers (rules 2, 3, 4) | epic | — |
| [P2-T1](P2-T1.md) | Implement blankline analyzer | task | P2 |
| [P2-T2](P2-T2.md) | Implement namederr analyzer | task | P2 |
| [P2-T3](P2-T3.md) | Implement testhandle analyzer | task | P2 |
| [P2-T4](P2-T4.md) | Add pathfilter helper | task | P2 |
| [P2-T5](P2-T5.md) | Register analyzers in cmd/tusk-lint | task | P2 |
| [P2-T6](P2-T6.md) | Verify Phase 2 CI green | task | P2 |
| [P3](P3.md) | Sweep service/ | epic | — |
| [P3-T1](P3-T1.md) | Drop service/ varnamelen exclusion | task | P3 |
| [P3-T2](P3-T2.md) | Drop service/ pathfilter entry | task | P3 |
| [P3-T3](P3-T3.md) | Apply STYLE.md fixes across service/ | task | P3 |
| [P3-T4](P3-T4.md) | Verify service/ tests pass | task | P3 |
| [P3-T5](P3-T5.md) | Verify service/ lint clean | task | P3 |
| [P4](P4.md) | Sweep internal/tui/ | epic | — |
| [P4-T1](P4-T1.md) | Drop internal/tui/ varnamelen exclusion | task | P4 |
| [P4-T2](P4-T2.md) | Drop internal/tui/ pathfilter entry | task | P4 |
| [P4-T3](P4-T3.md) | Apply STYLE.md fixes across internal/tui/ | task | P4 |
| [P4-T4](P4-T4.md) | Verify internal/tui/ tests pass | task | P4 |
| [P4-T5](P4-T5.md) | Verify internal/tui/ lint clean | task | P4 |
| [P5](P5.md) | Sweep internal/mcp/ + internal/portability/ | epic | — |
| [P5-T1](P5-T1.md) | Drop internal/mcp/ + internal/portability/ varnamelen exclusions | task | P5 |
| [P5-T2](P5-T2.md) | Drop internal/mcp/ + internal/portability/ pathfilter entries | task | P5 |
| [P5-T3](P5-T3.md) | Apply STYLE.md fixes across internal/mcp/ + internal/portability/ | task | P5 |
| [P5-T4](P5-T4.md) | Verify MCP and portability tests pass | task | P5 |
| [P5-T5](P5-T5.md) | Verify P5 packages lint clean | task | P5 |
| [P6](P6.md) | Sweep filter/ + domain/ + syntax/ | epic | — |
| [P6-T1](P6-T1.md) | Drop filter/ + domain/ + syntax/ varnamelen exclusions | task | P6 |
| [P6-T2](P6-T2.md) | Drop filter/ + domain/ + syntax/ pathfilter entries | task | P6 |
| [P6-T3](P6-T3.md) | Apply STYLE.md fixes across filter/ + domain/ + syntax/ | task | P6 |
| [P6-T4](P6-T4.md) | Verify filter/ + domain/ + syntax/ tests pass | task | P6 |
| [P6-T5](P6-T5.md) | Verify P6 packages lint clean | task | P6 |
| [P7](P7.md) | Sweep repository/ + sqlite/ + cmd/ + tests/e2e/ + root | epic | — |
| [P7-T1](P7-T1.md) | Drop remaining varnamelen exclusions | task | P7 |
| [P7-T2](P7-T2.md) | Drop remaining pathfilter entries | task | P7 |
| [P7-T3](P7-T3.md) | Apply STYLE.md fixes across remaining packages | task | P7 |
| [P7-T4](P7-T4.md) | Verify e2e and unit tests pass | task | P7 |
| [P7-T5](P7-T5.md) | Verify P7 packages lint clean | task | P7 |
| [P8](P8.md) | Lock-in | epic | — |
| [P8-T1](P8-T1.md) | Verify all varnamelen exclusions are gone | task | P8 |
| [P8-T2](P8-T2.md) | Verify pathfilter slice is empty | task | P8 |
| [P8-T3](P8-T3.md) | Add lint-style-locked CI guard | task | P8 |
| [P8-T4](P8-T4.md) | Mark STYLE.md as enforced | task | P8 |
| [P8-T5](P8-T5.md) | Verify Phase 8 CI green | task | P8 |

## Dependency summary

- P1 must ship before P2.
- P2 must ship before P3..P7.
- P3..P7 are mutually independent and may ship in any order.
- P8 must wait for all of P3..P7 to ship.
- Within a phase, tasks are sequential unless noted otherwise.
