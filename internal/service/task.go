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

// DefaultProjectID is the UUID of the seeded _default project.
// Tasks created without an explicit ProjectID are assigned to this project.
var DefaultProjectID = uuid.MustParse("00000000-0000-0000-0000-000000000000")

// TaskTxProvider gives TaskService a way to run task + project operations
// inside a database transaction for atomic propagation.
// The SQLite Store implements this via its WithTaskTx method.
type TaskTxProvider interface {
	WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository, pr repository.ProjectRepository) error) error
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
	if task.ProjectID == nil {
		id := DefaultProjectID
		task.ProjectID = &id
	}

	// Validate project exists
	project, err := s.projectRepo.GetByID(ctx, *task.ProjectID)
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
	statuses, err := s.workflowSvc.GetStatuses(ctx, *task.ProjectID, project.DefaultWorkflow)
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
		return fmt.Errorf("status %q is not valid for workflow %q", task.Status, project.DefaultWorkflow)
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
		task.Description = *upd.Description
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
		task.UDA = *upd.UDA
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
		if task.ProjectID == nil {
			return nil, fmt.Errorf("task must belong to a project")
		}
		_, err := s.projectRepo.GetByID(ctx, *task.ProjectID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, fmt.Errorf("project not found: %w", err)
			}
			return nil, fmt.Errorf("looking up project: %w", err)
		}
	}

	// Workflow validation for status changes
	if task.Status != oldStatus {
		project, err := s.projectRepo.GetByID(ctx, *task.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("looking up project for workflow: %w", err)
		}
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, *task.ProjectID, project.DefaultWorkflow, oldStatus, task.Status)
		if err != nil {
			return nil, fmt.Errorf("checking transition: %w", err)
		}
		if !allowed {
			return nil, fmt.Errorf("transition %q → %q not allowed: %w", oldStatus, task.Status, domain.ErrInvalidTransition)
		}
	}

	// Update metadata
	task.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)

	// Persist (repo handles version increment)
	if err := s.taskRepo.Update(ctx, task); err != nil {
		return nil, err
	}

	// Re-read to get the persisted state with bumped version
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

// ptr is a generic helper that returns a pointer to the given value.
func ptr[T any](v T) *T {
	return &v
}
