package service

import (
	"context"

	"github.com/germanamz/tusk/repository"
)

// WriteTx exposes every repository that mutating services may need inside a
// single transaction, plus the event repository for atomic emission.
//
// Phase 2 declares only the repositories v0.13 services need. As future
// initiatives adopt event emission they will add their repo accessors here
// (Projects, Workflows, Players, Notes, Annotations, Tags).
type WriteTx interface {
	Tasks() repository.TaskRepository
	Relations() repository.RelationRepository
	Events() repository.EventRepository
}

// WriteTxProvider runs fn inside a shared transaction whose repositories all
// write through the same *sql.Tx. Commits on nil return; rolls back otherwise.
type WriteTxProvider interface {
	WithTx(ctx context.Context, fn func(tx WriteTx) error) error
}
