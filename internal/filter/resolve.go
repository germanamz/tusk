package filter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/germanamz/tusk/internal/domain"
)

// TaskLookup is the subset of task operations the Resolver needs.
type TaskLookup interface {
	GetByShortID(ctx context.Context, shortID string) (*domain.Task, error)
}

// Resolver converts a parsed FilterSet into a domain.TaskFilter.
type Resolver struct {
	taskLookup TaskLookup
}

// NewResolver creates a Resolver with the given lookup dependencies.
func NewResolver(taskLookup TaskLookup) *Resolver {
	return &Resolver{
		taskLookup: taskLookup,
	}
}

// Resolve converts the AST into a domain.TaskFilter. Resolution errors
// (e.g., project not found) are collected rather than failing fast.
func (r *Resolver) Resolve(ctx context.Context, fs *FilterSet) (*domain.TaskFilter, []error) {
	var tf domain.TaskFilter
	var errs []error

	// Tags
	if inc := fs.IncludeTags(); len(inc) > 0 {
		tf.Tags = inc
	}
	if exc := fs.ExcludeTags(); len(exc) > 0 {
		tf.ExcludeTags = exc
	}

	// Default statuses when none specified
	hasStatus := false

	for _, field := range fs.Fields {
		switch field.Key {
		case "status":
			hasStatus = true
			tf.Statuses = strings.Split(field.Value, ",")

		case "project":
			id := field.Value
			tf.ProjectID = &id

		case "priority":
			if strings.Contains(field.Value, "..") {
				parts := strings.SplitN(field.Value, "..", 2)
				min, err := parsePriorityValue(parts[0])
				if err != nil {
					errs = append(errs, fmt.Errorf("priority range min: %w", err))
					continue
				}
				max, err := parsePriorityValue(parts[1])
				if err != nil {
					errs = append(errs, fmt.Errorf("priority range max: %w", err))
					continue
				}
				tf.PriorityMin = &min
				tf.PriorityMax = &max
			} else {
				v, err := parsePriorityValue(field.Value)
				if err != nil {
					errs = append(errs, fmt.Errorf("priority: %w", err))
					continue
				}
				tf.PriorityMin = &v
				tf.PriorityMax = &v
			}

		case "due":
			if strings.Contains(field.Value, "..") {
				start, end, err := parseDateRange(field.Value)
				if err != nil {
					errs = append(errs, fmt.Errorf("due range: %w", err))
					continue
				}
				tf.DueAfter = &start
				tf.DueBefore = &end
			} else {
				d, err := parseDate(field.Value)
				if err != nil {
					errs = append(errs, fmt.Errorf("due: %w", err))
					continue
				}
				tf.DueAfter = &d
				end := d.AddDate(0, 0, 1)
				tf.DueBefore = &end
			}

		case "parent":
			task, err := r.taskLookup.GetByShortID(ctx, field.Value)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					errs = append(errs, fmt.Errorf("parent task %q not found", field.Value))
				} else {
					errs = append(errs, fmt.Errorf("looking up parent %q: %w", field.Value, err))
				}
				continue
			}
			tf.ParentID = &task.ID

		case "tree":
			task, err := r.taskLookup.GetByShortID(ctx, field.Value)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					errs = append(errs, fmt.Errorf("tree root task %q not found", field.Value))
				} else {
					errs = append(errs, fmt.Errorf("looking up tree root %q: %w", field.Value, err))
				}
				continue
			}
			tf.RootID = &task.ID

		case "waiting":
			v := field.Value == "true"
			tf.WaitingOnly = &v
		}
	}

	if !hasStatus {
		tf.Statuses = []string{"pending", "active"}
	}

	return &tf, errs
}
