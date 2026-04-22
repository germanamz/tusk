package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

var _ repository.TaskRepository = (*TaskRepo)(nil)

func newTestTask() *domain.Task {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return &domain.Task{
		ID:          uuid.New(),
		ShortID:     uuid.New().String()[:8],
		Title:       "Test task",
		Description: "A test task",
		Status:      "pending",
		Priority:    2,
		Version:     1,
		UDA:         map[string]any{},
		CreatedAt:   now,
		ModifiedAt:  now,
	}
}

func mustCreateTask(t *testing.T, repo *TaskRepo, task *domain.Task) {
	t.Helper()
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatalf("mustCreateTask: %v", err)
	}
}

// mustCreateTestProject inserts a project row so FK-bound tasks can reference it.
// It uses the seeded kanban workflow (uuid.Nil).
func mustCreateTestProject(t *testing.T, s *Store, id uuid.UUID, name string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	repo := NewProjectRepo(s.DB())
	p := &domain.Project{
		ID:         id,
		Name:       name,
		WorkflowID: uuid.Nil,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("creating test project %q: %v", name, err)
	}
}

func TestTaskCreate(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Title != "Test task" {
		t.Fatalf("expected title 'Test task', got %q", got.Title)
	}
	if got.Version != 1 {
		t.Fatalf("expected version 1, got %d", got.Version)
	}
}

func TestTaskCreateWithNullables(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	due := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	wait := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	rrule := "FREQ=WEEKLY;BYDAY=MO"
	task := newTestTask()
	task.ProjectID = domain.DefaultProjectUUID
	task.DueAt = &due
	task.WaitUntil = &wait
	task.RecurrenceRule = &rrule
	task.UDA = map[string]any{"custom": "value"}
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ProjectID != domain.DefaultProjectUUID {
		t.Fatalf("expected project ID %v, got %v", domain.DefaultProjectUUID, got.ProjectID)
	}
	if got.DueAt == nil || !got.DueAt.Equal(due) {
		t.Fatalf("expected due %v, got %v", due, got.DueAt)
	}
	if got.WaitUntil == nil || !got.WaitUntil.Equal(wait) {
		t.Fatalf("expected wait %v, got %v", wait, got.WaitUntil)
	}
	if got.RecurrenceRule == nil || *got.RecurrenceRule != rrule {
		t.Fatalf("expected rrule %s, got %v", rrule, got.RecurrenceRule)
	}
	if got.UDA["custom"] != "value" {
		t.Fatalf("expected UDA custom=value, got %v", got.UDA)
	}
}

func TestTaskGetByShortID(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(t, repo, task)
	got, err := repo.GetByShortID(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if got.ID != task.ID {
		t.Fatalf("expected ID %s, got %s", task.ID, got.ID)
	}
}

func TestTaskGetByIDNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	_, err := repo.GetByID(context.Background(), uuid.New())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTaskGetByShortIDNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	_, err := repo.GetByShortID(context.Background(), "nonexist")
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTaskUpdate(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(t, repo, task)
	task.Title = "Updated title"
	task.Priority = 4
	if err := repo.Update(ctx, task); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if task.Version != 2 {
		t.Fatalf("expected version bumped to 2, got %d", task.Version)
	}
	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Updated title" {
		t.Fatalf("expected updated title, got %q", got.Title)
	}
	if got.Version != 2 {
		t.Fatalf("expected version 2, got %d", got.Version)
	}
}

func TestTaskUpdateNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	task := newTestTask()
	err := repo.Update(context.Background(), task)
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTaskUpdateConflict(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(t, repo, task)
	task.Version = 99
	task.Title = "Stale update"
	err := repo.Update(ctx, task)
	if err != domain.ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestTaskDelete(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(t, repo, task)
	if err := repo.Delete(ctx, task.ID, task.Version); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := repo.GetByID(ctx, task.ID)
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestTaskDeleteConflict(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(t, repo, task)
	err := repo.Delete(ctx, task.ID, 99)
	if err != domain.ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestTaskDeleteNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	err := repo.Delete(context.Background(), uuid.New(), 1)
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTaskListEmpty(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	tasks, err := repo.List(context.Background(), &domain.TermFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestTaskListAll(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		mustCreateTask(t, repo, newTestTask())
	}
	tasks, err := repo.List(ctx, &domain.TermFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
}

func TestTaskListByStatus(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	t1 := newTestTask()
	t1.Status = "pending"
	mustCreateTask(t, repo, t1)
	t2 := newTestTask()
	t2.Status = "active"
	mustCreateTask(t, repo, t2)
	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"active"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Status != "active" {
		t.Fatalf("expected active, got %s", tasks[0].Status)
	}
}

func TestTaskListByStatusMultiple(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	for _, status := range []string{"pending", "active", "completed"} {
		task := newTestTask()
		task.Status = status
		mustCreateTask(t, repo, task)
	}
	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"pending", "active"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestTaskListByProject(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	projID := uuid.New()
	mustCreateTestProject(t, s, projID, "backend-list")
	t1 := newTestTask()
	t1.ProjectID = projID
	mustCreateTask(t, repo, t1)

	t2 := newTestTask()
	mustCreateTask(t, repo, t2)

	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{ProjectID: &projID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
}

func TestTaskListByPriority(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	for _, p := range []int{1, 2, 3, 4} {
		task := newTestTask()
		task.Priority = p
		mustCreateTask(t, repo, task)
	}
	min, max := 2, 3
	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{PriorityMin: &min, PriorityMax: &max}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestTaskListByDueDate(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	d1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	d3 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	for _, d := range []*time.Time{&d1, &d2, &d3} {
		task := newTestTask()
		task.DueAt = d
		mustCreateTask(t, repo, task)
	}
	after := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	before := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{DueAfter: &after, DueBefore: &before}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestTaskListByParent(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	parent := newTestTask()
	mustCreateTask(t, repo, parent)
	child := newTestTask()
	child.ParentID = &parent.ID
	mustCreateTask(t, repo, child)
	orphan := newTestTask()
	mustCreateTask(t, repo, orphan)
	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{ParentID: &parent.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tasks))
	}
}

func TestTaskListWaitingOnly(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	future := time.Now().UTC().Add(24 * time.Hour)
	past := time.Now().UTC().Add(-24 * time.Hour)
	t1 := newTestTask()
	t1.WaitUntil = &future
	mustCreateTask(t, repo, t1)
	t2 := newTestTask()
	t2.WaitUntil = &past
	mustCreateTask(t, repo, t2)
	t3 := newTestTask()
	mustCreateTask(t, repo, t3)
	waitingOnly := true
	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{WaitingOnly: &waitingOnly}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 waiting task, got %d", len(tasks))
	}
}

func TestTaskListCombinedFilters(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	projID := uuid.New()
	mustCreateTestProject(t, s, projID, "combined-list")

	// Task matches both filters: status=active AND project=combined
	t1 := newTestTask()
	t1.Status = "active"
	t1.ProjectID = projID
	mustCreateTask(t, repo, t1)

	// Matches status but not project
	t2 := newTestTask()
	t2.Status = "active"
	mustCreateTask(t, repo, t2)

	// Matches project but not status
	t3 := newTestTask()
	t3.Status = "pending"
	t3.ProjectID = projID
	mustCreateTask(t, repo, t3)

	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{
		Statuses:  []string{"active"},
		ProjectID: &projID,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task matching both filters, got %d", len(tasks))
	}
	if tasks[0].ID != t1.ID {
		t.Fatalf("expected task %s, got %s", t1.ID, tasks[0].ID)
	}
}

// ── UDA filter tests ───────────────────────────────────────────────────

func TestBuildFilter_UDA(t *testing.T) {
	filter := domain.TaskFilter{
		UDA: map[string]string{"env": "prod"},
	}
	where, args := buildFilter(filter)
	if !strings.Contains(where, "json_extract") {
		t.Fatalf("expected json_extract in WHERE clause, got %q", where)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}
	if args[0] != "$.env" {
		t.Fatalf("expected first arg $.env, got %v", args[0])
	}
	if args[1] != "prod" {
		t.Fatalf("expected second arg prod, got %v", args[1])
	}
}

func TestBuildFilter_UDAEmptyValue(t *testing.T) {
	filter := domain.TaskFilter{
		UDA: map[string]string{"env": ""},
	}
	where, args := buildFilter(filter)
	if !strings.Contains(where, "IS NULL") {
		t.Fatalf("expected IS NULL in WHERE clause for empty value, got %q", where)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d: %v", len(args), args)
	}
}

func TestBuildFilter_UDAMultiple(t *testing.T) {
	filter := domain.TaskFilter{
		UDA: map[string]string{"env": "prod", "team": "backend"},
	}
	where, args := buildFilter(filter)
	if strings.Count(where, "json_extract") != 2 {
		t.Fatalf("expected 2 json_extract conditions, got %q", where)
	}
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d", len(args))
	}
}

// ── Phase 5: Hierarchy tests ───────────────────────────────────────────

func TestTaskGetChildren(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	parent := newTestTask()
	mustCreateTask(t, repo, parent)
	child1 := newTestTask()
	child1.ParentID = &parent.ID
	mustCreateTask(t, repo, child1)
	child2 := newTestTask()
	child2.ParentID = &parent.ID
	mustCreateTask(t, repo, child2)
	grandchild := newTestTask()
	grandchild.ParentID = &child1.ID
	mustCreateTask(t, repo, grandchild)
	children, err := repo.GetChildren(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
}

func TestTaskGetChildrenEmpty(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	task := newTestTask()
	mustCreateTask(t, repo, task)
	children, err := repo.GetChildren(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 0 {
		t.Fatalf("expected 0 children, got %d", len(children))
	}
}

func TestTaskGetDescendants(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	root := newTestTask()
	mustCreateTask(t, repo, root)
	child1 := newTestTask()
	child1.ParentID = &root.ID
	mustCreateTask(t, repo, child1)
	child2 := newTestTask()
	child2.ParentID = &root.ID
	mustCreateTask(t, repo, child2)
	grandchild := newTestTask()
	grandchild.ParentID = &child1.ID
	mustCreateTask(t, repo, grandchild)
	descendants, err := repo.GetDescendants(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(descendants) != 3 {
		t.Fatalf("expected 3 descendants, got %d", len(descendants))
	}
	ids := map[uuid.UUID]bool{}
	for _, d := range descendants {
		ids[d.ID] = true
	}
	for _, expected := range []uuid.UUID{child1.ID, child2.ID, grandchild.ID} {
		if !ids[expected] {
			t.Fatalf("missing descendant %s", expected)
		}
	}
}

func TestTaskGetDescendantsEmpty(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	task := newTestTask()
	mustCreateTask(t, repo, task)
	descendants, err := repo.GetDescendants(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(descendants) != 0 {
		t.Fatalf("expected 0 descendants, got %d", len(descendants))
	}
}

func TestTaskListByRootID(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	root := newTestTask()
	mustCreateTask(t, repo, root)
	child := newTestTask()
	child.ParentID = &root.ID
	mustCreateTask(t, repo, child)
	grandchild := newTestTask()
	grandchild.ParentID = &child.ID
	mustCreateTask(t, repo, grandchild)
	unrelated := newTestTask()
	mustCreateTask(t, repo, unrelated)
	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{RootID: &root.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 descendants via List, got %d", len(tasks))
	}
}

func TestBuildFilter_TitleContains(t *testing.T) {
	v := "auth"
	filter := domain.TaskFilter{TitleContains: &v}
	where, args := buildFilter(filter)
	if !strings.Contains(where, "LOWER(title)") {
		t.Fatalf("expected LOWER(title) in WHERE clause, got %q", where)
	}
	if len(args) != 1 || args[0] != "auth" {
		t.Fatalf("expected args [auth], got %v", args)
	}
}

func TestBuildFilter_DescriptionContains(t *testing.T) {
	v := "implement"
	filter := domain.TaskFilter{DescriptionContains: &v}
	where, args := buildFilter(filter)
	if !strings.Contains(where, "LOWER(description)") {
		t.Fatalf("expected LOWER(description) in WHERE clause, got %q", where)
	}
	if len(args) != 1 || args[0] != "implement" {
		t.Fatalf("expected args [implement], got %v", args)
	}
}

func TestList_TitleContains(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t1.Title = "Implement auth middleware"
	mustCreateTask(t, repo, t1)

	t2 := newTestTask()
	t2.Title = "Write unit tests"
	mustCreateTask(t, repo, t2)

	v := "auth"
	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{TitleContains: &v}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Title != "Implement auth middleware" {
		t.Fatalf("expected auth task, got %q", tasks[0].Title)
	}
}

func TestList_DescriptionContains(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t1.Title = "Task A"
	t1.Description = "This handles authentication"
	mustCreateTask(t, repo, t1)

	t2 := newTestTask()
	t2.Title = "Task B"
	t2.Description = "This handles logging"
	mustCreateTask(t, repo, t2)

	v := "authentication"
	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{DescriptionContains: &v}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Title != "Task A" {
		t.Fatalf("expected Task A, got %q", tasks[0].Title)
	}
}

func TestBuildFilterExpr_And(t *testing.T) {
	expr := &domain.AndFilter{Children: []domain.FilterExpr{
		&domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"active"}}},
		&domain.TermFilter{TaskFilter: domain.TaskFilter{Tags: []string{"api"}}},
	}}
	where, _ := buildFilterExpr(expr)
	if !strings.Contains(where, " AND ") {
		t.Fatalf("expected AND in WHERE, got %q", where)
	}
}

func TestBuildFilterExpr_Or(t *testing.T) {
	expr := &domain.OrFilter{Children: []domain.FilterExpr{
		&domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"active"}}},
		&domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"pending"}}},
	}}
	where, _ := buildFilterExpr(expr)
	if !strings.Contains(where, " OR ") {
		t.Fatalf("expected OR in WHERE, got %q", where)
	}
}

