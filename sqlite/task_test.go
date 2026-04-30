package sqlite

import (
	"context"
	"reflect"
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

func mustCreateTask(test *testing.T, repo *TaskRepo, task *domain.Task) {
	test.Helper()

	if err := repo.Create(context.Background(), task); err != nil {
		test.Fatalf("mustCreateTask: %v", err)
	}
}

// mustCreateTestProject inserts a project row so FK-bound tasks can reference it.
// It uses the seeded kanban workflow (uuid.Nil).
func mustCreateTestProject(test *testing.T, store *Store, id uuid.UUID, name string) {
	test.Helper()

	now := time.Now().UTC().Truncate(time.Millisecond)
	repo := NewProjectRepo(store.DB())
	project := &domain.Project{
		ID:         id,
		Name:       name,
		WorkflowID: uuid.Nil,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := repo.Create(context.Background(), project); err != nil {
		test.Fatalf("creating test project %q: %v", name, err)
	}
}

func TestTaskCreate(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	task := newTestTask()

	if err := repo.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, task.ID)

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.Title != "Test task" {
		test.Fatalf("expected title 'Test task', got %q", got.Title)
	}
	if got.Version != 1 {
		test.Fatalf("expected version 1, got %d", got.Version)
	}
}

func TestTaskCreateWithNullables(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
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
		test.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, task.ID)

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.ProjectID != domain.DefaultProjectUUID {
		test.Fatalf("expected project ID %v, got %v", domain.DefaultProjectUUID, got.ProjectID)
	}
	if got.DueAt == nil || !got.DueAt.Equal(due) {
		test.Fatalf("expected due %v, got %v", due, got.DueAt)
	}
	if got.WaitUntil == nil || !got.WaitUntil.Equal(wait) {
		test.Fatalf("expected wait %v, got %v", wait, got.WaitUntil)
	}
	if got.RecurrenceRule == nil || *got.RecurrenceRule != rrule {
		test.Fatalf("expected rrule %s, got %v", rrule, got.RecurrenceRule)
	}
	if got.UDA["custom"] != "value" {
		test.Fatalf("expected UDA custom=value, got %v", got.UDA)
	}
}

func TestTaskGetByShortID(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(test, repo, task)

	got, err := repo.GetByShortID(ctx, task.ShortID)

	if err != nil {
		test.Fatalf("GetByShortID: %v", err)
	}

	if got.ID != task.ID {
		test.Fatalf("expected ID %s, got %s", task.ID, got.ID)
	}
}

