// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/internal/portability"
	"github.com/google/uuid"
)

// validate runs every check defined in the data-portability spec and
// returns nil when the dump is clean. Otherwise it returns an *ImportError
// carrying every issue found in a single pass — the caller renders the
// full picture in one round-trip rather than reporting issues piecemeal.
//
// validate is read-only. Workspace residency lookups are issued through
// the bundle's repositories outside any transaction; the apply pass
// rechecks each existence inside the transaction via the same logic.
func (service *PortabilityService) validate(ctx context.Context, ws *portability.PortableWorkspace, opts ImportOptions) *portability.ImportError {
	var issues []portability.ImportIssue

	if ws.SchemaVersion != portability.SchemaVersion {
		issues = append(issues, portability.ImportIssue{
			Kind: "schema",
			Message: fmt.Sprintf(
				"unsupported schema_version %d (this tusk supports %d)",
				ws.SchemaVersion, portability.SchemaVersion,
			),
		})
	}

	dumpIDs := newDumpIndex(ws)

	issues = append(issues, service.checkReferentialIntegrity(ctx, ws, dumpIDs, opts)...)
	issues = append(issues, service.checkTaxonomy(ctx, ws)...)
	issues = append(issues, checkRelationCycles(ws.Relations)...)
	issues = append(issues, checkWorkflowWellFormed(ws.Workflows)...)
	if !opts.Truncate {
		issues = append(issues, service.checkCollisions(ctx, ws, opts)...)
	}

	if len(issues) == 0 {
		return nil
	}
	return &portability.ImportError{Issues: issues}
}

// dumpIndex is the set of entity identifiers reachable inside a single
// dump. validate uses it to short-circuit FK checks for refs that
// resolve within the dump itself.
type dumpIndex struct {
	tasks     map[uuid.UUID]struct{}
	projects  map[uuid.UUID]struct{}
	workflows map[uuid.UUID]struct{}
	players   map[string]struct{}
	tagsByID  map[uuid.UUID]struct{}
	tagNames  map[string]struct{}
}

func newDumpIndex(ws *portability.PortableWorkspace) *dumpIndex {
	idx := &dumpIndex{
		tasks:     make(map[uuid.UUID]struct{}, len(ws.Tasks)),
		projects:  make(map[uuid.UUID]struct{}, len(ws.Projects)),
		workflows: make(map[uuid.UUID]struct{}, len(ws.Workflows)),
		players:   make(map[string]struct{}, len(ws.Players)),
		tagsByID:  make(map[uuid.UUID]struct{}, len(ws.Tags)),
		tagNames:  make(map[string]struct{}, len(ws.Tags)),
	}
	for _, task := range ws.Tasks {
		idx.tasks[task.ID] = struct{}{}
	}
	for _, project := range ws.Projects {
		idx.projects[project.ID] = struct{}{}
	}
	for _, workflow := range ws.Workflows {
		idx.workflows[workflow.ID] = struct{}{}
	}
	for _, player := range ws.Players {
		idx.players[player.ID] = struct{}{}
	}
	for _, tag := range ws.Tags {
		idx.tagsByID[tag.ID] = struct{}{}
		idx.tagNames[tag.Name] = struct{}{}
	}
	return idx
}

// taskIdentifier returns the short ID when present, otherwise the full
// UUID so every issue carries a stable handle the caller can show.
func taskIdentifier(task portability.PortableTask) string {
	if task.ShortID != "" {
		return task.ShortID
	}
	return task.ID.String()
}

