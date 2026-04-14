package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// DefaultProjectName is the config-level name of the built-in default project.
// Via ProjectRepository.GetByName it resolves to domain.DefaultProjectUUID.
// The SQLite projects table stores the row as "_default"; the config file
// ships it as "default". Both map to uuid.Nil.
const DefaultProjectName = "default"

// TaskService implements task business logic including validation,
// workflow enforcement, and optimistic locking. It routes per-task
// operations to the SQLite store that owns the task's project via a
// BundleResolver.
type TaskService struct {
	resolve     BundleResolver
	projects    ProjectLister
	projectRepo repository.ProjectRepository
	workflowSvc *WorkflowService
	engine      *UrgencyEngine
}

// NewTaskService creates a new TaskService wired to the given resolver,
// project lister, project repo, workflow service, and optional urgency
// engine.
func NewTaskService(
	resolve BundleResolver,
	projects ProjectLister,
	projectRepo repository.ProjectRepository,
	workflowSvc *WorkflowService,
	engine *UrgencyEngine,
) *TaskService {
	return &TaskService{
		resolve:     resolve,
		projects:    projects,
		projectRepo: projectRepo,
		workflowSvc: workflowSvc,
		engine:      engine,
	}
}

// defaultProjectID returns the stored UUID of the built-in default project.
// Used by entry points that need the default project but did not receive
// a specific project from the caller. The SQLite seed row and the config
// helper both write it under uuid.Nil, so we look it up by ID to avoid the
// name drift between "default" (config) and "_default" (legacy SQL seed).
func (s *TaskService) defaultProjectID(ctx context.Context) (uuid.UUID, error) {
	p, err := s.projectRepo.GetByID(ctx, domain.DefaultProjectUUID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("looking up default project: %w", err)
	}
	return p.ID, nil
}

// workflowName resolves the project's workflow UUID to its name via the
// workflow service. Centralizes the lookup so callers that previously read
// project.Workflow (a compat string field removed in Phase 4) share one path.
func (s *TaskService) workflowName(ctx context.Context, project *domain.Project) (string, error) {
	wf, err := s.workflowSvc.GetByID(ctx, project.WorkflowID)
	if err != nil {
		return "", fmt.Errorf("looking up workflow %v: %w", project.WorkflowID, err)
	}
	return wf.Name, nil
}

// ResolveProjectName looks up a project by name and returns its UUID.
// CLI and MCP callers use this to translate user-entered project names
// into the typed Task.ProjectID value before calling Create.
// Returns domain.ErrNotFound if the project does not exist.
func (s *TaskService) ResolveProjectName(ctx context.Context, name string) (uuid.UUID, error) {
	if name == "" {
		return s.defaultProjectID(ctx)
	}
	p, err := s.projectRepo.GetByName(ctx, name)
	if err != nil {
		return uuid.Nil, err
	}
	return p.ID, nil
}

// defaultBundle resolves the bundle backing the default project. Player
// records and tag definitions are global resources kept in the default
// store, so operations involving them go through this bundle regardless
// of which project a task lives in.
func (s *TaskService) defaultBundle(ctx context.Context) (*RepoBundle, error) {
	defID, err := s.defaultProjectID(ctx)
	if err != nil {
		return nil, err
	}
	return s.resolve(ctx, defID)
}

func (s *TaskService) bundleForShortID(ctx context.Context, shortID string) (*RepoBundle, *domain.Task, error) {
	defID, err := s.defaultProjectID(ctx)
	if err != nil {
		return nil, nil, err
	}
	bundle, err := s.resolve(ctx, defID)
	if err != nil {
		return nil, nil, err
	}
	task, err := bundle.Tasks.GetByShortID(ctx, shortID)
	if err != nil {
		return nil, nil, err
	}
	return bundle, task, nil
}

func (s *TaskService) bundleForID(ctx context.Context, id uuid.UUID) (*RepoBundle, *domain.Task, error) {
	defID, err := s.defaultProjectID(ctx)
	if err != nil {
		return nil, nil, err
	}
	bundle, err := s.resolve(ctx, defID)
	if err != nil {
		return nil, nil, err
	}
	task, err := bundle.Tasks.GetByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return bundle, task, nil
}