func TestTaskGetByIDNotFound(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	_, err := repo.GetByID(context.Background(), uuid.New())
	if err != domain.ErrNotFound {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTaskGetByShortIDNotFound(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	_, err := repo.GetByShortID(context.Background(), "nonexist")
	if err != domain.ErrNotFound {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTaskUpdate(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(test, repo, task)
	task.Title = "Updated title"
	task.Priority = 4

	if err := repo.Update(ctx, task); err != nil {
		test.Fatalf("Update: %v", err)
	}

	if task.Version != 2 {
		test.Fatalf("expected version bumped to 2, got %d", task.Version)
	}

	got, err := repo.GetByID(ctx, task.ID)

	if err != nil {
		test.Fatal(err)
	}

	if got.Title != "Updated title" {
		test.Fatalf("expected updated title, got %q", got.Title)
	}
	if got.Version != 2 {
		test.Fatalf("expected version 2, got %d", got.Version)
	}
}

func TestTaskUpdateNotFound(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	task := newTestTask()
	err := repo.Update(context.Background(), task)
	if err != domain.ErrNotFound {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTaskUpdateConflict(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(test, repo, task)
	task.Version = 99
	task.Title = "Stale update"
	err := repo.Update(ctx, task)
	if err != domain.ErrConflict {
		test.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestTaskDelete(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(test, repo, task)

	if err := repo.Delete(ctx, task.ID, task.Version); err != nil {
		test.Fatalf("Delete: %v", err)
	}

	_, err := repo.GetByID(ctx, task.ID)

	if err != domain.ErrNotFound {
		test.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestTaskDeleteConflict(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(test, repo, task)
	err := repo.Delete(ctx, task.ID, 99)
	if err != domain.ErrConflict {
		test.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestTaskDeleteNotFound(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	err := repo.Delete(context.Background(), uuid.New(), 1)
	if err != domain.ErrNotFound {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTaskListEmpty(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	tasks, err := repo.List(context.Background(), &domain.TermFilter{})

	if err != nil {
		test.Fatalf("List: %v", err)
	}

	if len(tasks) != 0 {
		test.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestTaskListAll(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	for index := 0; index < 3; index++ {
		mustCreateTask(test, repo, newTestTask())
	}

	tasks, err := repo.List(ctx, &domain.TermFilter{})

	if err != nil {
		test.Fatal(err)
	}

	if len(tasks) != 3 {
		test.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
}

func TestTaskListByStatus(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	task1 := newTestTask()
	task1.Status = "pending"
	mustCreateTask(test, repo, task1)
	task2 := newTestTask()
	task2.Status = "active"
	mustCreateTask(test, repo, task2)

	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"active"}}})

	if err != nil {
		test.Fatal(err)
	}

	if len(tasks) != 1 {
		test.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Status != "active" {
		test.Fatalf("expected active, got %s", tasks[0].Status)
	}
}

func TestTaskListByStatusMultiple(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	for _, status := range []string{"pending", "active", "completed"} {
		task := newTestTask()
		task.Status = status
		mustCreateTask(test, repo, task)
	}

	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"pending", "active"}}})

	if err != nil {
		test.Fatal(err)
	}

	if len(tasks) != 2 {
		test.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func seedLeveledTasks(test *testing.T, repo *TaskRepo) (story, task, unset *domain.Task) {
	test.Helper()
	story = newTestTask()
	storyLvl := "story"
	story.Level = &storyLvl
	mustCreateTask(test, repo, story)

	task = newTestTask()
	taskLvl := "task"
	task.Level = &taskLvl
	mustCreateTask(test, repo, task)

	unset = newTestTask()
	mustCreateTask(test, repo, unset)
	return story, task, unset
}

func TestTaskListByLevelSingle(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	story, _, _ := seedLeveledTasks(test, repo)

	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{Levels: []string{"story"}}})

	if err != nil {
		test.Fatal(err)
	}

	if len(tasks) != 1 {
		test.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].ID != story.ID {
		test.Fatalf("expected story task %s, got %s", story.ID, tasks[0].ID)
	}
}

func TestTaskListByLevelMultiple(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	seedLeveledTasks(test, repo)

	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{Levels: []string{"story", "task"}}})

	if err != nil {
		test.Fatal(err)
	}

	if len(tasks) != 2 {
		test.Fatalf("expected 2 tasks (story + task), got %d", len(tasks))
	}
	gotLevels := map[string]bool{}
	for _, ts := range tasks {
		if ts.Level == nil {
			test.Fatalf("unexpected task with NULL level: %s", ts.ID)
		}
		gotLevels[*ts.Level] = true
	}
	if !gotLevels["story"] || !gotLevels["task"] {
		test.Fatalf("expected levels {story, task}, got %v", gotLevels)
	}
}

func TestTaskListByLevelNilReturnsAll(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	seedLeveledTasks(test, repo)

	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{Levels: nil}})

	if err != nil {
		test.Fatal(err)
	}

	if len(tasks) != 3 {
		test.Fatalf("expected 3 tasks (including NULL level), got %d", len(tasks))
	}
}

func TestTaskListByProject(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()

	projID := uuid.New()
	mustCreateTestProject(test, store, projID, "backend-list")
	task1 := newTestTask()
	task1.ProjectID = projID
	mustCreateTask(test, repo, task1)

	task2 := newTestTask()
	mustCreateTask(test, repo, task2)

	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{ProjectID: &projID}})

	if err != nil {
		test.Fatal(err)
	}

	if len(tasks) != 1 {
		test.Fatalf("expected 1 task, got %d", len(tasks))
	}
}

func TestTaskListByPriority(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	for _, priority := range []int{1, 2, 3, 4} {
		task := newTestTask()
		task.Priority = priority
		mustCreateTask(test, repo, task)
	}
	min, max := 2, 3

	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{PriorityMin: &min, PriorityMax: &max}})

	if err != nil {
		test.Fatal(err)
	}

	if len(tasks) != 2 {
		test.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestTaskListByDueDate(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	date1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	date2 := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	date3 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	for _, due := range []*time.Time{&date1, &date2, &date3} {
		task := newTestTask()
		task.DueAt = due
		mustCreateTask(test, repo, task)
	}
	after := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	before := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)

	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{DueAfter: &after, DueBefore: &before}})

	if err != nil {
		test.Fatal(err)
	}

	if len(tasks) != 2 {
		test.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestTaskListByParent(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	parent := newTestTask()
	mustCreateTask(test, repo, parent)
	child := newTestTask()
	child.ParentID = &parent.ID
	mustCreateTask(test, repo, child)
	orphan := newTestTask()
	mustCreateTask(test, repo, orphan)

	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{ParentID: &parent.ID}})

	if err != nil {
		test.Fatal(err)
	}

	if len(tasks) != 1 {
		test.Fatalf("expected 1 child, got %d", len(tasks))
	}
}

func TestTaskListWaitingOnly(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	future := time.Now().UTC().Add(24 * time.Hour)
	past := time.Now().UTC().Add(-24 * time.Hour)
	task1 := newTestTask()
	task1.WaitUntil = &future
	mustCreateTask(test, repo, task1)
	task2 := newTestTask()
	task2.WaitUntil = &past
	mustCreateTask(test, repo, task2)
	task3 := newTestTask()
	mustCreateTask(test, repo, task3)
	waitingOnly := true

	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{WaitingOnly: &waitingOnly}})

	if err != nil {
		test.Fatal(err)
	}

	if len(tasks) != 1 {
		test.Fatalf("expected 1 waiting task, got %d", len(tasks))
	}
}

