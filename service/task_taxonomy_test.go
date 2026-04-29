package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

// taxonomyTestEnv wires a TaskService with a ProjectService configured to use
// the supplied workspace taxonomy. projectRepo is returned so tests can seed
// additional projects with override taxonomies.
func taxonomyTestEnv(test *testing.T, workspaceLevels [][]string) (*testEnv, *sqlite.ProjectRepo, *RepoBundle) {
	test.Helper()
	bundle, projectRepo, _ := newSeededBundle(test)
	workflowRepo := sqlite.NewWorkflowRepo(bundle.Store.DB())

	resolver, projects := singleBundleResolver(bundle, domain.DefaultProjectUUID)
	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)

	cfg := &config.Config{Taxonomy: config.TaxonomyConfig{Levels: workspaceLevels}}
	projectSvc := NewProjectService(projectRepo, nil, nil, ProjectDefaults{}, cfg)

	taskSvc := NewTaskService(resolver, projects, projectRepo, projectSvc, workflowSvc, nil)

	env := &testEnv{
		taskSvc:     taskSvc,
		workflowSvc: workflowSvc,
		store:       bundle.Store,
	}

	return env, projectRepo, bundle
}

// basicTaxonomy is the shared three-rank taxonomy used by most tests.
var basicTaxonomy = [][]string{{"milestone"}, {"story"}, {"task", "spike"}}

func assertTaxonomyReason(test *testing.T, err error, reason string) {
	test.Helper()

	if err == nil {
		test.Fatalf("expected taxonomy error with reason %q, got nil", reason)
	}

	if !errors.Is(err, domain.ErrTaxonomyViolation) {
		test.Fatalf("expected ErrTaxonomyViolation, got %v", err)
	}

	var taxonomyErr *domain.TaxonomyError

	if !errors.As(err, &taxonomyErr) {
		test.Fatalf("expected *TaxonomyError, got %T: %v", err, err)
	}

	if taxonomyErr.Reason != reason {
		test.Fatalf("reason = %q, want %q", taxonomyErr.Reason, reason)
	}
}

func TestTaxonomy_Create_MissingLevel(test *testing.T) {
	env, _, _ := taxonomyTestEnv(test, basicTaxonomy)
	ctx := context.Background()

	task := newMinimalTask("no level")
	err := env.taskSvc.Create(ctx, task)
	assertTaxonomyReason(test, err, "missing")
}

func TestTaxonomy_Create_UnknownLevel(test *testing.T) {
	env, _, _ := taxonomyTestEnv(test, basicTaxonomy)
	ctx := context.Background()

	task := newMinimalTask("bogus")
	task.Level = ptr("bogus")
	err := env.taskSvc.Create(ctx, task)
	assertTaxonomyReason(test, err, "unknown_level")
}

func TestTaxonomy_Create_RootMustBeTopRank(test *testing.T) {
	env, _, _ := taxonomyTestEnv(test, basicTaxonomy)
	ctx := context.Background()

	task := newMinimalTask("orphan story")
	task.Level = ptr("story")
	err := env.taskSvc.Create(ctx, task)
	assertTaxonomyReason(test, err, "root_requires_top_rank")
}

func TestTaxonomy_Create_ParentRankNotLower(test *testing.T) {
	env, _, _ := taxonomyTestEnv(test, basicTaxonomy)
	ctx := context.Background()

	// Milestone → root.
	root := newMinimalTask("roadmap")
	root.Level = ptr("milestone")
	mustCreateTask(test, env.taskSvc, root)

	// Sibling milestone under another milestone is rejected — parent rank
	// is not strictly lower than child rank.
	child := newMinimalTask("another milestone")
	child.Level = ptr("milestone")
	child.ParentID = &root.ID
	err := env.taskSvc.Create(ctx, child)
	assertTaxonomyReason(test, err, "parent_rank_not_lower")
}

func TestTaxonomy_Create_Success(test *testing.T) {
	env, _, _ := taxonomyTestEnv(test, basicTaxonomy)
	ctx := context.Background()

	root := newMinimalTask("roadmap")
	root.Level = ptr("milestone")

	if err := env.taskSvc.Create(ctx, root); err != nil {
		test.Fatalf("Create root: %v", err)
	}

	child := newMinimalTask("q1 story")
	child.Level = ptr("story")
	child.ParentID = &root.ID

	if err := env.taskSvc.Create(ctx, child); err != nil {
		test.Fatalf("Create child: %v", err)
	}

	got, err := env.taskSvc.GetByShortID(ctx, child.ShortID)

	if err != nil {
		test.Fatalf("GetByShortID: %v", err)
	}

	if got.Level == nil || *got.Level != "story" {
		test.Fatalf("persisted level = %v, want 'story'", got.Level)
	}
}

