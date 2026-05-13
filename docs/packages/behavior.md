---
type: package
title: internal/behavior — pack engine
import-path: github.com/germanamz/tusk/internal/behavior
status: stable
last-touched-by: Plan 7.b
---

# internal/behavior

Behavior-pack engine. Defines the 8-slot hook surface, dispatches hooks to registered pack instances, and detects collisions between packs that try to claim the same node-type's reserved keys (notably `status`, which a workflow pack reserves exclusively).

## Public surface

- `Engine` — orchestrates dispatch with recovery-aware semantics.
- `Pack` interface — what an in-tree pack registration must satisfy.
- `ReservedKey` + `ReservedKeys()` — the contract surface for collision detection (`engine.go:125 detectCollisions`).
- Hook-firing entry points: `FireNodeWriteValidateWithRecovery`, etc.

## Notes

Cross-check `ReservedKeys()` semantics in advance when designing packs that touch any property a workflow might own — surfaced as a brainstorm-time false premise during 7.c.3 (kanban pack tried to declare an enum `status` on `ticket` but the workflow already reserved it).