func TestTaskListCombinedFilters(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()

	projID := uuid.New()
	mustCreateTestProject(test, store, projID, "combined-list")

	// Task matches both filters: status=active AND project=combined
	task1 := newTestTask()
	task1.Status = "active"
	task1.ProjectID = projID
	mustCreateTask(test, repo, task1)

	// Matches status but not project
	task2 := newTestTask()
	task2.Status = "active"
	mustCreateTask(test, repo, task2)

	// Matches project but not status
	task3 := newTestTask()
	task3.Status = "pending"
	task3.ProjectID = projID
	mustCreateTask(test, repo, task3)

	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{
		Statuses:  []string{"active"},
		ProjectID: &projID,
	}})

	if err != nil {
		test.Fatal(err)
	}

	if len(tasks) != 1 {
		test.Fatalf("expected 1 task matching both filters, got %d", len(tasks))
	}
	if tasks[0].ID != task1.ID {
		test.Fatalf("expected task %s, got %s", task1.ID, tasks[0].ID)
	}
}

// ── UDA filter tests ───────────────────────────────────────────────────

func TestBuildFilter_UDA(test *testing.T) {
	filter := domain.TaskFilter{
		UDA: map[string]string{"env": "prod"},
	}
	where, args := buildFilter(filter)
	if !strings.Contains(where, "json_extract") {
		test.Fatalf("expected json_extract in WHERE clause, got %q", where)
	}
	if len(args) != 2 {
		test.Fatalf("expected 2 args, got %d", len(args))
	}
	if args[0] != "$.env" {
		test.Fatalf("expected first arg $.env, got %v", args[0])
	}
	if args[1] != "prod" {
		test.Fatalf("expected second arg prod, got %v", args[1])
	}
}

func TestBuildFilter_UDAEmptyValue(test *testing.T) {
	filter := domain.TaskFilter{
		UDA: map[string]string{"env": ""},
	}
	where, args := buildFilter(filter)
	if !strings.Contains(where, "IS NULL") {
		test.Fatalf("expected IS NULL in WHERE clause for empty value, got %q", where)
	}
	if len(args) != 2 {
		test.Fatalf("expected 2 args, got %d: %v", len(args), args)
	}
}

func TestBuildFilter_UDAMultiple(test *testing.T) {
	filter := domain.TaskFilter{
		UDA: map[string]string{"env": "prod", "team": "backend"},
	}
	where, args := buildFilter(filter)
	if strings.Count(where, "json_extract") != 2 {
		test.Fatalf("expected 2 json_extract conditions, got %q", where)
	}
	if len(args) != 4 {
		test.Fatalf("expected 4 args, got %d", len(args))
	}
}