func TestTaxonomy_Update_ReparentIncompatibleRank(test *testing.T) {
	env, _, _ := taxonomyTestEnv(test, basicTaxonomy)
	ctx := context.Background()

	rootA := newMinimalTask("roadmap A")
	rootA.Level = ptr("milestone")
	mustCreateTask(test, env.taskSvc, rootA)

	rootB := newMinimalTask("roadmap B")
	rootB.Level = ptr("milestone")
	mustCreateTask(test, env.taskSvc, rootB)

	// Re-parent a milestone under another milestone — ranks equal.
	pid := rootB.ID
	parentPtr := &pid
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  rootA.ShortID,
		Version:  rootA.Version,
		ParentID: &parentPtr,
	})
	assertTaxonomyReason(test, err, "parent_rank_not_lower")
}

func TestTaxonomy_Update_ReassignProjectToIncompatibleTaxonomy(test *testing.T) {
	env, projectRepo, _ := taxonomyTestEnv(test, [][]string{{"alpha"}, {"beta"}})
	ctx := context.Background()

	task := newMinimalTask("work")
	task.Level = ptr("alpha")
	mustCreateTask(test, env.taskSvc, task)

	// Seed a project whose own taxonomy does NOT include "alpha".
	overrideProject := seedProjectWithTaxonomy(test, projectRepo, "override", domain.Taxonomy{{"red"}, {"blue"}})

	newProjectID := overrideProject.ID
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:   task.ShortID,
		Version:   task.Version,
		ProjectID: &newProjectID,
	})
	assertTaxonomyReason(test, err, "unknown_level")
}

func TestTaxonomy_Update_LevelOnly_ReloadsParent(test *testing.T) {
	env, _, _ := taxonomyTestEnv(test, basicTaxonomy)
	ctx := context.Background()

	root := newMinimalTask("roadmap")
	root.Level = ptr("milestone")
	mustCreateTask(test, env.taskSvc, root)

	child := newMinimalTask("q1 task")
	child.Level = ptr("task")
	child.ParentID = &root.ID
	mustCreateTask(test, env.taskSvc, child)

	// Changing Level only — parent must be re-loaded by validateTaxonomy.
	newLevel := "story"
	levelPtr := &newLevel
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Level:   &levelPtr,
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	if updated.Level == nil || *updated.Level != "story" {
		test.Fatalf("level = %v, want 'story'", updated.Level)
	}
}

func TestTaxonomy_Update_ClearLevel_WithoutTaxonomy_Accepted(test *testing.T) {
	// No workspace taxonomy configured → validator short-circuits.
	env, _, _ := taxonomyTestEnv(test, nil)
	ctx := context.Background()

	task := newMinimalTask("plain")
	task.Level = ptr("legacy")
	mustCreateTask(test, env.taskSvc, task)

	var nilStr *string
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		Level:   &nilStr,
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	if updated.Level != nil {
		test.Fatalf("level = %v, want nil", updated.Level)
	}
}

func TestTaxonomy_Update_ChangingLevel_EmitsTaskModifiedWithLevelDiff(test *testing.T) {
	env, _, _ := taxonomyTestEnv(test, basicTaxonomy)
	ctx := WithActor(context.Background(), "german")

	root := newMinimalTask("roadmap")
	root.Level = ptr("milestone")
	mustCreateTask(test, env.taskSvc, root)

	child := newMinimalTask("child")
	child.Level = ptr("task")
	child.ParentID = &root.ID
	mustCreateTask(test, env.taskSvc, child)

	newLevel := "spike"
	levelPtr := &newLevel

	if _, updateErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Level:   &levelPtr,
	}); updateErr != nil {
		test.Fatalf("Update: %v", updateErr)
	}

	events := listAllEvents(test, env.store)
	event := firstEventOfType(test, events, domain.EventTaskModified)
	payload, ok := event.Payload.(domain.TaskModifiedPayload)

	if !ok {
		test.Fatalf("payload: got %T, want TaskModifiedPayload", event.Payload)
	}

	change, hasLevel := payload.Changes["level"]

	if !hasLevel {
		test.Fatalf("changes should include 'level', got keys=%v", keysOf(payload.Changes))
	}

	if change.From != "task" || change.To != "spike" {
		test.Fatalf("level change = %v → %v, want 'task' → 'spike'", change.From, change.To)
	}
}