// Create validates and persists a new task. It populates the task's ID,
// ShortID, Version, timestamps, and default ProjectID before saving.
func (s *TaskService) Create(ctx context.Context, task *domain.Task) error {
	if strings.TrimSpace(task.Title) == "" {
		return fmt.Errorf("title must not be empty")
	}
	if task.Priority < 0 || task.Priority > 4 {
		return fmt.Errorf("priority must be between 0 and 4")
	}

	if task.ProjectID == uuid.Nil {
		defID, err := s.defaultProjectID(ctx)
		if err != nil {
			return err
		}
		task.ProjectID = defID
	}

	project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("project not found: %w", err)
		}
		return fmt.Errorf("looking up project: %w", err)
	}

	wfName, err := s.workflowName(ctx, project)
	if err != nil {
		return err
	}

	bundle, err := s.resolve(ctx, task.ProjectID)
	if err != nil {
		return fmt.Errorf("resolving project store: %w", err)
	}

	if task.ParentID != nil {
		_, err := bundle.Tasks.GetByID(ctx, *task.ParentID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return fmt.Errorf("parent task not found: %w", err)
			}
			return fmt.Errorf("looking up parent task: %w", err)
		}
	}

	if task.Status == "" {
		initialStatus, err := s.workflowSvc.GetStatusByRole(ctx, wfName, domain.RoleInitial)
		if err != nil {
			return fmt.Errorf("resolving initial status: %w", err)
		}
		task.Status = initialStatus
	}
	statuses, err := s.workflowSvc.GetStatuses(ctx, wfName)
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
		return fmt.Errorf("status %q is not valid for workflow %q", task.Status, wfName)
	}

	task.ID = uuid.New()
	shortID, err := s.generateShortID(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("generating short ID: %w", err)
	}
	task.ShortID = shortID

	now := time.Now().UTC().Truncate(time.Millisecond)
	task.Version = 1
	task.CreatedAt = now
	task.ModifiedAt = now
	if task.UDA != nil {
		if err := domain.ValidateUDA(task.UDA); err != nil {
			return err
		}
	} else {
		task.UDA = map[string]any{}
	}

	return bundle.Tasks.Create(ctx, task)
}

// GetByShortID retrieves a task by its short ID, searching every known
// project store.
func (s *TaskService) GetByShortID(ctx context.Context, shortID string) (*domain.Task, error) {
	_, task, err := s.bundleForShortID(ctx, shortID)
	return task, err
}

// GetByID retrieves a task by its full UUID, searching every known
// project store.
func (s *TaskService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	_, task, err := s.bundleForID(ctx, id)
	return task, err
}

// List returns tasks matching the given filter, scored and sorted by
// urgency.
func (s *TaskService) List(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error) {
	defID, err := s.defaultProjectID(ctx)
	if err != nil {
		return nil, err
	}
	bundle, err := s.resolve(ctx, defID)
	if err != nil {
		return nil, err
	}
	return s.listInBundle(ctx, bundle, filter)
}

// listInBundle runs the filter against a single bundle and scores the
// resulting tasks using that bundle's own relation, annotation, and tag
// repos.
func (s *TaskService) listInBundle(ctx context.Context, bundle *RepoBundle, filter domain.FilterExpr) ([]*domain.Task, error) {
	tasks, err := bundle.Tasks.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 || s.engine == nil {
		return tasks, nil
	}

	taskIDs := make([]uuid.UUID, len(tasks))
	for i, t := range tasks {
		taskIDs[i] = t.ID
	}

	blockingCounts, err := bundle.Relations.CountBlockingByTasks(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("loading blocking counts: %w", err)
	}
	blockedByCounts, err := bundle.Relations.CountBlockedByTasks(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("loading blocked-by counts: %w", err)
	}
	annotationCounts, err := bundle.Annotations.CountByTasks(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("loading annotation counts: %w", err)
	}
	tagsByTask, err := bundle.Tags.GetTaskTagsBatch(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("loading tag counts: %w", err)
	}
	tagCounts := make(map[uuid.UUID]int, len(tagsByTask))
	for id, tags := range tagsByTask {
		tagCounts[id] = len(tags)
	}

	sctx := ScoringContext{
		BlockingCount:   blockingCounts,
		BlockedByCount:  blockedByCounts,
		AnnotationCount: annotationCounts,
		TagCount:        tagCounts,
		ProjectWeights:  s.buildProjectWeights(ctx, tasks),
	}
	s.engine.ScoreAndSort(tasks, sctx)
	return tasks, nil
}

