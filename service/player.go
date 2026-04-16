package service

import (
	"context"
	"fmt"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
)

// PlayerService handles player registration and liveness tracking.
type PlayerService struct {
	repo repository.PlayerRepository
}

// NewPlayerService creates a new PlayerService.
func NewPlayerService(repo repository.PlayerRepository) *PlayerService {
	return &PlayerService{repo: repo}
}

// Register creates a new player. Type must be "human" or "agent".
// Returns domain.ErrConflict if a player with the same ID already exists.
func (s *PlayerService) Register(ctx context.Context, id, playerType string) (*domain.Player, error) {
	if id == "" {
		return nil, fmt.Errorf("player ID must not be empty")
	}
	if playerType != "human" && playerType != "agent" {
		return nil, fmt.Errorf("player type must be \"human\" or \"agent\", got %q", playerType)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	player := &domain.Player{
		ID:           id,
		Type:         playerType,
		RegisteredAt: now,
		LastSeenAt:   now,
	}
	if err := s.repo.Create(ctx, player); err != nil {
		return nil, err
	}
	return player, nil
}

// GetByID retrieves a player by ID.
func (s *PlayerService) GetByID(ctx context.Context, id string) (*domain.Player, error) {
	return s.repo.GetByID(ctx, id)
}

// UpdateLastSeen refreshes a player's last_seen_at timestamp.
func (s *PlayerService) UpdateLastSeen(ctx context.Context, id string) error {
	return s.repo.UpdateLastSeen(ctx, id)
}

// List returns all registered players.
func (s *PlayerService) List(ctx context.Context) ([]*domain.Player, error) {
	return s.repo.List(ctx)
}

// SetNoteWindowSize updates the caller-scoped note-window override on a
// player. Passing a non-nil size persists that size as the override;
// passing nil clears the override so the player falls back to project and
// global defaults. Size must be positive when non-nil.
//
// Returns domain.ErrNotFound if no player matches.
func (s *PlayerService) SetNoteWindowSize(ctx context.Context, id string, size *int) (*domain.Player, error) {
	if id == "" {
		return nil, fmt.Errorf("player ID must not be empty")
	}
	if size != nil && *size <= 0 {
		return nil, fmt.Errorf("note window size must be positive, got %d", *size)
	}

	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return nil, fmt.Errorf("player %q: %w", id, err)
	}

	if err := s.repo.UpdateNoteWindowSize(ctx, id, size); err != nil {
		return nil, fmt.Errorf("updating player %q note window size: %w", id, err)
	}

	updated, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("reloading player %q: %w", id, err)
	}
	return updated, nil
}
