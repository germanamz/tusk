---
type: package
title: internal/behavior — pack engine
import-path: github.com/germanamz/tusk/internal/behavior
status: stable
---

# internal/behavior

Behavior-pack engine. Defines the 6-slot hook surface, dispatches hooks to registered pack instances, and detects collisions between packs that try to claim the same node-type's reserved keys (notably `status`, which a workflow pack reserves exclusively).

## Public surface

- `Engine` — orchestrates dispatch with recovery-aware semantics.
- `Instance` interface — one configured pack (produced by `Kind.NewInstance`); `Kind` interface — constructs `Instance`s from TOML config. There is no `Pack` type.
- `ReservedKey` + `ReservedKeys()` — the contract surface for collision detection (`detectCollisions` in `engine.go`).
- Hook-firing entry points: `FireNodeWriteValidateWithRecovery`, etc.

## Notes

Cross-check `ReservedKeys()` semantics in advance when designing packs that touch any property a workflow might own — surfaced as a brainstorm-time false premise during 7.c.3 (kanban pack tried to declare an enum `status` on `ticket` but the workflow already reserved it).