func TestBuildFilterExpr_Not(t *testing.T) {
	expr := &domain.NotFilter{
		Child: &domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"deleted"}}},
	}
	where, _ := buildFilterExpr(expr)
	if !strings.Contains(where, "NOT (") {
		t.Fatalf("expected NOT in WHERE, got %q", where)
	}
}

func TestBuildFilterExpr_Nil(t *testing.T) {
	where, args := buildFilterExpr(nil)
	if where != "" {
		t.Fatalf("expected empty WHERE for nil, got %q", where)
	}
	if len(args) != 0 {
		t.Fatalf("expected no args for nil, got %v", args)
	}
}

func TestBuildFilterExpr_TreeWithOtherFilters(t *testing.T) {
	rootID := uuid.New()
	expr := &domain.AndFilter{Children: []domain.FilterExpr{
		&domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"active"}}},
		&domain.TermFilter{TaskFilter: domain.TaskFilter{RootID: &rootID}},
	}}
	where, args := buildFilterExpr(expr)
	if !strings.Contains(where, "WITH RECURSIVE") {
		t.Fatalf("expected inline CTE in WHERE, got %q", where)
	}
	// Args must be in placeholder order: status first, then root ID
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d: %v", len(args), args)
	}
	if args[0] != "active" {
		t.Fatalf("expected first arg 'active', got %v", args[0])
	}
	if args[1] != rootID.String() {
		t.Fatalf("expected second arg root ID, got %v", args[1])
	}
}

