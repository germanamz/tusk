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

func (service *RelationService) findTask(ctx context.Context, shortID string) (*RepoBundle, *domain.Task, error) {
	bundle, bundleErr := service.resolve(ctx, domain.DefaultProjectUUID)

	if bundleErr != nil {
		return nil, nil, bundleErr
	}

	task, taskErr := bundle.Tasks.GetByShortID(ctx, shortID)

	if taskErr != nil {
		return nil, nil, taskErr
	}

	return bundle, task, nil
}

// Add creates a new relation between two tasks identified by short IDs.
//
// Both branches run inside a single WriteTx. For "blocks" relations, cycle
// detection runs against the transactional RelationRepository so it observes
// other writers' committed state. The relation row and its relation_added
// event are written atomically — a failure in either rolls back both.
func (service *RelationService) Add(ctx context.Context, sourceShortID, targetShortID, relType string) (*domain.Relation, error) {
	if !validRelationTypes[relType] {
		return nil, fmt.Errorf("invalid relation type %q: must be one of blocks, relates_to, duplicates", relType)
	}

	sourceBundle, source, sourceErr := service.findTask(ctx, sourceShortID)

	if sourceErr != nil {
		if errors.Is(sourceErr, domain.ErrNotFound) {
			return nil, domain.ErrSourceNotFound
		}
		return nil, fmt.Errorf("resolving source task: %w", sourceErr)
	}

	_, target, targetErr := service.findTask(ctx, targetShortID)

	if targetErr != nil {
		if errors.Is(targetErr, domain.ErrNotFound) {
			return nil, domain.ErrTargetNotFound
		}
		return nil, fmt.Errorf("resolving target task: %w", targetErr)
	}

	rel := &domain.Relation{
		ID:           uuid.New(),
		SourceID:     source.ID,
		TargetID:     target.ID,
		RelationType: relType,
		CreatedAt:    time.Now().UTC().Truncate(time.Millisecond),
	}

	actor := ActorFromContext(ctx)
	writeErr := sourceBundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		if relType == "blocks" {
			if err := service.checkCycle(ctx, tx.Relations(), source.ID, target.ID); err != nil {
				return err
			}
		}
		if err := tx.Relations().Create(ctx, rel); err != nil {
			return err
		}
		return tx.Events().Record(ctx, domain.NewRelationAddedEvent(rel, source.ShortID, target.ShortID, actor))
	})

	if writeErr != nil {
		return nil, writeErr
	}

	return rel, nil
}

// Remove deletes an existing relation between two tasks. The deletion and its
// relation_removed event are written atomically inside a single WriteTx.
func (service *RelationService) Remove(ctx context.Context, sourceShortID, targetShortID, relType string) error {
	if !validRelationTypes[relType] {
		return fmt.Errorf("invalid relation type %q: must be one of blocks, relates_to, duplicates", relType)
	}

	sourceBundle, source, sourceErr := service.findTask(ctx, sourceShortID)

	if sourceErr != nil {
		if errors.Is(sourceErr, domain.ErrNotFound) {
			return domain.ErrSourceNotFound
		}
		return fmt.Errorf("resolving source task: %w", sourceErr)
	}

	_, target, targetErr := service.findTask(ctx, targetShortID)

	if targetErr != nil {
		if errors.Is(targetErr, domain.ErrNotFound) {
			return domain.ErrTargetNotFound
		}
		return fmt.Errorf("resolving target task: %w", targetErr)
	}

	actor := ActorFromContext(ctx)
	return sourceBundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		// Look up the relation before deletion so the event can carry its ID.
		rel, relErr := tx.Relations().GetByFields(ctx, source.ID, target.ID, relType)

		if relErr != nil {
			return relErr
		}

		deleteErr := tx.Relations().DeleteByFields(ctx, source.ID, target.ID, relType)

		if deleteErr != nil {
			return deleteErr
		}

		return tx.Events().Record(ctx, domain.NewRelationRemovedEvent(rel, source.ShortID, target.ShortID, actor))
	})
}

// GetByTask returns all relations involving a task (as source or
// target). The task is identified by short ID.
func (service *RelationService) GetByTask(ctx context.Context, shortID string) ([]*domain.Relation, error) {
	bundle, task, err := service.findTask(ctx, shortID)

	if err != nil {
		return nil, err
	}

	return bundle.Relations.GetByTask(ctx, task.ID)
}

// checkCycle performs a DFS from targetID following outgoing "blocks"
// edges. If it reaches sourceID, that means inserting sourceID->targetID
// would form a cycle.
func (service *RelationService) checkCycle(ctx context.Context, txRepo repository.RelationRepository, sourceID, targetID uuid.UUID) error {
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

		blocking, blockingErr := txRepo.GetBlocking(ctx, current)

		if blockingErr != nil {
			return fmt.Errorf("checking cycle: %w", blockingErr)
		}

		for _, rel := range blocking {
			if !visited[rel.TargetID] {
				stack = append(stack, rel.TargetID)
			}
		}
	}
	return nil
}
