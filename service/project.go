package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/germanamz/tusk/config"
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
	Name        string
	WorkflowID  uuid.UUID
	Description string
	Settings    domain.ProjectSettings
}

// UrgencyMutation describes urgency-weight changes for ModifyProjectInput.
// Each map is keyed by the canonical config key (e.g. "priority_weight",
// "blocking_weight"). Absolute writes (Set) and deltas (Delta) may not both
// target the same key in a single call.
type UrgencyMutation struct {
	Set   map[string]float64
	Delta map[string]float64
}

// TaxonomyMutation describes a change to project.Settings.Taxonomy.
// When Clear is true, Settings.Taxonomy is reset to nil (inherit workspace
// default). When Clear is false, Settings.Taxonomy is set to a pointer to
// Value — an empty Value persists as explicit opt-out, a populated Value
// persists as the project-specific override.
type TaxonomyMutation struct {
	Clear bool
	Value domain.Taxonomy
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
	Description     **string
	Urgency         UrgencyMutation
	Taxonomy        *TaxonomyMutation
}

// ProjectService provides read + write access to projects.
type ProjectService struct {
	projectRepo repository.ProjectRepository
	taskCounter TaskCountByProject
	tx          ProjectTxProvider
	defaults    ProjectDefaults
	cfg         *config.Config
}

// NewProjectService creates a new ProjectService. taskCounter, tx, and defaults
// are required by Create/Modify/Delete — they may be nil only in tests that
// exercise read-only paths (GetByID/GetByName/List). cfg supplies the workspace
// taxonomy consulted by EffectiveTaxonomy; nil is treated as "no workspace
// taxonomy configured".
func NewProjectService(
	projectRepo repository.ProjectRepository,
	taskCounter TaskCountByProject,
	tx ProjectTxProvider,
	defaults ProjectDefaults,
	cfg *config.Config,
) *ProjectService {
	return &ProjectService{
		projectRepo: projectRepo,
		taskCounter: taskCounter,
		tx:          tx,
		defaults:    defaults,
		cfg:         cfg,
	}
}

// TaxonomySource identifies where EffectiveTaxonomy resolved its value from.
type TaxonomySource int

const (
	// TaxonomySourceNone indicates no taxonomy is in effect (levels disabled).
	TaxonomySourceNone TaxonomySource = iota
	// TaxonomySourceWorkspace indicates the taxonomy came from config.Taxonomy.
	TaxonomySourceWorkspace
	// TaxonomySourceProjectOverride indicates ProjectSettings.Taxonomy is set
	// (either populated or explicit opt-out via &empty).
	TaxonomySourceProjectOverride
)

// EffectiveTaxonomy resolves the taxonomy that governs tasks in project p.
// Resolution order:
//  1. If p.Settings.Taxonomy != nil, return its value (including &empty as
//     an explicit opt-out). Source: ProjectOverride.
//  2. Otherwise, if the workspace config has a taxonomy configured, return
//     a clone of it. Source: Workspace.
//  3. Otherwise, return an empty taxonomy. Source: None.
//
// Callers must treat the returned Taxonomy as read-only unless they know
// it was cloned; the ProjectOverride path returns the project's own slice.
func (service *ProjectService) EffectiveTaxonomy(project *domain.Project) (domain.Taxonomy, TaxonomySource) {
	if project != nil && project.Settings.Taxonomy != nil {
		return *project.Settings.Taxonomy, TaxonomySourceProjectOverride
	}
	if service.cfg != nil && len(service.cfg.Taxonomy.Levels) > 0 {
		return domain.Taxonomy(service.cfg.Taxonomy.Levels).Clone(), TaxonomySourceWorkspace
	}
	return domain.Taxonomy{}, TaxonomySourceNone
}

// GetByName retrieves a project by its human-readable name.
// Returns domain.ErrNotFound if not found.
func (service *ProjectService) GetByName(ctx context.Context, name string) (*domain.Project, error) {
	return service.projectRepo.GetByName(ctx, name)
}

// GetByID retrieves a project by its typed UUID.
// Returns domain.ErrNotFound if not found.
func (service *ProjectService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	return service.projectRepo.GetByID(ctx, id)
}

// List returns all projects, sorted by name.
func (service *ProjectService) List(ctx context.Context) ([]*domain.Project, error) {
	return service.projectRepo.List(ctx)
}

