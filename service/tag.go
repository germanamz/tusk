package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// TagService encapsulates tag business logic. Tag definitions (name,
// color) are a global resource stored only in the default project's
// SQLite file; task-tag junctions are written per-project through each
// task's own bundle. This service is the entry point for definition
// reads and writes, so it always routes through the default bundle.
type TagService struct {
	resolve BundleResolver
}

// NewTagService creates a new TagService backed by the default bundle
// resolved from the given BundleResolver.
func NewTagService(resolve BundleResolver) *TagService {
	return &TagService{resolve: resolve}
}

// definitions returns the default bundle's tag repository. All tag
// definition operations (FindOrCreate, Create, Delete, Rename, Modify,
// List, ListWithUsage) run against this repo.
func (service *TagService) definitions(ctx context.Context) (repository.TagRepository, error) {
	bundle, bundleErr := service.resolve(ctx, domain.DefaultProjectUUID)

	if bundleErr != nil {
		return nil, fmt.Errorf("resolving default bundle for tags: %w", bundleErr)
	}

	return bundle.Tags, nil
}

// FindOrCreate returns the existing tag with the given name, or creates
// a new one if it doesn't exist. Empty or whitespace-only names are
// rejected.
func (service *TagService) FindOrCreate(ctx context.Context, name string) (*domain.Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tag name must not be empty")
	}

	repo, repoErr := service.definitions(ctx)

	if repoErr != nil {
		return nil, repoErr
	}

	tag, lookupErr := repo.GetByName(ctx, name)

	if lookupErr == nil {
		return tag, nil
	}

	if !errors.Is(lookupErr, domain.ErrNotFound) {
		return nil, fmt.Errorf("looking up tag %q: %w", name, lookupErr)
	}

	tag = &domain.Tag{
		ID:   uuid.New(),
		Name: name,
	}

	if createErr := repo.Create(ctx, tag); createErr != nil {
		return nil, fmt.Errorf("creating tag %q: %w", name, createErr)
	}

	return tag, nil
}

// Create explicitly creates a new tag with the given name and optional
// color. Fails with ErrConflict if the tag already exists.
func (service *TagService) Create(ctx context.Context, name string, color *string) (*domain.Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tag name must not be empty")
	}

	repo, repoErr := service.definitions(ctx)

	if repoErr != nil {
		return nil, repoErr
	}

	_, lookupErr := repo.GetByName(ctx, name)

	if lookupErr == nil {
		return nil, fmt.Errorf("tag %q already exists: %w", name, domain.ErrConflict)
	}

	if !errors.Is(lookupErr, domain.ErrNotFound) {
		return nil, fmt.Errorf("looking up tag %q: %w", name, lookupErr)
	}

	tag := &domain.Tag{
		ID:    uuid.New(),
		Name:  name,
		Color: color,
	}

	if createErr := repo.Create(ctx, tag); createErr != nil {
		return nil, fmt.Errorf("creating tag %q: %w", name, createErr)
	}

	return tag, nil
}

// Delete removes a tag by name. Returns the deleted tag on success.
func (service *TagService) Delete(ctx context.Context, name string) (*domain.Tag, error) {
	repo, repoErr := service.definitions(ctx)

	if repoErr != nil {
		return nil, repoErr
	}

	tag, lookupErr := repo.GetByName(ctx, name)

	if lookupErr != nil {
		return nil, fmt.Errorf("looking up tag %q: %w", name, lookupErr)
	}

	count, countErr := repo.CountTasksByTagID(ctx, tag.ID)

	if countErr != nil {
		return nil, fmt.Errorf("counting tasks for tag %q: %w", name, countErr)
	}

	if count > 0 {
		return nil, fmt.Errorf("tag %q is assigned to %d task(s): %w", name, count, domain.ErrTagInUse)
	}

	if deleteErr := repo.Delete(ctx, tag.ID); deleteErr != nil {
		return nil, fmt.Errorf("deleting tag %q: %w", name, deleteErr)
	}

	return tag, nil
}

