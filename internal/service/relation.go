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

// validRelationTypes defines the allowed relation type strings.
var validRelationTypes = map[string]bool{
	"blocks":     true,
	"relates_to": true,
	"duplicates": true,
}

// RelationTxProvider gives the service a way to run relation operations
// inside a database transaction without importing a concrete storage package.
// The SQLite Store implements this via its WithRelationTx method.
type RelationTxProvider interface {
	WithRelationTx(ctx context.Context, fn func(rr repository.RelationRepository) error) error
}

// RelationService implements relation business logic including validation
// and cycle detection for "blocks" relations.
type RelationService struct {
	relationRepo repository.RelationRepository
	taskRepo     repository.TaskRepository
	txProvider   RelationTxProvider
}

// NewRelationService creates a new RelationService with the given dependencies.
//   - rr: for non-transactional reads (GetByTask, Remove lookups)
//   - tr: to resolve short IDs to full task UUIDs
//   - txp: for atomic cycle-check + insert on "blocks" relations
func NewRelationService(
	rr repository.RelationRepository,
	tr repository.TaskRepository,
	txp RelationTxProvider,
) *RelationService {
	return &RelationService{
		relationRepo: rr,
		taskRepo:     tr,
		txProvider:   txp,
	}
}

// Add creates a new relation between two tasks identified by short IDs.
//
// For "blocks" relations, the creation is wrapped in a transaction with
// cycle detection (see checkCycle). For other types, no cycle check is needed.
//
// Returns the created Relation or an error:
//   - domain.ErrNotFound if either task short ID doesn't exist
//   - domain.ErrCyclicBlock if adding a "blocks" relation would create a cycle
//   - domain.ErrDuplicateRelation if the exact relation already exists
//   - a validation error if relType is not one of: blocks, relates_to, duplicates
func (s *RelationService) Add(ctx context.Context, sourceShortID, targetShortID, relType string) (*domain.Relation, error) {
	if !validRelationTypes[relType] {
		return nil, fmt.Errorf("invalid relation type %q: must be one of blocks, relates_to, duplicates", relType)
	}

	source, err := s.taskRepo.GetByShortID(ctx, sourceShortID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrSourceNotFound
		}
		return nil, fmt.Errorf("resolving source task: %w", err)
	}

	target, err := s.taskRepo.GetByShortID(ctx, targetShortID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrTargetNotFound
		}
		return nil, fmt.Errorf("resolving target task: %w", err)
	}

	rel := &domain.Relation{
		ID:           uuid.New(),
		SourceID:     source.ID,
		TargetID:     target.ID,
		RelationType: relType,
		CreatedAt:    time.Now().UTC().Truncate(time.Millisecond),
	}

	if relType == "blocks" {
		// Cycle check + insert must be atomic
		if err := s.txProvider.WithRelationTx(ctx, func(txRepo repository.RelationRepository) error {
			if err := s.checkCycle(ctx, txRepo, source.ID, target.ID); err != nil {
				return err
			}
			return txRepo.Create(ctx, rel)
		}); err != nil {
			return nil, err
		}
		return rel, nil
	}

	// Non-blocks: no cycle concern, insert directly
	if err := s.relationRepo.Create(ctx, rel); err != nil {
		return nil, err
	}
	return rel, nil
}

// Remove deletes an existing relation between two tasks.
//
// Uses a direct delete by (source, target, type) fields rather than
// fetching all relations and scanning.
//
// Returns:
//   - domain.ErrSourceNotFound if the source task short ID doesn't exist
//   - domain.ErrTargetNotFound if the target task short ID doesn't exist
//   - domain.ErrNotFound if the relation doesn't exist
func (s *RelationService) Remove(ctx context.Context, sourceShortID, targetShortID, relType string) error {
	source, err := s.taskRepo.GetByShortID(ctx, sourceShortID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrSourceNotFound
		}
		return fmt.Errorf("resolving source task: %w", err)
	}

	target, err := s.taskRepo.GetByShortID(ctx, targetShortID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrTargetNotFound
		}
		return fmt.Errorf("resolving target task: %w", err)
	}

	return s.relationRepo.DeleteByFields(ctx, source.ID, target.ID, relType)
}

// GetByTask returns all relations involving a task (as source or target).
// The task is identified by short ID.
func (s *RelationService) GetByTask(ctx context.Context, shortID string) ([]*domain.Relation, error) {
	task, err := s.taskRepo.GetByShortID(ctx, shortID)
	if err != nil {
		return nil, err
	}
	return s.relationRepo.GetByTask(ctx, task.ID)
}

// checkCycle performs a DFS from targetID following outgoing "blocks" edges.
// If it reaches sourceID, that means inserting sourceID->targetID would form a cycle.
//
// Must be called inside a transaction so that no concurrent writer can insert
// a conflicting edge between the check and the subsequent insert.
func (s *RelationService) checkCycle(ctx context.Context, txRepo repository.RelationRepository, sourceID, targetID uuid.UUID) error {
	visited := map[uuid.UUID]bool{}
	stack := []uuid.UUID{targetID}

	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if current == sourceID {
			return domain.ErrCyclicBlock
		}
		if visited[current] {
			continue
		}
		visited[current] = true

		blocking, err := txRepo.GetBlocking(ctx, current)
		if err != nil {
			return fmt.Errorf("checking cycle: %w", err)
		}
		for _, rel := range blocking {
			if !visited[rel.TargetID] {
				stack = append(stack, rel.TargetID)
			}
		}
	}
	return nil
}