// Create inserts a new project with the given name, workflow, and settings.
// The caller is expected to have resolved the workflow name to an ID already
// (e.g. via WorkflowService.GetByName).
func (service *ProjectService) Create(ctx context.Context, input CreateProjectInput) (*domain.Project, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("%w: project name is empty", domain.ErrNotFound)
	}
	if input.Name == defaultProjectName {
		return nil, fmt.Errorf("cannot create project %q: name is reserved", input.Name)
	}
	existing, err := service.projectRepo.GetByName(ctx, input.Name)

	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, fmt.Errorf("checking existing project %q: %w", input.Name, err)
	}

	if existing != nil {
		return nil, fmt.Errorf("project %q already exists: %w", input.Name, domain.ErrConflict)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	domainProject := &domain.Project{
		ID:          uuid.New(),
		Name:        input.Name,
		WorkflowID:  input.WorkflowID,
		Description: input.Description,
		Settings:    input.Settings,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	createErr := service.projectRepo.Create(ctx, domainProject)

	if createErr != nil {
		return nil, fmt.Errorf("creating project %q: %w", input.Name, createErr)
	}

	return domainProject, nil
}

// Modify applies a mutation to an existing project. Optimistic locking is
// enforced via input.ExpectedVersion; a mismatch returns domain.ErrConflict.
// Urgency deltas are resolved against the global defaults passed to
// NewProjectService.
func (service *ProjectService) Modify(ctx context.Context, input ModifyProjectInput) (*domain.Project, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("%w: project name is empty", domain.ErrNotFound)
	}
	domainProject, err := service.projectRepo.GetByName(ctx, input.Name)

	if err != nil {
		return nil, err
	}

	if domainProject.Version != input.ExpectedVersion {
		return nil, fmt.Errorf("project %q: expected version %d, got %d: %w",
			input.Name, input.ExpectedVersion, domainProject.Version, domain.ErrConflict)
	}

	if input.WorkflowID != nil {
		domainProject.WorkflowID = *input.WorkflowID
	}
	if input.Description != nil {
		if *input.Description == nil {
			domainProject.Description = ""
		} else {
			domainProject.Description = **input.Description
		}
	}
	if input.AutoComplete != nil {
		ac := *input.AutoComplete
		domainProject.Settings.AutoCompleteParent = &ac
	}
	if input.AutoRevert != nil {
		ar := *input.AutoRevert
		domainProject.Settings.AutoRevertParent = &ar
	}

	if input.Taxonomy != nil {
		if input.Taxonomy.Clear {
			domainProject.Settings.Taxonomy = nil
		} else {
			if !input.Taxonomy.Value.IsEmpty() {
				if err := input.Taxonomy.Value.Validate(); err != nil {
					return nil, fmt.Errorf("invalid taxonomy: %w", err)
				}
			}
			tax := input.Taxonomy.Value.Clone()
			if tax == nil {
				tax = domain.Taxonomy{}
			}
			domainProject.Settings.Taxonomy = &tax
		}
	}

	for key := range input.Urgency.Set {
		if _, dup := input.Urgency.Delta[key]; dup {
			return nil, fmt.Errorf("urgency key %q has both absolute and delta", key)
		}
	}
	if len(input.Urgency.Set) > 0 || len(input.Urgency.Delta) > 0 {
		if domainProject.Settings.Urgency == nil {
			domainProject.Settings.Urgency = &domain.UrgencyOverrides{}
		}
		for key, value := range input.Urgency.Set {
			if err := urgencySetAbsolute(domainProject.Settings.Urgency, key, value); err != nil {
				return nil, err
			}
		}
		for key, delta := range input.Urgency.Delta {
			base, ok := urgencyDefaultByKey(service.defaults.Urgency, key)
			if !ok {
				return nil, fmt.Errorf("unknown urgency key %q", key)
			}
			current := urgencyOverrideByKey(domainProject.Settings.Urgency, key)
			if current != nil {
				base = *current
			}
			if err := urgencySetAbsolute(domainProject.Settings.Urgency, key, base+delta); err != nil {
				return nil, err
			}
		}
	}

	updateErr := service.projectRepo.Update(ctx, domainProject)

	if updateErr != nil {
		return nil, fmt.Errorf("updating project %q: %w", input.Name, updateErr)
	}

	return domainProject, nil
}