// Next returns the highest-urgency actionable task. Actionable means:
// non-terminal status, not waiting, not blocked. Returns
// domain.ErrNotFound if no actionable task exists.
func (s *TaskService) Next(ctx context.Context) (*domain.Task, error) {
	nonTerminal, err := s.collectNonTerminalStatuses(ctx)
	if err != nil {
		return nil, err
	}
	defID, err := s.defaultProjectID(ctx)
	if err != nil {
		return nil, err
	}
	bundle, err := s.resolve(ctx, defID)
	if err != nil {
		return nil, err
	}
	filter := &domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: nonTerminal}}
	tasks, err := s.listInBundle(ctx, bundle, filter)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	for _, t := range tasks {
		if t.WaitUntil != nil && t.WaitUntil.After(now) {
			continue
		}
		blockedBy, err := bundle.Relations.CountBlockedByTasks(ctx, []uuid.UUID{t.ID})
		if err != nil {
			return nil, fmt.Errorf("checking blocked status: %w", err)
		}
		if blockedBy[t.ID] > 0 {
			continue
		}
		return t, nil
	}
	return nil, domain.ErrNotFound
}

// collectNonTerminalStatuses returns the union of non-terminal status
// names across all configured workflows. Used by Available and Next to
// find actionable tasks regardless of which workflow defines the status.
func (s *TaskService) collectNonTerminalStatuses(ctx context.Context) ([]string, error) {
	workflows, err := s.workflowSvc.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing workflows: %w", err)
	}
	seen := make(map[string]bool)
	var result []string
	for _, wf := range workflows {
		for _, name := range wf.NonTerminalStatuses() {
			if !seen[name] {
				seen[name] = true
				result = append(result, name)
			}
		}
	}
	return result, nil
}

// buildProjectWeights constructs per-project merged urgency weights for
// all distinct projects found in the task list.
func (s *TaskService) buildProjectWeights(ctx context.Context, tasks []*domain.Task) map[uuid.UUID]*UrgencyWeights {
	if s.engine == nil {
		return nil
	}
	seen := make(map[uuid.UUID]bool)
	for _, t := range tasks {
		seen[t.ProjectID] = true
	}
	weights := make(map[uuid.UUID]*UrgencyWeights, len(seen))
	for projectID := range seen {
		project, err := s.projectRepo.GetByID(ctx, projectID)
		if err != nil {
			continue
		}
		if project.Settings.Urgency == nil {
			continue
		}
		merged := MergeWeights(s.engine.defaults, project.Settings.Urgency)
		weights[projectID] = &merged
	}
	return weights
}

// GetChildren returns the direct children of a task. The parent task
// must exist in some known project store.
func (s *TaskService) GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error) {
	bundle, _, err := s.bundleForID(ctx, parentID)
	if err != nil {
		return nil, err
	}
	return bundle.Tasks.GetChildren(ctx, parentID)
}

// GetDescendants returns all descendants of a task (recursive).
func (s *TaskService) GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error) {
	bundle, _, err := s.bundleForID(ctx, rootID)
	if err != nil {
		return nil, err
	}
	return bundle.Tasks.GetDescendants(ctx, rootID)
}