// ── Phase 5: Hierarchy tests ───────────────────────────────────────────

func TestTaskGetChildren(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	parent := newTestTask()
	mustCreateTask(test, repo, parent)
	child1 := newTestTask()
	child1.ParentID = &parent.ID
	mustCreateTask(test, repo, child1)
	child2 := newTestTask()
	child2.ParentID = &parent.ID
	mustCreateTask(test, repo, child2)
	grandchild := newTestTask()
	grandchild.ParentID = &child1.ID
	mustCreateTask(test, repo, grandchild)

	children, err := repo.GetChildren(ctx, parent.ID)

	if err != nil {
		test.Fatal(err)
	}

	if len(children) != 2 {
		test.Fatalf("expected 2 children, got %d", len(children))
	}
}

func TestTaskGetChildrenEmpty(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	task := newTestTask()
	mustCreateTask(test, repo, task)

	children, err := repo.GetChildren(context.Background(), task.ID)

	if err != nil {
		test.Fatal(err)
	}

	if len(children) != 0 {
		test.Fatalf("expected 0 children, got %d", len(children))
	}
}

func TestTaskGetDescendants(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	root := newTestTask()
	mustCreateTask(test, repo, root)
	child1 := newTestTask()
	child1.ParentID = &root.ID
	mustCreateTask(test, repo, child1)
	child2 := newTestTask()
	child2.ParentID = &root.ID
	mustCreateTask(test, repo, child2)
	grandchild := newTestTask()
	grandchild.ParentID = &child1.ID
	mustCreateTask(test, repo, grandchild)

	descendants, err := repo.GetDescendants(ctx, root.ID)

	if err != nil {
		test.Fatal(err)
	}

	if len(descendants) != 3 {
		test.Fatalf("expected 3 descendants, got %d", len(descendants))
	}
	ids := map[uuid.UUID]bool{}
	for _, desc := range descendants {
		ids[desc.ID] = true
	}
	for _, expected := range []uuid.UUID{child1.ID, child2.ID, grandchild.ID} {
		if !ids[expected] {
			test.Fatalf("missing descendant %s", expected)
		}
	}
}

func TestTaskGetDescendantsEmpty(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	task := newTestTask()
	mustCreateTask(test, repo, task)

	descendants, err := repo.GetDescendants(context.Background(), task.ID)

	if err != nil {
		test.Fatal(err)
	}

	if len(descendants) != 0 {
		test.Fatalf("expected 0 descendants, got %d", len(descendants))
	}
}

func TestTaskListByRootID(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	root := newTestTask()
	mustCreateTask(test, repo, root)
	child := newTestTask()
	child.ParentID = &root.ID
	mustCreateTask(test, repo, child)
	grandchild := newTestTask()
	grandchild.ParentID = &child.ID
	mustCreateTask(test, repo, grandchild)
	unrelated := newTestTask()
	mustCreateTask(test, repo, unrelated)

	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{RootID: &root.ID}})

	if err != nil {
		test.Fatal(err)
	}

	if len(tasks) != 2 {
		test.Fatalf("expected 2 descendants via List, got %d", len(tasks))
	}
}

func TestBuildFilter_TitleContains(test *testing.T) {
	filterValue := "auth"
	filter := domain.TaskFilter{TitleContains: &filterValue}
	where, args := buildFilter(filter)
	if !strings.Contains(where, "LOWER(title)") {
		test.Fatalf("expected LOWER(title) in WHERE clause, got %q", where)
	}
	if len(args) != 1 || args[0] != "auth" {
		test.Fatalf("expected args [auth], got %v", args)
	}
}

func TestBuildFilter_DescriptionContains(test *testing.T) {
	filterValue := "implement"
	filter := domain.TaskFilter{DescriptionContains: &filterValue}
	where, args := buildFilter(filter)
	if !strings.Contains(where, "LOWER(description)") {
		test.Fatalf("expected LOWER(description) in WHERE clause, got %q", where)
	}
	if len(args) != 1 || args[0] != "implement" {
		test.Fatalf("expected args [implement], got %v", args)
	}
}

