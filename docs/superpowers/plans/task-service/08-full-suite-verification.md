# Phase 8: Full Test Suite Verification

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Run the complete test suite across all packages to verify no regressions.

**Prereqs:** All previous phases (1–7) must be complete.

**Files:** None — this is a verification-only phase.

---

## Task 1: Run the full test suite

- [ ] **Step 1: Run all service tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -v`

Expected: all 45 tests PASS:
- 5 WorkflowService tests
- 10 TaskService Create tests
- 7 TaskService read tests
- 9 TaskService Update tests
- 6 TaskService convenience transition tests
- 8 TaskService annotation tests

- [ ] **Step 2: Run the full project test suite**

Run: `cd /Users/germanamz/projects/tusk && go test ./... -v`

Expected: all tests PASS across all packages:
- `internal/sqlite/` — all existing repo tests
- `internal/service/` — all new service tests

If any test fails, investigate the failure:
- Read the error message carefully
- Check if it's a test you wrote or a pre-existing test
- If it's a pre-existing test, check if your changes broke something (e.g., a compile error in a package that imports `domain`)
- Fix the issue, run the failing test again, then re-run the full suite

- [ ] **Step 3: Run `go vet` to check for issues**

Run: `cd /Users/germanamz/projects/tusk && go vet ./...`

Expected: no output (clean). If there are warnings, fix them.

- [ ] **Step 4: Verify the build**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`

Expected: no output (clean compile).

- [ ] **Step 5: Final commit if any fixes were needed**

If you had to fix anything in the previous steps, commit those fixes:

```bash
git add -A
git commit -m "fix(service): address test failures in full suite verification"
```

If everything passed with no changes, no commit needed. You're done!

---

## Summary of what was built

After completing all 8 phases, the codebase has:

| Component | File | Methods |
|---|---|---|
| `TaskUpdate` struct | `internal/domain/task.go` | — |
| `WorkflowService` | `internal/service/workflow.go` | `IsTransitionAllowed`, `GetStatuses` |
| `TaskService` | `internal/service/task.go` | `Create`, `GetByShortID`, `GetByID`, `List`, `GetChildren`, `GetDescendants`, `Update`, `Start`, `Complete`, `Delete`, `Annotate`, `GetAnnotations`, `DeleteAnnotation` |

All methods are integration-tested against a real SQLite database.