// Update applies a partial update to a task. It validates the patched
// state, enforces workflow transitions, and uses optimistic locking.
func (s *TaskService) Update(ctx context.Context, upd domain.TaskUpdate) (*domain.Task, error) {
	bundle, task, err := s.bundleForShortID(ctx, upd.ShortID)
	if err != nil {
		return nil, err
	}

	if task.Version != upd.Version {
		return nil, domain.ErrConflict
	}

	oldStatus := task.Status

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
	if upd.ClaimedBy != nil {
		task.ClaimedBy = *upd.ClaimedBy
	}
	if upd.ClaimedAt != nil {
		task.ClaimedAt = *upd.ClaimedAt
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

	if strings.TrimSpace(task.Title) == "" {
		return nil, fmt.Errorf("title must not be empty")
	}
	if task.Priority < 0 || task.Priority > 4 {
		return nil, fmt.Errorf("priority must be between 0 and 4")
	}

	if upd.ParentID != nil && task.ParentID != nil {
		if *task.ParentID == task.ID {
			return nil, fmt.Errorf("task cannot be its own parent")
		}
		_, err := bundle.Tasks.GetByID(ctx, *task.ParentID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, fmt.Errorf("parent task not found: %w", err)
			}
			return nil, fmt.Errorf("looking up parent task: %w", err)
		}
		if err := s.detectParentCycle(ctx, bundle.Tasks, task.ID, *task.ParentID); err != nil {
			return nil, err
		}
	}

	if upd.ProjectID != nil {
		_, err := s.projectRepo.GetByID(ctx, task.ProjectID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, fmt.Errorf("project not found: %w", err)
			}
			return nil, fmt.Errorf("looking up project: %w", err)
		}
	}

	if task.Status != oldStatus {
		project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("looking up project for workflow: %w", err)
		}
		wfName, err := s.workflowName(ctx, project)
		if err != nil {
			return nil, err
		}
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, wfName, oldStatus, task.Status)
		if err != nil {
			return nil, fmt.Errorf("checking transition: %w", err)
		}
		if !allowed {
			return nil, fmt.Errorf("transition %q → %q not allowed: %w", oldStatus, task.Status, domain.ErrInvalidTransition)
		}
	}

	task.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)

	statusChanged := task.Status != oldStatus

	if statusChanged && bundle.Store != nil {
		var result *domain.Task
		err := bundle.Store.WithTaskTx(ctx, func(txTaskRepo repository.TaskRepository) error {
			if err := txTaskRepo.Update(ctx, task); err != nil {
				return err
			}
			updated, err := txTaskRepo.GetByID(ctx, task.ID)
			if err != nil {
				return err
			}
			result = updated
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

	if err := bundle.Tasks.Update(ctx, task); err != nil {
		return nil, err
	}
	return bundle.Tasks.GetByID(ctx, task.ID)
}

// Start transitions a task from its current status to its workflow's
// start-role status. If playerID is non-empty, it auto-claims the task
// for the player.
func (s *TaskService) Start(ctx context.Context, shortID string, version int, playerID string) (*domain.Task, error) {
	bundle, task, err := s.bundleForShortID(ctx, shortID)
	if err != nil {
		return nil, err
	}
	project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("loading project %v: %w", task.ProjectID, err)
	}
	wfName, err := s.workflowName(ctx, project)
	if err != nil {
		return nil, err
	}
	startStatus, err := s.workflowSvc.GetStatusByRole(ctx, wfName, domain.RoleStart)
	if err != nil {
		return nil, fmt.Errorf("resolving start status: %w", err)
	}

	if playerID != "" {
		players, err := s.playerRepo(ctx)
		if err != nil {
			return nil, err
		}
		if players != nil {
			if _, err := players.GetByID(ctx, playerID); err != nil {
				return nil, fmt.Errorf("player %q: %w", playerID, err)
			}

			if task.ClaimedBy != nil && *task.ClaimedBy != playerID {
				return nil, domain.ErrTaskClaimed
			}

			upd := domain.TaskUpdate{
				ShortID: shortID,
				Version: version,
				Status:  ptr(startStatus),
			}
			if task.ClaimedBy == nil {
				now := time.Now().UTC().Truncate(time.Millisecond)
				claimedBy := ptr(playerID)
				claimedAt := ptr(now)
				upd.ClaimedBy = &claimedBy
				upd.ClaimedAt = &claimedAt
			}

			result, err := s.Update(ctx, upd)
			if err != nil {
				return nil, err
			}

			players.UpdateLastSeen(ctx, playerID) //nolint:errcheck
			return result, nil
		}
	}

	_ = bundle // bundle is fetched for parity with the player path
	return s.Update(ctx, domain.TaskUpdate{
		ShortID: shortID,
		Version: version,
		Status:  ptr(startStatus),
	})
}

// playerRepo returns the default bundle's PlayerRepository. Players are
// a global resource stored only in the default store. Returns nil if the
// default bundle has no player repo wired (minimal-dependency usage in
// tests).
func (s *TaskService) playerRepo(ctx context.Context) (repository.PlayerRepository, error) {
	bundle, err := s.defaultBundle(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving default bundle for players: %w", err)
	}
	return bundle.Players, nil
}

// Claim assigns a task to a player. Returns ErrTaskClaimed if claimed
// by another player. Re-claiming by the same player is idempotent.
func (s *TaskService) Claim(ctx context.Context, shortID, playerID string, version int) (*domain.Task, error) {
	players, err := s.playerRepo(ctx)
	if err != nil {
		return nil, err
	}
	if players == nil {
		return nil, fmt.Errorf("player support not configured")
	}

	if _, err := players.GetByID(ctx, playerID); err != nil {
		return nil, fmt.Errorf("player %q: %w", playerID, err)
	}

	_, task, err := s.bundleForShortID(ctx, shortID)
	if err != nil {
		return nil, err
	}

	if task.ClaimedBy != nil && *task.ClaimedBy != playerID {
		return nil, domain.ErrTaskClaimed
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	claimedBy := ptr(playerID)
	claimedAt := ptr(now)
	result, err := s.Update(ctx, domain.TaskUpdate{
		ShortID:   shortID,
		Version:   version,
		ClaimedBy: &claimedBy,
		ClaimedAt: &claimedAt,
	})
	if err != nil {
		return nil, err
	}

	players.UpdateLastSeen(ctx, playerID) //nolint:errcheck
	return result, nil
}

// Release clears a task's claim. Only the current claimant can release.
func (s *TaskService) Release(ctx context.Context, shortID, playerID string, version int) (*domain.Task, error) {
	players, err := s.playerRepo(ctx)
	if err != nil {
		return nil, err
	}
	if players == nil {
		return nil, fmt.Errorf("player support not configured")
	}

	_, task, err := s.bundleForShortID(ctx, shortID)
	if err != nil {
		return nil, err
	}

	if task.ClaimedBy == nil {
		return nil, fmt.Errorf("task is not claimed")
	}
	if *task.ClaimedBy != playerID {
		return nil, fmt.Errorf("task is claimed by a different player")
	}

	var nilStr *string
	var nilTime *time.Time
	result, err := s.Update(ctx, domain.TaskUpdate{
		ShortID:   shortID,
		Version:   version,
		ClaimedBy: &nilStr,
		ClaimedAt: &nilTime,
	})
	if err != nil {
		return nil, err
	}

	players.UpdateLastSeen(ctx, playerID) //nolint:errcheck
	return result, nil
}

// Complete transitions a task to its workflow's done-role status.
func (s *TaskService) Complete(ctx context.Context, shortID string, version int) (*domain.Task, error) {
	_, task, err := s.bundleForShortID(ctx, shortID)
	if err != nil {
		return nil, err
	}
	project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("loading project %v: %w", task.ProjectID, err)
	}
	wfName, err := s.workflowName(ctx, project)
	if err != nil {
		return nil, err
	}
	doneStatus, err := s.workflowSvc.GetStatusByRole(ctx, wfName, domain.RoleDone)
	if err != nil {
		return nil, fmt.Errorf("resolving done status: %w", err)
	}
	return s.Update(ctx, domain.TaskUpdate{
		ShortID: shortID,
		Version: version,
		Status:  ptr(doneStatus),
	})
}

// Delete soft-deletes a task by transitioning to its workflow's
// delete-role status.
func (s *TaskService) Delete(ctx context.Context, shortID string, version int) (*domain.Task, error) {
	_, task, err := s.bundleForShortID(ctx, shortID)
	if err != nil {
		return nil, err
	}
	project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("loading project %v: %w", task.ProjectID, err)
	}
	wfName, err := s.workflowName(ctx, project)
	if err != nil {
		return nil, err
	}
	deleteStatus, err := s.workflowSvc.GetStatusByRole(ctx, wfName, domain.RoleDelete)
	if err != nil {
		return nil, fmt.Errorf("resolving delete status: %w", err)
	}
	return s.Update(ctx, domain.TaskUpdate{
		ShortID: shortID,
		Version: version,
		Status:  ptr(deleteStatus),
	})
}

