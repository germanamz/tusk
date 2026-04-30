package filter

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/germanamz/tusk/domain"
)

// TaskLookup is the subset of task operations the Resolver needs.
type TaskLookup interface {
	GetByShortID(ctx context.Context, shortID string) (*domain.Task, error)
}

// ProjectLookup is the subset of project operations the Resolver needs.
type ProjectLookup interface {
	GetByName(ctx context.Context, name string) (*domain.Project, error)
}

// Resolver converts a parsed FilterSet into a domain.TaskFilter.
type Resolver struct {
	taskLookup      TaskLookup
	projectLookup   ProjectLookup
	defaultStatuses []string
}

// NewResolver creates a Resolver with the given lookup dependencies and the
// list of statuses to inject when no explicit status filter is provided.
func NewResolver(taskLookup TaskLookup, projectLookup ProjectLookup, defaultStatuses []string) *Resolver {
	return &Resolver{
		taskLookup:      taskLookup,
		projectLookup:   projectLookup,
		defaultStatuses: defaultStatuses,
	}
}

// Resolve converts the AST into a domain.TaskFilter. Resolution errors
// (e.g., project not found) are collected rather than failing fast.
func (resolver *Resolver) Resolve(ctx context.Context, fs *FilterSet) (*domain.TaskFilter, []error) {
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
		if field.Key == "status" {
			hasStatus = true
		}
		if err := resolver.resolveField(ctx, field, &tf); err != nil {
			errs = append(errs, err)
		}
	}

	if !hasStatus {
		tf.Statuses = resolver.defaultStatuses
	}

	return &tf, errs
}

// ResolveExpr converts a parsed expression tree into a domain.FilterExpr.
// Resolution errors are collected rather than failing fast.
// If the expression tree contains no explicit status term, the result is wrapped
// in AndFilter{TermFilter{Statuses: [pending, active]}, userExpr} to preserve
// default status behavior.
func (resolver *Resolver) ResolveExpr(ctx context.Context, expr Expr) (domain.FilterExpr, []error) {
	if expr == nil {
		return nil, nil
	}

	var errs []error
	result := resolver.resolveNode(ctx, expr, &errs)

	// Default status injection: if no status term anywhere in the tree, wrap
	if !exprHasStatus(expr) {
		defaultStatus := &domain.TermFilter{
			TaskFilter: domain.TaskFilter{
				Statuses: resolver.defaultStatuses,
			},
		}
		result = &domain.AndFilter{
			Children: []domain.FilterExpr{defaultStatus, result},
		}
	}

	return result, errs
}

// ResolveExprAllStatuses converts a parsed expression tree into a
// domain.FilterExpr without the default-status wrapper. Callers that need to
// scan every task regardless of status (e.g., tusk task level-check) use this
// so terminal tasks stay in the result set. When expr is nil, the returned
// expression is nil as well, meaning "no filter — match every task".
func (resolver *Resolver) ResolveExprAllStatuses(ctx context.Context, expr Expr) (domain.FilterExpr, []error) {
	if expr == nil {
		return nil, nil
	}
	var errs []error
	result := resolver.resolveNode(ctx, expr, &errs)
	return result, errs
}

