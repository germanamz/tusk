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

		case "title":
			v := field.Value
			tf.TitleContains = &v

		case "description":
			v := field.Value
			tf.DescriptionContains = &v

		default:
			if udaKey, ok := strings.CutPrefix(field.Key, "uda."); ok {
				if tf.UDA == nil {
					tf.UDA = make(map[string]string)
				}
				tf.UDA[udaKey] = field.Value
			}
		}
	}

	if !hasStatus {
		tf.Statuses = []string{"pending", "active"}
	}

	return &tf, errs
}

// ResolveExpr converts a parsed expression tree into a domain.FilterExpr.
// Resolution errors are collected rather than failing fast.
// If the expression tree contains no explicit status term, the result is wrapped
// in AndFilter{TermFilter{Statuses: [pending, active]}, userExpr} to preserve
// default status behavior.
func (r *Resolver) ResolveExpr(ctx context.Context, expr Expr) (domain.FilterExpr, []error) {
	if expr == nil {
		return nil, nil
	}

	var errs []error
	result := r.resolveNode(ctx, expr, &errs)

	// Default status injection: if no status term anywhere in the tree, wrap
	if !exprHasStatus(expr) {
		defaultStatus := &domain.TermFilter{
			TaskFilter: domain.TaskFilter{
				Statuses: []string{"pending", "active"},
			},
		}
		result = &domain.AndFilter{
			Children: []domain.FilterExpr{defaultStatus, result},
		}
	}

	return result, errs
}

func (r *Resolver) resolveNode(ctx context.Context, expr Expr, errs *[]error) domain.FilterExpr {
	switch e := expr.(type) {
	case AndExpr:
		children := make([]domain.FilterExpr, 0, len(e.Children))
		for _, child := range e.Children {
			resolved := r.resolveNode(ctx, child, errs)
			if resolved != nil {
				children = append(children, resolved)
			}
		}
		if len(children) == 0 {
			return nil
		}
		if len(children) == 1 {
			return children[0]
		}
		return &domain.AndFilter{Children: children}

	case OrExpr:
		children := make([]domain.FilterExpr, 0, len(e.Children))
		for _, child := range e.Children {
			resolved := r.resolveNode(ctx, child, errs)
			if resolved != nil {
				children = append(children, resolved)
			}
		}
		if len(children) == 0 {
			return nil
		}
		if len(children) == 1 {
			return children[0]
		}
		return &domain.OrFilter{Children: children}

	case NotExpr:
		child := r.resolveNode(ctx, e.Child, errs)
		if child == nil {
			return nil
		}
		return &domain.NotFilter{Child: child}

	case TermExpr:
		return r.resolveTerm(ctx, e, errs)

	default:
		return nil
	}
}

func (r *Resolver) resolveTerm(ctx context.Context, term TermExpr, errs *[]error) domain.FilterExpr {
	var tf domain.TaskFilter

	if term.Tag != nil {
		if term.Tag.Exclude {
			tf.ExcludeTags = []string{term.Tag.Name}
		} else {
			tf.Tags = []string{term.Tag.Name}
		}
	}

	if term.Field != nil {
		field := *term.Field
		switch field.Key {
		case "status":
			tf.Statuses = strings.Split(field.Value, ",")

		case "project":
			id := field.Value
			tf.ProjectID = &id

		case "priority":
			if strings.Contains(field.Value, "..") {
				parts := strings.SplitN(field.Value, "..", 2)
				min, err := parsePriorityValue(parts[0])
				if err != nil {
					*errs = append(*errs, fmt.Errorf("priority range min: %w", err))
					return nil
				}
				max, err := parsePriorityValue(parts[1])
				if err != nil {
					*errs = append(*errs, fmt.Errorf("priority range max: %w", err))
					return nil
				}
				tf.PriorityMin = &min
				tf.PriorityMax = &max
			} else {
				v, err := parsePriorityValue(field.Value)
				if err != nil {
					*errs = append(*errs, fmt.Errorf("priority: %w", err))
					return nil
				}
				tf.PriorityMin = &v
				tf.PriorityMax = &v
			}

		case "due":
			if strings.Contains(field.Value, "..") {
				start, end, err := parseDateRange(field.Value)
				if err != nil {
					*errs = append(*errs, fmt.Errorf("due range: %w", err))
					return nil
				}
				tf.DueAfter = &start
				tf.DueBefore = &end
			} else {
				d, err := parseDate(field.Value)
				if err != nil {
					*errs = append(*errs, fmt.Errorf("due: %w", err))
					return nil
				}
				tf.DueAfter = &d
				end := d.AddDate(0, 0, 1)
				tf.DueBefore = &end
			}

		case "parent":
			task, err := r.taskLookup.GetByShortID(ctx, field.Value)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					*errs = append(*errs, fmt.Errorf("parent task %q not found", field.Value))
				} else {
					*errs = append(*errs, fmt.Errorf("looking up parent %q: %w", field.Value, err))
				}
				return nil
			}
			tf.ParentID = &task.ID

		case "tree":
			task, err := r.taskLookup.GetByShortID(ctx, field.Value)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					*errs = append(*errs, fmt.Errorf("tree root task %q not found", field.Value))
				} else {
					*errs = append(*errs, fmt.Errorf("looking up tree root %q: %w", field.Value, err))
				}
				return nil
			}
			tf.RootID = &task.ID

		case "waiting":
			v := field.Value == "true"
			tf.WaitingOnly = &v

		case "title":
			v := field.Value
			tf.TitleContains = &v

		case "description":
			v := field.Value
			tf.DescriptionContains = &v

		default:
			if udaKey, ok := strings.CutPrefix(field.Key, "uda."); ok {
				if tf.UDA == nil {
					tf.UDA = make(map[string]string)
				}
				tf.UDA[udaKey] = field.Value
			}
		}
	}

	return &domain.TermFilter{TaskFilter: tf}
}

// exprHasStatus checks if any TermExpr in the tree has a status field.
func exprHasStatus(expr Expr) bool {
	switch e := expr.(type) {
	case TermExpr:
		return e.Field != nil && e.Field.Key == "status"
	case AndExpr:
		for _, child := range e.Children {
			if exprHasStatus(child) {
				return true
			}
		}
	case OrExpr:
		for _, child := range e.Children {
			if exprHasStatus(child) {
				return true
			}
		}
	case NotExpr:
		return exprHasStatus(e.Child)
	}
	return false
}
