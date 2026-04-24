package service

import (
	"context"
	"sort"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

// Resequence rewrites every sibling under parentID to dense integer orders
// (1.0, 2.0, 3.0, ...), preserving the current sort order from
// TaskRepository.GetChildren (order ASC NULLS LAST, created_at ASC). parentID
// nil scopes to root-level siblings. Each row whose order actually changes
// emits a task_modified event with Changes["order"] = {old, new}. Resequence
// never changes a parent, so it does not emit task_moved. Returns the count
// of rows rewritten.
func (s *TaskService) Resequence(ctx context.Context, parentID *uuid.UUID, actorID *string) (int, error) {
	defID, err := s.defaultProjectID(ctx)
	if err != nil {
		return 0, err
	}
	bundle, err := s.resolve(ctx, defID)
	if err != nil {
		return 0, err
	}

	actor := actorID
	if actor == nil {
		actor = ActorFromContext(ctx)
	}

	var rewritten int
	err = bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		tr := tx.Tasks()
		var children []*domain.Task
		if parentID == nil {
			// GetChildren requires a UUID; root groups need a bespoke path.
			children, err = rootChildren(ctx, tr)
		} else {
			children, err = tr.GetChildren(ctx, *parentID)
		}
		if err != nil {
			return err
		}
		if len(children) == 0 {
			return nil
		}
		now := time.Now().UTC().Truncate(time.Millisecond)
		for i, child := range children {
			seq := float64(i + 1)
			if child.Order != nil && *child.Order == seq {
				continue
			}
			oldOrder := child.Order
			newVersion, err := tr.UpdateOrderAndParent(ctx, child.ID, child.ParentID, seq, child.Version, now)
			if err != nil {
				return err
			}
			updated := *child
			s := seq
			updated.Order = &s
			updated.Version = newVersion
			updated.ModifiedAt = now
			changes := map[string]domain.FieldChange{
				"order": {From: float64PtrValue(oldOrder), To: seq},
			}
			evt := domain.NewTaskModifiedEvent(&updated, changes, actor)
			if err := tx.Events().Record(ctx, evt); err != nil {
				return err
			}
			rewritten++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return rewritten, nil
}

// rootChildren returns all root-level tasks sorted the same way
// TaskRepository.GetChildren sorts its results. We route through List with an
// empty filter and then sort in memory by (order ASC NULLS LAST, created_at
// ASC, id ASC) — the repo's canonical sibling sort.
//
// A dedicated repo method would be cleaner, but Resequence is the only site
// that needs this and the post-backfill row counts at root level are small
// (tens, not thousands), so an in-memory sort is acceptable.
func rootChildren(ctx context.Context, tr interface {
	List(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error)
}) ([]*domain.Task, error) {
	all, err := tr.List(ctx, nil)
	if err != nil {
		return nil, err
	}
	roots := make([]*domain.Task, 0, len(all))
	for _, t := range all {
		if t.ParentID == nil {
			roots = append(roots, t)
		}
	}
	sortSiblings(roots)
	return roots, nil
}

// sortSiblings sorts tasks in place with the same ordering GetChildren emits:
// order ASC NULLS LAST, then created_at ASC, then id ASC as a tie-breaker.
func sortSiblings(tasks []*domain.Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		return siblingLess(tasks[i], tasks[j])
	})
}

func siblingLess(a, b *domain.Task) bool {
	switch {
	case a.Order == nil && b.Order != nil:
		return false
	case a.Order != nil && b.Order == nil:
		return true
	case a.Order != nil && b.Order != nil && *a.Order != *b.Order:
		return *a.Order < *b.Order
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.ID.String() < b.ID.String()
}

func float64PtrValue(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