func (resolver *Resolver) resolveNode(ctx context.Context, expr Expr, errs *[]error) domain.FilterExpr {
	switch node := expr.(type) {
	case AndExpr:
		children := make([]domain.FilterExpr, 0, len(node.Children))
		for _, child := range node.Children {
			resolved := resolver.resolveNode(ctx, child, errs)
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
		children := make([]domain.FilterExpr, 0, len(node.Children))
		for _, child := range node.Children {
			resolved := resolver.resolveNode(ctx, child, errs)
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
		child := resolver.resolveNode(ctx, node.Child, errs)
		if child == nil {
			return nil
		}
		return &domain.NotFilter{Child: child}

	case TermExpr:
		return resolver.resolveTerm(ctx, node, errs)

	default:
		return nil
	}
}

func (resolver *Resolver) resolveTerm(ctx context.Context, term TermExpr, errs *[]error) domain.FilterExpr {
	if term.Text != "" {
		*errs = append(*errs, fmt.Errorf("free text %q is not supported in filter expressions", term.Text))
		return nil
	}

	var tf domain.TaskFilter

	if term.Tag != nil {
		if term.Tag.Exclude {
			tf.ExcludeTags = []string{term.Tag.Name}
		} else {
			tf.Tags = []string{term.Tag.Name}
		}
	}

	if term.Field != nil {
		if err := resolver.resolveField(ctx, *term.Field, &tf); err != nil {
			*errs = append(*errs, err)
			return nil
		}
	}

	return &domain.TermFilter{TaskFilter: tf}
}

// resolveField applies a single FieldFilter to the given TaskFilter.
// Returns a non-nil error if resolution fails (e.g., task lookup not found).
func (resolver *Resolver) resolveField(ctx context.Context, field FieldFilter, tf *domain.TaskFilter) error {
	switch field.Key {
	case "status":
		tf.Statuses = strings.Split(field.Value, ",")

	case "level":
		tf.Levels = strings.Split(field.Value, ",")

	case "project":
		proj, projErr := resolver.projectLookup.GetByName(ctx, field.Value)

		if projErr != nil {
			if errors.Is(projErr, domain.ErrNotFound) {
				return fmt.Errorf("project %q not found", field.Value)
			}
			return fmt.Errorf("looking up project %q: %w", field.Value, projErr)
		}
		id := proj.ID
		tf.ProjectID = &id

	case "priority":
		if strings.Contains(field.Value, "..") {
			parts := strings.SplitN(field.Value, "..", 2)
			minVal, minErr := parsePriorityValue(parts[0])

			if minErr != nil {
				return fmt.Errorf("priority range min: %w", minErr)
			}

			maxVal, maxErr := parsePriorityValue(parts[1])

			if maxErr != nil {
				return fmt.Errorf("priority range max: %w", maxErr)
			}

			tf.PriorityMin = &minVal
			tf.PriorityMax = &maxVal
		} else {
			priorityVal, priorityErr := parsePriorityValue(field.Value)

			if priorityErr != nil {
				return fmt.Errorf("priority: %w", priorityErr)
			}

			tf.PriorityMin = &priorityVal
			tf.PriorityMax = &priorityVal
		}

	case "order":
		if field.Value == "" {
			isNull := true
			tf.OrderIsNull = &isNull
			return nil
		}
		if strings.Contains(field.Value, "..") {
			parts := strings.SplitN(field.Value, "..", 2)
			lo, minErr := strconv.ParseFloat(parts[0], 64)

			if minErr != nil {
				return fmt.Errorf("order range min: %w", minErr)
			}

			hi, maxErr := strconv.ParseFloat(parts[1], 64)

			if maxErr != nil {
				return fmt.Errorf("order range max: %w", maxErr)
			}

			tf.OrderMin = &lo
			tf.OrderMax = &hi
		} else {
			orderVal, orderErr := strconv.ParseFloat(field.Value, 64)

			if orderErr != nil {
				return fmt.Errorf("order: %w", orderErr)
			}

			tf.OrderMin = &orderVal
			tf.OrderMax = &orderVal
		}

	case "due":
		if strings.Contains(field.Value, "..") {
			start, end, rangeErr := parseDateRange(field.Value)

			if rangeErr != nil {
				return fmt.Errorf("due range: %w", rangeErr)
			}

			tf.DueAfter = &start
			tf.DueBefore = &end
		} else {
			dueDate, dueDateErr := parseDate(field.Value)

			if dueDateErr != nil {
				return fmt.Errorf("due: %w", dueDateErr)
			}

			tf.DueAfter = &dueDate
			end := dueDate.AddDate(0, 0, 1)
			tf.DueBefore = &end
		}

	case "parent":
		parentTask, parentErr := resolver.taskLookup.GetByShortID(ctx, field.Value)

		if parentErr != nil {
			if errors.Is(parentErr, domain.ErrNotFound) {
				return fmt.Errorf("parent task %q not found", field.Value)
			}
			return fmt.Errorf("looking up parent %q: %w", field.Value, parentErr)
		}
		tf.ParentID = &parentTask.ID

	case "tree":
		rootTask, rootErr := resolver.taskLookup.GetByShortID(ctx, field.Value)

		if rootErr != nil {
			if errors.Is(rootErr, domain.ErrNotFound) {
				return fmt.Errorf("tree root task %q not found", field.Value)
			}
			return fmt.Errorf("looking up tree root %q: %w", field.Value, rootErr)
		}
		tf.RootID = &rootTask.ID

	case "waiting":
		waitingVal := field.Value == "true"
		tf.WaitingOnly = &waitingVal

	case "title":
		titleVal := field.Value
		tf.TitleContains = &titleVal

	case "description":
		descVal := field.Value
		tf.DescriptionContains = &descVal

	case "claimed_by":
		claimedByVal := field.Value
		tf.ClaimedBy = &claimedByVal

	case "unclaimed":
		unclaimedVal := field.Value == "true"
		tf.Unclaimed = &unclaimedVal

	default:
		if udaKey, ok := strings.CutPrefix(field.Key, "uda."); ok {
			if tf.UDA == nil {
				tf.UDA = make(map[string]string)
			}
			tf.UDA[udaKey] = field.Value
		}
	}

	return nil
}

// exprHasStatus checks if any TermExpr in the tree has a status field.
func exprHasStatus(expr Expr) bool {
	switch node := expr.(type) {
	case TermExpr:
		return node.Field != nil && node.Field.Key == "status"
	case AndExpr:
		for _, child := range node.Children {
			if exprHasStatus(child) {
				return true
			}
		}
	case OrExpr:
		for _, child := range node.Children {
			if exprHasStatus(child) {
				return true
			}
		}
	case NotExpr:
		return exprHasStatus(node.Child)
	}
	return false
}
