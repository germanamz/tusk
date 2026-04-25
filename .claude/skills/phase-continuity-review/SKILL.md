---
name: phase-continuity-review
description: Review checklist to run after all phase plans are drafted, before handing any phase to an implementer agent. Validates cross-phase consistency and completeness.
---

## Phase Continuity Review

Run this review after all phases are drafted, before handing any phase to an
implementer agent. This is the planning agent's responsibility — it is the only
agent with visibility across all phases and knowledge of the reasoning behind
the phase split.

Walk the full sequence end-to-end and verify each of the following. If any check
fails, revise the relevant phases before handing them off. Implementer agents
cannot recover from plan-level errors — they will execute exactly what the phase
doc says, with no ability to course-correct across phase boundaries.

These checks verify compliance with the Phase Planning Rules. They introduce no
new requirements — if a check references something, the planning rules already
mandate it.

1. **No orphaned dependencies.**
   Every type, interface, function, or module that a phase consumes must be
   introduced in a prior phase or already exist in the codebase. If phase N
   references something, you must be able to point to the exact phase (≤ N)
   where it was created. An orphaned dependency will surface as a compile error
   that the implementer agent must troubleshoot without understanding the
   cross-phase intent behind it.
   _Verifies: rules 3, 4 (compilation safety and shippability)._

2. **Prerequisite graph is valid.**
   Confirm every phase declares its prerequisites (rule 6). Verify there are
   no circular dependencies. Confirm that any phases declared as parallelizable
   have no actual data or interface dependencies on each other. The planning
   agent uses this graph to determine handoff order — errors here mean an
   implementer receives a codebase that doesn't match its "Inherits From"
   expectations.
   _Verifies: rule 6 (declare prerequisites explicitly)._

3. **Boundary contracts match.**
   For each adjacent phase pair (N → N+1): confirm phase N's "Changes
   Introduced" section exists and that phase N+1's "Inherits From" section
   acknowledges it. If the output of phase N does not match the input
   expectations of phase N+1, the implementer for phase N+1 will be working
   from a false description of the codebase.
   _Verifies: rule 7 (document the phase boundary)._

4. **Bridge code ledger is complete.**
   Collect all bridge code tagged across every phase. Verify that every entry
   has a removal target phase (rule 8) and that the target phase's doc includes
   an explicit removal task. If any bridge code survives the final phase with
   no removal, the plan is incomplete. Untracked bridge code is unlikely to be
   cleaned up — implementer agents follow their phase doc's tasks, not hunt
   for unmarked technical debt.
   _Verifies: rules 4, 8 (compilation safety and bridge code tagging)._

5. **No silent behavior changes.**
   At each phase boundary, verify the user-visible behaviors listed in the
   prior phase still hold. If a phase deprecates a behavior, confirm it is
   explicitly marked as deprecated in that phase's doc — not silently dropped.
   Implementer agents use the behavior list as acceptance criteria (rule 9);
   an incomplete list means the implementer has no way to catch a regression.
   _Verifies: rule 9 (preserve user-visible behavior)._

6. **Task count bounds.**
   Confirm every phase has 4–6 tasks. Flag any phase outside this range and
   verify it either needs splitting (>6) or has a documented reason to be
   narrow (<4). Oversized phases increase the risk of an implementer agent
   losing coherence mid-execution.
   _Verifies: rule 5 (cap each phase at 4–6 tasks)._

7. **Self-containment check.**
   Read each phase doc on its own. For every task, confirm the doc provides
   enough information to execute it without depending on context buried in
   other phase docs or unwritten assumptions. The implementer has the design
   spec and other phases for reference, but should not need to piece together
   instructions from multiple documents to complete a task. If a task only
   makes sense after reading another phase doc, the task is underspecified.
   _Verifies: rule 2 (the phase doc is the implementer's primary directive)._
