package repository

import (
	"context"

	"github.com/germanamz/tusk/internal/domain"
)

// PlayerRepository defines storage operations for Player entities.
// Create returns domain.ErrConflict if a player with the same ID already exists.
// GetByID returns domain.ErrNotFound if no player matches.
type PlayerRepository interface {
	Create(ctx context.Context, player *domain.Player) error
	GetByID(ctx context.Context, id string) (*domain.Player, error)
	UpdateLastSeen(ctx context.Context, id string) error
	List(ctx context.Context) ([]*domain.Player, error)
}
