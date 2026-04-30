package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/germanamz/tusk/domain"
)

const playerColumns = `id, type, note_window_size, registered_at, last_seen_at`

// PlayerRepo implements repository.PlayerRepository using SQLite.
type PlayerRepo struct {
	db DBTX
}

// NewPlayerRepo creates a PlayerRepo.
func NewPlayerRepo(db DBTX) *PlayerRepo {
	return &PlayerRepo{db: db}
}

// Create inserts a new player. Returns domain.ErrConflict if the ID already exists.
func (repo *PlayerRepo) Create(ctx context.Context, player *domain.Player) error {
	var noteWindowSize any
	if player.NoteWindowSize != nil {
		noteWindowSize = *player.NoteWindowSize
	}

	_, execErr := repo.db.ExecContext(ctx,
		`INSERT INTO players (id, type, note_window_size, registered_at, last_seen_at) VALUES (?, ?, ?, ?, ?)`,
		player.ID, player.Type, noteWindowSize,
		player.RegisteredAt.UTC().Format(timeFormat),
		player.LastSeenAt.UTC().Format(timeFormat),
	)

	if execErr != nil {
		// SQLite returns UNIQUE constraint error for duplicate PK.
		// Check if it's a conflict by attempting to look up the ID.
		if _, lookupErr := repo.GetByID(ctx, player.ID); lookupErr == nil {
			return domain.ErrConflict
		}

		return execErr
	}

	return nil
}

// GetByID retrieves a player by ID. Returns domain.ErrNotFound if missing.
func (repo *PlayerRepo) GetByID(ctx context.Context, id string) (*domain.Player, error) {
	row := repo.db.QueryRowContext(ctx,
		`SELECT `+playerColumns+` FROM players WHERE id = ?`, id)

	return scanPlayer(row)
}

// UpdateNoteWindowSize sets the player's note window preference.
// Pass nil to clear the preference (sets column to NULL).
// Returns domain.ErrNotFound if the player does not exist.
func (repo *PlayerRepo) UpdateNoteWindowSize(ctx context.Context, id string, size *int) error {
	var val any
	if size != nil {
		val = *size
	}

	res, execErr := repo.db.ExecContext(ctx,
		`UPDATE players SET note_window_size = ? WHERE id = ?`, val, id)

	if execErr != nil {
		return execErr
	}

	rowsAffected, rowsErr := res.RowsAffected()

	if rowsErr != nil {
		return rowsErr
	}

	if rowsAffected == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// UpdateLastSeen updates the last_seen_at timestamp for a player.
// Returns domain.ErrNotFound if the player does not exist.
func (repo *PlayerRepo) UpdateLastSeen(ctx context.Context, id string) error {
	now := time.Now().UTC().Truncate(time.Millisecond).Format(timeFormat)
	res, execErr := repo.db.ExecContext(ctx,
		`UPDATE players SET last_seen_at = ? WHERE id = ?`, now, id)

	if execErr != nil {
		return execErr
	}

	rowsAffected, rowsErr := res.RowsAffected()

	if rowsErr != nil {
		return rowsErr
	}

	if rowsAffected == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// List returns all registered players ordered by registration time.
func (repo *PlayerRepo) List(ctx context.Context) ([]*domain.Player, error) {
	rows, err := repo.db.QueryContext(ctx,
		`SELECT `+playerColumns+` FROM players ORDER BY registered_at`)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	result := make([]*domain.Player, 0)

	for rows.Next() {
		player, scanErr := scanPlayer(rows)

		if scanErr != nil {
			return nil, scanErr
		}

		result = append(result, player)
	}

	return result, rows.Err()
}

// playerScanner abstracts *sql.Row and *sql.Rows for scanPlayer.
type playerScanner interface {
	Scan(dest ...any) error
}

func scanPlayer(scanner playerScanner) (*domain.Player, error) {
	var (
		player         domain.Player
		noteWindowSize sql.NullInt64
		registeredAt   string
		lastSeenAt     string
	)

	scanErr := scanner.Scan(&player.ID, &player.Type, &noteWindowSize, &registeredAt, &lastSeenAt)

	if scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}

		return nil, scanErr
	}

	if noteWindowSize.Valid {
		windowSize := int(noteWindowSize.Int64)
		player.NoteWindowSize = &windowSize
	}

	parseRegisteredAtErr := error(nil)
	player.RegisteredAt, parseRegisteredAtErr = time.Parse(timeFormat, registeredAt)

	if parseRegisteredAtErr != nil {
		return nil, parseRegisteredAtErr
	}

	parseLastSeenAtErr := error(nil)
	player.LastSeenAt, parseLastSeenAtErr = time.Parse(timeFormat, lastSeenAt)

	if parseLastSeenAtErr != nil {
		return nil, parseLastSeenAtErr
	}

	return &player, nil
}
