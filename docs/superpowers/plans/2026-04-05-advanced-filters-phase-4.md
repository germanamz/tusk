# Phase 4: Boolean Resolver, SQL Generator, and Wiring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the expression tree from Phase 3 through the resolver, domain, SQL generator, repository interface, service layer, CLI, and MCP — completing the boolean filter feature end-to-end.

**Architecture:** New domain types (`FilterExpr`, `AndFilter`, `OrFilter`, `NotFilter`, `TermFilter`) mirror the parse AST. A new `ResolveExpr` method walks the tree. A new `buildFilterExpr` function generates nested SQL. The `TaskRepository.List` interface changes from `TaskFilter` to `FilterExpr`. CLI `runList` switches to `ParseExpr` → `ResolveExpr`. MCP gains a `filter` string parameter.

**Tech Stack:** Go standard library only (no new dependencies).

**Prerequisites:** Phase 1, Phase 2, and Phase 3 must all be completed.

---

## Inherits From

**Phase 1** modified `internal/filter/token.go` — quoted-aware character scanner.

**Phase 2** modified:
- `internal/domain/filter.go` — `TaskFilter` has `TitleContains` and `DescriptionContains` fields
- `internal/filter/validators.go` — `validateNonEmpty` function
- `internal/filter/parser.go` — `fieldValidators` includes `title` and `description`
- `internal/filter/resolve.go` — Handles `title` and `description` in `Resolve()`
- `internal/sqlite/task.go` — `buildFilter()` handles `TitleContains` and `DescriptionContains`
- `internal/mcp/server.go` and `tools.go` — `tusk_task_list` has `title` and `description` params

**Phase 3** added:
- `internal/filter/token.go` — `TokenAnd`, `TokenOr`, `TokenNot`, `TokenLParen`, `TokenRParen` constants; keyword/paren detection in `Lex()`
- `internal/filter/expr.go` — `Expr` interface, `AndExpr`, `OrExpr`, `NotExpr`, `TermExpr`
- `internal/filter/parse_expr.go` — `ParseExpr()` function with Pratt parser

The implementer can rely on:
- `ParseExpr(input)` returns an `Expr` tree with proper precedence
- `fieldValidators` in `parser.go` validates fields in both `Parse` and `ParseExpr`
- `buildFilter(domain.TaskFilter)` handles all existing filter fields including title/description
- The `Resolve()` method handles all existing fields — the new `ResolveExpr` method reuses the same logic

---

### Task 1: Add Domain FilterExpr Types

**Files:**
- Modify: `internal/domain/filter.go`

- [ ] **Step 1: Add FilterExpr interface and types**

Add the following types at the end of `internal/domain/filter.go` (after the `TaskFilter` struct):

```go
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
```

- [ ] **Step 2: Verify compilation**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/domain/filter.go
git commit -m "$(cat <<'EOF'
feat(domain): add FilterExpr types for boolean filter expressions

Add FilterExpr interface with AndFilter, OrFilter, NotFilter, and
TermFilter. These mirror the parse AST and are used by the repository
and SQL layers for boolean query generation.
EOF
)"
```

---

### Task 2: Implement ResolveExpr

**Files:**
- Modify: `internal/filter/resolve.go`
- Create: `internal/filter/resolve_expr_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/filter/resolve_expr_test.go`:

```go
package filter

import (
	"context"
	"testing"
)

func TestResolveExpr_SingleTerm(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{})
	expr := TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// Should be a TermFilter with Statuses set
	tf, ok := result.(*domain.TermFilter)
	if !ok {
		t.Fatalf("expected *domain.TermFilter, got %T", result)
	}
	if len(tf.Statuses) != 1 || tf.Statuses[0] != "active" {
		t.Fatalf("expected Statuses [active], got %v", tf.Statuses)
	}
}

func TestResolveExpr_AndExpr(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{})
	expr := AndExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		TermExpr{Tag: &TagFilter{Name: "api"}},
	}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	af, ok := result.(*domain.AndFilter)
	if !ok {
		t.Fatalf("expected *domain.AndFilter, got %T", result)
	}
	if len(af.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(af.Children))
	}
}

