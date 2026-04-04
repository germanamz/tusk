package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// TagService encapsulates tag business logic including find-or-create
// semantics and bulk assign/remove operations.
type TagService struct {
	tagRepo repository.TagRepository
}

// NewTagService creates a new TagService with the given repository.
func NewTagService(tagRepo repository.TagRepository) *TagService {
	return &TagService{tagRepo: tagRepo}
}

// FindOrCreate returns the existing tag with the given name, or creates
// a new one if it doesn't exist. Empty or whitespace-only names are rejected.
func (s *TagService) FindOrCreate(ctx context.Context, name string) (*domain.Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tag name must not be empty")
	}

	tag, err := s.tagRepo.GetByName(ctx, name)
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
	if err := s.tagRepo.Create(ctx, tag); err != nil {
		return nil, fmt.Errorf("creating tag %q: %w", name, err)
	}
	return tag, nil
}

// Create explicitly creates a new tag with the given name and optional color.
// Unlike FindOrCreate, this fails with ErrConflict if the tag already exists.
func (s *TagService) Create(ctx context.Context, name string, color *string) (*domain.Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tag name must not be empty")
	}

	_, err := s.tagRepo.GetByName(ctx, name)
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
	if err := s.tagRepo.Create(ctx, tag); err != nil {
		return nil, fmt.Errorf("creating tag %q: %w", name, err)
	}
	return tag, nil
}

// Delete removes a tag by name. Returns ErrTagInUse if the tag is still
// assigned to any tasks. Returns ErrNotFound if the tag doesn't exist.
func (s *TagService) Delete(ctx context.Context, name string) error {
	tag, err := s.tagRepo.GetByName(ctx, name)
	if err != nil {
		return fmt.Errorf("looking up tag %q: %w", name, err)
	}

	count, err := s.tagRepo.CountTasksByTagID(ctx, tag.ID)
	if err != nil {
		return fmt.Errorf("counting tasks for tag %q: %w", name, err)
	}
	if count > 0 {
		return fmt.Errorf("tag %q is assigned to %d task(s): %w", name, count, domain.ErrTagInUse)
	}

	if err := s.tagRepo.Delete(ctx, tag.ID); err != nil {
		return fmt.Errorf("deleting tag %q: %w", name, err)
	}
	return nil
}

// Rename changes a tag's name. Returns ErrNotFound if the old name doesn't
// exist, ErrConflict if the new name is already taken.
func (s *TagService) Rename(ctx context.Context, oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("new tag name must not be empty")
	}

	tag, err := s.tagRepo.GetByName(ctx, oldName)
	if err != nil {
		return fmt.Errorf("looking up tag %q: %w", oldName, err)
	}

	_, err = s.tagRepo.GetByName(ctx, newName)
	if err == nil {
		return fmt.Errorf("tag %q already exists: %w", newName, domain.ErrConflict)
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("checking tag %q: %w", newName, err)
	}

	tag.Name = newName
	if err := s.tagRepo.Update(ctx, tag); err != nil {
		return fmt.Errorf("renaming tag to %q: %w", newName, err)
	}
	return nil
}

// Modify updates a tag's color. Pass a non-nil pointer to set a color,
// or nil to clear it. Returns ErrNotFound if the tag doesn't exist.
func (s *TagService) Modify(ctx context.Context, name string, color *string) (*domain.Tag, error) {
	tag, err := s.tagRepo.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("looking up tag %q: %w", name, err)
	}

	tag.Color = color
	if err := s.tagRepo.Update(ctx, tag); err != nil {
		return nil, fmt.Errorf("updating tag %q: %w", name, err)
	}
	return tag, nil
}

// ListWithUsage returns all tags with their task assignment counts.
func (s *TagService) ListWithUsage(ctx context.Context) ([]domain.TagWithUsage, error) {
	return s.tagRepo.ListWithUsage(ctx)
}

// AssignToTask finds-or-creates each tag by name and assigns them to the task.
// An empty tagNames slice is a no-op.
func (s *TagService) AssignToTask(ctx context.Context, taskID uuid.UUID, tagNames []string) error {
	for _, name := range tagNames {
		tag, err := s.FindOrCreate(ctx, name)
		if err != nil {
			return err
		}
		if err := s.tagRepo.AssignToTask(ctx, taskID, tag.ID); err != nil {
			return fmt.Errorf("assigning tag %q to task: %w", name, err)
		}
	}
	return nil
}

// GetTaskTags returns all tags assigned to a task.
func (s *TagService) GetTaskTags(ctx context.Context, taskID uuid.UUID) ([]*domain.Tag, error) {
	return s.tagRepo.GetTaskTags(ctx, taskID)
}

// RemoveFromTask removes the named tags from the task.
// If a tag name doesn't exist or isn't assigned, it's silently skipped.
// An empty tagNames slice is a no-op.
func (s *TagService) RemoveFromTask(ctx context.Context, taskID uuid.UUID, tagNames []string) error {
	for _, name := range tagNames {
		tag, err := s.tagRepo.GetByName(ctx, name)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("looking up tag %q: %w", name, err)
		}
		err = s.tagRepo.RemoveFromTask(ctx, taskID, tag.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("removing tag %q from task: %w", name, err)
		}
	}
	return nil
}

// GetTaskTagsBatch returns tags for multiple tasks in a single query.
func (s *TagService) GetTaskTagsBatch(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID][]*domain.Tag, error) {
	return s.tagRepo.GetTaskTagsBatch(ctx, taskIDs)
}

// List returns all tags in the system.
func (s *TagService) List(ctx context.Context) ([]*domain.Tag, error) {
	return s.tagRepo.List(ctx)
}
