package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// DefaultProjectID is the string ID of the default project from config.
// Tasks created without an explicit ProjectID are assigned to this project.
const DefaultProjectID = "default"

// TaskTxProvider gives TaskService a way to run task + project operations
// inside a database transaction for atomic propagation.
// The SQLite Store implements this via its WithTaskTx method.
type TaskTxProvider interface {
	WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository) error) error
}

// TaskService implements task business logic including validation,
// workflow enforcement, and optimistic locking.
type TaskService struct {
	taskRepo       repository.TaskRepository
	annotationRepo repository.AnnotationRepository
	projectRepo    repository.ProjectRepository
	workflowSvc    *WorkflowService
	txProvider     TaskTxProvider
}

// NewTaskService creates a new TaskService with the given dependencies.
func NewTaskService(
	tr repository.TaskRepository,
	ar repository.AnnotationRepository,
	pr repository.ProjectRepository,
	ws *WorkflowService,
	txp TaskTxProvider,
) *TaskService {
	return &TaskService{
		taskRepo:       tr,
		annotationRepo: ar,
		projectRepo:    pr,
		workflowSvc:    ws,
		txProvider:     txp,
	}
}

// Create validates and persists a new task. It populates the task's ID,
// ShortID, Version, timestamps, and default ProjectID before saving.
func (s *TaskService) Create(ctx context.Context, task *domain.Task) error {
	// Validate title
	if strings.TrimSpace(task.Title) == "" {
		return fmt.Errorf("title must not be empty")
	}

	// Validate priority
	if task.Priority < 0 || task.Priority > 4 {
		return fmt.Errorf("priority must be between 0 and 4")
	}

	// Assign default project if not set
	if task.ProjectID == "" {
		task.ProjectID = DefaultProjectID
	}

	// Validate project exists
	project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("project not found: %w", err)
		}
		return fmt.Errorf("looking up project: %w", err)
	}

	// Validate parent exists and would not create a cycle
	if task.ParentID != nil {
		_, err := s.taskRepo.GetByID(ctx, *task.ParentID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return fmt.Errorf("parent task not found: %w", err)
			}
			return fmt.Errorf("looking up parent task: %w", err)
		}
		// Note: On Create, cycles are impossible because the new task has no ID yet
		// that other tasks could reference as a parent. The cycle check is only
		// meaningful in Update. We keep the existence check here for clarity.
	}

	// Default and validate status
	if task.Status == "" {
		task.Status = "pending"
	}
	statuses, err := s.workflowSvc.GetStatuses(ctx, project.Workflow)
	if err != nil {
		return fmt.Errorf("loading workflow statuses: %w", err)
	}
	validStatus := false
	for _, st := range statuses {
		if st == task.Status {
			validStatus = true
			break
		}
	}
	if !validStatus {
		return fmt.Errorf("status %q is not valid for workflow %q", task.Status, project.Workflow)
	}

	// Generate ID and ShortID
	task.ID = uuid.New()
	shortID, err := s.generateShortID(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("generating short ID: %w", err)
	}
	task.ShortID = shortID

	// Set metadata
	now := time.Now().UTC().Truncate(time.Millisecond)
	task.Version = 1
	task.CreatedAt = now
	task.ModifiedAt = now
	if task.UDA == nil {
		task.UDA = map[string]any{}
	}

	return s.taskRepo.Create(ctx, task)
}

// GetByShortID retrieves a task by its short ID.
func (s *TaskService) GetByShortID(ctx context.Context, shortID string) (*domain.Task, error) {
	return s.taskRepo.GetByShortID(ctx, shortID)
}

// GetByID retrieves a task by its full UUID.
func (s *TaskService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	return s.taskRepo.GetByID(ctx, id)
}

// List returns tasks matching the given filter.
func (s *TaskService) List(ctx context.Context, filter domain.TaskFilter) ([]*domain.Task, error) {
	return s.taskRepo.List(ctx, filter)
}

// GetChildren returns the direct children of a task.
func (s *TaskService) GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error) {
	return s.taskRepo.GetChildren(ctx, parentID)
}

// GetDescendants returns all descendants of a task (recursive).
func (s *TaskService) GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error) {
	return s.taskRepo.GetDescendants(ctx, rootID)
}

