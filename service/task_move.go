package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// MovePosition selects where to place the subject task relative to the
// MoveRequest's TargetID or ParentID.
type MovePosition int

const (
	// MovePositionBefore places the subject immediately before TargetID in the
	// target's sibling group. TargetID is required. ParentID is ignored.
	MovePositionBefore MovePosition = iota + 1
	// MovePositionAfter places the subject immediately after TargetID.
	// TargetID is required. ParentID is ignored.
	MovePositionAfter
	// MovePositionFirst places the subject at the head of its (optionally
	// re-homed) sibling group. TargetID must be nil.
	MovePositionFirst
	// MovePositionLast places the subject at the tail of its (optionally
	// re-homed) sibling group. TargetID must be nil.
	MovePositionLast
)

// MoveRequest captures everything TaskService.Move needs to decide where a
// task should land. ParentID is a double-pointer so callers can distinguish
// "keep current parent" (nil), "move to root" (*nil), and "move under parent X"
// (*&id). ParentID is only honored for First/Last positions; Before/After take
// the parent from the target.
type MoveRequest struct {
	TaskID   uuid.UUID
	Version  int
	Position MovePosition
	TargetID *uuid.UUID
	ParentID **uuid.UUID
	ActorID  *string
}

// Move relocates a task within the sibling-order plane. It opens a write
// transaction, validates the request against fresh repository state, computes
// a new `order` value using dense-append (First/Last) or midpoint (Before/After)
// math, writes a single row via TaskRepository.UpdateOrderAndParent, and records
// one task_moved event. Cycle, conflict, and ErrOrderGapExhausted errors are
// surfaced verbatim. On underflow the returned error wraps ErrOrderGapExhausted
// with the parent's short ID so callers can point the user at
// `tusk task move --resequence <parent>`.
func (s *TaskService) Move(ctx context.Context, req MoveRequest) (*domain.Task, error) {
	switch req.Position {
	case MovePositionBefore, MovePositionAfter:
		if req.TargetID == nil {
			return nil, fmt.Errorf("move position %d requires TargetID", req.Position)
		}
	case MovePositionFirst, MovePositionLast:
		if req.TargetID != nil {
			return nil, fmt.Errorf("move position %d does not accept a TargetID", req.Position)
		}
	default:
		return nil, fmt.Errorf("invalid move position: %d", req.Position)
	}

	bundle, subject, err := s.bundleForID(ctx, req.TaskID)

	if err != nil {
		return nil, err
	}

	if subject.Version != req.Version {
		return nil, domain.ErrConflict
	}

	actor := req.ActorID
	if actor == nil {
		actor = ActorFromContext(ctx)
	}

	var result *domain.Task

	err = bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		taskRepo := tx.Tasks()

		fresh, fetchErr := taskRepo.GetByID(ctx, req.TaskID)

		if fetchErr != nil {
			return fetchErr
		}

		if fresh.Version != req.Version {
			return domain.ErrConflict
		}

		newParent, refParent, resolveErr := resolveMoveParent(ctx, taskRepo, fresh, req)

		if resolveErr != nil {
			return resolveErr
		}

		if cycleErr := ensureNotDescendant(ctx, taskRepo, fresh.ID, newParent); cycleErr != nil {
			return cycleErr
		}

		newOrder, orderErr := computeMoveOrder(ctx, taskRepo, req, newParent, refParent)

		if orderErr != nil {
			return orderErr
		}

		now := time.Now().UTC().Truncate(time.Millisecond)

		if _, updateErr := taskRepo.UpdateOrderAndParent(ctx, fresh.ID, newParent, newOrder, fresh.Version, now); updateErr != nil {
			return updateErr
		}

		updated, reloadErr := taskRepo.GetByID(ctx, fresh.ID)

		if reloadErr != nil {
			return reloadErr
		}

		result = updated

		event := domain.NewTaskMovedEvent(updated, fresh.ParentID, newParent, fresh.Order, &newOrder, actor)
		return tx.Events().Record(ctx, event)
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// resolveMoveParent returns the post-move parent for the subject along with a
// "reference parent" used when computing midpoint math. For Before/After the
// reference parent is the target's parent (which is also the new parent). For
// First/Last it is simply the new parent.
func resolveMoveParent(
	ctx context.Context,
	taskRepo repository.TaskRepository,
	subject *domain.Task,
	req MoveRequest,
) (newParent *uuid.UUID, refParent *uuid.UUID, err error) {
	switch req.Position {
	case MovePositionBefore, MovePositionAfter:
		target, fetchErr := taskRepo.GetByID(ctx, *req.TargetID)

		if fetchErr != nil {
			return nil, nil, fetchErr
		}

		return target.ParentID, target.ParentID, nil

	case MovePositionFirst, MovePositionLast:
		if req.ParentID == nil {
			return subject.ParentID, subject.ParentID, nil
		}

		desired := *req.ParentID

		if desired == nil {
			return nil, nil, nil
		}

		if _, checkErr := taskRepo.GetByID(ctx, *desired); checkErr != nil {
			return nil, nil, checkErr
		}

		return desired, desired, nil
	}
	return nil, nil, fmt.Errorf("invalid move position: %d", req.Position)
}

// ensureNotDescendant guarantees that newParent is neither the subject itself
// nor one of its descendants — either case would create a cycle in the
// parent→child graph.
func ensureNotDescendant(
	ctx context.Context,
	taskRepo repository.TaskRepository,
	subjectID uuid.UUID,
	newParent *uuid.UUID,
) error {
	if newParent == nil {
		return nil
	}
	if *newParent == subjectID {
		return domain.ErrCyclicParent
	}
	descendants, err := taskRepo.GetDescendants(ctx, subjectID)

	if err != nil {
		return fmt.Errorf("loading descendants for cycle check: %w", err)
	}

	for _, descendant := range descendants {
		if descendant.ID == *newParent {
			return domain.ErrCyclicParent
		}
	}
	return nil
}

// computeMoveOrder returns the `order` value the subject should land on.
// Before/After use NeighborOrders + computeMidpoint; First/Last use the
// repo's FirstOrder / NextOrder helpers (dense ±1 off the extremes).
func computeMoveOrder(
	ctx context.Context,
	taskRepo repository.TaskRepository,
	req MoveRequest,
	newParent *uuid.UUID,
	refParent *uuid.UUID,
) (float64, error) {
	switch req.Position {
	case MovePositionBefore, MovePositionAfter:
		target, fetchErr := taskRepo.GetByID(ctx, *req.TargetID)

		if fetchErr != nil {
			return 0, fetchErr
		}

		pivot := 1.0

		if target.Order != nil {
			pivot = *target.Order
		}

		prev, next, neighborErr := taskRepo.NeighborOrders(ctx, refParent, pivot)

		if neighborErr != nil {
			return 0, neighborErr
		}

		if req.Position == MovePositionBefore {
			if prev == nil {
				return pivot - 1.0, nil
			}

			mid, midErr := computeMidpoint(*prev, pivot)

			if midErr != nil {
				return 0, fmt.Errorf("%w: parent %s", midErr, formatParentShortID(refParent))
			}

			return mid, nil
		}

		if next == nil {
			return pivot + 1.0, nil
		}

		mid, midErr := computeMidpoint(pivot, *next)

		if midErr != nil {
			return 0, fmt.Errorf("%w: parent %s", midErr, formatParentShortID(refParent))
		}

		return mid, nil

	case MovePositionFirst:
		return taskRepo.FirstOrder(ctx, newParent)

	case MovePositionLast:
		return taskRepo.NextOrder(ctx, newParent)
	}
	return 0, fmt.Errorf("invalid move position: %d", req.Position)
}

// formatParentShortID renders a parent ID for error messages. nil → "root";
// otherwise the first 8 hex characters of the UUID, which is the same shape
// users see in the CLI and can pass back into commands.
func formatParentShortID(id *uuid.UUID) string {
	if id == nil {
		return "root"
	}

	hex := strings.ReplaceAll(id.String(), "-", "")

	if len(hex) < 8 {
		return hex
	}

	return hex[:8]
}
