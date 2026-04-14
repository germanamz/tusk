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
func (s *TagService) definitions(ctx context.Context) (repository.TagRepository, error) {
	bundle, err := s.resolve(ctx, domain.DefaultProjectUUID)
	if err != nil {
		return nil, fmt.Errorf("resolving default bundle for tags: %w", err)
	}
	return bundle.Tags, nil
}

// FindOrCreate returns the existing tag with the given name, or creates
// a new one if it doesn't exist. Empty or whitespace-only names are
// rejected.
func (s *TagService) FindOrCreate(ctx context.Context, name string) (*domain.Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tag name must not be empty")
	}

	repo, err := s.definitions(ctx)
	if err != nil {
		return nil, err
	}

	tag, err := repo.GetByName(ctx, name)
	if err == nil {
		return tag, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, fmt.Errorf("looking up tag %q: %w", name, err)
	}

	tag = &domain.Tag{
		ID:   uuid.New(),
		Name: name,
	}
	if err := repo.Create(ctx, tag); err != nil {
		return nil, fmt.Errorf("creating tag %q: %w", name, err)
	}
	return tag, nil
}

// Create explicitly creates a new tag with the given name and optional
// color. Fails with ErrConflict if the tag already exists.
func (s *TagService) Create(ctx context.Context, name string, color *string) (*domain.Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tag name must not be empty")
	}

	repo, err := s.definitions(ctx)
	if err != nil {
		return nil, err
	}

	_, err = repo.GetByName(ctx, name)
	if err == nil {
		return nil, fmt.Errorf("tag %q already exists: %w", name, domain.ErrConflict)
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, fmt.Errorf("looking up tag %q: %w", name, err)
	}

	tag := &domain.Tag{
		ID:    uuid.New(),
		Name:  name,
		Color: color,
	}
	if err := repo.Create(ctx, tag); err != nil {
		return nil, fmt.Errorf("creating tag %q: %w", name, err)
	}
	return tag, nil
}

// Delete removes a tag by name. Returns the deleted tag on success.
func (s *TagService) Delete(ctx context.Context, name string) (*domain.Tag, error) {
	repo, err := s.definitions(ctx)
	if err != nil {
		return nil, err
	}

	tag, err := repo.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("looking up tag %q: %w", name, err)
	}

	count, err := repo.CountTasksByTagID(ctx, tag.ID)
	if err != nil {
		return nil, fmt.Errorf("counting tasks for tag %q: %w", name, err)
	}
	if count > 0 {
		return nil, fmt.Errorf("tag %q is assigned to %d task(s): %w", name, count, domain.ErrTagInUse)
	}

	if err := repo.Delete(ctx, tag.ID); err != nil {
		return nil, fmt.Errorf("deleting tag %q: %w", name, err)
	}
	return tag, nil
}

// Rename changes a tag's name.
func (s *TagService) Rename(ctx context.Context, oldName, newName string) (*domain.Tag, error) {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return nil, fmt.Errorf("new tag name must not be empty")
	}

	repo, err := s.definitions(ctx)
	if err != nil {
		return nil, err
	}

	tag, err := repo.GetByName(ctx, oldName)
	if err != nil {
		return nil, fmt.Errorf("looking up tag %q: %w", oldName, err)
	}

	_, err = repo.GetByName(ctx, newName)
	if err == nil {
		return nil, fmt.Errorf("tag %q already exists: %w", newName, domain.ErrConflict)
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, fmt.Errorf("checking tag %q: %w", newName, err)
	}

	tag.Name = newName
	if err := repo.Update(ctx, tag); err != nil {
		return nil, fmt.Errorf("renaming tag to %q: %w", newName, err)
	}
	return tag, nil
}

// Modify updates a tag's color.
func (s *TagService) Modify(ctx context.Context, name string, color *string) (*domain.Tag, error) {
	repo, err := s.definitions(ctx)
	if err != nil {
		return nil, err
	}

	tag, err := repo.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("looking up tag %q: %w", name, err)
	}

	tag.Color = color
	if err := repo.Update(ctx, tag); err != nil {
		return nil, fmt.Errorf("updating tag %q: %w", name, err)
	}
	return tag, nil
}

// ListWithUsage returns all tags with their task assignment counts.
func (s *TagService) ListWithUsage(ctx context.Context) ([]domain.TagWithUsage, error) {
	repo, err := s.definitions(ctx)
	if err != nil {
		return nil, err
	}
	return repo.ListWithUsage(ctx)
}

// AssignToTask finds-or-creates each tag by name and assigns them to
// the task. Task-tag junctions are written to the default bundle's tag
// repository — this will need to move to each task's own store when
// per-project SQLite files are in use for cross-project tagging.
func (s *TagService) AssignToTask(ctx context.Context, taskID uuid.UUID, tagNames []string) error {
	repo, err := s.definitions(ctx)
	if err != nil {
		return err
	}
	for _, name := range tagNames {
		tag, err := s.FindOrCreate(ctx, name)
		if err != nil {
			return err
		}
		if err := repo.AssignToTask(ctx, taskID, tag.ID); err != nil {
			return fmt.Errorf("assigning tag %q to task: %w", name, err)
		}
	}
	return nil
}

// GetTaskTags returns all tags assigned to a task.
func (s *TagService) GetTaskTags(ctx context.Context, taskID uuid.UUID) ([]*domain.Tag, error) {
	repo, err := s.definitions(ctx)
	if err != nil {
		return nil, err
	}
	return repo.GetTaskTags(ctx, taskID)
}

// RemoveFromTask removes the named tags from the task.
func (s *TagService) RemoveFromTask(ctx context.Context, taskID uuid.UUID, tagNames []string) error {
	repo, err := s.definitions(ctx)
	if err != nil {
		return err
	}
	for _, name := range tagNames {
		tag, err := repo.GetByName(ctx, name)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("looking up tag %q: %w", name, err)
		}
		err = repo.RemoveFromTask(ctx, taskID, tag.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("removing tag %q from task: %w", name, err)
		}
	}
	return nil
}

// GetTaskTagsBatch returns tags for multiple tasks in a single query.
func (s *TagService) GetTaskTagsBatch(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID][]*domain.Tag, error) {
	repo, err := s.definitions(ctx)
	if err != nil {
		return nil, err
	}
	return repo.GetTaskTagsBatch(ctx, taskIDs)
}

// List returns all tags in the system.
func (s *TagService) List(ctx context.Context) ([]*domain.Tag, error) {
	repo, err := s.definitions(ctx)
	if err != nil {
		return nil, err
	}
	return repo.List(ctx)
}