func TestResolveExpr_OrExpr(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{})
	expr := OrExpr{Children: []Expr{
		TermExpr{Field: &FieldFilter{Key: "status", Value: "active"}},
		TermExpr{Field: &FieldFilter{Key: "status", Value: "pending"}},
	}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	of, ok := result.(*domain.OrFilter)
	if !ok {
		t.Fatalf("expected *domain.OrFilter, got %T", result)
	}
	if len(of.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(of.Children))
	}
}

func TestResolveExpr_NotExpr(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{})
	expr := NotExpr{
		Child: TermExpr{Field: &FieldFilter{Key: "status", Value: "deleted"}},
	}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	nf, ok := result.(*domain.NotFilter)
	if !ok {
		t.Fatalf("expected *domain.NotFilter, got %T", result)
	}
	if nf.Child == nil {
		t.Fatal("expected non-nil child")
	}
}

func TestResolveExpr_DefaultStatuses(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{})
	// Expression with no status term — should get default status wrapping
	expr := TermExpr{Tag: &TagFilter{Name: "api"}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// Should be wrapped in AND(TermFilter{Statuses: [pending, active]}, original)
	af, ok := result.(*domain.AndFilter)
	if !ok {
		t.Fatalf("expected *domain.AndFilter for default status wrapping, got %T", result)
	}
	if len(af.Children) != 2 {
		t.Fatalf("expected 2 children (default status + original), got %d", len(af.Children))
	}
	defaultTerm, ok := af.Children[0].(*domain.TermFilter)
	if !ok {
		t.Fatalf("expected first child to be *domain.TermFilter, got %T", af.Children[0])
	}
	if len(defaultTerm.Statuses) != 2 || defaultTerm.Statuses[0] != "pending" || defaultTerm.Statuses[1] != "active" {
		t.Fatalf("expected default statuses [pending active], got %v", defaultTerm.Statuses)
	}
}

func TestResolveExpr_WithStatusSkipsDefault(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{})
	expr := TermExpr{Field: &FieldFilter{Key: "status", Value: "completed"}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// Should NOT be wrapped in AND — status is present
	tf, ok := result.(*domain.TermFilter)
	if !ok {
		t.Fatalf("expected *domain.TermFilter (no wrapping), got %T", result)
	}
	if len(tf.Statuses) != 1 || tf.Statuses[0] != "completed" {
		t.Fatalf("expected Statuses [completed], got %v", tf.Statuses)
	}
}

func TestResolveExpr_TagTerms(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{})
	expr := AndExpr{Children: []Expr{
		TermExpr{Tag: &TagFilter{Name: "api"}},
		TermExpr{Tag: &TagFilter{Name: "docs", Exclude: true}},
	}}

	result, errs := resolver.ResolveExpr(context.Background(), expr)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// Should be AndFilter wrapping default statuses + AND(tag terms)
	af, ok := result.(*domain.AndFilter)
	if !ok {
		t.Fatalf("expected *domain.AndFilter, got %T", result)
	}
	// First child is default status TermFilter, second is the user's AndFilter
	if len(af.Children) < 2 {
		t.Fatalf("expected at least 2 children, got %d", len(af.Children))
	}
}

func TestResolveExpr_NilExpr(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{})
	result, errs := resolver.ResolveExpr(context.Background(), nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if result != nil {
		t.Fatalf("expected nil result for nil input, got %+v", result)
	}
}
```

Note: This test file uses `mockTaskLookup` which is already defined in `internal/filter/resolve_uda_test.go` (line 12).

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/filter/ -run "TestResolveExpr"`
Expected: FAIL — `ResolveExpr` doesn't exist.

- [ ] **Step 3: Implement ResolveExpr**

Add the following to the end of `internal/filter/resolve.go`:

```go
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
				tf.UDA = map[string]string{udaKey: field.Value}
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
```

- [ ] **Step 4: Run resolver tests**

Run: `cd /Users/germanamz/projects/tusk && go test -v ./internal/filter/ -run "TestResolveExpr"`
Expected: ALL PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/filter/resolve.go internal/filter/resolve_expr_test.go
git commit -m "$(cat <<'EOF'
feat(filter): implement ResolveExpr for boolean expression trees