func TestListByRootID_WithStatusFilter(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	root := newTestTask()
	root.Status = "active"
	mustCreateTask(t, repo, root)

	activeChild := newTestTask()
	activeChild.ParentID = &root.ID
	activeChild.Status = "active"
	mustCreateTask(t, repo, activeChild)

	pendingChild := newTestTask()
	pendingChild.ParentID = &root.ID
	pendingChild.Status = "pending"
	mustCreateTask(t, repo, pendingChild)

	// Compound: status=active AND tree=<root> — should return only the active child
	expr := &domain.AndFilter{Children: []domain.FilterExpr{
		&domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"active"}}},
		&domain.TermFilter{TaskFilter: domain.TaskFilter{RootID: &root.ID}},
	}}
	tasks, err := repo.List(ctx, expr)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 active descendant, got %d", len(tasks))
	}
	if tasks[0].ID != activeChild.ID {
		t.Fatalf("expected active child, got %v", tasks[0].ID)
	}
}

// ── Level (Phase 1) ────────────────────────────────────────────────────

func TestTaskLevel_CreateRoundTrip(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	story := "story"
	task.Level = &story
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Level == nil || *got.Level != "story" {
		t.Fatalf("expected level 'story', got %v", got.Level)
	}
}

func TestTaskLevel_UpdateSwitchAndClear(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	story := "story"
	task.Level = &story
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	next := "task"
	task.Level = &next
	if err := repo.Update(ctx, task); err != nil {
		t.Fatalf("Update (switch): %v", err)
	}
	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Level == nil || *got.Level != "task" {
		t.Fatalf("expected level 'task', got %v", got.Level)
	}

	task.Level = nil
	if err := repo.Update(ctx, task); err != nil {
		t.Fatalf("Update (clear): %v", err)
	}
	got, err = repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Level != nil {
		t.Fatalf("expected level to be nil after clear, got %v", *got.Level)
	}
}

func TestTaskLevel_ScanOneNullable(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Level != nil {
		t.Fatalf("expected Level == nil for task without level, got %v", *got.Level)
	}
}