func (service *PortabilityService) checkReferentialIntegrity(
	ctx context.Context,
	ws *portability.PortableWorkspace,
	idx *dumpIndex,
	opts ImportOptions,
) []portability.ImportIssue {
	var issues []portability.ImportIssue

	taskExists := func(id uuid.UUID) bool {
		if _, ok := idx.tasks[id]; ok {
			return true
		}
		if !opts.Replace {
			return false
		}
		_, err := service.bundle.Tasks.GetByID(ctx, id)
		return err == nil
	}
	projectExists := func(id uuid.UUID) bool {
		if _, ok := idx.projects[id]; ok {
			return true
		}
		if !opts.Replace {
			return false
		}
		_, err := service.projects.GetByID(ctx, id)
		return err == nil
	}
	workflowExists := func(id uuid.UUID) bool {
		if _, ok := idx.workflows[id]; ok {
			return true
		}
		if !opts.Replace {
			return false
		}
		_, err := service.workflows.GetByID(ctx, id)
		return err == nil
	}
	playerExists := func(id string) bool {
		if id == "" {
			return true
		}
		if _, ok := idx.players[id]; ok {
			return true
		}
		if !opts.Replace {
			return false
		}
		_, err := service.players.GetByID(ctx, id)
		return err == nil
	}
	var (
		liveTagNames     map[string]struct{}
		liveTagNamesOnce bool
	)
	tagNameExists := func(name string) bool {
		if _, ok := idx.tagNames[name]; ok {
			return true
		}
		if !opts.Replace {
			return false
		}
		// TagService has no GetByName helper, so prefetch the live tag
		// set once and reuse it for every reference. Pulling the list
		// per missing tag would be O(N×M) over the dump size.
		if !liveTagNamesOnce {
			liveTagNamesOnce = true
			tags, err := service.tags.List(ctx)
			if err == nil {
				liveTagNames = make(map[string]struct{}, len(tags))
				for _, tag := range tags {
					liveTagNames[tag.Name] = struct{}{}
				}
			}
		}
		_, ok := liveTagNames[name]
		return ok
	}

	for _, task := range ws.Tasks {
		if task.ParentID != nil && !taskExists(*task.ParentID) {
			issues = append(issues, portability.ImportIssue{
				Kind:       "fk",
				EntityKind: "task",
				EntityID:   taskIdentifier(task),
				Message:    fmt.Sprintf("parent_id %s does not resolve to a task in the dump or workspace", task.ParentID),
			})
		}
		if !projectExists(task.ProjectID) {
			issues = append(issues, portability.ImportIssue{
				Kind:       "fk",
				EntityKind: "task",
				EntityID:   taskIdentifier(task),
				Message:    fmt.Sprintf("project_id %s does not resolve to a project in the dump or workspace", task.ProjectID),
			})
		}
		for _, name := range task.Tags {
			if !tagNameExists(name) {
				issues = append(issues, portability.ImportIssue{
					Kind:       "fk",
					EntityKind: "task",
					EntityID:   taskIdentifier(task),
					Message:    fmt.Sprintf("tag %q is not declared in the dump or workspace", name),
				})
			}
		}
	}

	for _, relation := range ws.Relations {
		if !taskExists(relation.SourceID) {
			issues = append(issues, portability.ImportIssue{
				Kind:       "fk",
				EntityKind: "relation",
				EntityID:   relation.ID.String(),
				Message:    fmt.Sprintf("source_id %s does not resolve to a task in the dump or workspace", relation.SourceID),
			})
		}
		if !taskExists(relation.TargetID) {
			issues = append(issues, portability.ImportIssue{
				Kind:       "fk",
				EntityKind: "relation",
				EntityID:   relation.ID.String(),
				Message:    fmt.Sprintf("target_id %s does not resolve to a task in the dump or workspace", relation.TargetID),
			})
		}
	}

	for _, annotation := range ws.Annotations {
		if !taskExists(annotation.TaskID) {
			issues = append(issues, portability.ImportIssue{
				Kind:       "fk",
				EntityKind: "annotation",
				EntityID:   annotation.ID.String(),
				Message:    fmt.Sprintf("task_id %s does not resolve to a task in the dump or workspace", annotation.TaskID),
			})
		}
	}

	for _, note := range ws.Notes {
		if !projectExists(note.ProjectID) {
			issues = append(issues, portability.ImportIssue{
				Kind:       "fk",
				EntityKind: "note",
				EntityID:   note.ID.String(),
				Message:    fmt.Sprintf("project_id %s does not resolve to a project in the dump or workspace", note.ProjectID),
			})
		}
		if !playerExists(note.PlayerID) {
			issues = append(issues, portability.ImportIssue{
				Kind:       "fk",
				EntityKind: "note",
				EntityID:   note.ID.String(),
				Message:    fmt.Sprintf("player_id %q does not resolve to a player in the dump or workspace", note.PlayerID),
			})
		}
		if note.TaskID != nil && !taskExists(*note.TaskID) {
			issues = append(issues, portability.ImportIssue{
				Kind:       "fk",
				EntityKind: "note",
				EntityID:   note.ID.String(),
				Message:    fmt.Sprintf("task_id %s does not resolve to a task in the dump or workspace", *note.TaskID),
			})
		}
	}

	for _, project := range ws.Projects {
		if !workflowExists(project.WorkflowID) {
			issues = append(issues, portability.ImportIssue{
				Kind:       "fk",
				EntityKind: "project",
				EntityID:   project.ID.String(),
				Message:    fmt.Sprintf("workflow_id %s does not resolve to a workflow in the dump or workspace", project.WorkflowID),
			})
		}
	}

	return issues
}

