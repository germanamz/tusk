package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

// testTagEnv creates a fully wired test environment for TagService tests.
func testTagEnv(test *testing.T) (*TagService, *sqlite.Store) {
	test.Helper()
	bundle := newTestBundle(test)
	resolver, _ := singleBundleResolver(bundle, domain.DefaultProjectUUID)
	tagSvc := NewTagService(resolver)
	return tagSvc, bundle.Store
}

func TestFindOrCreate_NewTag(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	tag, err := tagSvc.FindOrCreate(ctx, "backend")

	if err != nil {
		test.Fatalf("FindOrCreate: %v", err)
	}

	if tag.ID == uuid.Nil {
		test.Fatal("expected non-nil ID")
	}
	if tag.Name != "backend" {
		test.Fatalf("expected name 'backend', got %q", tag.Name)
	}
}

func TestFindOrCreate_ExistingTag(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	first, firstErr := tagSvc.FindOrCreate(ctx, "api")

	if firstErr != nil {
		test.Fatalf("first FindOrCreate: %v", firstErr)
	}

	second, secondErr := tagSvc.FindOrCreate(ctx, "api")

	if secondErr != nil {
		test.Fatalf("second FindOrCreate: %v", secondErr)
	}

	if first.ID != second.ID {
		test.Fatalf("expected same ID, got %s and %s", first.ID, second.ID)
	}
}

func TestFindOrCreate_EmptyName(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	_, err := tagSvc.FindOrCreate(ctx, "")
	if err == nil {
		test.Fatal("expected error for empty name")
	}
}

func TestFindOrCreate_WhitespaceName(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	_, err := tagSvc.FindOrCreate(ctx, "   ")
	if err == nil {
		test.Fatal("expected error for whitespace-only name")
	}
}

// mustCreateTaskForTags creates a task via TaskService for use in tag tests.
func mustCreateTaskForTags(test *testing.T, store *sqlite.Store) *domain.Task {
	test.Helper()
	db := store.DB()
	bundle := bundleFromStore(store)
	projectRepo := sqlite.NewProjectRepo(db)
	workflowRepo := sqlite.NewWorkflowRepo(db)
	resolver, projects := singleBundleResolver(bundle, domain.DefaultProjectUUID)
	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)
	taskSvc := NewTaskService(resolver, projects, projectRepo, nil, workflowSvc, nil)

	task := &domain.Task{Title: "test task"}

	if err := taskSvc.Create(context.Background(), task); err != nil {
		test.Fatalf("creating test task: %v", err)
	}

	return task
}

func TestAssignToTask_MultipleTags(test *testing.T) {
	tagSvc, store := testTagEnv(test)
	ctx := context.Background()
	task := mustCreateTaskForTags(test, store)

	assignErr := tagSvc.AssignToTask(ctx, task.ID, []string{"bug", "urgent"})

	if assignErr != nil {
		test.Fatalf("AssignToTask: %v", assignErr)
	}

	tags, tagsErr := tagSvc.GetTaskTags(ctx, task.ID)

	if tagsErr != nil {
		test.Fatalf("GetTaskTags: %v", tagsErr)
	}

	if len(tags) != 2 {
		test.Fatalf("expected 2 tags, got %d", len(tags))
	}

	names := map[string]bool{}
	for _, tag := range tags {
		names[tag.Name] = true
	}
	if !names["bug"] || !names["urgent"] {
		test.Fatalf("expected tags 'bug' and 'urgent', got %v", names)
	}
}

func TestAssignToTask_EmptySlice(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	err := tagSvc.AssignToTask(ctx, uuid.New(), []string{})

	if err != nil {
		test.Fatalf("AssignToTask with empty slice should be no-op, got: %v", err)
	}
}

func TestAssignToTask_Idempotent(test *testing.T) {
	tagSvc, store := testTagEnv(test)
	ctx := context.Background()
	task := mustCreateTaskForTags(test, store)

	if err := tagSvc.AssignToTask(ctx, task.ID, []string{"api"}); err != nil {
		test.Fatalf("first AssignToTask: %v", err)
	}
	if err := tagSvc.AssignToTask(ctx, task.ID, []string{"api"}); err != nil {
		test.Fatalf("second AssignToTask (should be idempotent): %v", err)
	}

	tags, tagsErr := tagSvc.GetTaskTags(ctx, task.ID)

	if tagsErr != nil {
		test.Fatalf("GetTaskTags: %v", tagsErr)
	}

	if len(tags) != 1 {
		test.Fatalf("expected 1 tag after idempotent assign, got %d", len(tags))
	}
}