// Update applies a partial update to a task. It validates the patched state,
// enforces workflow transitions, and uses optimistic locking.
// Returns the updated task with the new version number.
func (s *TaskService) Update(ctx context.Context, upd domain.TaskUpdate) (*domain.Task, error) {
	// Load current task
	task, err := s.taskRepo.GetByShortID(ctx, upd.ShortID)
	if err != nil {
		return nil, err
	}

	// Early version check
	if task.Version != upd.Version {
		return nil, domain.ErrConflict
	}

	oldStatus := task.Status

	// Apply patch
	if upd.Title != nil {
		task.Title = *upd.Title
	}
	if upd.Description != nil {
		if *upd.Description == nil {
			task.Description = ""
		} else {
			task.Description = **upd.Description
		}
	}
	if upd.Status != nil {
		task.Status = *upd.Status
	}
	if upd.Priority != nil {
		task.Priority = *upd.Priority
	}
	if upd.ParentID != nil {
		task.ParentID = *upd.ParentID
	}
	if upd.ProjectID != nil {
		task.ProjectID = *upd.ProjectID
	}
	if upd.DueAt != nil {
		task.DueAt = *upd.DueAt
	}
	if upd.WaitUntil != nil {
		task.WaitUntil = *upd.WaitUntil
	}
	if upd.RecurrenceRule != nil {
		task.RecurrenceRule = *upd.RecurrenceRule
	}
	if upd.UDA != nil {
		if err := domain.ValidateUDA(*upd.UDA); err != nil {
			return nil, err
		}
		if task.UDA == nil {
			task.UDA = map[string]any{}
		}
		for k, v := range *upd.UDA {
			if v == "" {
				delete(task.UDA, k)
			} else {
				task.UDA[k] = v
			}
		}
	}

	// Validate patched state
	if strings.TrimSpace(task.Title) == "" {
		return nil, fmt.Errorf("title must not be empty")
	}
	if task.Priority < 0 || task.Priority > 4 {
		return nil, fmt.Errorf("priority must be between 0 and 4")
	}

	// Validate parent if changed
	if upd.ParentID != nil && task.ParentID != nil {
		if *task.ParentID == task.ID {
			return nil, fmt.Errorf("task cannot be its own parent")
		}
		_, err := s.taskRepo.GetByID(ctx, *task.ParentID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, fmt.Errorf("parent task not found: %w", err)
			}
			return nil, fmt.Errorf("looking up parent task: %w", err)
		}
		// Check for cycles: walk up from proposed parent — if we reach this task, it's a cycle
		if err := s.detectParentCycle(ctx, task.ID, *task.ParentID); err != nil {
			return nil, err
		}
	}

	// Validate project if changed
	if upd.ProjectID != nil {
		if task.ProjectID == "" {
			return nil, fmt.Errorf("task must belong to a project")
		}
		_, err := s.projectRepo.GetByID(ctx, task.ProjectID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, fmt.Errorf("project not found: %w", err)
			}
			return nil, fmt.Errorf("looking up project: %w", err)
		}
	}

	// Workflow validation for status changes
	if task.Status != oldStatus {
		project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("looking up project for workflow: %w", err)
		}
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, project.Workflow, oldStatus, task.Status)
		if err != nil {
			return nil, fmt.Errorf("checking transition: %w", err)
		}
		if !allowed {
			return nil, fmt.Errorf("transition %q → %q not allowed: %w", oldStatus, task.Status, domain.ErrInvalidTransition)
		}
	}

	// Update metadata
	task.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)

	statusChanged := task.Status != oldStatus

	// If status changed, wrap persist + propagation in a transaction.
	// Otherwise, persist directly (no transaction needed).
	if statusChanged && s.txProvider != nil {
		var result *domain.Task
		err := s.txProvider.WithTaskTx(ctx, func(txTaskRepo repository.TaskRepository) error {
			if err := txTaskRepo.Update(ctx, task); err != nil {
				return err
			}
			updated, err := txTaskRepo.GetByID(ctx, task.ID)
			if err != nil {
				return err
			}
			result = updated
			// Propagation: auto-complete and auto-revert are mutually exclusive
			// in practice — a single status change cannot simultaneously reach
			// and leave the trigger status — so at most one of these fires.
			if err := s.checkAutoComplete(ctx, updated, txTaskRepo); err != nil {
				return err
			}
			return s.checkAutoRevert(ctx, updated, oldStatus, txTaskRepo)
		})
		if err != nil {
			return nil, err
		}
		return result, nil
	}

	// Non-status-change path: persist directly
	if err := s.taskRepo.Update(ctx, task); err != nil {
		return nil, err
	}
	return s.taskRepo.GetByID(ctx, task.ID)
}

