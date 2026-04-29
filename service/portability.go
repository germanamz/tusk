// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"fmt"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/internal/portability"
	"github.com/germanamz/tusk/repository"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

// PortabilityService orchestrates workspace-wide Export and Import.
//
// It reads through the existing per-entity services and the RepoBundle for
// entity kinds that lack a service (annotations, raw events). Writes go
// through WriteTxProvider so the entire import — including the
// workspace_imported event — is atomic.
type PortabilityService struct {
	writeTx     WriteTxProvider
	tasks       *TaskService
	projects    *ProjectService
	workflows   *WorkflowService
	relations   *RelationService
	tags        *TagService
	players     *PlayerService
	notes       *NoteService
	bundle      *RepoBundle
	events      repository.EventRepository
	tuskVersion string
}

// NewPortabilityService wires the dependencies the service needs to read
// every entity kind for Export and to apply a dump inside a single
// transaction in Import. Reads outside the apply transaction reuse the
// existing services and the default RepoBundle; the events repository is
// constructed from the bundle's underlying SQLite store because the
// RepoBundle struct does not expose an event accessor.
func NewPortabilityService(
	writeTx WriteTxProvider,
	tasks *TaskService,
	projects *ProjectService,
	workflows *WorkflowService,
	relations *RelationService,
	tags *TagService,
	players *PlayerService,
	notes *NoteService,
	bundle *RepoBundle,
	tuskVersion string,
) *PortabilityService {
	var events repository.EventRepository
	if bundle != nil && bundle.Store != nil {
		events = sqlite.NewEventRepo(bundle.Store.DB(), 0, 0)
	}
	return &PortabilityService{
		writeTx:     writeTx,
		tasks:       tasks,
		projects:    projects,
		workflows:   workflows,
		relations:   relations,
		tags:        tags,
		players:     players,
		notes:       notes,
		bundle:      bundle,
		events:      events,
		tuskVersion: tuskVersion,
	}
}

// ImportOptions controls Import behavior. Zero value is strict mode: fail
// on any collision, no truncation, full apply.
type ImportOptions struct {
	Replace  bool // row-level upsert on collision
	Truncate bool // wipe-and-restore mode; requires Replace
	DryRun   bool // run validation pass; report counts; no writes
}

// ImportReport summarizes what an Import did. Counts populate even on
// DryRun. Replaced is the number of rows updated under --replace (not
// net-new inserts). Truncated reflects whether tables were wiped before
// the apply pass. EventID is the workspace_imported event ID; uuid.Nil on
// DryRun.
type ImportReport struct {
	Workflows, Projects, Players, Tags, Tasks,
	Relations, Annotations, Notes, Events int

	Replaced  int
	Truncated bool
	EventID   uuid.UUID
}

// Export reads the entire workspace into a PortableWorkspace value.
//
// Export does not wrap the read in a transaction — under SQLite WAL,
// concurrent writers may produce a slightly inconsistent dump. Workspaces
// using portability for backup should pause writers themselves.
// (See the spec → "Known limitation".)
func (s *PortabilityService) Export(ctx context.Context) (*portability.PortableWorkspace, error) {
	workflows, err := s.workflows.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing workflows: %w", err)
	}
	projects, err := s.projects.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	players, err := s.players.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing players: %w", err)
	}
	tags, err := s.tags.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing tags: %w", err)
	}

	// Bypass TaskService.List so terminal tasks (completed/deleted) are
	// included and so we skip the urgency-scoring pass — portability is
	// workspace-wide, not user-facing.
	tasks, err := s.bundle.Tasks.List(ctx, &domain.TermFilter{})
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}

	taskIDs := make([]uuid.UUID, len(tasks))
	for index, task := range tasks {
		taskIDs[index] = task.ID
	}
	tagsByTask, err := s.bundle.Tags.GetTaskTagsBatch(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("loading task tags: %w", err)
	}

	relationDTOs, err := s.exportRelations(ctx, taskIDs)
	if err != nil {
		return nil, err
	}
	annotationDTOs, err := s.exportAnnotations(ctx, taskIDs)
	if err != nil {
		return nil, err
	}
	noteDTOs, err := s.exportNotes(ctx, projects)
	if err != nil {
		return nil, err
	}
	eventDTOs, err := s.exportEvents(ctx)
	if err != nil {
		return nil, err
	}

	workflowDTOs := make([]portability.PortableWorkflow, len(workflows))
	for index, workflow := range workflows {
		workflowDTOs[index] = workflowToPortable(workflow)
	}
	projectDTOs := make([]portability.PortableProject, len(projects))
	for index, project := range projects {
		projectDTOs[index] = projectToPortable(project)
	}
	playerDTOs := make([]portability.PortablePlayer, len(players))
	for index, player := range players {
		playerDTOs[index] = playerToPortable(player)
	}
	tagDTOs := make([]portability.PortableTag, len(tags))
	for index, tag := range tags {
		tagDTOs[index] = tagToPortable(tag)
	}
	taskDTOs := make([]portability.PortableTask, len(tasks))
	for index, task := range tasks {
		taskDTOs[index] = taskToPortable(task, tagsByTask[task.ID])
	}

	return &portability.PortableWorkspace{
		SchemaVersion: portability.SchemaVersion,
		TuskVersion:   s.tuskVersion,
		ExportedAt:    time.Now().UTC(),
		Workflows:     workflowDTOs,
		Projects:      projectDTOs,
		Players:       playerDTOs,
		Tags:          tagDTOs,
		Tasks:         taskDTOs,
		Relations:     relationDTOs,
		Annotations:   annotationDTOs,
		Notes:         noteDTOs,
		Events:        eventDTOs,
	}, nil
}