func TestRemoveFromTask_ExistingTag(test *testing.T) {
	tagSvc, store := testTagEnv(test)
	ctx := context.Background()
	task := mustCreateTaskForTags(test, store)

	if err := tagSvc.AssignToTask(ctx, task.ID, []string{"bug", "urgent"}); err != nil {
		test.Fatalf("AssignToTask: %v", err)
	}

	if err := tagSvc.RemoveFromTask(ctx, task.ID, []string{"bug"}); err != nil {
		test.Fatalf("RemoveFromTask: %v", err)
	}

	tags, tagsErr := tagSvc.GetTaskTags(ctx, task.ID)

	if tagsErr != nil {
		test.Fatalf("GetTaskTags: %v", tagsErr)
	}

	if len(tags) != 1 {
		test.Fatalf("expected 1 tag, got %d", len(tags))
	}
	if tags[0].Name != "urgent" {
		test.Fatalf("expected remaining tag 'urgent', got %q", tags[0].Name)
	}
}

func TestRemoveFromTask_NonexistentTag(test *testing.T) {
	tagSvc, store := testTagEnv(test)
	ctx := context.Background()
	task := mustCreateTaskForTags(test, store)

	// Removing a tag that was never assigned — should be a silent no-op
	err := tagSvc.RemoveFromTask(ctx, task.ID, []string{"nonexistent"})

	if err != nil {
		test.Fatalf("RemoveFromTask for nonexistent tag should be no-op, got: %v", err)
	}
}

func TestRemoveFromTask_EmptySlice(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	err := tagSvc.RemoveFromTask(ctx, uuid.New(), []string{})

	if err != nil {
		test.Fatalf("RemoveFromTask with empty slice should be no-op, got: %v", err)
	}
}

func TestCreate_NewTag(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	tag, err := tagSvc.Create(ctx, "feature", nil)

	if err != nil {
		test.Fatalf("Create: %v", err)
	}

	if tag.Name != "feature" {
		test.Fatalf("expected name 'feature', got %q", tag.Name)
	}
	if tag.Color != nil {
		test.Fatalf("expected nil color, got %v", tag.Color)
	}
}

func TestCreate_WithColor(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	color := "#ff0000"
	tag, err := tagSvc.Create(ctx, "urgent", &color)

	if err != nil {
		test.Fatalf("Create: %v", err)
	}

	if tag.Color == nil || *tag.Color != "#ff0000" {
		test.Fatalf("expected color '#ff0000', got %v", tag.Color)
	}
}

func TestCreate_Duplicate(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	if _, err := tagSvc.Create(ctx, "dup", nil); err != nil {
		test.Fatal(err)
	}

	_, dupErr := tagSvc.Create(ctx, "dup", nil)
	if !errors.Is(dupErr, domain.ErrConflict) {
		test.Fatalf("expected ErrConflict, got %v", dupErr)
	}
}

func TestCreate_EmptyName(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	_, err := tagSvc.Create(ctx, "", nil)
	if err == nil {
		test.Fatal("expected error for empty name")
	}
}

func TestCreate_WhitespaceName(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	_, err := tagSvc.Create(ctx, "   ", nil)
	if err == nil {
		test.Fatal("expected error for whitespace-only name")
	}
}

func TestDelete_UnusedTag(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	if _, err := tagSvc.Create(ctx, "removable", nil); err != nil {
		test.Fatal(err)
	}

	deleted, deleteErr := tagSvc.Delete(ctx, "removable")

	if deleteErr != nil {
		test.Fatalf("Delete: %v", deleteErr)
	}

	if deleted.Name != "removable" {
		test.Fatalf("expected deleted tag name 'removable', got %q", deleted.Name)
	}

	// Verify it's gone
	tags, listErr := tagSvc.List(ctx)

	if listErr != nil {
		test.Fatal(listErr)
	}

	if len(tags) != 0 {
		test.Fatalf("expected 0 tags after delete, got %d", len(tags))
	}
}

func TestDelete_TagInUse(test *testing.T) {
	tagSvc, store := testTagEnv(test)
	ctx := context.Background()

	task := mustCreateTaskForTags(test, store)
	if err := tagSvc.AssignToTask(ctx, task.ID, []string{"busy"}); err != nil {
		test.Fatal(err)
	}

	_, err := tagSvc.Delete(ctx, "busy")
	if !errors.Is(err, domain.ErrTagInUse) {
		test.Fatalf("expected ErrTagInUse, got %v", err)
	}
}

