// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/internal/portability"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// Import validates the dump in one pass, then applies it inside a single
// WriteTx. Returns *portability.ImportError on validation failure; the
// error carries every issue detected.
//
// On opts.DryRun the validation pass runs, the report's per-kind counts
// populate, and no writes happen.
//
// Import bypasses the usual optimistic-locking version check so faithful
// round-trips preserve the dump's version values exactly.
// (See spec → "Optimistic locking under --replace".)
func (service *PortabilityService) Import(
	ctx context.Context,
	ws *portability.PortableWorkspace,
	opts ImportOptions,
) (*ImportReport, error) {
	report := &ImportReport{}

	if opts.Truncate && !opts.Replace {
		return report, &portability.ImportError{Issues: []portability.ImportIssue{{
			Kind:    "schema",
			Message: "--truncate requires --replace",
		}}}
	}

	if validationErr := service.validate(ctx, ws, opts); validationErr != nil {
		populateCounts(report, ws)
		return report, validationErr
	}

	if opts.DryRun {
		populateCounts(report, ws)
		return report, nil
	}

	err := service.writeTx.WithTx(ctx, func(tx WriteTx) error {
		if opts.Truncate {
			if err := tx.TruncateAll(ctx); err != nil {
				return fmt.Errorf("truncating workspace: %w", err)
			}
			report.Truncated = true
		}
		if err := service.applyWorkflows(ctx, tx, ws, report); err != nil {
			return err
		}
		if err := service.applyPlayers(ctx, tx, ws, report); err != nil {
			return err
		}
		if err := service.applyProjects(ctx, tx, ws, report); err != nil {
			return err
		}
		if err := service.applyTags(ctx, tx, ws, report); err != nil {
			return err
		}
		if err := service.applyTasks(ctx, tx, ws, report); err != nil {
			return err
		}
		if err := service.applyTaskTags(ctx, tx, ws); err != nil {
			return err
		}
		if err := service.applyRelations(ctx, tx, ws, report); err != nil {
			return err
		}
		if err := service.applyAnnotations(ctx, tx, ws, report); err != nil {
			return err
		}
		if err := service.applyNotes(ctx, tx, ws, report); err != nil {
			return err
		}
		if err := service.applyEvents(ctx, tx, ws, report); err != nil {
			return err
		}
		evtID, evtErr := service.recordImportEvent(ctx, tx, ws, opts, report)

		if evtErr != nil {
			return evtErr
		}

		report.EventID = evtID
		return nil
	})

	if err != nil {
		return report, err
	}

	return report, nil
}

// populateCounts fills the report with per-kind sizes from the dump.
// Used by DryRun and on validation failure so callers see what would have
// been applied without inspecting the dump themselves.
func populateCounts(report *ImportReport, ws *portability.PortableWorkspace) {
	report.Workflows = len(ws.Workflows)
	report.Projects = len(ws.Projects)
	report.Players = len(ws.Players)
	report.Tags = len(ws.Tags)
	report.Tasks = len(ws.Tasks)
	report.Relations = len(ws.Relations)
	report.Annotations = len(ws.Annotations)
	report.Notes = len(ws.Notes)
	report.Events = len(ws.Events)
}

func (service *PortabilityService) applyWorkflows(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	for _, workflow := range ws.Workflows {
		dom := workflowFromPortable(workflow)
		existing, err := tx.Workflows().GetByID(ctx, workflow.ID)

		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("looking up workflow %s: %w", workflow.ID, err)
		}

		if existing != nil {
			// projects.workflow_id has ON DELETE RESTRICT, so a
			// delete-then-create would fail whenever a project still
			// points at this workflow. Use --truncate when faithful
			// workflow replacement matters; otherwise preserve the
			// existing row.
			continue
		}
		if err := tx.Workflows().Create(ctx, dom); err != nil {
			return fmt.Errorf("creating workflow %s: %w", workflow.ID, err)
		}
		report.Workflows++
	}
	return nil
}