func TestList_TitleContains(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()

	task1 := newTestTask()
	task1.Title = "Implement auth middleware"
	mustCreateTask(test, repo, task1)

	task2 := newTestTask()
	task2.Title = "Write unit tests"
	mustCreateTask(test, repo, task2)

	filterValue := "auth"

	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{TitleContains: &filterValue}})

	if err != nil {
		test.Fatalf("List: %v", err)
	}

	if len(tasks) != 1 {
		test.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Title != "Implement auth middleware" {
		test.Fatalf("expected auth task, got %q", tasks[0].Title)
	}
}

func TestList_DescriptionContains(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()

	task1 := newTestTask()
	task1.Title = "Task A"
	task1.Description = "This handles authentication"
	mustCreateTask(test, repo, task1)

	task2 := newTestTask()
	task2.Title = "Task B"
	task2.Description = "This handles logging"
	mustCreateTask(test, repo, task2)

	filterValue := "authentication"

	tasks, err := repo.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{DescriptionContains: &filterValue}})

	if err != nil {
		test.Fatalf("List: %v", err)
	}

	if len(tasks) != 1 {
		test.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Title != "Task A" {
		test.Fatalf("expected Task A, got %q", tasks[0].Title)
	}
}

func TestBuildFilterExpr_And(test *testing.T) {
	expr := &domain.AndFilter{Children: []domain.FilterExpr{
		&domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"active"}}},
		&domain.TermFilter{TaskFilter: domain.TaskFilter{Tags: []string{"api"}}},
	}}
	where, _ := buildFilterExpr(expr)
	if !strings.Contains(where, " AND ") {
		test.Fatalf("expected AND in WHERE, got %q", where)
	}
}

func TestBuildFilterExpr_Or(test *testing.T) {
	expr := &domain.OrFilter{Children: []domain.FilterExpr{
		&domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"active"}}},
		&domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"pending"}}},
	}}
	where, _ := buildFilterExpr(expr)
	if !strings.Contains(where, " OR ") {
		test.Fatalf("expected OR in WHERE, got %q", where)
	}
}

func TestBuildFilterExpr_Not(test *testing.T) {
	expr := &domain.NotFilter{
		Child: &domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"deleted"}}},
	}
	where, _ := buildFilterExpr(expr)
	if !strings.Contains(where, "NOT (") {
		test.Fatalf("expected NOT in WHERE, got %q", where)
	}
}

func TestBuildFilterExpr_Nil(test *testing.T) {
	where, args := buildFilterExpr(nil)
	if where != "" {
		test.Fatalf("expected empty WHERE for nil, got %q", where)
	}
	if len(args) != 0 {
		test.Fatalf("expected no args for nil, got %v", args)
	}
}

func TestBuildFilterExpr_TreeWithOtherFilters(test *testing.T) {
	rootID := uuid.New()
	expr := &domain.AndFilter{Children: []domain.FilterExpr{
		&domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"active"}}},
		&domain.TermFilter{TaskFilter: domain.TaskFilter{RootID: &rootID}},
	}}
	where, args := buildFilterExpr(expr)
	if !strings.Contains(where, "WITH RECURSIVE") {
		test.Fatalf("expected inline CTE in WHERE, got %q", where)
	}
	// Args must be in placeholder order: status first, then root ID
	if len(args) != 2 {
		test.Fatalf("expected 2 args, got %d: %v", len(args), args)
	}
	if args[0] != "active" {
		test.Fatalf("expected first arg 'active', got %v", args[0])
	}
	if args[1] != rootID.String() {
		test.Fatalf("expected second arg root ID, got %v", args[1])
	}
}

func TestListByRootID_WithStatusFilter(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()

	root := newTestTask()
	root.Status = "active"
	mustCreateTask(test, repo, root)

	activeChild := newTestTask()
	activeChild.ParentID = &root.ID
	activeChild.Status = "active"
	mustCreateTask(test, repo, activeChild)

	pendingChild := newTestTask()
	pendingChild.ParentID = &root.ID
	pendingChild.Status = "pending"
	mustCreateTask(test, repo, pendingChild)

	// Compound: status=active AND tree=<root> — should return only the active child
	expr := &domain.AndFilter{Children: []domain.FilterExpr{
		&domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"active"}}},
		&domain.TermFilter{TaskFilter: domain.TaskFilter{RootID: &root.ID}},
	}}

	tasks, err := repo.List(ctx, expr)

	if err != nil {
		test.Fatalf("List: %v", err)
	}

	if len(tasks) != 1 {
		test.Fatalf("expected 1 active descendant, got %d", len(tasks))
	}
	if tasks[0].ID != activeChild.ID {
		test.Fatalf("expected active child, got %v", tasks[0].ID)
	}
}

