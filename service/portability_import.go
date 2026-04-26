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
func (s *PortabilityService) Import(
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

	if validationErr := s.validate(ctx, ws, opts); validationErr != nil {
		populateCounts(report, ws)
		return report, validationErr
	}

	if opts.DryRun {
		populateCounts(report, ws)
		return report, nil
	}

	err := s.writeTx.WithTx(ctx, func(tx WriteTx) error {
		if opts.Truncate {
			if err := tx.TruncateAll(ctx); err != nil {
				return fmt.Errorf("truncating workspace: %w", err)
			}
			report.Truncated = true
		}
		if err := s.applyWorkflows(ctx, tx, ws, report); err != nil {
			return err
		}
		if err := s.applyPlayers(ctx, tx, ws, report); err != nil {
			return err
		}
		if err := s.applyProjects(ctx, tx, ws, report); err != nil {
			return err
		}
		if err := s.applyTags(ctx, tx, ws, report); err != nil {
			return err
		}
		if err := s.applyTasks(ctx, tx, ws, report); err != nil {
			return err
		}
		if err := s.applyTaskTags(ctx, tx, ws); err != nil {
			return err
		}
		if err := s.applyRelations(ctx, tx, ws, report); err != nil {
			return err
		}
		if err := s.applyAnnotations(ctx, tx, ws, report); err != nil {
			return err
		}
		if err := s.applyNotes(ctx, tx, ws, report); err != nil {
			return err
		}
		if err := s.applyEvents(ctx, tx, ws, report); err != nil {
			return err
		}
		evtID, err := s.recordImportEvent(ctx, tx, ws, opts, report)
		if err != nil {
			return err
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
func populateCounts(r *ImportReport, ws *portability.PortableWorkspace) {
	r.Workflows = len(ws.Workflows)
	r.Projects = len(ws.Projects)
	r.Players = len(ws.Players)
	r.Tags = len(ws.Tags)
	r.Tasks = len(ws.Tasks)
	r.Relations = len(ws.Relations)
	r.Annotations = len(ws.Annotations)
	r.Notes = len(ws.Notes)
	r.Events = len(ws.Events)
}

func (s *PortabilityService) applyWorkflows(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	for _, w := range ws.Workflows {
		dom := workflowFromPortable(w)
		existing, err := tx.Workflows().GetByID(ctx, w.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("looking up workflow %s: %w", w.ID, err)
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
			return fmt.Errorf("creating workflow %s: %w", w.ID, err)
		}
		report.Workflows++
	}
	return nil
}

func (s *PortabilityService) applyPlayers(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	for _, p := range ws.Players {
		dom := playerFromPortable(p)
		existing, err := tx.Players().GetByID(ctx, p.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("looking up player %q: %w", p.ID, err)
		}
		if existing != nil {
			// PlayerRepository has no Delete primitive, so a faithful
			// overwrite is only possible after --truncate. Without it,
			// preserve the existing row to keep --replace from
			// crashing on a UNIQUE collision.
			continue
		}
		if err := tx.Players().Create(ctx, dom); err != nil {
			return fmt.Errorf("creating player %q: %w", p.ID, err)
		}
		report.Players++
	}
	return nil
}

func (s *PortabilityService) applyProjects(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	for _, p := range ws.Projects {
		dom := projectFromPortable(p)
		existing, err := tx.Projects().GetByID(ctx, p.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("looking up project %s: %w", p.ID, err)
		}
		if existing != nil {
			// tasks.project_id and notes.project_id both use ON DELETE
			// RESTRICT, so we cannot drop and recreate a project that
			// owns rows. Preserve the existing row; --truncate is the
			// supported path when faithful project replacement matters.
			continue
		}
		if err := tx.Projects().Create(ctx, dom); err != nil {
			return fmt.Errorf("creating project %s: %w", p.ID, err)
		}
		report.Projects++
	}
	return nil
}

func (s *PortabilityService) applyTags(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	for _, t := range ws.Tags {
		dom := tagFromPortable(t)
		existing, err := tx.Tags().GetByID(ctx, t.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("looking up tag %s: %w", t.ID, err)
		}
		if existing != nil {
			if err := tx.Tags().Delete(ctx, existing.ID); err != nil {
				return fmt.Errorf("replacing tag %s: %w", t.ID, err)
			}
			report.Replaced++
		}
		if err := tx.Tags().Create(ctx, dom); err != nil {
			return fmt.Errorf("creating tag %s: %w", t.ID, err)
		}
		report.Tags++
	}
	return nil
}

// applyTasks inserts every task from the dump in a parent-before-child
// order so each task's parent_id resolves on the initial Create call —
// no second pass is required, version values land verbatim, and the
// optimistic-locking column is never advanced.
func (s *PortabilityService) applyTasks(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	if len(ws.Tasks) == 0 {
		return nil
	}
	byID := make(map[uuid.UUID]portability.PortableTask, len(ws.Tasks))
	for _, t := range ws.Tasks {
		byID[t.ID] = t
	}

	inserted := make(map[uuid.UUID]struct{}, len(ws.Tasks))
	pending := make([]portability.PortableTask, len(ws.Tasks))
	copy(pending, ws.Tasks)

	for {
		progress := false
		next := pending[:0]
		for _, t := range pending {
			ready := false
			if t.ParentID == nil {
				ready = true
			} else if _, ok := inserted[*t.ParentID]; ok {
				ready = true
			} else if _, ok := byID[*t.ParentID]; !ok {
				// Parent already lives in the workspace (validation
				// confirmed it exists when --replace is set). Insert
				// now — sqlite resolves the FK against the live row.
				ready = true
			}
			if !ready {
				next = append(next, t)
				continue
			}
			if err := s.applyOneTask(ctx, tx, t, report); err != nil {
				return err
			}
			inserted[t.ID] = struct{}{}
			progress = true
		}
		pending = next
		if len(pending) == 0 {
			return nil
		}
		if !progress {
			orphans := make([]string, 0, len(pending))
			for _, t := range pending {
				orphans = append(orphans, taskIdentifier(t))
			}
			return fmt.Errorf("portability: cycle in task hierarchy among %v", orphans)
		}
	}
}

func (s *PortabilityService) applyOneTask(
	ctx context.Context,
	tx WriteTx,
	t portability.PortableTask,
	report *ImportReport,
) error {
	dom := taskFromPortable(t)
	existing, err := tx.Tasks().GetByID(ctx, t.ID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("looking up task %s: %w", t.ID, err)
	}
	if existing != nil {
		if err := tx.Tasks().Delete(ctx, existing.ID, existing.Version); err != nil {
			return fmt.Errorf("replacing task %s: %w", t.ID, err)
		}
		report.Replaced++
	}
	if err := tx.Tasks().Create(ctx, dom); err != nil {
		return fmt.Errorf("creating task %s: %w", t.ID, err)
	}
	report.Tasks++
	return nil
}

// applyTaskTags rewrites the task_tags join after the tasks themselves
// have landed. We use the tag repository's AssignToTask/RemoveFromTask
// primitives which already implement INSERT OR IGNORE, so reapplying the
// same dump is idempotent without requiring extra reconciliation logic.
func (s *PortabilityService) applyTaskTags(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
) error {
	if len(ws.Tasks) == 0 {
		return nil
	}
	tagsByName := make(map[string]uuid.UUID)
	for _, t := range ws.Tags {
		tagsByName[t.Name] = t.ID
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

	for _, t := range ws.Tasks {
		// Drop existing assignments so the join table mirrors the dump
		// when --replace is set without --truncate. GetTaskTags returns
		// every assignment regardless of source; remove what is no longer
		// in the dump's Tags slice and add what is missing.
		existing, err := tx.Tags().GetTaskTags(ctx, t.ID)
		if err != nil {
			return fmt.Errorf("loading existing tag assignments for task %s: %w", t.ID, err)
		}
		want := make(map[string]struct{}, len(t.Tags))
		for _, name := range t.Tags {
			want[name] = struct{}{}
		}
		for _, tag := range existing {
			if _, keep := want[tag.Name]; keep {
				continue
			}
			if err := tx.Tags().RemoveFromTask(ctx, t.ID, tag.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
				return fmt.Errorf("removing tag %q from task %s: %w", tag.Name, t.ID, err)
			}
		}
		for _, name := range t.Tags {
			tagID, err := getTagID(name)
			if err != nil {
				return err
			}
			if err := tx.Tags().AssignToTask(ctx, t.ID, tagID); err != nil {
				return fmt.Errorf("assigning tag %q to task %s: %w", name, t.ID, err)
			}
		}
	}
	return nil
}

func (s *PortabilityService) applyRelations(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	for _, r := range ws.Relations {
		dom := relationFromPortable(r)
		existing, err := s.relationExists(ctx, tx, r.SourceID, r.ID)
		if err != nil {
			return err
		}
		if existing {
			if err := tx.Relations().Delete(ctx, r.ID); err != nil {
				return fmt.Errorf("replacing relation %s: %w", r.ID, err)
			}
			report.Replaced++
		}
		if err := tx.Relations().Create(ctx, dom); err != nil {
			return fmt.Errorf("creating relation %s: %w", r.ID, err)
		}
		report.Relations++
	}
	return nil
}

func (s *PortabilityService) relationExists(
	ctx context.Context,
	tx WriteTx,
	sourceID, relationID uuid.UUID,
) (bool, error) {
	rels, err := tx.Relations().GetByTask(ctx, sourceID)
	if err != nil {
		return false, fmt.Errorf("looking up relations on task %s: %w", sourceID, err)
	}
	for _, r := range rels {
		if r.ID == relationID {
			return true, nil
		}
	}
	return false, nil
}

func (s *PortabilityService) applyAnnotations(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	for _, a := range ws.Annotations {
		dom := annotationFromPortable(a)
		existing, err := s.annotationExists(ctx, tx, a.TaskID, a.ID)
		if err != nil {
			return err
		}
		if existing {
			if err := tx.Annotations().Delete(ctx, a.ID); err != nil {
				return fmt.Errorf("replacing annotation %s: %w", a.ID, err)
			}
			report.Replaced++
		}
		if err := tx.Annotations().Create(ctx, dom); err != nil {
			return fmt.Errorf("creating annotation %s: %w", a.ID, err)
		}
		report.Annotations++
	}
	return nil
}

func (s *PortabilityService) annotationExists(
	ctx context.Context,
	tx WriteTx,
	taskID, annotationID uuid.UUID,
) (bool, error) {
	anns, err := tx.Annotations().GetByTask(ctx, taskID)
	if err != nil {
		return false, fmt.Errorf("looking up annotations on task %s: %w", taskID, err)
	}
	for _, a := range anns {
		if a.ID == annotationID {
			return true, nil
		}
	}
	return false, nil
}

func (s *PortabilityService) applyNotes(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	for _, n := range ws.Notes {
		dom := noteFromPortable(n)
		existing, err := tx.Notes().GetByID(ctx, n.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("looking up note %s: %w", n.ID, err)
		}
		if existing != nil {
			// NoteRepository is append-only at the storage layer, so
			// without --truncate we cannot rewrite an existing note's
			// body. Preserve the live row to keep --replace safe.
			continue
		}
		if err := tx.Notes().Create(ctx, dom); err != nil {
			return fmt.Errorf("creating note %s: %w", n.ID, err)
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
func (s *PortabilityService) applyEvents(
	ctx context.Context,
	tx WriteTx,
	ws *portability.PortableWorkspace,
	report *ImportReport,
) error {
	if len(ws.Events) == 0 {
		return nil
	}
	existing, err := tx.Events().List(ctx, eventListFilter())
	if err != nil {
		return fmt.Errorf("listing existing events: %w", err)
	}
	seen := make(map[uuid.UUID]struct{}, len(existing))
	for _, e := range existing {
		seen[e.ID] = struct{}{}
	}
	for _, e := range ws.Events {
		if _, dup := seen[e.ID]; dup {
			continue
		}
		dom, err := eventFromPortable(e)
		if err != nil {
			return err
		}
		if err := tx.Events().Record(ctx, dom); err != nil {
			return fmt.Errorf("recording imported event %s: %w", e.ID, err)
		}
		seen[e.ID] = struct{}{}
		report.Events++
	}
	return nil
}

// eventListFilter returns an EventFilter that matches every row. Defined
// as a helper because the empty-filter literal is verbose enough to be
// noise inline.
func eventListFilter() repository.EventFilter {
	return repository.EventFilter{}
}

// recordImportEvent emits the workspace_imported event inside the apply
// transaction so a successful Import is atomic with its audit-trail
// announcement. Counts mirror the per-kind report fields after the
// apply pass; Replaced and Truncated come straight from opts.
func (s *PortabilityService) recordImportEvent(
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