// checkTaxonomy applies the project's effective taxonomy to every task in
// that project. Parent levels are pre-computed by walking the dump once
// so the check works on dumps that do not match the live workspace yet.
func (service *PortabilityService) checkTaxonomy(
	ctx context.Context,
	ws *portability.PortableWorkspace,
) []portability.ImportIssue {
	if len(ws.Tasks) == 0 {
		return nil
	}

	projectsByID := make(map[uuid.UUID]*domain.Project, len(ws.Projects))
	for index := range ws.Projects {
		domainProject := projectFromPortable(ws.Projects[index])
		projectsByID[domainProject.ID] = domainProject
	}
	resolveProject := func(id uuid.UUID) *domain.Project {
		if project, ok := projectsByID[id]; ok {
			return project
		}
		live, err := service.projects.GetByID(ctx, id)

		if err != nil {
			return nil
		}

		projectsByID[id] = live
		return live
	}

	taskByID := make(map[uuid.UUID]portability.PortableTask, len(ws.Tasks))
	for _, task := range ws.Tasks {
		taskByID[task.ID] = task
	}

	parentLevel := func(task portability.PortableTask) *string {
		if task.ParentID == nil {
			return nil
		}
		parent, ok := taskByID[*task.ParentID]
		if !ok {
			// Parent not in dump (must already exist in workspace under
			// --replace; FK check covers --strict); cannot resolve a
			// definitive level so skip the parent-rank constraint.
			return nil
		}
		if parent.Level == nil {
			empty := ""
			return &empty
		}
		v := *parent.Level
		return &v
	}

	var issues []portability.ImportIssue
	for _, task := range ws.Tasks {
		project := resolveProject(task.ProjectID)
		if project == nil {
			// Project resolves nowhere — the FK check reports that
			// separately, so taxonomy enforcement is moot.
			continue
		}
		taxonomy, _ := service.projects.EffectiveTaxonomy(project)
		if taxonomy.IsEmpty() {
			continue
		}
		domainTask := taskFromPortable(task)
		err := domain.TaxonomyValidator{}.Check(domain.ValidationContext{
			Taxonomy:    taxonomy,
			ParentLevel: parentLevel(task),
		}, domainTask)
		if err == nil {
			continue
		}
		issues = append(issues, portability.ImportIssue{
			Kind:       "taxonomy",
			EntityKind: "task",
			EntityID:   taskIdentifier(task),
			Message:    err.Error(),
		})
	}
	return issues
}

// checkRelationCycles runs a DFS over every "blocks" edge in the dump and
// reports each cycle as a single issue. The implementation mirrors the
// canonical DFS in service/relation.go (RelationService.checkCycle); the
// two versions diverge because that one queries through a transactional
// repository while this one walks the dump's slice directly.
func checkRelationCycles(rels []portability.PortableRelation) []portability.ImportIssue {
	if len(rels) == 0 {
		return nil
	}
	adj := make(map[uuid.UUID][]uuid.UUID)
	relByEdge := make(map[[2]uuid.UUID]uuid.UUID)
	for _, relation := range rels {
		if relation.RelationType != "blocks" {
			continue
		}
		adj[relation.SourceID] = append(adj[relation.SourceID], relation.TargetID)
		relByEdge[[2]uuid.UUID{relation.SourceID, relation.TargetID}] = relation.ID
	}
	if len(adj) == 0 {
		return nil
	}

	const (
		stateVisiting = 1
		stateDone     = 2
	)
	state := make(map[uuid.UUID]int)
	var issues []portability.ImportIssue
	reported := make(map[uuid.UUID]struct{})

	var dfs func(node uuid.UUID, stack []uuid.UUID)
	dfs = func(node uuid.UUID, stack []uuid.UUID) {
		state[node] = stateVisiting
		stack = append(stack, node)
		for _, next := range adj[node] {
			switch state[next] {
			case stateVisiting:
				cycle := append([]uuid.UUID(nil), stack...)
				cycle = append(cycle, next)
				participants := make([]string, 0, len(cycle))
				start := 0
				for index, id := range cycle {
					if id == next {
						start = index
						break
					}
				}
				for _, id := range cycle[start:] {
					participants = append(participants, id.String())
				}
				edgeID, ok := relByEdge[[2]uuid.UUID{node, next}]
				if !ok {
					edgeID = uuid.Nil
				}
				if _, dup := reported[edgeID]; dup {
					continue
				}
				reported[edgeID] = struct{}{}
				issues = append(issues, portability.ImportIssue{
					Kind:       "cycle",
					EntityKind: "relation",
					EntityID:   edgeID.String(),
					Message:    fmt.Sprintf("blocks cycle through %s", strings.Join(participants, " -> ")),
				})
			case stateDone:
				continue
			default:
				dfs(next, stack)
			}
		}
		state[node] = stateDone
	}

	roots := make([]uuid.UUID, 0, len(adj))
	for src := range adj {
		roots = append(roots, src)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].String() < roots[j].String() })
	for _, root := range roots {
		if state[root] == stateDone {
			continue
		}
		dfs(root, nil)
	}
	return issues
}