// exportRelations walks every task and aggregates relation rows, deduping
// by ID. The relation repository does not currently support a workspace-
// wide list, and adding one is out of scope for this phase.
func (s *PortabilityService) exportRelations(ctx context.Context, taskIDs []uuid.UUID) ([]portability.PortableRelation, error) {
	seen := make(map[uuid.UUID]struct{}, len(taskIDs))
	out := make([]portability.PortableRelation, 0)
	for _, id := range taskIDs {
		rels, err := s.bundle.Relations.GetByTask(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("loading relations for task %s: %w", id, err)
		}
		for _, relation := range rels {
			if _, dup := seen[relation.ID]; dup {
				continue
			}
			seen[relation.ID] = struct{}{}
			out = append(out, relationToPortable(relation))
		}
	}
	return out, nil
}

// exportAnnotations walks every task and concatenates annotation rows.
// Annotations are scoped to a single task, so dedupe is unnecessary.
func (s *PortabilityService) exportAnnotations(ctx context.Context, taskIDs []uuid.UUID) ([]portability.PortableAnnotation, error) {
	out := make([]portability.PortableAnnotation, 0)
	for _, id := range taskIDs {
		anns, err := s.bundle.Annotations.GetByTask(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("loading annotations for task %s: %w", id, err)
		}
		for _, annotation := range anns {
			out = append(out, annotationToPortable(annotation))
		}
	}
	return out, nil
}

// exportNotes iterates every project and lists its notes (active +
// archived) with no window cap. NoteService.List enforces a window
// override that would clip the dump, so we go through the bundle's note
// repo directly.
func (s *PortabilityService) exportNotes(ctx context.Context, projects []*domain.Project) ([]portability.PortableNote, error) {
	out := make([]portability.PortableNote, 0)
	for _, project := range projects {
		notes, err := s.bundle.Notes.List(ctx, repository.NoteListOptions{
			ProjectID:       project.ID,
			IncludeArchived: true,
			Limit:           0,
		})
		if err != nil {
			return nil, fmt.Errorf("loading notes for project %s: %w", project.ID, err)
		}
		for _, note := range notes {
			out = append(out, noteToPortable(note))
		}
	}
	return out, nil
}

// exportEvents reads every event from the workspace event log. Returns an
// empty slice when no event repository is wired (e.g. a pathological
// bundle), so callers always receive a non-nil list.
func (s *PortabilityService) exportEvents(ctx context.Context) ([]portability.PortableEvent, error) {
	if s.events == nil {
		return []portability.PortableEvent{}, nil
	}
	events, err := s.events.List(ctx, repository.EventFilter{})
	if err != nil {
		return nil, fmt.Errorf("listing events: %w", err)
	}
	out := make([]portability.PortableEvent, len(events))
	for index, event := range events {
		dto, err := eventToPortable(event)
		if err != nil {
			return nil, err
		}
		out[index] = dto
	}
	return out, nil
}
