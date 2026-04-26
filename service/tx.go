package service

import (
	"context"

	"github.com/germanamz/tusk/repository"
)

// WriteTx exposes every repository that mutating services may need inside a
// single transaction, plus the event repository for atomic emission. The
// portability service requires every entity accessor here so a workspace
// import can upsert workflows, projects, players, tags, tasks, relations,
// annotations, notes, and events atomically.
type WriteTx interface {
	Tasks() repository.TaskRepository
	Relations() repository.RelationRepository
	Events() repository.EventRepository

	// New in v0.13 — required by the portability service for atomic
	// multi-entity imports. Existing callers that don't need these
	// accessors can ignore them.
	Projects() repository.ProjectRepository
	Workflows() repository.WorkflowRepository
	Players() repository.PlayerRepository
	Tags() repository.TagRepository
	Annotations() repository.AnnotationRepository
	Notes() repository.NoteRepository

	// TruncateAll wipes every entity table inside the current
	// transaction in reverse-FK order. Used exclusively by the
	// PortabilityService under --replace --truncate. Returns the first
	// error encountered, leaving the transaction's rollback policy to
	// the caller.
	TruncateAll(ctx context.Context) error
}

// WriteTxProvider runs fn inside a shared transaction whose repositories all
// write through the same *sql.Tx. Commits on nil return; rolls back otherwise.
type WriteTxProvider interface {
	WithTx(ctx context.Context, fn func(tx WriteTx) error) error
}