func TestDelete_NotFound(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	_, err := tagSvc.Delete(ctx, "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestList(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	if _, err := tagSvc.FindOrCreate(ctx, "alpha"); err != nil {
		test.Fatalf("FindOrCreate: %v", err)
	}
	if _, err := tagSvc.FindOrCreate(ctx, "beta"); err != nil {
		test.Fatalf("FindOrCreate: %v", err)
	}

	tags, listErr := tagSvc.List(ctx)

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(tags) != 2 {
		test.Fatalf("expected 2 tags, got %d", len(tags))
	}
}

func TestRename_Success(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	if _, err := tagSvc.Create(ctx, "oldname", nil); err != nil {
		test.Fatal(err)
	}

	renamed, renameErr := tagSvc.Rename(ctx, "oldname", "newname")

	if renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	if renamed.Name != "newname" {
		test.Fatalf("expected renamed tag name 'newname', got %q", renamed.Name)
	}

	// Old name should not exist
	tags, listErr := tagSvc.List(ctx)

	if listErr != nil {
		test.Fatal(listErr)
	}

	if len(tags) != 1 {
		test.Fatalf("expected 1 tag, got %d", len(tags))
	}
	if tags[0].Name != "newname" {
		test.Fatalf("expected 'newname', got %q", tags[0].Name)
	}
}

func TestRename_Conflict(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	if _, err := tagSvc.Create(ctx, "aaa", nil); err != nil {
		test.Fatal(err)
	}
	if _, err := tagSvc.Create(ctx, "bbb", nil); err != nil {
		test.Fatal(err)
	}

	_, err := tagSvc.Rename(ctx, "aaa", "bbb")
	if !errors.Is(err, domain.ErrConflict) {
		test.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestRename_NotFound(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	_, err := tagSvc.Rename(ctx, "nonexistent", "whatever")
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRename_EmptyNewName(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	if _, err := tagSvc.Create(ctx, "src", nil); err != nil {
		test.Fatal(err)
	}

	_, err := tagSvc.Rename(ctx, "src", "")
	if err == nil {
		test.Fatal("expected error for empty new name")
	}
}

func TestModify_SetColor(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	if _, err := tagSvc.Create(ctx, "plain", nil); err != nil {
		test.Fatal(err)
	}

	color := "#00ff00"
	tag, modifyErr := tagSvc.Modify(ctx, "plain", &color)

	if modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	if tag.Color == nil || *tag.Color != "#00ff00" {
		test.Fatalf("expected color '#00ff00', got %v", tag.Color)
	}
}

func TestModify_ClearColor(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	color := "#ff0000"
	if _, err := tagSvc.Create(ctx, "colored", &color); err != nil {
		test.Fatal(err)
	}

	tag, modifyErr := tagSvc.Modify(ctx, "colored", nil)

	if modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	if tag.Color != nil {
		test.Fatalf("expected nil color after clearing, got %v", tag.Color)
	}
}

func TestModify_NotFound(test *testing.T) {
	tagSvc, _ := testTagEnv(test)
	ctx := context.Background()

	color := "#aabbcc"
	_, err := tagSvc.Modify(ctx, "ghost", &color)
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListWithUsage(test *testing.T) {
	tagSvc, store := testTagEnv(test)
	ctx := context.Background()

	if _, err := tagSvc.Create(ctx, "active", nil); err != nil {
		test.Fatal(err)
	}
	if _, err := tagSvc.Create(ctx, "idle", nil); err != nil {
		test.Fatal(err)
	}

	task := mustCreateTaskForTags(test, store)
	if err := tagSvc.AssignToTask(ctx, task.ID, []string{"active"}); err != nil {
		test.Fatal(err)
	}

	results, listErr := tagSvc.ListWithUsage(ctx)

	if listErr != nil {
		test.Fatalf("ListWithUsage: %v", listErr)
	}

	if len(results) != 2 {
		test.Fatalf("expected 2 tags, got %d", len(results))
	}

	byName := map[string]domain.TagWithUsage{}
	for _, tagUsage := range results {
		byName[tagUsage.Tag.Name] = tagUsage
	}
	if byName["active"].TaskCount != 1 {
		test.Fatalf("expected 'active' task count 1, got %d", byName["active"].TaskCount)
	}
	if byName["idle"].TaskCount != 0 {
		test.Fatalf("expected 'idle' task count 0, got %d", byName["idle"].TaskCount)
	}
}