func (service *PortabilityService) applyPlayers(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	for _, player := range ws.Players {
		dom := playerFromPortable(player)
		existing, err := tx.Players().GetByID(ctx, player.ID)

		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("looking up player %q: %w", player.ID, err)
		}

		if existing != nil {
			// PlayerRepository has no Delete primitive, so a faithful
			// overwrite is only possible after --truncate. Without it,
			// preserve the existing row to keep --replace from
			// crashing on a UNIQUE collision.
			continue
		}
		if err := tx.Players().Create(ctx, dom); err != nil {
			return fmt.Errorf("creating player %q: %w", player.ID, err)
		}
		report.Players++
	}
	return nil
}

func (service *PortabilityService) applyProjects(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	for _, project := range ws.Projects {
		dom := projectFromPortable(project)
		existing, err := tx.Projects().GetByID(ctx, project.ID)

		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("looking up project %s: %w", project.ID, err)
		}

		if existing != nil {
			// tasks.project_id and notes.project_id both use ON DELETE
			// RESTRICT, so we cannot drop and recreate a project that
			// owns rows. Preserve the existing row; --truncate is the
			// supported path when faithful project replacement matters.
			continue
		}
		if err := tx.Projects().Create(ctx, dom); err != nil {
			return fmt.Errorf("creating project %s: %w", project.ID, err)
		}
		report.Projects++
	}
	return nil
}

func (service *PortabilityService) applyTags(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	for _, tag := range ws.Tags {
		dom := tagFromPortable(tag)
		existing, err := tx.Tags().GetByID(ctx, tag.ID)

		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("looking up tag %s: %w", tag.ID, err)
		}

		if existing != nil {
			if err := tx.Tags().Delete(ctx, existing.ID); err != nil {
				return fmt.Errorf("replacing tag %s: %w", tag.ID, err)
			}
			report.Replaced++
		}
		if err := tx.Tags().Create(ctx, dom); err != nil {
			return fmt.Errorf("creating tag %s: %w", tag.ID, err)
		}
		report.Tags++
	}
	return nil
}

// applyTasks inserts every task from the dump in a parent-before-child
// order so each task's parent_id resolves on the initial Create call —
// no second pass is required, version values land verbatim, and the
// optimistic-locking column is never advanced.
func (service *PortabilityService) applyTasks(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	if len(ws.Tasks) == 0 {
		return nil
	}
	byID := make(map[uuid.UUID]portability.PortableTask, len(ws.Tasks))
	for _, task := range ws.Tasks {
		byID[task.ID] = task
	}

	inserted := make(map[uuid.UUID]struct{}, len(ws.Tasks))
	pending := make([]portability.PortableTask, len(ws.Tasks))
	copy(pending, ws.Tasks)

	for {
		progress := false
		next := pending[:0]
		for _, task := range pending {
			ready := false
			if task.ParentID == nil {
				ready = true
			} else if _, ok := inserted[*task.ParentID]; ok {
				ready = true
			} else if _, ok := byID[*task.ParentID]; !ok {
				// Parent already lives in the workspace (validation
				// confirmed it exists when --replace is set). Insert
				// now — sqlite resolves the FK against the live row.
				ready = true
			}
			if !ready {
				next = append(next, task)
				continue
			}
			if err := service.applyOneTask(ctx, tx, task, report); err != nil {
				return err
			}
			inserted[task.ID] = struct{}{}
			progress = true
		}
		pending = next
		if len(pending) == 0 {
			return nil
		}
		if !progress {
			orphans := make([]string, 0, len(pending))
			for _, task := range pending {
				orphans = append(orphans, taskIdentifier(task))
			}
			return fmt.Errorf("portability: cycle in task hierarchy among %v", orphans)
		}
	}
}

func (service *PortabilityService) applyOneTask(
	ctx context.Context,
	tx WriteTx,
	task portability.PortableTask,
	report *ImportReport,
) error {
	dom := taskFromPortable(task)
	existing, err := tx.Tasks().GetByID(ctx, task.ID)

	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("looking up task %s: %w", task.ID, err)
	}

	if existing != nil {
		if err := tx.Tasks().Delete(ctx, existing.ID, existing.Version); err != nil {
			return fmt.Errorf("replacing task %s: %w", task.ID, err)
		}
		report.Replaced++
	}
	if err := tx.Tasks().Create(ctx, dom); err != nil {
		return fmt.Errorf("creating task %s: %w", task.ID, err)
	}
	report.Tasks++
	return nil
}

