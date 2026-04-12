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

// RelationService implements relation business logic including
// validation and cycle detection for "blocks" relations. Operations
// route through a BundleResolver: both endpoints of a relation must
// live in the same project store.
type RelationService struct {
	resolve  BundleResolver
	projects ProjectLister
}

// NewRelationService creates a new RelationService wired to the given
// resolver and project lister.
func NewRelationService(resolve BundleResolver, projects ProjectLister) *RelationService {
	return &RelationService{resolve: resolve, projects: projects}
}

// findTask searches every known project store for a task by short ID
// and returns the owning bundle along with the task.
func (s *RelationService) findTask(ctx context.Context, shortID string) (*RepoBundle, *domain.Task, error) {
	ids, err := s.projects(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, pid := range ids {
		bundle, err := s.resolve(ctx, pid)
		if err != nil {
			return nil, nil, err
		}
		task, err := bundle.Tasks.GetByShortID(ctx, shortID)
		if err == nil {
			return bundle, task, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return nil, nil, err
		}
	}
	return nil, nil, domain.ErrNotFound
}

// Add creates a new relation between two tasks identified by short IDs.
//
// Both endpoints must live in the same project store; cross-store
// relations return domain.ErrCrossStoreRelation.
//
// For "blocks" relations, the creation is wrapped in a transaction with
// cycle detection. For other types, no cycle check is needed.
func (s *RelationService) Add(ctx context.Context, sourceShortID, targetShortID, relType string) (*domain.Relation, error) {
	if !validRelationTypes[relType] {
		return nil, fmt.Errorf("invalid relation type %q: must be one of blocks, relates_to, duplicates", relType)
	}

	sourceBundle, source, err := s.findTask(ctx, sourceShortID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrSourceNotFound
		}
		return nil, fmt.Errorf("resolving source task: %w", err)
	}

	targetBundle, target, err := s.findTask(ctx, targetShortID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrTargetNotFound
		}
		return nil, fmt.Errorf("resolving target task: %w", err)
	}

	if sourceBundle != targetBundle {
		return nil, domain.ErrCrossStoreRelation
	}

	rel := &domain.Relation{
		ID:           uuid.New(),
		SourceID:     source.ID,
		TargetID:     target.ID,
		RelationType: relType,
		CreatedAt:    time.Now().UTC().Truncate(time.Millisecond),
	}

	if relType == "blocks" {
		if err := sourceBundle.Store.WithRelationTx(ctx, func(txRepo repository.RelationRepository) error {
			if err := s.checkCycle(ctx, txRepo, source.ID, target.ID); err != nil {
				return err
			}
			return txRepo.Create(ctx, rel)
		}); err != nil {
			return nil, err
		}
		return rel, nil
	}

	if err := sourceBundle.Relations.Create(ctx, rel); err != nil {
		return nil, err
	}
	return rel, nil
}

// Remove deletes an existing relation between two tasks. Both endpoints
// must live in the same project store.
func (s *RelationService) Remove(ctx context.Context, sourceShortID, targetShortID, relType string) error {
	sourceBundle, source, err := s.findTask(ctx, sourceShortID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrSourceNotFound
		}
		return fmt.Errorf("resolving source task: %w", err)
	}

	targetBundle, target, err := s.findTask(ctx, targetShortID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrTargetNotFound
		}
		return fmt.Errorf("resolving target task: %w", err)
	}

	if sourceBundle != targetBundle {
		return domain.ErrCrossStoreRelation
	}

	return sourceBundle.Relations.DeleteByFields(ctx, source.ID, target.ID, relType)
}

// GetByTask returns all relations involving a task (as source or
// target). The task is identified by short ID.
func (s *RelationService) GetByTask(ctx context.Context, shortID string) ([]*domain.Relation, error) {
	bundle, task, err := s.findTask(ctx, shortID)
	if err != nil {
		return nil, err
	}
	return bundle.Relations.GetByTask(ctx, task.ID)
}

// checkCycle performs a DFS from targetID following outgoing "blocks"
// edges. If it reaches sourceID, that means inserting sourceID->targetID
// would form a cycle.
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