Walks Expr tree and produces domain.FilterExpr. Reuses the same field
resolution logic as Resolve(). Injects default statuses when no status
term is present anywhere in the tree.
EOF
)"
```

---

### Task 3: Implement buildFilterExpr and Update Repository Interface

**Files:**
- Modify: `internal/repository/task.go` (change List signature)
- Modify: `internal/sqlite/task.go` (add `buildFilterExpr`, update `List`)
- Modify: `internal/service/task.go` (update `List`)
- Modify: `internal/sqlite/task_test.go`

- [ ] **Step 1: Change TaskRepository.List signature**

In `internal/repository/task.go`, change line 16 from:

```go
List(ctx context.Context, filter domain.TaskFilter) ([]*domain.Task, error)
```

to:

```go
List(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error)
```

- [ ] **Step 2: Implement buildFilterExpr in sqlite/task.go**

Add the following function at the end of `internal/sqlite/task.go` (before the `scanOne`/`scanRows` methods):

```go
// buildFilterExpr recursively translates a domain.FilterExpr tree into SQL.
// It returns a CTE prefix (for tree: filters), WHERE clause body, and args.
func buildFilterExpr(expr domain.FilterExpr) (ctePrefix string, where string, args []any) {
	if expr == nil {
		return "", "", nil
	}

	switch e := expr.(type) {
	case *domain.TermFilter:
		return buildFilter(e.TaskFilter)

	case *domain.AndFilter:
		var ctes []string
		var conditions []string
		for _, child := range e.Children {
			cte, w, a := buildFilterExpr(child)
			if cte != "" {
				ctes = append(ctes, cte)
			}
			if w != "" {
				conditions = append(conditions, w)
				args = append(args, a...)
			}
		}
		if len(ctes) > 0 {
			ctePrefix = ctes[0] // TODO: handle multiple CTEs if needed
		}
		if len(conditions) == 0 {
			return ctePrefix, "", args
		}
		return ctePrefix, "(" + strings.Join(conditions, " AND ") + ")", args

	case *domain.OrFilter:
		var ctes []string
		var conditions []string
		for _, child := range e.Children {
			cte, w, a := buildFilterExpr(child)
			if cte != "" {
				ctes = append(ctes, cte)
			}
			if w != "" {
				conditions = append(conditions, w)
				args = append(args, a...)
			}
		}
		if len(ctes) > 0 {
			ctePrefix = ctes[0]
		}
		if len(conditions) == 0 {
			return ctePrefix, "", args
		}
		return ctePrefix, "(" + strings.Join(conditions, " OR ") + ")", args

	case *domain.NotFilter:
		cte, w, a := buildFilterExpr(e.Child)
		if w == "" {
			return cte, "", a
		}
		return cte, "NOT (" + w + ")", a

	default:
		return "", "", nil
	}
}
```

- [ ] **Step 3: Update TaskRepo.List to accept FilterExpr**

Replace the `List` method in `internal/sqlite/task.go` (lines 124-136) with:

```go
// List retrieves tasks matching the given filter expression. An nil filter
// returns all tasks.
func (r *TaskRepo) List(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error) {
	ctePrefix, where, args := buildFilterExpr(filter)
	query := ctePrefix + fmt.Sprintf(`SELECT %s FROM tasks`, taskColumns)
	if where != "" {
		query += " WHERE " + where
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanRows(rows)
}
```

- [ ] **Step 4: Update TaskService.List**

In `internal/service/task.go`, change the `List` method (line 148) from:

```go
func (s *TaskService) List(ctx context.Context, filter domain.TaskFilter) ([]*domain.Task, error) {
```

to:

```go
func (s *TaskService) List(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error) {
```

- [ ] **Step 5: Fix all compilation errors**

After changing the interface, callers must wrap `domain.TaskFilter` in `&domain.TermFilter{}`. The places that need updating:

1. **`internal/tui/commands.go`** — `runList` (line 235): currently passes `*tf` (a `domain.TaskFilter`). This will be fully rewritten in Task 5 to use `ParseExpr`. For now, change:
   ```go
   tasks, err := a.taskSvc.List(ctx, *tf)
   ```
   to:
   ```go
   tasks, err := a.taskSvc.List(ctx, &domain.TermFilter{TaskFilter: *tf})
   ```

2. **`internal/mcp/tools.go`** — `handleTaskList` (line 363): currently passes `filter` (a `domain.TaskFilter`). Change:
   ```go
   tasks, err := s.taskSvc.List(ctx, filter)
   ```
   to:
   ```go
   tasks, err := s.taskSvc.List(ctx, &domain.TermFilter{TaskFilter: filter})
   ```

3. **Any other callers** — search for `.List(ctx,` in the codebase and update similarly. Check `internal/service/task.go` for internal calls (e.g., completion propagation may call List).

- [ ] **Step 6: Update existing sqlite tests**

In `internal/sqlite/task_test.go`, all calls to `repo.List(ctx, domain.TaskFilter{...})` must wrap the filter. For example, change:

```go
tasks, err := repo.List(ctx, domain.TaskFilter{TitleContains: &v})
```

to:

```go
tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{TitleContains: &v}})
```

Apply this pattern to ALL `repo.List` calls in the test file.

- [ ] **Step 7: Add buildFilterExpr tests**

Add at the end of `internal/sqlite/task_test.go`:

```go
func TestBuildFilterExpr_And(t *testing.T) {
	expr := &domain.AndFilter{Children: []domain.FilterExpr{
		&domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"active"}}},
		&domain.TermFilter{TaskFilter: domain.TaskFilter{Tags: []string{"api"}}},
	}}
	_, where, _ := buildFilterExpr(expr)
	if !strings.Contains(where, " AND ") {
		t.Fatalf("expected AND in WHERE, got %q", where)
	}
}