// TestTaxonomy_LifecyclePaths_DoNotValidate verifies that Start, Claim,
// Release, and Complete do not re-run taxonomy validation. The task is
// seeded directly through the bundle so it violates the workspace
// taxonomy; each lifecycle call must still succeed.
func TestTaxonomy_LifecyclePaths_DoNotValidate(test *testing.T) {
	env, _, bundle := taxonomyTestEnv(test, basicTaxonomy)
	ctx := context.Background()
	registerTestPlayer(test, env, "agent-1")

	// Insert a task that violates the taxonomy (no level).
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.Task{
		ID:         uuid.New(),
		ShortID:    "deadbeef",
		ProjectID:  domain.DefaultProjectUUID,
		Title:      "legacy",
		Status:     "pending",
		Version:    1,
		CreatedAt:  now,
		ModifiedAt: now,
		UDA:        map[string]any{},
	}

	if err := bundle.Tasks.Create(ctx, task); err != nil {
		test.Fatalf("seed task: %v", err)
	}

	// Start — must not invoke validator.
	started, startErr := env.taskSvc.Start(ctx, task.ShortID, task.Version, "")

	if startErr != nil {
		test.Fatalf("Start: %v", startErr)
	}

	// Claim — must not invoke validator.
	claimed, claimErr := env.taskSvc.Claim(ctx, started.ShortID, "agent-1", started.Version)

	if claimErr != nil {
		test.Fatalf("Claim: %v", claimErr)
	}

	// Release — must not invoke validator.
	released, releaseErr := env.taskSvc.Release(ctx, claimed.ShortID, "agent-1", claimed.Version)

	if releaseErr != nil {
		test.Fatalf("Release: %v", releaseErr)
	}

	// Complete — must not invoke validator.
	if _, completeErr := env.taskSvc.Complete(ctx, released.ShortID, released.Version); completeErr != nil {
		test.Fatalf("Complete: %v", completeErr)
	}
}

// TestTaxonomy_Delete_DoesNotValidate verifies Delete from pending does
// not re-run taxonomy validation even when the task violates the
// workspace taxonomy.
func TestTaxonomy_Delete_DoesNotValidate(test *testing.T) {
	env, _, bundle := taxonomyTestEnv(test, basicTaxonomy)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.Task{
		ID:         uuid.New(),
		ShortID:    "feedface",
		ProjectID:  domain.DefaultProjectUUID,
		Title:      "legacy delete",
		Status:     "pending",
		Version:    1,
		CreatedAt:  now,
		ModifiedAt: now,
		UDA:        map[string]any{},
	}

	if err := bundle.Tasks.Create(ctx, task); err != nil {
		test.Fatalf("seed task: %v", err)
	}

	if _, deleteErr := env.taskSvc.Delete(ctx, task.ShortID, task.Version); deleteErr != nil {
		test.Fatalf("Delete: %v", deleteErr)
	}
}

func TestTaxonomy_Pop_DoesNotValidate(test *testing.T) {
	env, _, bundle := taxonomyTestEnv(test, basicTaxonomy)
	ctx := context.Background()
	registerTestPlayer(test, env, "agent-1")

	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.Task{
		ID:         uuid.New(),
		ShortID:    "cafebabe",
		ProjectID:  domain.DefaultProjectUUID,
		Title:      "legacy pop",
		Status:     "pending",
		Version:    1,
		CreatedAt:  now,
		ModifiedAt: now,
		UDA:        map[string]any{},
	}

	if err := bundle.Tasks.Create(ctx, task); err != nil {
		test.Fatalf("seed task: %v", err)
	}

	if _, popErr := env.taskSvc.Pop(ctx, "agent-1", nil); popErr != nil {
		test.Fatalf("Pop: %v", popErr)
	}
}

// seedProjectWithTaxonomy inserts a project bound to the builtin kanban
// workflow and with the given override taxonomy.
func seedProjectWithTaxonomy(test *testing.T, repo *sqlite.ProjectRepo, name string, tax domain.Taxonomy) *domain.Project {
	test.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	project := &domain.Project{
		ID:         uuid.New(),
		Name:       name,
		WorkflowID: uuid.Nil,
		Settings:   domain.ProjectSettings{Taxonomy: &tax},
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := repo.Create(context.Background(), project); err != nil {
		test.Fatalf("seed project %q: %v", name, err)
	}

	return project
}
