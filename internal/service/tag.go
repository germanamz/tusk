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