func TestBuildFilterExpr_Or(t *testing.T) {
	expr := &domain.OrFilter{Children: []domain.FilterExpr{
		&domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"active"}}},
		&domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"pending"}}},
	}}
	_, where, _ := buildFilterExpr(expr)
	if !strings.Contains(where, " OR ") {
		t.Fatalf("expected OR in WHERE, got %q", where)
	}
}

func TestBuildFilterExpr_Not(t *testing.T) {
	expr := &domain.NotFilter{
		Child: &domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"deleted"}}},
	}
	_, where, _ := buildFilterExpr(expr)
	if !strings.Contains(where, "NOT (") {
		t.Fatalf("expected NOT in WHERE, got %q", where)
	}
}

func TestBuildFilterExpr_Nil(t *testing.T) {
	_, where, args := buildFilterExpr(nil)
	if where != "" {
		t.Fatalf("expected empty WHERE for nil, got %q", where)
	}
	if len(args) != 0 {
		t.Fatalf("expected no args for nil, got %v", args)
	}
}
```

- [ ] **Step 8: Verify compilation and run all tests**

Run: `cd /Users/germanamz/projects/tusk && go build ./... && make test`
Expected: ALL PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/repository/task.go internal/sqlite/task.go internal/sqlite/task_test.go internal/service/task.go internal/tui/commands.go internal/mcp/tools.go
git commit -m "$(cat <<'EOF'
feat(sqlite): implement buildFilterExpr and update List to use FilterExpr

Change TaskRepository.List signature from TaskFilter to FilterExpr.
Add buildFilterExpr for recursive AND/OR/NOT SQL generation.
Wrap existing callers in TermFilter for compatibility.
EOF
)"
```

---

### Task 4: Wire ParseExpr into CLI runList

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/app.go` (if resolver wiring needs changes)

- [ ] **Step 1: Update runList to use ParseExpr and ResolveExpr**

In `internal/tui/commands.go`, replace the filter parsing section of `runList` (approximately lines 218-237). The current code does:

```go
input := strings.Join(args, " ")
fs, parseErrs := filter.Parse(input)
// ... error handling ...
tf, resolveErrs := a.resolver.Resolve(ctx, fs)
// ... error handling ...
tasks, err := a.taskSvc.List(ctx, &domain.TermFilter{TaskFilter: *tf})
```

Replace with:

```go
input := strings.Join(args, " ")
expr, parseErrs := filter.ParseExpr(input)
if len(parseErrs) > 0 {
    return fmt.Errorf("filter errors:\n%s", filter.FormatErrors(parseErrs))
}

var filterExpr domain.FilterExpr
if expr != nil {
    var resolveErrs []error
    filterExpr, resolveErrs = a.resolver.ResolveExpr(ctx, expr)
    if len(resolveErrs) > 0 {
        return resolveErrs[0]
    }
}

tasks, err := a.taskSvc.List(ctx, filterExpr)
```

Make sure to add the `domain` import if not already present. The `filter` package import should already be there.

- [ ] **Step 2: Verify compilation**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`
Expected: PASS.

