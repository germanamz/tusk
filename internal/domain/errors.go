package domain

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("version conflict")
	ErrCyclicBlock       = errors.New("relation would create a cycle in blocks graph")
	ErrCyclicParent      = errors.New("parent would create a cycle in task hierarchy")
	ErrInvalidTransition = errors.New("status transition not allowed by workflow")
	ErrDuplicateRelation = errors.New("relation already exists")
	ErrTagInUse          = errors.New("tag is assigned to tasks")
	ErrSourceNotFound    = fmt.Errorf("source task: %w", ErrNotFound)
	ErrTargetNotFound    = fmt.Errorf("target task: %w", ErrNotFound)
)