// Start transitions a task from its current status to "active".
func (s *TaskService) Start(ctx context.Context, shortID string, version int) (*domain.Task, error) {
	return s.Update(ctx, domain.TaskUpdate{
		ShortID: shortID,
		Version: version,
		Status:  ptr("active"),
	})
}

// Complete transitions a task from its current status to "completed".
func (s *TaskService) Complete(ctx context.Context, shortID string, version int) (*domain.Task, error) {
	return s.Update(ctx, domain.TaskUpdate{
		ShortID: shortID,
		Version: version,
		Status:  ptr("completed"),
	})
}

// Delete soft-deletes a task by transitioning its status to "deleted".
func (s *TaskService) Delete(ctx context.Context, shortID string, version int) (*domain.Task, error) {
	return s.Update(ctx, domain.TaskUpdate{
		ShortID: shortID,
		Version: version,
		Status:  ptr("deleted"),
	})
}

// maxParentDepth is the maximum ancestor chain length detectParentCycle will walk
// before returning an error. This guards against corrupted data causing infinite loops.
const maxParentDepth = 100

// detectParentCycle walks up the ancestor chain from proposedParentID.
// If it encounters taskID, the proposed parent relationship would create a cycle.
// Returns ErrCyclicParent if a cycle is detected, nil otherwise.
func (s *TaskService) detectParentCycle(ctx context.Context, taskID, proposedParentID uuid.UUID) error {
	current := proposedParentID
	for depth := 0; depth < maxParentDepth; depth++ {
		if current == taskID {
			return domain.ErrCyclicParent
		}
		parent, err := s.taskRepo.GetByID(ctx, current)
		if err != nil {
			return fmt.Errorf("checking parent cycle: %w", err)
		}
		if parent.ParentID == nil {
			return nil
		}
		current = *parent.ParentID
	}
	return fmt.Errorf("parent chain exceeds maximum depth (%d)", maxParentDepth)
}

// generateShortID derives a short ID from the task's UUID.
// It starts with 8 hex characters and extends if a collision is detected.
func (s *TaskService) generateShortID(ctx context.Context, id uuid.UUID) (string, error) {
	hex := strings.ReplaceAll(id.String(), "-", "")
	for length := 8; length <= len(hex); length++ {
		candidate := hex[:length]
		_, err := s.taskRepo.GetByShortID(ctx, candidate)
		if errors.Is(err, domain.ErrNotFound) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		// Collision — try longer prefix
	}
	return "", fmt.Errorf("could not generate unique short ID")
}

// Annotate adds a timestamped note to a task.
func (s *TaskService) Annotate(ctx context.Context, taskShortID string, body string) (*domain.Annotation, error) {
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("annotation body must not be empty")
	}

	task, err := s.taskRepo.GetByShortID(ctx, taskShortID)
	if err != nil {
		return nil, err
	}

	ann := &domain.Annotation{
		ID:        uuid.New(),
		TaskID:    task.ID,
		Body:      body,
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	if err := s.annotationRepo.Create(ctx, ann); err != nil {
		return nil, err
	}
	return ann, nil
}

// GetAnnotations returns all annotations for a task, identified by short ID.
func (s *TaskService) GetAnnotations(ctx context.Context, taskShortID string) ([]*domain.Annotation, error) {
	task, err := s.taskRepo.GetByShortID(ctx, taskShortID)
	if err != nil {
		return nil, err
	}
	return s.annotationRepo.GetByTask(ctx, task.ID)
}

// DeleteAnnotation removes an annotation by its ID.
func (s *TaskService) DeleteAnnotation(ctx context.Context, annotationID uuid.UUID) error {
	return s.annotationRepo.Delete(ctx, annotationID)
}