// ── Level (Phase 1) ────────────────────────────────────────────────────

func TestTaskLevel_CreateRoundTrip(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	task := newTestTask()
	story := "story"
	task.Level = &story

	if err := repo.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, task.ID)

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.Level == nil || *got.Level != "story" {
		test.Fatalf("expected level 'story', got %v", got.Level)
	}
}

func TestTaskLevel_UpdateSwitchAndClear(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	task := newTestTask()
	story := "story"
	task.Level = &story

	if err := repo.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	next := "task"
	task.Level = &next

	if err := repo.Update(ctx, task); err != nil {
		test.Fatalf("Update (switch): %v", err)
	}

	got, err := repo.GetByID(ctx, task.ID)

	if err != nil {
		test.Fatal(err)
	}

	if got.Level == nil || *got.Level != "task" {
		test.Fatalf("expected level 'task', got %v", got.Level)
	}

	task.Level = nil

	if err := repo.Update(ctx, task); err != nil {
		test.Fatalf("Update (clear): %v", err)
	}

	got, err = repo.GetByID(ctx, task.ID)

	if err != nil {
		test.Fatal(err)
	}

	if got.Level != nil {
		test.Fatalf("expected level to be nil after clear, got %v", *got.Level)
	}
}

func TestTaskLevel_ScanOneNullable(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()
	task := newTestTask()

	if err := repo.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, task.ID)

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.Level != nil {
		test.Fatalf("expected Level == nil for task without level, got %v", *got.Level)
	}
}

// ── Phase 1 (subtree urgency overrides): urgency_overrides round-trip ──

func TestTaskUrgencyOverridesRoundTrip(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()

	// Round-trip a populated overrides struct via Update.
	task := newTestTask()
	mustCreateTask(test, repo, task)

	task.UrgencyOverrides = &domain.UrgencyOverrides{
		BlockingWeight: ptrFloat(20.0),
		DueWeight:      ptrFloat(3.5),
	}

	updateErr := repo.Update(ctx, task)

	if updateErr != nil {
		test.Fatalf("Update: %v", updateErr)
	}

	got, getErr := repo.GetByID(ctx, task.ID)

	if getErr != nil {
		test.Fatalf("GetByID: %v", getErr)
	}

	if got.UrgencyOverrides == nil {
		test.Fatalf("expected UrgencyOverrides round-trip, got nil")
	}
	want := &domain.UrgencyOverrides{
		BlockingWeight: ptrFloat(20.0),
		DueWeight:      ptrFloat(3.5),
	}
	if !reflect.DeepEqual(got.UrgencyOverrides, want) {
		test.Fatalf("round-trip mismatch:\n  want %+v\n  got  %+v", want, got.UrgencyOverrides)
	}

	// Confirm a task with nil overrides reads back as nil (NOT &{}).
	plain := newTestTask()
	mustCreateTask(test, repo, plain)

	gotPlain, plainErr := repo.GetByID(ctx, plain.ID)

	if plainErr != nil {
		test.Fatalf("GetByID plain: %v", plainErr)
	}

	if gotPlain.UrgencyOverrides != nil {
		test.Fatalf("expected nil UrgencyOverrides for task without overrides, got %+v", gotPlain.UrgencyOverrides)
	}

	// Verify the column itself is NULL (not, e.g., the literal string "null").
	var raw any

	rawErr := store.DB().QueryRow(`SELECT urgency_overrides FROM tasks WHERE id = ?`, plain.ID.String()).Scan(&raw)

	if rawErr != nil {
		test.Fatalf("raw scan: %v", rawErr)
	}

	if raw != nil {
		test.Fatalf("expected NULL urgency_overrides column, got %v (%T)", raw, raw)
	}
}