- [ ] **Step 3: Run existing E2E tests**

Run: `cd /Users/germanamz/projects/tusk && make test-e2e`
Expected: ALL PASS — existing filters use implicit AND which ParseExpr handles identically.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/commands.go
git commit -m "$(cat <<'EOF'
feat(tui): switch runList from Parse to ParseExpr for boolean filters

tusk list now supports AND, OR, NOT operators and parenthesized
grouping in filter expressions.
EOF
)"
```

---

### Task 5: Wire ParseExpr into MCP and Add E2E Tests

**Files:**
- Modify: `internal/mcp/server.go` (add `filter` parameter)
- Modify: `internal/mcp/tools.go` (handle `filter` parameter)
- Modify: `tests/e2e/filtering_test.go` (E2E tests)

- [ ] **Step 1: Add filter string parameter to tusk_task_list MCP tool**

In `internal/mcp/server.go`, add a `filter` string parameter to the `tusk_task_list` tool definition. Add it as the first parameter (before `status`):

```go
			mcp.WithString("filter",
				mcp.Description("Filter expression with AND/OR/NOT/parentheses support (e.g. 'status:active OR +urgent'). When provided, other filter parameters are ignored."),
			),
```

- [ ] **Step 2: Handle filter parameter in handleTaskList**

In `internal/mcp/tools.go`, at the beginning of `handleTaskList` (after line 299), add:

```go
	// If a filter string is provided, use ParseExpr for full boolean support
	if filterStr, err := request.RequireString("filter"); err == nil {
		expr, parseErrs := filter.ParseExpr(filterStr)
		if len(parseErrs) > 0 {
			return mcp.NewToolResultError("filter parse error: " + filter.FormatErrors(parseErrs)), nil
		}

		var filterExpr domain.FilterExpr
		if expr != nil {
			resolver := filter.NewResolver(s.taskSvc)
			var resolveErrs []error
			filterExpr, resolveErrs = resolver.ResolveExpr(ctx, expr)
			if len(resolveErrs) > 0 {
				return mcp.NewToolResultError(resolveErrs[0].Error()), nil
			}
		}

		tasks, err := s.taskSvc.List(ctx, filterExpr)
		if err != nil {
			return nil, err
		}

		taskIDs := make([]uuid.UUID, len(tasks))
		for i, t := range tasks {
			taskIDs[i] = t.ID
		}
		tagsByTask, err := s.tagSvc.GetTaskTagsBatch(ctx, taskIDs)
		if err != nil {
			return nil, err
		}

		results := make([]taskResponse, len(tasks))
		for i, t := range tasks {
			results[i] = toTaskResponse(t, tagsByTask[t.ID])
		}

		return toolResultJSON(results)
	}