// Rename changes a tag's name.
func (service *TagService) Rename(ctx context.Context, oldName, newName string) (*domain.Tag, error) {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return nil, fmt.Errorf("new tag name must not be empty")
	}

	repo, repoErr := service.definitions(ctx)

	if repoErr != nil {
		return nil, repoErr
	}

	tag, lookupErr := repo.GetByName(ctx, oldName)

	if lookupErr != nil {
		return nil, fmt.Errorf("looking up tag %q: %w", oldName, lookupErr)
	}

	_, conflictErr := repo.GetByName(ctx, newName)

	if conflictErr == nil {
		return nil, fmt.Errorf("tag %q already exists: %w", newName, domain.ErrConflict)
	}

	if !errors.Is(conflictErr, domain.ErrNotFound) {
		return nil, fmt.Errorf("checking tag %q: %w", newName, conflictErr)
	}

	tag.Name = newName

	if updateErr := repo.Update(ctx, tag); updateErr != nil {
		return nil, fmt.Errorf("renaming tag to %q: %w", newName, updateErr)
	}

	return tag, nil
}

// Modify updates a tag's color.
func (service *TagService) Modify(ctx context.Context, name string, color *string) (*domain.Tag, error) {
	repo, repoErr := service.definitions(ctx)

	if repoErr != nil {
		return nil, repoErr
	}

	tag, lookupErr := repo.GetByName(ctx, name)

	if lookupErr != nil {
		return nil, fmt.Errorf("looking up tag %q: %w", name, lookupErr)
	}

	tag.Color = color

	if updateErr := repo.Update(ctx, tag); updateErr != nil {
		return nil, fmt.Errorf("updating tag %q: %w", name, updateErr)
	}

	return tag, nil
}

// ListWithUsage returns all tags with their task assignment counts.
func (service *TagService) ListWithUsage(ctx context.Context) ([]domain.TagWithUsage, error) {
	repo, repoErr := service.definitions(ctx)

	if repoErr != nil {
		return nil, repoErr
	}

	return repo.ListWithUsage(ctx)
}

// AssignToTask finds-or-creates each tag by name and assigns them to
// the task. Task-tag junctions are written to the default bundle's tag
// repository — this will need to move to each task's own store when
// per-project SQLite files are in use for cross-project tagging.
func (service *TagService) AssignToTask(ctx context.Context, taskID uuid.UUID, tagNames []string) error {
	repo, repoErr := service.definitions(ctx)

	if repoErr != nil {
		return repoErr
	}

	for _, name := range tagNames {
		tag, findErr := service.FindOrCreate(ctx, name)

		if findErr != nil {
			return findErr
		}

		if assignErr := repo.AssignToTask(ctx, taskID, tag.ID); assignErr != nil {
			return fmt.Errorf("assigning tag %q to task: %w", name, assignErr)
		}
	}

	return nil
}

// GetTaskTags returns all tags assigned to a task.
func (service *TagService) GetTaskTags(ctx context.Context, taskID uuid.UUID) ([]*domain.Tag, error) {
	repo, repoErr := service.definitions(ctx)

	if repoErr != nil {
		return nil, repoErr
	}

	return repo.GetTaskTags(ctx, taskID)
}

// RemoveFromTask removes the named tags from the task.
func (service *TagService) RemoveFromTask(ctx context.Context, taskID uuid.UUID, tagNames []string) error {
	repo, repoErr := service.definitions(ctx)

	if repoErr != nil {
		return repoErr
	}

	for _, name := range tagNames {
		tag, lookupErr := repo.GetByName(ctx, name)

		if errors.Is(lookupErr, domain.ErrNotFound) {
			continue
		}

		if lookupErr != nil {
			return fmt.Errorf("looking up tag %q: %w", name, lookupErr)
		}

		removeErr := repo.RemoveFromTask(ctx, taskID, tag.ID)

		if removeErr != nil && !errors.Is(removeErr, domain.ErrNotFound) {
			return fmt.Errorf("removing tag %q from task: %w", name, removeErr)
		}
	}

	return nil
}

// GetTaskTagsBatch returns tags for multiple tasks in a single query.
func (service *TagService) GetTaskTagsBatch(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID][]*domain.Tag, error) {
	repo, repoErr := service.definitions(ctx)

	if repoErr != nil {
		return nil, repoErr
	}

	return repo.GetTaskTagsBatch(ctx, taskIDs)
}

// List returns all tags in the system.
func (service *TagService) List(ctx context.Context) ([]*domain.Tag, error) {
	repo, repoErr := service.definitions(ctx)

	if repoErr != nil {
		return nil, repoErr
	}

	return repo.List(ctx)
}