// Delete removes a project by ID with optimistic locking. Rejects the built-in
// _default project and projects with referencing tasks unless force is true.
// Under force, referencing tasks are bulk-reassigned to _default in the same
// transaction as the delete so the FK on projects(id) does not fire.
func (service *ProjectService) Delete(ctx context.Context, id uuid.UUID, expectedVersion int, force bool) error {
	if id == domain.DefaultProjectUUID && !force {
		return fmt.Errorf("cannot delete built-in %q project (use --force to override)", defaultProjectName)
	}

	if service.taskCounter == nil {
		return fmt.Errorf("project delete requires task counter")
	}
	count, countErr := service.taskCounter.CountByProject(ctx, id)

	if countErr != nil {
		return fmt.Errorf("counting referencing tasks: %w", countErr)
	}

	if count > 0 && !force {
		return fmt.Errorf("project %s has %d referencing task(s): %w", id, count, domain.ErrProjectHasTasks)
	}

	if service.tx == nil {
		return fmt.Errorf("project delete requires transactional store")
	}
	return service.tx.WithProjectTx(ctx, func(projects repository.ProjectRepository, tasks repository.TaskRepository) error {
		if count > 0 && force {
			_, reassignErr := tasks.ReassignProject(ctx, id, domain.DefaultProjectUUID)

			if reassignErr != nil {
				return fmt.Errorf("reassigning tasks off project %s: %w", id, reassignErr)
			}
		}
		deleteErr := projects.Delete(ctx, id, expectedVersion)

		if deleteErr != nil {
			if errors.Is(deleteErr, domain.ErrConflict) {
				return fmt.Errorf("project %s: version conflict: %w", id, domain.ErrConflict)
			}
			return fmt.Errorf("deleting project %s: %w", id, deleteErr)
		}

		return nil
	})
}

// defaultProjectName mirrors config.DefaultProjectID without creating a
// service → config import cycle. Must stay in sync.
const defaultProjectName = "default"

// urgencyDefaultByKey maps a canonical config key to the corresponding
// default weight in UrgencyWeights.
func urgencyDefaultByKey(weights UrgencyWeights, key string) (float64, bool) {
	switch key {
	case "priority_weight":
		return weights.Priority, true
	case "due_weight":
		return weights.Due, true
	case "age_weight":
		return weights.Age, true
	case "active_weight":
		return weights.Active, true
	case "blocking_weight":
		return weights.Blocking, true
	case "blocked_weight":
		return weights.Blocked, true
	case "tags_weight":
		return weights.Tags, true
	case "project_weight":
		return weights.Project, true
	case "annotations_weight":
		return weights.Annotations, true
	case "waiting_weight":
		return weights.Waiting, true
	}
	return 0, false
}

// urgencyOverrideByKey returns the current per-project override value for a
// canonical config key, or nil if not set.
func urgencyOverrideByKey(overrides *domain.UrgencyOverrides, key string) *float64 {
	if overrides == nil {
		return nil
	}
	switch key {
	case "priority_weight":
		return overrides.PriorityWeight
	case "due_weight":
		return overrides.DueWeight
	case "age_weight":
		return overrides.AgeWeight
	case "active_weight":
		return overrides.ActiveWeight
	case "blocking_weight":
		return overrides.BlockingWeight
	case "blocked_weight":
		return overrides.BlockedWeight
	case "tags_weight":
		return overrides.TagsWeight
	case "project_weight":
		return overrides.ProjectWeight
	case "annotations_weight":
		return overrides.AnnotationsWeight
	case "waiting_weight":
		return overrides.WaitingWeight
	}
	return nil
}

// urgencySetAbsolute writes an absolute weight value to the override struct
// for the given canonical config key.
func urgencySetAbsolute(overrides *domain.UrgencyOverrides, key string, value float64) error {
	val := value
	switch key {
	case "priority_weight":
		overrides.PriorityWeight = &val
	case "due_weight":
		overrides.DueWeight = &val
	case "age_weight":
		overrides.AgeWeight = &val
	case "active_weight":
		overrides.ActiveWeight = &val
	case "blocking_weight":
		overrides.BlockingWeight = &val
	case "blocked_weight":
		overrides.BlockedWeight = &val
	case "tags_weight":
		overrides.TagsWeight = &val
	case "project_weight":
		overrides.ProjectWeight = &val
	case "annotations_weight":
		overrides.AnnotationsWeight = &val
	case "waiting_weight":
		overrides.WaitingWeight = &val
	default:
		return fmt.Errorf("unknown urgency key %q", key)
	}
	return nil
}
