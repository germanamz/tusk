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

// TaskService implements task business logic including validation,
// workflow enforcement, and optimistic locking.
type TaskService struct {
	taskRepo       repository.TaskRepository
	annotationRepo repository.AnnotationRepository
	projectRepo    repository.ProjectRepository
	workflowSvc    *WorkflowService
}

// NewTaskService creates a new TaskService with the given dependencies.
func NewTaskService(
	tr repository.TaskRepository,
	ar repository.AnnotationRepository,
	pr repository.ProjectRepository,
	ws *WorkflowService,
) *TaskService {
	return &TaskService{
		taskRepo:       tr,
		annotationRepo: ar,
		projectRepo:    pr,
		workflowSvc:    ws,
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

	// Validate parent exists
	if task.ParentID != nil {
		_, err := s.taskRepo.GetByID(ctx, *task.ParentID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return fmt.Errorf("parent task not found: %w", err)
			}
			return fmt.Errorf("looking up parent task: %w", err)
		}
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

// ptr is a generic helper that returns a pointer to the given value.
func ptr[T any](v T) *T {
	return &v
}
