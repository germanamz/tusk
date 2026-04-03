# Completion Propagation — Phase 3: Auto-Revert Logic

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement auto-revert propagation — when a child moves away from the trigger status, automatically revert the parent (and ancestors) to the configured target status.

**Architecture:** A new `checkAutoRevert` method in `TaskService` mirrors `checkAutoComplete`. It fires when a task transitions *away from* the auto-revert trigger status, checks if the parent is at the auto-complete target status (meaning it was likely auto-completed), validates the workflow transition, and reverts the parent. Recurses up the ancestor chain.

**Tech Stack:** Go, SQLite

**Spec:** `docs/superpowers/specs/2026-04-03-completion-propagation-design.md`

**Prerequisite:** Phase 2 must be completed (TaskTxProvider, auto-complete logic, transactional Update).

---

### Task 1: Auto-Revert Propagation Logic

**Files:**
- Modify: `internal/service/task.go` (add `checkAutoRevert` method, wire into transaction)
- Modify: `internal/service/task_test.go` (add auto-revert tests)

- [ ] **Step 1: Write tests for auto-revert**

Add to `internal/service/task_test.go`:

```go
func TestAutoRevert_ChildReopened(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Enable both auto-complete and auto-revert
	projRepo := sqlite.NewProjectRepo(env.store.DB())
	proj, _ := projRepo.GetByID(ctx, DefaultProjectID)
	proj.Settings = domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
		AutoRevertParent: &domain.AutoRevertConfig{
			TriggerStatus: "completed",
			TargetStatus:  "active",
		},
	}
	projRepo.Update(ctx, proj)

	// Create parent + child
	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version)

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version)

	// Complete child -> parent auto-completes
	child, err := env.taskSvc.Complete(ctx, child.ShortID, child.Version)
	if err != nil {
		t.Fatalf("Complete child: %v", err)
	}
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		t.Fatalf("expected parent 'completed' after child completed, got %q", parentCheck.Status)
	}

	// Re-open child (completed -> pending)
	child, _ = env.taskSvc.GetByShortID(ctx, child.ShortID)
	child, err = env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Status:  ptr("pending"),
	})
	if err != nil {
		t.Fatalf("Reopen child: %v", err)
	}

	// Parent should be reverted to "active"
	parentCheck, _ = env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "active" {
		t.Fatalf("expected parent 'active' after child reopened, got %q", parentCheck.Status)
	}
}

func TestAutoRevert_Disabled(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Enable auto-complete but NOT auto-revert
	projRepo := sqlite.NewProjectRepo(env.store.DB())
	proj, _ := projRepo.GetByID(ctx, DefaultProjectID)
	proj.Settings = domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
		// AutoRevertParent intentionally nil
	}
	projRepo.Update(ctx, proj)

	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version)

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version)

	// Complete child -> parent auto-completes
	child, _ = env.taskSvc.Complete(ctx, child.ShortID, child.Version)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		t.Fatalf("expected parent 'completed', got %q", parentCheck.Status)
	}

	// Re-open child — parent should NOT revert (auto-revert disabled)
	child, _ = env.taskSvc.GetByShortID(ctx, child.ShortID)
	env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Status:  ptr("pending"),
	})

	parentCheck, _ = env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		t.Fatalf("expected parent still 'completed' (revert disabled), got %q", parentCheck.Status)
	}
}

func TestAutoRevert_Recursive(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Enable both auto-complete and auto-revert
	projRepo := sqlite.NewProjectRepo(env.store.DB())
	proj, _ := projRepo.GetByID(ctx, DefaultProjectID)
	proj.Settings = domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
		AutoRevertParent: &domain.AutoRevertConfig{
			TriggerStatus: "completed",
			TargetStatus:  "active",
		},
	}
	projRepo.Update(ctx, proj)

	// grandparent -> parent -> child
	grandparent := newMinimalTask("Grandparent")
	mustCreateTask(t, env.taskSvc, grandparent)
	grandparent, _ = env.taskSvc.Start(ctx, grandparent.ShortID, grandparent.Version)

	parent := &domain.Task{Title: "Parent", ParentID: &grandparent.ID}
	mustCreateTask(t, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version)

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version)

	// Complete child — cascades up
	child, _ = env.taskSvc.Complete(ctx, child.ShortID, child.Version)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	grandparentCheck, _ := env.taskSvc.GetByShortID(ctx, grandparent.ShortID)
	if parentCheck.Status != "completed" || grandparentCheck.Status != "completed" {
		t.Fatalf("expected both completed, got parent=%q grandparent=%q", parentCheck.Status, grandparentCheck.Status)
	}

	// Re-open child — should cascade revert
	child, _ = env.taskSvc.GetByShortID(ctx, child.ShortID)
	child, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Status:  ptr("pending"),
	})
	if err != nil {
		t.Fatalf("Reopen child: %v", err)
	}

	parentCheck, _ = env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "active" {
		t.Fatalf("expected parent 'active' after revert, got %q", parentCheck.Status)
	}

	grandparentCheck, _ = env.taskSvc.GetByShortID(ctx, grandparent.ShortID)
	if grandparentCheck.Status != "active" {
		t.Fatalf("expected grandparent 'active' after revert, got %q", grandparentCheck.Status)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v ./internal/service -run "TestAutoRevert_"`