```

Add the `filter` import to the imports of `tools.go`:
```go
"github.com/germanamz/tusk/internal/filter"
```

- [ ] **Step 3: Add E2E tests for boolean filters**

Add these scenarios to the `scenarios` slice in `TestFiltering` (in `tests/e2e/filtering_test.go`):

```go
{
    Name: "filter_or_operator",
    Steps: []Step{
        {Args: []string{"add", "Active task"}},
        {Args: []string{"start", "$1.short_id"}},
        {Args: []string{"add", "Pending task"}},
        {Args: []string{"add", "Done task"}},
        {Args: []string{"start", "$4.short_id"}},
        {Args: []string{"done", "$4.short_id"}},
        {
            Args: []string{"list", "status:active", "OR", "status:completed"},
            AssertJSON: func(t *testing.T, parsed any) {
                arr := jsonArray(t, parsed)
                if len(arr) != 2 {
                    t.Fatalf("expected 2 tasks (active + completed), got %d", len(arr))
                }
            },
            AssertText: func(t *testing.T, output string) {
                assertContains(t, output, "Active task")
                assertContains(t, output, "Done task")
                assertNotContains(t, output, "Pending task")
            },
        },
    },
},
{
    Name: "filter_not_operator",
    Steps: []Step{
        {Args: []string{"add", "Keep this"}},
        {Args: []string{"add", "Delete this"}},
        {Args: []string{"delete", "$2.short_id"}},
        {
            Args: []string{"list", "NOT", "status:deleted"},
            AssertJSON: func(t *testing.T, parsed any) {
                arr := jsonArray(t, parsed)
                if len(arr) != 1 {
                    t.Fatalf("expected 1 task, got %d", len(arr))
                }
                assertEqual(t, arr[0].(map[string]any)["title"], "Keep this")
            },
            AssertText: func(t *testing.T, output string) {
                assertContains(t, output, "Keep this")
                assertNotContains(t, output, "Delete this")
            },
        },
    },
},
{
    Name: "filter_parenthesized_grouping",
    Steps: []Step{
        {Args: []string{"add", "Backend active", "project:backend"}},
        {Args: []string{"start", "$1.short_id"}},
        {Args: []string{"add", "Frontend active", "project:frontend"}},
        {Args: []string{"start", "$3.short_id"}},
        {Args: []string{"add", "Backend pending", "project:backend"}},
        {
            // Only active tasks from backend or frontend
            Args: []string{"list", "(project:backend", "OR", "project:frontend)", "AND", "status:active"},
            AssertJSON: func(t *testing.T, parsed any) {
                arr := jsonArray(t, parsed)
                if len(arr) != 2 {
                    t.Fatalf("expected 2 active tasks, got %d", len(arr))
                }
            },
            AssertText: func(t *testing.T, output string) {
                assertContains(t, output, "Backend active")
                assertContains(t, output, "Frontend active")
                assertNotContains(t, output, "Backend pending")
            },
        },
    },
},
```

- [ ] **Step 4: Run E2E tests**

Run: `cd /Users/germanamz/projects/tusk && make test-e2e`
Expected: ALL PASS.

- [ ] **Step 5: Run full test suite**

Run: `cd /Users/germanamz/projects/tusk && make test`
Expected: ALL PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/server.go internal/mcp/tools.go tests/e2e/filtering_test.go
git commit -m "$(cat <<'EOF'
feat(mcp): add filter string parameter with boolean expression support

tusk_task_list MCP tool now accepts a filter string parameter that
supports AND/OR/NOT/parentheses. E2E tests cover OR, NOT, and
parenthesized grouping.
EOF
)"
```

---

## Changes Introduced

**New files:**
- `internal/filter/resolve_expr_test.go` — Tests for `ResolveExpr`

**Modified files:**
- `internal/domain/filter.go` — Added `FilterExpr` interface, `AndFilter`, `OrFilter`, `NotFilter`, `TermFilter` types
- `internal/repository/task.go` — `TaskRepository.List` signature changed from `domain.TaskFilter` to `domain.FilterExpr`
- `internal/sqlite/task.go` — Added `buildFilterExpr()` function; updated `List()` to use `buildFilterExpr`
- `internal/sqlite/task_test.go` — Updated all `List` calls to use `&domain.TermFilter{}`; added `buildFilterExpr` tests
- `internal/service/task.go` — Updated `List` signature to `domain.FilterExpr`
- `internal/filter/resolve.go` — Added `ResolveExpr()`, `resolveNode()`, `resolveTerm()`, `exprHasStatus()`
- `internal/tui/commands.go` — `runList` switched from `Parse`/`Resolve` to `ParseExpr`/`ResolveExpr`
- `internal/mcp/server.go` — Added `filter` string parameter to `tusk_task_list`
- `internal/mcp/tools.go` — Handle `filter` parameter with `ParseExpr`/`ResolveExpr`; added `filter` import
- `tests/e2e/filtering_test.go` — E2E scenarios for OR, NOT, and parenthesized filters

**No new dependencies, migrations, or environment variables.**

**No bridge code introduced.**

**User-visible behaviors preserved:**
- `tusk list status:active +api` works (implicit AND, same as before)
- `tusk list` with no filters defaults to `[pending, active]` statuses
- `tusk add` and `tusk modify` continue using `Parse`/`FilterSet` (unchanged)
- All existing MCP structured parameters continue to work
- All prior E2E tests pass

**New user-visible behaviors:**
- `tusk list status:active OR +urgent` — OR operator
- `tusk list NOT status:deleted` — NOT operator
- `tusk list (project:backend OR project:frontend) AND +api` — parenthesized grouping
- `tusk list status:active AND +api` — explicit AND (same result as implicit)
- MCP `tusk_task_list` accepts `filter` string parameter for boolean expressions