// checkWorkflowWellFormed validates each workflow against the same rules
// the service layer enforces on Create/Modify so a dump can never write a
// workflow the live system would refuse.
func checkWorkflowWellFormed(workflows []portability.PortableWorkflow) []portability.ImportIssue {
	var issues []portability.ImportIssue
	for _, workflow := range workflows {
		domainWorkflow := workflowFromPortable(workflow)
		if err := domain.ValidateWorkflow(domainWorkflow); err != nil {
			issues = append(issues, portability.ImportIssue{
				Kind:       "workflow",
				EntityKind: "workflow",
				EntityID:   workflow.ID.String(),
				Message:    err.Error(),
			})
		}
	}
	return issues
}

// checkCollisions reports every entity in the dump whose ID already
// exists in the workspace when --replace is not set. Under --replace the
// apply pass takes care of overwriting the row; under --truncate the
// caller already cleared the table, so we skip the check entirely.
func (service *PortabilityService) checkCollisions(
	ctx context.Context,
	ws *portability.PortableWorkspace,
	opts ImportOptions,
) []portability.ImportIssue {
	if opts.Replace {
		return nil
	}
	var issues []portability.ImportIssue

	for _, workflow := range ws.Workflows {
		if _, err := service.workflows.GetByID(ctx, workflow.ID); err == nil {
			issues = append(issues, collisionIssue("workflow", workflow.ID.String()))
		}
	}
	for _, project := range ws.Projects {
		if _, err := service.projects.GetByID(ctx, project.ID); err == nil {
			issues = append(issues, collisionIssue("project", project.ID.String()))
		}
	}
	for _, player := range ws.Players {
		if _, err := service.players.GetByID(ctx, player.ID); err == nil {
			issues = append(issues, collisionIssue("player", player.ID))
		}
	}
	tagByID := make(map[uuid.UUID]struct{}, len(ws.Tags))
	if len(ws.Tags) > 0 {
		existing, err := service.tags.List(ctx)
		if err == nil {
			for _, tag := range existing {
				tagByID[tag.ID] = struct{}{}
			}
		}
		for _, tag := range ws.Tags {
			if _, ok := tagByID[tag.ID]; ok {
				issues = append(issues, collisionIssue("tag", tag.ID.String()))
			}
		}
	}
	for _, task := range ws.Tasks {
		if _, err := service.bundle.Tasks.GetByID(ctx, task.ID); err == nil {
			issues = append(issues, collisionIssue("task", taskIdentifier(task)))
		} else if !errors.Is(err, domain.ErrNotFound) {
			// Surface unexpected errors as a collision too — silent
			// failures here would let the apply pass crash mid-import.
			issues = append(issues, portability.ImportIssue{
				Kind:       "collision",
				EntityKind: "task",
				EntityID:   taskIdentifier(task),
				Message:    fmt.Sprintf("could not check collision: %v", err),
			})
		}
	}
	for _, relation := range ws.Relations {
		existing, err := service.bundle.Relations.GetByTask(ctx, relation.SourceID)

		if err != nil {
			continue
		}

		for _, existing2 := range existing {
			if existing2.ID == relation.ID {
				issues = append(issues, collisionIssue("relation", relation.ID.String()))
				break
			}
		}
	}
	for _, annotation := range ws.Annotations {
		existing, err := service.bundle.Annotations.GetByTask(ctx, annotation.TaskID)

		if err != nil {
			continue
		}

		for _, existing2 := range existing {
			if existing2.ID == annotation.ID {
				issues = append(issues, collisionIssue("annotation", annotation.ID.String()))
				break
			}
		}
	}
	for _, note := range ws.Notes {
		if _, err := service.bundle.Notes.GetByID(ctx, note.ID); err == nil {
			issues = append(issues, collisionIssue("note", note.ID.String()))
		}
	}
	return issues
}

func collisionIssue(kind, id string) portability.ImportIssue {
	return portability.ImportIssue{
		Kind:       "collision",
		EntityKind: kind,
		EntityID:   id,
		Message:    "entity already exists; use --replace to overwrite",
	}
}