// Available returns unclaimed, actionable, unblocked tasks sorted by
// urgency.
func (s *TaskService) Available(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error) {
	nonTerminal, err := s.collectNonTerminalStatuses(ctx)
	if err != nil {
		return nil, err
	}
	baseFilter := &domain.AndFilter{
		Children: []domain.FilterExpr{
			&domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: nonTerminal}},
			&domain.TermFilter{TaskFilter: domain.TaskFilter{Unclaimed: ptr(true)}},
		},
	}
	if filter != nil {
		baseFilter.Children = append(baseFilter.Children, filter)
	}

	defID, err := s.defaultProjectID(ctx)
	if err != nil {
		return nil, err
	}
	bundle, err := s.resolve(ctx, defID)
	if err != nil {
		return nil, err
	}
	return s.availableInBundle(ctx, bundle, baseFilter)
}

// availableInBundle runs the availability filter against a single
// bundle, removing blocked and waiting tasks.
func (s *TaskService) availableInBundle(ctx context.Context, bundle *RepoBundle, filter domain.FilterExpr) ([]*domain.Task, error) {
	tasks, err := s.listInBundle(ctx, bundle, filter)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return tasks, nil
	}

	taskIDs := make([]uuid.UUID, len(tasks))
	for i, t := range tasks {
		taskIDs[i] = t.ID
	}

	blockedCounts, err := bundle.Relations.CountBlockedByIncompleteTasks(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("checking blocked status: %w", err)
	}

	now := time.Now()
	result := make([]*domain.Task, 0, len(tasks))
	for _, t := range tasks {
		if blockedCounts[t.ID] > 0 {
			continue
		}
		if t.WaitUntil != nil && t.WaitUntil.After(now) {
			continue
		}
		result = append(result, t)
	}
	return result, nil
}

