package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/germanamz/tusk/domain"
)

const playerColumns = `id, type, registered_at, last_seen_at`

// PlayerRepo implements repository.PlayerRepository using SQLite.
type PlayerRepo struct {
	db DBTX
}

// NewPlayerRepo creates a PlayerRepo.
func NewPlayerRepo(db DBTX) *PlayerRepo {
	return &PlayerRepo{db: db}
}

// Create inserts a new player. Returns domain.ErrConflict if the ID already exists.
func (r *PlayerRepo) Create(ctx context.Context, player *domain.Player) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO players (id, type, registered_at, last_seen_at) VALUES (?, ?, ?, ?)`,
		player.ID, player.Type,
		player.RegisteredAt.UTC().Format(timeFormat),
		player.LastSeenAt.UTC().Format(timeFormat),
	)
	if err != nil {
		// SQLite returns UNIQUE constraint error for duplicate PK.
		// Check if it's a conflict by attempting to look up the ID.
		if _, lookupErr := r.GetByID(ctx, player.ID); lookupErr == nil {
			return domain.ErrConflict
		}
		return err
	}
	return nil
}

// GetByID retrieves a player by ID. Returns domain.ErrNotFound if missing.
func (r *PlayerRepo) GetByID(ctx context.Context, id string) (*domain.Player, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+playerColumns+` FROM players WHERE id = ?`, id)
	return scanPlayer(row)
}

// UpdateLastSeen updates the last_seen_at timestamp for a player.
// Returns domain.ErrNotFound if the player does not exist.
func (r *PlayerRepo) UpdateLastSeen(ctx context.Context, id string) error {
	now := time.Now().UTC().Truncate(time.Millisecond).Format(timeFormat)
	res, err := r.db.ExecContext(ctx,
		`UPDATE players SET last_seen_at = ? WHERE id = ?`, now, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// List returns all registered players ordered by registration time.
func (r *PlayerRepo) List(ctx context.Context) ([]*domain.Player, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+playerColumns+` FROM players ORDER BY registered_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]*domain.Player, 0)
	for rows.Next() {
		p, err := scanPlayer(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// playerScanner abstracts *sql.Row and *sql.Rows for scanPlayer.
type playerScanner interface {
	Scan(dest ...any) error
}

func scanPlayer(s playerScanner) (*domain.Player, error) {
	var (
		p            domain.Player
		registeredAt string
		lastSeenAt   string
	)
	err := s.Scan(&p.ID, &p.Type, &registeredAt, &lastSeenAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	p.RegisteredAt, err = time.Parse(timeFormat, registeredAt)
	if err != nil {
		return nil, err
	}
	p.LastSeenAt, err = time.Parse(timeFormat, lastSeenAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
