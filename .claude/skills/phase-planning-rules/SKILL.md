---
name: phase-planning-rules
description: Rules for drafting phased implementation plans for multi-agent execution. Use when breaking work into phases that will be handed to separate implementer agents.
---

## Phase Planning Rules

### Execution Model

Each phase will be implemented by a separate implementer agent. Implementer agents
have access to the current codebase, the design spec, and all phase plan docs —
these are committed to the repository for reference. However, the implementer's
own phase doc must be the single authoritative source for what to do and why.
Reference material provides context; the phase doc provides direction. Implementer
agents cannot communicate with the planning agent or other implementer agents
during execution.

The planning agent drafts all phases, runs the continuity review, and — after all
phases are implemented — performs the post-implementation review and verification.
Only the planning agent understands the reasoning behind the phase split. All plan
docs are cleaned up from the repository after the final review and verification.

### Rules

Apply these constraints when drafting each individual phase.

1. **One plan doc per phase.**
   Each phase gets its own standalone document — never combine multiple phases
   into a single doc. Each doc will be handed to a different implementer agent
   as its primary working directive.

2. **The phase doc is the implementer's primary directive.**
   The implementer agent has access to the design spec and other phase docs for
   reference, but must be able to execute every task from this phase doc alone.
   No implied steps. No "see phase 2 for details." No "and then wire everything
   up." Specify exact files, functions, types, and expected behavior. If context
   from the codebase or design spec is needed, point to the specific location —
   do not assume the implementer will explore on its own.

3. **Each phase must be independently shippable.**
   At the end of every phase, the system must be deployable and functional.
   No phase should leave the product in a broken or half-migrated state. Even
   though the implementer can see future phases for context, "the next phase
   will fix this" is never acceptable — each phase must stand on its own.

4. **Every phase must be compilation-safe.**
   The code must compile and pass type-checking after each phase is applied in
   isolation. If a later phase depends on interfaces not yet implemented,
   introduce bridge code (stubs, feature flags, adapter layers, no-op
   implementations) within the current phase to maintain compilation. Include
   bridge code tasks explicitly in the phase doc — the implementer will not
   infer the need for them.

5. **Cap each phase at 4–6 tasks.**
   If a phase exceeds 6 tasks, split it. If it has fewer than 4, consider
   merging with an adjacent phase — unless it is intentionally narrow (e.g.,
   a cleanup or migration phase). Keeping phases small bounds the scope an
   individual implementer agent must hold in context.

6. **Declare prerequisites explicitly.**
   Each phase doc must list which prior phases must be completed before it can
   begin. Never rely on "it's obvious from the numbering." If a phase has no
   prerequisites beyond the base codebase, state that explicitly. If phases can
   be executed in parallel, say so. The planning agent uses this to sequence
   implementer handoffs correctly.

7. **Document the phase boundary.**
   Every phase doc must end with a **"Changes Introduced"** section listing:
   new files, modified interfaces, new environment variables, schema migrations,
   added dependencies, and any bridge code introduced with its removal target
   phase. Every phase doc (after phase 1) must begin with an **"Inherits From"**
   section describing the state of the codebase the implementer should expect
   to find — what prior phases changed and what the implementer can rely on
   being in place. Treat phase boundaries like API contracts between
   implementer agents.

8. **Tag all bridge code with a removal target.**
   Whenever a phase introduces bridge code (stubs, no-ops, feature flags,
   adapter layers), tag it in the phase doc with the specific phase number
   where it will be replaced by real implementation. The removal must appear
   as an explicit task in the target phase's doc. If you cannot name a removal
   phase, the plan is incomplete.

9. **Preserve user-visible behavior.**
   If a phase swaps an implementation behind an interface, the prior phase's
   behavioral guarantees carry forward unless explicitly deprecated in the
   phase doc. List the user-visible behaviors that must still work after the
   phase is applied. The implementer agent uses this list as its acceptance
   criteria.
