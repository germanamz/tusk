package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// TaskCountByProject counts tasks referencing a project.
// Satisfied by *sqlite.TaskRepo via CountByProject.
type TaskCountByProject interface {
	CountByProject(ctx context.Context, projectID uuid.UUID) (int, error)
}

// ProjectTxProvider runs a callback inside a transaction that exposes both
// ProjectRepository and TaskRepository. Used by ProjectService.Delete to
// reassign referencing tasks under --force and delete the project row in one
// atomic unit. Satisfied by *sqlite.Store via WithProjectTx.
type ProjectTxProvider interface {
	WithProjectTx(ctx context.Context, fn func(projects repository.ProjectRepository, tasks repository.TaskRepository) error) error
}

// ProjectDefaults carries baseline values used to resolve project-scoped
// mutations. Urgency holds the global urgency weights against which
// +urgency.xxx-weight=N / -urgency.xxx-weight=N delta operations are applied.
type ProjectDefaults struct {
	Urgency UrgencyWeights
}

// CreateProjectInput describes a new project to be persisted.
type CreateProjectInput struct {
	Name       string
	WorkflowID uuid.UUID
	Settings   domain.ProjectSettings
}

// UrgencyMutation describes urgency-weight changes for ModifyProjectInput.
// Each map is keyed by the canonical config key (e.g. "priority_weight",
// "blocking_weight"). Absolute writes (Set) and deltas (Delta) may not both
// target the same key in a single call.
type UrgencyMutation struct {
	Set   map[string]float64
	Delta map[string]float64
}

// ModifyProjectInput describes the mutation to apply to an existing project.
// Pointer fields: nil = leave unchanged. ExpectedVersion drives optimistic
// locking — the Modify call fails with domain.ErrConflict if the current
// version on disk does not match.
type ModifyProjectInput struct {
	Name            string
	ExpectedVersion int
	WorkflowID      *uuid.UUID
	AutoComplete    *domain.AutoCompleteConfig
	AutoRevert      *domain.AutoRevertConfig
	Urgency         UrgencyMutation
}

// ProjectService provides read + write access to projects.
type ProjectService struct {
	projectRepo repository.ProjectRepository
	taskCounter TaskCountByProject
	tx          ProjectTxProvider
	defaults    ProjectDefaults
}

// NewProjectService creates a new ProjectService. taskCounter, tx, and defaults
// are required by Create/Modify/Delete — they may be nil only in tests that
// exercise read-only paths (GetByID/GetByName/List).
func NewProjectService(
	projectRepo repository.ProjectRepository,
	taskCounter TaskCountByProject,
	tx ProjectTxProvider,
	defaults ProjectDefaults,
) *ProjectService {
	return &ProjectService{
		projectRepo: projectRepo,
		taskCounter: taskCounter,
		tx:          tx,
		defaults:    defaults,
	}
}

// GetByName retrieves a project by its human-readable name.
// Returns domain.ErrNotFound if not found.
func (s *ProjectService) GetByName(ctx context.Context, name string) (*domain.Project, error) {
	return s.projectRepo.GetByName(ctx, name)
}

// GetByID retrieves a project by its typed UUID.
// Returns domain.ErrNotFound if not found.
func (s *ProjectService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	return s.projectRepo.GetByID(ctx, id)
}

// List returns all projects, sorted by name.
func (s *ProjectService) List(ctx context.Context) ([]*domain.Project, error) {
	return s.projectRepo.List(ctx)
}

// Create inserts a new project with the given name, workflow, and settings.
// The caller is expected to have resolved the workflow name to an ID already
// (e.g. via WorkflowService.GetByName).
func (s *ProjectService) Create(ctx context.Context, input CreateProjectInput) (*domain.Project, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("%w: project name is empty", domain.ErrNotFound)
	}
	if input.Name == defaultProjectName {
		return nil, fmt.Errorf("cannot create project %q: name is reserved", input.Name)
	}
	existing, err := s.projectRepo.GetByName(ctx, input.Name)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, fmt.Errorf("checking existing project %q: %w", input.Name, err)
	}
	if existing != nil {
		return nil, fmt.Errorf("project %q already exists: %w", input.Name, domain.ErrConflict)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	p := &domain.Project{
		ID:         uuid.New(),
		Name:       input.Name,
		WorkflowID: input.WorkflowID,
		Settings:   input.Settings,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.projectRepo.Create(ctx, p); err != nil {
		return nil, fmt.Errorf("creating project %q: %w", input.Name, err)
	}
	return p, nil
}

// Modify applies a mutation to an existing project. Optimistic locking is
// enforced via input.ExpectedVersion; a mismatch returns domain.ErrConflict.
// Urgency deltas are resolved against the global defaults passed to
// NewProjectService.
func (s *ProjectService) Modify(ctx context.Context, input ModifyProjectInput) (*domain.Project, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("%w: project name is empty", domain.ErrNotFound)
	}
	p, err := s.projectRepo.GetByName(ctx, input.Name)
	if err != nil {
		return nil, err
	}
	if p.Version != input.ExpectedVersion {
		return nil, fmt.Errorf("project %q: expected version %d, got %d: %w",
			input.Name, input.ExpectedVersion, p.Version, domain.ErrConflict)
	}

	if input.WorkflowID != nil {
		p.WorkflowID = *input.WorkflowID
	}
	if input.AutoComplete != nil {
		ac := *input.AutoComplete
		p.Settings.AutoCompleteParent = &ac
	}
	if input.AutoRevert != nil {
		ar := *input.AutoRevert
		p.Settings.AutoRevertParent = &ar
	}

	for k := range input.Urgency.Set {
		if _, dup := input.Urgency.Delta[k]; dup {
			return nil, fmt.Errorf("urgency key %q has both absolute and delta", k)
		}
	}
	if len(input.Urgency.Set) > 0 || len(input.Urgency.Delta) > 0 {
		if p.Settings.Urgency == nil {
			p.Settings.Urgency = &domain.UrgencyOverrides{}
		}
		for k, v := range input.Urgency.Set {
			if err := urgencySetAbsolute(p.Settings.Urgency, k, v); err != nil {
				return nil, err
			}
		}
		for k, delta := range input.Urgency.Delta {
			base, ok := urgencyDefaultByKey(s.defaults.Urgency, k)
			if !ok {
				return nil, fmt.Errorf("unknown urgency key %q", k)
			}
			current := urgencyOverrideByKey(p.Settings.Urgency, k)
			if current != nil {
				base = *current
			}
			if err := urgencySetAbsolute(p.Settings.Urgency, k, base+delta); err != nil {
				return nil, err
			}
		}
	}

	if err := s.projectRepo.Update(ctx, p); err != nil {
		return nil, fmt.Errorf("updating project %q: %w", input.Name, err)
	}
	return p, nil
}