func TestGetAncestorOverrides(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()

	// Build root → A → B → C plus an unrelated sibling under root.
	root := newTestTask()
	root.UrgencyOverrides = &domain.UrgencyOverrides{BlockingWeight: ptrFloat(10)}
	mustCreateTask(test, repo, root)

	nodeA := newTestTask()
	nodeA.ParentID = &root.ID
	mustCreateTask(test, repo, nodeA)

	nodeB := newTestTask()
	nodeB.ParentID = &nodeA.ID
	nodeB.UrgencyOverrides = &domain.UrgencyOverrides{DueWeight: ptrFloat(5)}
	mustCreateTask(test, repo, nodeB)

	nodeC := newTestTask()
	nodeC.ParentID = &nodeB.ID
	mustCreateTask(test, repo, nodeC)

	sibling := newTestTask()
	sibling.ParentID = &root.ID
	mustCreateTask(test, repo, sibling)

	// Walk from C only — should yield {C, B, A, root}, sibling absent.
	got, gotErr := repo.GetAncestorOverrides(ctx, []uuid.UUID{nodeC.ID})

	if gotErr != nil {
		test.Fatalf("GetAncestorOverrides: %v", gotErr)
	}

	byID := map[uuid.UUID]repository.AncestorOverride{}
	for _, override := range got {
		byID[override.TaskID] = override
	}
	wantIDs := []uuid.UUID{nodeC.ID, nodeB.ID, nodeA.ID, root.ID}
	if len(got) != len(wantIDs) {
		test.Fatalf("expected %d nodes, got %d (%+v)", len(wantIDs), len(got), got)
	}
	for _, id := range wantIDs {
		if _, ok := byID[id]; !ok {
			test.Fatalf("missing expected ancestor %s", id)
		}
	}
	if _, ok := byID[sibling.ID]; ok {
		test.Fatalf("sibling %s should not appear in C's ancestor walk", sibling.ID)
	}

	// Verify Overrides pointers per node.
	if byID[root.ID].Overrides == nil {
		test.Fatalf("root should have non-nil Overrides")
	}
	if byID[nodeB.ID].Overrides == nil {
		test.Fatalf("B should have non-nil Overrides")
	}
	if byID[nodeA.ID].Overrides != nil {
		test.Fatalf("A should have nil Overrides, got %+v", byID[nodeA.ID].Overrides)
	}
	if byID[nodeC.ID].Overrides != nil {
		test.Fatalf("C should have nil Overrides, got %+v", byID[nodeC.ID].Overrides)
	}

	// Verify ParentID semantics: root has nil, leaves chain correctly.
	if byID[root.ID].ParentID != nil {
		test.Fatalf("root.ParentID should be nil, got %v", byID[root.ID].ParentID)
	}
	if byID[nodeC.ID].ParentID == nil || *byID[nodeC.ID].ParentID != nodeB.ID {
		test.Fatalf("C.ParentID should be B (%s), got %v", nodeB.ID, byID[nodeC.ID].ParentID)
	}

	// Walk from {C, sibling} — union should be {C, B, A, root, sibling}, no dup root.
	got2, got2Err := repo.GetAncestorOverrides(ctx, []uuid.UUID{nodeC.ID, sibling.ID})

	if got2Err != nil {
		test.Fatalf("GetAncestorOverrides (2): %v", got2Err)
	}

	seen := map[uuid.UUID]int{}
	for _, override := range got2 {
		seen[override.TaskID]++
	}
	wantIDs2 := []uuid.UUID{nodeC.ID, nodeB.ID, nodeA.ID, root.ID, sibling.ID}
	if len(got2) != len(wantIDs2) {
		test.Fatalf("expected %d nodes (deduped), got %d (%+v)", len(wantIDs2), len(got2), got2)
	}
	for _, id := range wantIDs2 {
		if seen[id] != 1 {
			test.Fatalf("expected exactly 1 occurrence of %s, got %d", id, seen[id])
		}
	}

	// Empty input returns empty slice, no error.
	empty, emptyErr := repo.GetAncestorOverrides(ctx, []uuid.UUID{})

	if emptyErr != nil {
		test.Fatalf("GetAncestorOverrides empty: %v", emptyErr)
	}

	if empty == nil {
		test.Fatalf("expected non-nil empty slice, got nil")
	}
	if len(empty) != 0 {
		test.Fatalf("expected empty slice, got %d items", len(empty))
	}
}
