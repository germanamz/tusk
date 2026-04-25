---
name: phase-post-implementation-review
description: Post-implementation review and verification checklist for the planning agent after phases are implemented. Covers per-phase gates and final sequence validation.
---

## Post-Implementation Review and Verification

This review is performed by the planning agent — the same agent that drafted the
phases and ran the continuity review. It is the only agent that understands the
reasoning behind the phase split and has visibility across the full plan.

Each phase is implemented by a separate implementer agent. While implementer agents
have access to the design spec and all phase docs in the repository for reference,
they cannot coordinate with each other or flag cross-phase issues during execution.
That makes this review the only point where plan-level intent is compared against
actual code across the full sequence.

### Per-Phase Verification

Perform these checks after each phase is implemented, before handing the codebase
to the next implementer agent. This is the planning agent's gate between phases —
if a check fails, fix the issue before proceeding. Errors that pass this gate will
compound, and no downstream implementer agent can detect or correct them.

1. **Compilation and type-checking.**
   The codebase must compile and pass all type checks with zero errors and zero
   new warnings. Run the full build, not just the files the implementer touched.
   If the project has a strict/pedantic compiler mode, use it. A compile failure
   here means the next implementer agent inherits a broken starting point.
   _Verifies: planning rule 4 (compilation safety)._

2. **Task completion is literal.**
   Walk the phase doc task-by-task. For each task, identify the exact code
   change that satisfies it. If a task cannot be mapped to a concrete change,
   it was either skipped, done implicitly inside another task, or the plan was
   wrong. All three are failures. A skipped task usually means the phase doc
   was ambiguous or underspecified — flag it and fix the doc for future
   reference even if you fix the code now.
   _Verifies: planning rule 2 (phase doc is the implementer's primary directive)._

3. **Shippability gate.**
   The system must be deployable and functional at this point. Run the
   application. Verify it starts, serves traffic (or performs its core
   function), and does not crash. If the project has a staging environment
   or deploy script, execute it. "It compiles" is not the same as "it ships."
   _Verifies: planning rule 3 (independently shippable)._

4. **Behavioral regression.**
   Execute the user-visible behaviors listed in the phase doc (planning rule 9).
   Every behavior from prior phases that was not explicitly deprecated must
   still work. If the project has automated tests, run the full suite — not
   just tests related to the current phase. If it does not, manually verify
   each listed behavior. The implementer agent treated this list as acceptance
   criteria; the planning agent now validates that the criteria were actually met.
   _Verifies: planning rule 9 (preserve user-visible behavior)._

5. **Boundary contract fulfillment.**
   Compare what was actually changed against the phase doc's "Changes Introduced"
   section (planning rule 7). Check for:
   - Changes listed in the doc that were not implemented.
   - Changes made in code that are not listed in the doc.
   - Interface signatures, environment variables, or schemas that differ from
     what the doc specified.
   If the next phase's "Inherits From" section no longer matches reality,
   update the next phase's doc before handing it to its implementer agent.
   Never hand an implementer a doc whose "Inherits From" is stale.
   _Verifies: planning rule 7 (document the phase boundary)._

6. **Bridge code audit.**
   Verify all bridge code introduced in this phase is tagged with a removal
   target (planning rule 8). Verify all bridge code scheduled for removal in
   this phase has actually been removed. Check that no untagged stubs, no-ops,
   or feature flags were introduced outside of the plan. Implementer agents
   sometimes create their own workarounds when they encounter something
   unexpected — these unplanned shims must be caught here.
   _Verifies: planning rules 4, 8 (compilation safety and bridge code tagging)._

7. **No scope creep, no deferred shortcuts.**
   The implementation must match the phase scope — nothing more, nothing less.
   Flag any of the following:
   - Work done that belongs to a later phase (pulled forward).
   - Work skipped with a TODO/FIXME/HACK pointing at a later phase (pushed back).
   - Unplanned refactors, dependency upgrades, or "while I'm here" changes.
   Implementer agents, even with access to the broader plan for reference, are
   prone to making locally reasonable decisions that contaminate the phase
   boundary. Any scope deviation invalidates the continuity review's assumptions
   about downstream phases — if found, re-evaluate affected phase docs before
   continuing.
   _Verifies: planning rules 5, 6, 7 (task cap, prerequisites, boundary contracts)._

### Final Sequence Verification

Run these checks once after all phases are implemented. This is the planning agent's
final pass — the only review that sees the codebase as a whole through the lens of
the original plan.

1. **Bridge code is fully resolved.**
   The bridge code ledger from the continuity review should now be empty.
   Search the codebase for any remaining stubs, no-ops, or feature flags that
   were introduced as bridge code. Include unplanned shims discovered during
   per-phase bridge code audits. If any survive, the implementation is
   incomplete.

2. **Full behavioral sweep.**
   Collect every user-visible behavior listed across all phase docs. Verify
   each one works in the final state of the codebase. This catches regressions
   that may have been introduced in the final phase where no subsequent
   per-phase review would have caught them.

3. **Plan-to-code reconciliation.**
   Walk every task across every phase doc. Confirm each maps to committed code.
   Confirm no committed code exists that is not accounted for in any phase doc.
   The plan and the codebase should be a 1:1 match at completion. Deviations
   found during per-phase reviews (check 7) should already have been resolved —
   this is the final confirmation.

4. **Clean build from scratch.**
   Clone the repository fresh. Install dependencies. Build. Run. Verify the
   system works end-to-end with no reliance on local state, caches, or manual
   steps accumulated during the multi-agent implementation process.

5. **Cross-phase coherence.**
   Review the codebase for stylistic and architectural consistency. Multiple
   implementer agents will produce code with different patterns, naming
   conventions, and structural preferences. Identify inconsistencies that
   affect maintainability and flag them for a final normalization pass if
   needed. This is expected — it is a natural consequence of the multi-agent
   model, not a failure of any individual implementer.

6. **Plan doc cleanup.**
   Remove all phase plan docs, the design spec, and any planning artifacts
   from the repository. These documents served as coordination scaffolding
   for the multi-agent implementation process and should not persist in the
   codebase after verification is complete.