// applyTaskTags rewrites the task_tags join after the tasks themselves
// have landed. We use the tag repository's AssignToTask/RemoveFromTask
// primitives which already implement INSERT OR IGNORE, so reapplying the
// same dump is idempotent without requiring extra reconciliation logic.
func (service *PortabilityService) applyTaskTags(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
) error {
	if len(ws.Tasks) == 0 {
		return nil
	}
	tagsByName := make(map[string]uuid.UUID)
	for _, tag := range ws.Tags {
		tagsByName[tag.Name] = tag.ID
	}
	getTagID := func(name string) (uuid.UUID, error) {
		if id, ok := tagsByName[name]; ok {
			return id, nil
		}
		live, err := tx.Tags().GetByName(ctx, name)

		if err != nil {
			return uuid.Nil, fmt.Errorf("resolving tag %q: %w", name, err)
		}

		tagsByName[name] = live.ID
		return live.ID, nil
	}

	for _, task := range ws.Tasks {
		// Drop existing assignments so the join table mirrors the dump
		// when --replace is set without --truncate. GetTaskTags returns
		// every assignment regardless of source; remove what is no longer
		// in the dump's Tags slice and add what is missing.
		existing, err := tx.Tags().GetTaskTags(ctx, task.ID)

		if err != nil {
			return fmt.Errorf("loading existing tag assignments for task %s: %w", task.ID, err)
		}

		want := make(map[string]struct{}, len(task.Tags))
		for _, name := range task.Tags {
			want[name] = struct{}{}
		}
		for _, tag := range existing {
			if _, keep := want[tag.Name]; keep {
				continue
			}
			if err := tx.Tags().RemoveFromTask(ctx, task.ID, tag.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
				return fmt.Errorf("removing tag %q from task %s: %w", tag.Name, task.ID, err)
			}
		}
		for _, name := range task.Tags {
			tagID, tagErr := getTagID(name)

			if tagErr != nil {
				return tagErr
			}

			if err := tx.Tags().AssignToTask(ctx, task.ID, tagID); err != nil {
				return fmt.Errorf("assigning tag %q to task %s: %w", name, task.ID, err)
			}
		}
	}
	return nil
}

func (service *PortabilityService) applyRelations(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	for _, relation := range ws.Relations {
		dom := relationFromPortable(relation)
		existing, err := service.relationExists(ctx, tx, relation.SourceID, relation.ID)

		if err != nil {
			return err
		}

		if existing {
			if err := tx.Relations().Delete(ctx, relation.ID); err != nil {
				return fmt.Errorf("replacing relation %s: %w", relation.ID, err)
			}
			report.Replaced++
		}
		if err := tx.Relations().Create(ctx, dom); err != nil {
			return fmt.Errorf("creating relation %s: %w", relation.ID, err)
		}
		report.Relations++
	}
	return nil
}

func (service *PortabilityService) relationExists(
	ctx context.Context,
	tx WriteTx,
	sourceID, relationID uuid.UUID,
) (bool, error) {
	rels, err := tx.Relations().GetByTask(ctx, sourceID)

	if err != nil {
		return false, fmt.Errorf("looking up relations on task %s: %w", sourceID, err)
	}

	for _, rel := range rels {
		if rel.ID == relationID {
			return true, nil
		}
	}
	return false, nil
}

func (service *PortabilityService) applyAnnotations(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	for _, annotation := range ws.Annotations {
		dom := annotationFromPortable(annotation)
		existing, err := service.annotationExists(ctx, tx, annotation.TaskID, annotation.ID)

		if err != nil {
			return err
		}

		if existing {
			if err := tx.Annotations().Delete(ctx, annotation.ID); err != nil {
				return fmt.Errorf("replacing annotation %s: %w", annotation.ID, err)
			}
			report.Replaced++
		}
		if err := tx.Annotations().Create(ctx, dom); err != nil {
			return fmt.Errorf("creating annotation %s: %w", annotation.ID, err)
		}
		report.Annotations++
	}
	return nil
}

