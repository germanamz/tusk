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
func (service *TaskService) Resequence(ctx context.Context, parentID *uuid.UUID, actorID *string) (int, error) {
	defaultID, defaultIDErr := service.defaultProjectID(ctx)

	if defaultIDErr != nil {
		return 0, defaultIDErr
	}

	bundle, resolveErr := service.resolve(ctx, defaultID)

	if resolveErr != nil {
		return 0, resolveErr
	}

	actor := actorID
	if actor == nil {
		actor = ActorFromContext(ctx)
	}

	var rewritten int

	txErr := bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		taskRepo := tx.Tasks()

		var children []*domain.Task
		var childrenErr error

		if parentID == nil {
			// GetChildren requires a UUID; root groups need a bespoke path.
			children, childrenErr = rootChildren(ctx, taskRepo)
		} else {
			children, childrenErr = taskRepo.GetChildren(ctx, *parentID)
		}

		if childrenErr != nil {
			return childrenErr
		}

		if len(children) == 0 {
			return nil
		}

		now := time.Now().UTC().Truncate(time.Millisecond)

		for index, child := range children {
			seq := float64(index + 1)

			if child.Order != nil && *child.Order == seq {
				continue
			}

			oldOrder := child.Order

			newVersion, updateErr := taskRepo.UpdateOrderAndParent(ctx, child.ID, child.ParentID, seq, child.Version, now)

			if updateErr != nil {
				return updateErr
			}

			updated := *child
			seqCopy := seq
			updated.Order = &seqCopy
			updated.Version = newVersion
			updated.ModifiedAt = now
			changes := map[string]domain.FieldChange{
				"order": {From: float64PtrValue(oldOrder), To: seq},
			}
			event := domain.NewTaskModifiedEvent(&updated, changes, actor)

			if recordErr := tx.Events().Record(ctx, event); recordErr != nil {
				return recordErr
			}

			rewritten++
		}

		return nil
	})

	if txErr != nil {
		return 0, txErr
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
func rootChildren(ctx context.Context, taskRepo interface {
	List(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error)
}) ([]*domain.Task, error) {
	all, err := taskRepo.List(ctx, nil)

	if err != nil {
		return nil, err
	}

	roots := make([]*domain.Task, 0, len(all))

	for _, task := range all {
		if task.ParentID == nil {
			roots = append(roots, task)
		}
	}
	sortSiblings(roots)
	return roots, nil
}

// sortSiblings sorts tasks in place with the same ordering GetChildren emits:
// order ASC NULLS LAST, then created_at ASC, then id ASC as a tie-breaker.
func sortSiblings(tasks []*domain.Task) {
	sort.SliceStable(tasks, func(ii, jj int) bool {
		return siblingLess(tasks[ii], tasks[jj])
	})
}

func siblingLess(left, right *domain.Task) bool {
	switch {
	case left.Order == nil && right.Order != nil:
		return false
	case left.Order != nil && right.Order == nil:
		return true
	case left.Order != nil && right.Order != nil && *left.Order != *right.Order:
		return *left.Order < *right.Order
	}

	if !left.CreatedAt.Equal(right.CreatedAt) {
		return left.CreatedAt.Before(right.CreatedAt)
	}

	return left.ID.String() < right.ID.String()
}

func float64PtrValue(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