Expected: FAIL — `TestAutoRevert_ChildReopened` and `TestAutoRevert_Recursive` fail because no revert logic exists. `TestAutoRevert_Disabled` should pass (it asserts revert does NOT happen).

- [ ] **Step 3: Implement checkAutoRevert**

Add the following method to `internal/service/task.go`:

```go
// checkAutoRevert checks whether a task moving away from the trigger status
// should revert its parent. If the parent was at the auto-complete target
// status (presumably auto-completed) and the workflow allows the revert
// transition, the parent is reverted. Recurses up the ancestor chain.
func (s *TaskService) checkAutoRevert(
	ctx context.Context,
	task *domain.Task,
	oldStatus string,
	txTaskRepo repository.TaskRepository,
	txProjectRepo repository.ProjectRepository,
) error {
	if task.ParentID == nil {
		return nil
	}

	parent, err := txTaskRepo.GetByID(ctx, *task.ParentID)
	if err != nil {
		return fmt.Errorf("loading parent for revert: %w", err)
	}

	if parent.ProjectID == nil {
		return nil
	}

	project, err := txProjectRepo.GetByID(ctx, *parent.ProjectID)
	if err != nil {
		return fmt.Errorf("loading project for revert: %w", err)
	}

	revertCfg := project.Settings.AutoRevertParent
	if revertCfg == nil {
		return nil
	}

	// Only trigger if the child moved AWAY FROM the trigger status
	if oldStatus != revertCfg.TriggerStatus {
		return nil
	}
	// And the child is no longer at the trigger status
	if task.Status == revertCfg.TriggerStatus {
		return nil
	}

	// Only revert if the parent is at the auto-complete target status
	completeCfg := project.Settings.AutoCompleteParent
	if completeCfg == nil {
		return nil
	}
	if parent.Status != completeCfg.TargetStatus {
		return nil
	}

	// Validate workflow transition
	allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, *parent.ProjectID, project.DefaultWorkflow, parent.Status, revertCfg.TargetStatus)
	if err != nil {
		return fmt.Errorf("checking revert transition: %w", err)
	}
	if !allowed {
		return nil
	}

	// Revert the parent
	oldParentStatus := parent.Status
	parent.Status = revertCfg.TargetStatus
	parent.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)
	if err := txTaskRepo.Update(ctx, parent); err != nil {
		return fmt.Errorf("reverting parent: %w", err)
	}

	// Recurse up the ancestor chain
	updatedParent, err := txTaskRepo.GetByID(ctx, parent.ID)
	if err != nil {
		return fmt.Errorf("re-reading parent after revert: %w", err)
	}
	return s.checkAutoRevert(ctx, updatedParent, oldParentStatus, txTaskRepo, txProjectRepo)
}
```