// Delete removes a project by ID with optimistic locking. Rejects the built-in
// _default project and projects with referencing tasks unless force is true.
// Under force, referencing tasks are bulk-reassigned to _default in the same
// transaction as the delete so the FK on projects(id) does not fire.
func (s *ProjectService) Delete(ctx context.Context, id uuid.UUID, expectedVersion int, force bool) error {
	if id == domain.DefaultProjectUUID && !force {
		return fmt.Errorf("cannot delete built-in %q project (use --force to override)", defaultProjectName)
	}

	if s.taskCounter == nil {
		return fmt.Errorf("project delete requires task counter")
	}
	count, err := s.taskCounter.CountByProject(ctx, id)
	if err != nil {
		return fmt.Errorf("counting referencing tasks: %w", err)
	}
	if count > 0 && !force {
		return fmt.Errorf("project %s has %d referencing task(s): %w", id, count, domain.ErrProjectHasTasks)
	}

	if s.tx == nil {
		return fmt.Errorf("project delete requires transactional store")
	}
	return s.tx.WithProjectTx(ctx, func(projects repository.ProjectRepository, tasks repository.TaskRepository) error {
		if count > 0 && force {
			if _, err := tasks.ReassignProject(ctx, id, domain.DefaultProjectUUID); err != nil {
				return fmt.Errorf("reassigning tasks off project %s: %w", id, err)
			}
		}
		if err := projects.Delete(ctx, id, expectedVersion); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				return fmt.Errorf("project %s: version conflict: %w", id, domain.ErrConflict)
			}
			return fmt.Errorf("deleting project %s: %w", id, err)
		}
		return nil
	})
}

// defaultProjectName mirrors config.DefaultProjectID without creating a
// service → config import cycle. Must stay in sync.
const defaultProjectName = "default"

// urgencyDefaultByKey maps a canonical config key to the corresponding
// default weight in UrgencyWeights.
func urgencyDefaultByKey(w UrgencyWeights, key string) (float64, bool) {
	switch key {
	case "priority_weight":
		return w.Priority, true
	case "due_weight":
		return w.Due, true
	case "age_weight":
		return w.Age, true
	case "active_weight":
		return w.Active, true
	case "blocking_weight":
		return w.Blocking, true
	case "blocked_weight":
		return w.Blocked, true
	case "tags_weight":
		return w.Tags, true
	case "project_weight":
		return w.Project, true
	case "annotations_weight":
		return w.Annotations, true
	case "waiting_weight":
		return w.Waiting, true
	}
	return 0, false
}

// urgencyOverrideByKey returns the current per-project override value for a
// canonical config key, or nil if not set.
func urgencyOverrideByKey(o *domain.UrgencyOverrides, key string) *float64 {
	if o == nil {
		return nil
	}
	switch key {
	case "priority_weight":
		return o.PriorityWeight
	case "due_weight":
		return o.DueWeight
	case "age_weight":
		return o.AgeWeight
	case "active_weight":
		return o.ActiveWeight
	case "blocking_weight":
		return o.BlockingWeight
	case "blocked_weight":
		return o.BlockedWeight
	case "tags_weight":
		return o.TagsWeight
	case "project_weight":
		return o.ProjectWeight
	case "annotations_weight":
		return o.AnnotationsWeight
	case "waiting_weight":
		return o.WaitingWeight
	}
	return nil
}

// urgencySetAbsolute writes an absolute weight value to the override struct
// for the given canonical config key.
func urgencySetAbsolute(o *domain.UrgencyOverrides, key string, value float64) error {
	v := value
	switch key {
	case "priority_weight":
		o.PriorityWeight = &v
	case "due_weight":
		o.DueWeight = &v
	case "age_weight":
		o.AgeWeight = &v
	case "active_weight":
		o.ActiveWeight = &v
	case "blocking_weight":
		o.BlockingWeight = &v
	case "blocked_weight":
		o.BlockedWeight = &v
	case "tags_weight":
		o.TagsWeight = &v
	case "project_weight":
		o.ProjectWeight = &v
	case "annotations_weight":
		o.AnnotationsWeight = &v
	case "waiting_weight":
		o.WaitingWeight = &v
	default:
		return fmt.Errorf("unknown urgency key %q", key)
	}
	return nil
}