// Pop claims and starts the highest-urgency available task for the
// given player. Retries on claim-conflict and optimistic-lock errors.
// Returns domain.ErrNoAvailableTasks if nothing can be claimed.
func (s *TaskService) Pop(ctx context.Context, playerID string, filter domain.FilterExpr) (*domain.Task, error) {
	tasks, err := s.Available(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, domain.ErrNoAvailableTasks
	}

	for _, task := range tasks {
		claimed, err := s.Claim(ctx, task.ShortID, playerID, task.Version)
		if err != nil {
			if errors.Is(err, domain.ErrTaskClaimed) || errors.Is(err, domain.ErrConflict) {
				continue
			}
			return nil, err
		}

		started, err := s.Start(ctx, task.ShortID, claimed.Version, playerID)
		if err != nil {
			if errors.Is(err, domain.ErrConflict) {
				_, relErr := s.Release(ctx, task.ShortID, playerID, claimed.Version)
				if relErr != nil {
					_ = relErr
				}
				continue
			}
			return nil, err
		}

		return started, nil
	}

	return nil, domain.ErrNoAvailableTasks
}

// maxParentDepth guards cycle detection and auto-complete propagation
// against corrupted data causing infinite loops.
const maxParentDepth = 100

// detectParentCycle walks up the ancestor chain from proposedParentID.
// If it encounters taskID, the proposed parent relationship would
// create a cycle.
func (s *TaskService) detectParentCycle(ctx context.Context, tasks repository.TaskRepository, taskID, proposedParentID uuid.UUID) error {
	current := proposedParentID
	for depth := 0; depth < maxParentDepth; depth++ {
		if current == taskID {
			return domain.ErrCyclicParent
		}
		parent, err := tasks.GetByID(ctx, current)
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

// generateShortID derives a short ID from the task's UUID. It starts
// with 8 hex characters and extends if a collision is detected in any
// known project store (short IDs are globally unique).
func (s *TaskService) generateShortID(ctx context.Context, id uuid.UUID) (string, error) {
	hex := strings.ReplaceAll(id.String(), "-", "")
	for length := 8; length <= len(hex); length++ {
		candidate := hex[:length]
		_, _, err := s.bundleForShortID(ctx, candidate)
		if errors.Is(err, domain.ErrNotFound) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("could not generate unique short ID")
}

// Annotate adds a timestamped note to a task.
func (s *TaskService) Annotate(ctx context.Context, taskShortID string, body string) (*domain.Annotation, error) {
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("annotation body must not be empty")
	}

	bundle, task, err := s.bundleForShortID(ctx, taskShortID)
	if err != nil {
		return nil, err
	}

	ann := &domain.Annotation{
		ID:        uuid.New(),
		TaskID:    task.ID,
		Body:      body,
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	if err := bundle.Annotations.Create(ctx, ann); err != nil {
		return nil, err
	}
	return ann, nil
}

// GetAnnotations returns all annotations for a task, identified by
// short ID.
func (s *TaskService) GetAnnotations(ctx context.Context, taskShortID string) ([]*domain.Annotation, error) {
	bundle, task, err := s.bundleForShortID(ctx, taskShortID)
	if err != nil {
		return nil, err
	}
	return bundle.Annotations.GetByTask(ctx, task.ID)
}

// DeleteAnnotation removes an annotation by its ID. Fan-out: every
// project store is asked to delete; returns domain.ErrNotFound if no
// store held the row.
func (s *TaskService) DeleteAnnotation(ctx context.Context, annotationID uuid.UUID) error {
	ids, err := s.projects(ctx)
	if err != nil {
		return err
	}
	deleted := false
	for _, pid := range ids {
		bundle, err := s.resolve(ctx, pid)
		if err != nil {
			return err
		}
		err = bundle.Annotations.Delete(ctx, annotationID)
		if err == nil {
			deleted = true
			continue
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}
	if !deleted {
		return domain.ErrNotFound
	}
	return nil
}

// checkAutoComplete checks whether completing a task should trigger
// automatic completion of its parent. If the task has a parent, all
// non-deleted siblings are at the trigger status, and the workflow
// allows the transition, the parent is auto-completed.
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

		project, err := s.projectRepo.GetByID(ctx, parent.ProjectID)
		if err != nil {
			return fmt.Errorf("loading project for propagation: %w", err)
		}

		cfg := project.Settings.AutoCompleteParent
		if cfg == nil {
			return nil
		}

		if current.Status != cfg.TriggerStatus {
			return nil
		}

		wfName, err := s.workflowName(ctx, project)
		if err != nil {
			return err
		}

		children, err := txTaskRepo.GetChildren(ctx, parent.ID)
		if err != nil {
			return fmt.Errorf("loading siblings for propagation: %w", err)
		}

		deleteStatus, err := s.workflowSvc.GetDeleteStatus(ctx, wfName)
		if err != nil {
			return fmt.Errorf("resolving delete status for propagation: %w", err)
		}

		allReady := true
		for _, child := range children {
			if child.Status == deleteStatus {
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

		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, wfName, parent.Status, cfg.TargetStatus)
		if err != nil {
			return fmt.Errorf("checking propagation transition: %w", err)
		}
		if !allowed {
			return nil
		}

		parent.Status = cfg.TargetStatus
		parent.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)
		if err := txTaskRepo.Update(ctx, parent); err != nil {
			return fmt.Errorf("auto-completing parent: %w", err)
		}

		current, err = txTaskRepo.GetByID(ctx, parent.ID)
		if err != nil {
			return fmt.Errorf("re-reading parent after propagation: %w", err)
		}
	}
	return fmt.Errorf("auto-complete propagation exceeded maximum depth (%d)", maxParentDepth)
}

// checkAutoRevert checks whether a task moving away from the trigger
// status should revert its parent.
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

		project, err := s.projectRepo.GetByID(ctx, parent.ProjectID)
		if err != nil {
			return fmt.Errorf("loading project for revert: %w", err)
		}

		revertCfg := project.Settings.AutoRevertParent
		if revertCfg == nil {
			return nil
		}

		if currentOldStatus != revertCfg.TriggerStatus {
			return nil
		}
		if current.Status == revertCfg.TriggerStatus {
			return nil
		}

		completeCfg := project.Settings.AutoCompleteParent
		if completeCfg == nil {
			return nil
		}
		if parent.Status != completeCfg.TargetStatus {
			return nil
		}

		wfName, err := s.workflowName(ctx, project)
		if err != nil {
			return err
		}

		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, wfName, parent.Status, revertCfg.TargetStatus)
		if err != nil {
			return fmt.Errorf("checking revert transition: %w", err)
		}
		if !allowed {
			return nil
		}

		prevParentStatus := parent.Status
		parent.Status = revertCfg.TargetStatus
		parent.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)
		if err := txTaskRepo.Update(ctx, parent); err != nil {
			return fmt.Errorf("reverting parent: %w", err)
		}

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