// checkAutoComplete checks whether completing a task should trigger automatic
// completion of its parent. If the task has a parent, all non-deleted siblings
// are at the trigger status, and the workflow allows the transition, the parent
// is auto-completed. Walks up the ancestor chain iteratively, bounded by
// maxParentDepth to guard against corrupted data.
func (s *TaskService) checkAutoComplete(
	ctx context.Context,
	task *domain.Task,
	txTaskRepo repository.TaskRepository,
) error {
	current := task
	for depth := 0; depth < maxParentDepth; depth++ {
		if current.ParentID == nil {
			return nil
		}

		parent, err := txTaskRepo.GetByID(ctx, *current.ParentID)
		if err != nil {
			return fmt.Errorf("loading parent for propagation: %w", err)
		}

		if parent.ProjectID == "" {
			return nil
		}

		project, err := s.projectRepo.GetByID(ctx, parent.ProjectID)
		if err != nil {
			return fmt.Errorf("loading project for propagation: %w", err)
		}

		cfg := project.Settings.AutoCompleteParent
		if cfg == nil {
			return nil
		}

		// Check if the completed task reached the trigger status
		if current.Status != cfg.TriggerStatus {
			return nil
		}

		// Load all children of the parent
		children, err := txTaskRepo.GetChildren(ctx, parent.ID)
		if err != nil {
			return fmt.Errorf("loading siblings for propagation: %w", err)
		}

		// Check if all non-deleted children are at the trigger status
		allReady := true
		for _, child := range children {
			if child.Status == "deleted" {
				continue
			}
			if child.Status != cfg.TriggerStatus {
				allReady = false
				break
			}
		}
		if !allReady {
			return nil
		}

		// Validate workflow transition for the parent
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, project.Workflow, parent.Status, cfg.TargetStatus)
		if err != nil {
			return fmt.Errorf("checking propagation transition: %w", err)
		}
		if !allowed {
			return nil // workflow doesn't allow it — silently skip
		}

		// Auto-complete the parent
		parent.Status = cfg.TargetStatus
		parent.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)
		if err := txTaskRepo.Update(ctx, parent); err != nil {
			return fmt.Errorf("auto-completing parent: %w", err)
		}

		// Re-read to get bumped version, then continue up the chain
		current, err = txTaskRepo.GetByID(ctx, parent.ID)
		if err != nil {
			return fmt.Errorf("re-reading parent after propagation: %w", err)
		}
	}
	return fmt.Errorf("auto-complete propagation exceeded maximum depth (%d)", maxParentDepth)
}

// checkAutoRevert checks whether a task moving away from the trigger status
// should revert its parent. If the parent was at the auto-complete target
// status (presumably auto-completed) and the workflow allows the revert
// transition, the parent is reverted. Walks up the ancestor chain iteratively,
// bounded by maxParentDepth to guard against corrupted data.
func (s *TaskService) checkAutoRevert(
	ctx context.Context,
	task *domain.Task,
	oldStatus string,
	txTaskRepo repository.TaskRepository,
) error {
	current := task
	currentOldStatus := oldStatus
	for depth := 0; depth < maxParentDepth; depth++ {
		if current.ParentID == nil {
			return nil
		}

		parent, err := txTaskRepo.GetByID(ctx, *current.ParentID)
		if err != nil {
			return fmt.Errorf("loading parent for revert: %w", err)
		}

		if parent.ProjectID == "" {
			return nil
		}

		project, err := s.projectRepo.GetByID(ctx, parent.ProjectID)
		if err != nil {
			return fmt.Errorf("loading project for revert: %w", err)
		}

		revertCfg := project.Settings.AutoRevertParent
		if revertCfg == nil {
			return nil
		}

		// Only trigger if the child moved AWAY FROM the trigger status
		if currentOldStatus != revertCfg.TriggerStatus {
			return nil
		}
		// And the child is no longer at the trigger status
		if current.Status == revertCfg.TriggerStatus {
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
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, project.Workflow, parent.Status, revertCfg.TargetStatus)
		if err != nil {
			return fmt.Errorf("checking revert transition: %w", err)
		}
		if !allowed {
			return nil
		}

		// Revert the parent
		prevParentStatus := parent.Status
		parent.Status = revertCfg.TargetStatus
		parent.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)
		if err := txTaskRepo.Update(ctx, parent); err != nil {
			return fmt.Errorf("reverting parent: %w", err)
		}

		// Re-read to get bumped version, then continue up the chain
		current, err = txTaskRepo.GetByID(ctx, parent.ID)
		if err != nil {
			return fmt.Errorf("re-reading parent after revert: %w", err)
		}
		currentOldStatus = prevParentStatus
	}
	return fmt.Errorf("auto-revert propagation exceeded maximum depth (%d)", maxParentDepth)
}

// ptr is a generic helper that returns a pointer to the given value.
func ptr[T any](v T) *T {
	return &v
}