- [ ] **Step 4: Wire checkAutoRevert into the transaction in Update()**

In `internal/service/task.go`, find the `WithTaskTx` callback. Currently it has:

```go
			// Auto-complete propagation: check if parent should be auto-completed
			return s.checkAutoComplete(ctx, updated, txTaskRepo, txProjectRepo)
```

Replace with:

```go
			// Auto-complete propagation
			if err := s.checkAutoComplete(ctx, updated, txTaskRepo, txProjectRepo); err != nil {
				return err
			}
			// Auto-revert propagation
			return s.checkAutoRevert(ctx, updated, oldStatus, txTaskRepo, txProjectRepo)
```

Note: The `oldStatus` variable is already captured earlier in `Update()` (line `oldStatus := task.Status`). It is accessible inside the closure because Go closures capture outer variables.

- [ ] **Step 5: Run the auto-revert tests**

Run: `go test -v ./internal/service -run "TestAutoRevert_"`
Expected: PASS — all three test cases pass.

- [ ] **Step 6: Run the full test suite**

Run: `make test`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/service/task.go internal/service/task_test.go
git commit -m "feat(service): add auto-revert parent propagation logic"
```

---

### Task 2: Custom Trigger/Target Status Test

**Files:**
- Modify: `internal/service/task_test.go`

This verifies that non-default trigger/target statuses work correctly.

- [ ] **Step 1: Write a test with custom statuses**

The default workflow has statuses: `pending`, `active`, `completed`, `deleted`. The transitions include `active -> completed` and `completed -> pending`. We can configure auto-complete to trigger on `active` and target `active` — but this doesn't make practical sense. Instead, let's test a realistic scenario: trigger on `completed`, target `completed` (standard), but with auto-revert targeting `pending` instead of `active`.

Add to `internal/service/task_test.go`:

```go
func TestAutoRevert_CustomTargetStatus(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Auto-revert targets "pending" instead of "active"
	projRepo := sqlite.NewProjectRepo(env.store.DB())
	proj, _ := projRepo.GetByID(ctx, DefaultProjectID)
	proj.Settings = domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
		AutoRevertParent: &domain.AutoRevertConfig{
			TriggerStatus: "completed",
			TargetStatus:  "pending",
		},
	}
	projRepo.Update(ctx, proj)

	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version)

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version)

	// Complete child -> parent auto-completes
	child, _ = env.taskSvc.Complete(ctx, child.ShortID, child.Version)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		t.Fatalf("expected parent 'completed', got %q", parentCheck.Status)
	}

	// Re-open child -> parent should revert to "pending" (not "active")
	child, _ = env.taskSvc.GetByShortID(ctx, child.ShortID)
	env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Status:  ptr("pending"),
	})

	parentCheck, _ = env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "pending" {
		t.Fatalf("expected parent 'pending' (custom revert target), got %q", parentCheck.Status)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test -v ./internal/service -run TestAutoRevert_CustomTargetStatus`
Expected: PASS — the configurable target status is used correctly.

- [ ] **Step 3: Run the full test suite**

Run: `make test`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/service/task_test.go
git commit -m "test(service): add custom trigger/target status propagation test"
```

---

### Task 3: Run Full Suite with Race Detector and Lint

**Files:**
- No new files — verification only

- [ ] **Step 1: Run the full test suite with race detector**

Run: `make test-race`
Expected: PASS — no race conditions. The transactional propagation should be safe because SQLite serializes writes.

- [ ] **Step 2: Run vet and lint**

Run: `make vet && make lint`
Expected: PASS — no issues.

- [ ] **Step 3: Run E2E tests**

Run: `make test-e2e`
Expected: PASS — existing E2E tests unaffected (propagation is disabled by default).