func (service *PortabilityService) annotationExists(
	ctx context.Context,
	tx WriteTx,
	taskID, annotationID uuid.UUID,
) (bool, error) {
	anns, err := tx.Annotations().GetByTask(ctx, taskID)

	if err != nil {
		return false, fmt.Errorf("looking up annotations on task %s: %w", taskID, err)
	}

	for _, ann := range anns {
		if ann.ID == annotationID {
			return true, nil
		}
	}
	return false, nil
}

func (service *PortabilityService) applyNotes(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	for _, note := range ws.Notes {
		dom := noteFromPortable(note)
		existing, err := tx.Notes().GetByID(ctx, note.ID)

		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("looking up note %s: %w", note.ID, err)
		}

		if existing != nil {
			// NoteRepository is append-only at the storage layer, so
			// without --truncate we cannot rewrite an existing note's
			// body. Preserve the live row to keep --replace safe.
			continue
		}
		if err := tx.Notes().Create(ctx, dom); err != nil {
			return fmt.Errorf("creating note %s: %w", note.ID, err)
		}
		report.Notes++
	}
	return nil
}

// applyEvents replays the dump's audit log in arrival order. Events whose
// ID already lives in the workspace are skipped so a partial dump replay
// is idempotent; under --truncate every dump event is new and lands. The
// codec stores payloads as raw JSON, so future event types round-trip
// even when the running tusk does not recognize them.
func (service *PortabilityService) applyEvents(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	if len(ws.Events) == 0 {
		return nil
	}
	existing, err := tx.Events().List(ctx, repository.EventFilter{})

	if err != nil {
		return fmt.Errorf("listing existing events: %w", err)
	}

	seen := make(map[uuid.UUID]struct{}, len(existing))
	for _, ev := range existing {
		seen[ev.ID] = struct{}{}
	}
	for _, ev := range ws.Events {
		if _, dup := seen[ev.ID]; dup {
			continue
		}
		dom, eventErr := eventFromPortable(ev)

		if eventErr != nil {
			return eventErr
		}

		if err := tx.Events().Record(ctx, dom); err != nil {
			return fmt.Errorf("recording imported event %s: %w", ev.ID, err)
		}
		seen[ev.ID] = struct{}{}
		report.Events++
	}
	return nil
}

// recordImportEvent emits the workspace_imported event inside the apply
// transaction so a successful Import is atomic with its audit-trail
// announcement. Counts mirror the per-kind report fields after the
// apply pass; Replaced and Truncated come straight from opts.
func (service *PortabilityService) recordImportEvent(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	opts ImportOptions,
	report *ImportReport,
) (uuid.UUID, error) {
	payload := domain.WorkspaceImportedPayload{
		Kind:          domain.EventWorkspaceImported,
		SchemaVersion: ws.SchemaVersion,
		SourceTuskVer: ws.TuskVersion,
		ExportedAt:    ws.ExportedAt,
		Replace:       opts.Replace,
		Truncate:      opts.Truncate,
		Counts: map[string]int{
			"workflows":   report.Workflows,
			"projects":    report.Projects,
			"players":     report.Players,
			"tags":        report.Tags,
			"tasks":       report.Tasks,
			"relations":   report.Relations,
			"annotations": report.Annotations,
			"notes":       report.Notes,
			"events":      report.Events,
		},
	}
	evt := &domain.Event{
		ID:         uuid.New(),
		Type:       domain.EventWorkspaceImported,
		EntityID:   "",
		EntityKind: domain.EntityWorkspace,
		PlayerID:   ActorFromContext(ctx),
		Payload:    payload,
		CreatedAt:  time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := tx.Events().Record(ctx, evt); err != nil {
		return uuid.Nil, fmt.Errorf("recording workspace_imported event: %w", err)
	}
	return evt.ID, nil
}
