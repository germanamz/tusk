package domain

import (
	"time"

	"github.com/google/uuid"
)

type TaskFilter struct {
	ProjectID           *string
	ParentID            *uuid.UUID
	RootID              *uuid.UUID // for tree: all descendants
	Statuses            []string   // OR match
	Tags                []string   // include
	ExcludeTags         []string   // exclude
	PriorityMin         *int
	PriorityMax         *int
	DueAfter            *time.Time
	DueBefore           *time.Time
	WaitingOnly         *bool             // if true, only tasks with wait_until in future
	TitleContains       *string           // substring match (case-insensitive)
	DescriptionContains *string           // substring match (case-insensitive)
	UDA                 map[string]string // filter by UDA key=value pairs (AND semantics); empty value = absent/empty match
	ClaimedBy           *string           // filter by player ID
	Unclaimed           *bool             // if true, only tasks where claimed_by IS NULL
}

// FilterExpr is the interface for boolean filter expression nodes.
// Used by the repository layer for queries with AND/OR/NOT logic.
type FilterExpr interface {
	filterExpr() // marker method
}

// AndFilter requires all children to match.
type AndFilter struct {
	Children []FilterExpr
}

// OrFilter requires at least one child to match.
type OrFilter struct {
	Children []FilterExpr
}

// NotFilter negates its child.
type NotFilter struct {
	Child FilterExpr
}

// TermFilter wraps a TaskFilter as a leaf node in a boolean expression.
type TermFilter struct {
	TaskFilter
}

func (AndFilter) filterExpr()  {}
func (OrFilter) filterExpr()   {}
func (NotFilter) filterExpr()  {}
func (TermFilter) filterExpr() {}
