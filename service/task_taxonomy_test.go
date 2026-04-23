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
func taxonomyTestEnv(t *testing.T, workspaceLevels [][]string) (*testEnv, *sqlite.ProjectRepo, *RepoBundle) {
	t.Helper()
	bundle, projectRepo, _ := newSeededBundle(t)
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

func assertTaxonomyReason(t *testing.T, err error, reason string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected taxonomy error with reason %q, got nil", reason)
	}
	if !errors.Is(err, domain.ErrTaxonomyViolation) {
		t.Fatalf("expected ErrTaxonomyViolation, got %v", err)
	}
	var te *domain.TaxonomyError
	if !errors.As(err, &te) {
		t.Fatalf("expected *TaxonomyError, got %T: %v", err, err)
	}
	if te.Reason != reason {
		t.Fatalf("reason = %q, want %q", te.Reason, reason)
	}
}

func TestTaxonomy_Create_MissingLevel(t *testing.T) {
	env, _, _ := taxonomyTestEnv(t, basicTaxonomy)
	ctx := context.Background()

	task := newMinimalTask("no level")
	err := env.taskSvc.Create(ctx, task)
	assertTaxonomyReason(t, err, "missing")
}

func TestTaxonomy_Create_UnknownLevel(t *testing.T) {
	env, _, _ := taxonomyTestEnv(t, basicTaxonomy)
	ctx := context.Background()

	task := newMinimalTask("bogus")
	task.Level = ptr("bogus")
	err := env.taskSvc.Create(ctx, task)
	assertTaxonomyReason(t, err, "unknown_level")
}

func TestTaxonomy_Create_RootMustBeTopRank(t *testing.T) {
	env, _, _ := taxonomyTestEnv(t, basicTaxonomy)
	ctx := context.Background()

	task := newMinimalTask("orphan story")
	task.Level = ptr("story")
	err := env.taskSvc.Create(ctx, task)
	assertTaxonomyReason(t, err, "root_requires_top_rank")
}

func TestTaxonomy_Create_ParentRankNotLower(t *testing.T) {
	env, _, _ := taxonomyTestEnv(t, basicTaxonomy)
	ctx := context.Background()

	// Milestone → root.
	root := newMinimalTask("roadmap")
	root.Level = ptr("milestone")
	mustCreateTask(t, env.taskSvc, root)

	// Sibling milestone under another milestone is rejected — parent rank
	// is not strictly lower than child rank.
	child := newMinimalTask("another milestone")
	child.Level = ptr("milestone")
	child.ParentID = &root.ID
	err := env.taskSvc.Create(ctx, child)
	assertTaxonomyReason(t, err, "parent_rank_not_lower")
}

func TestTaxonomy_Create_Success(t *testing.T) {
	env, _, _ := taxonomyTestEnv(t, basicTaxonomy)
	ctx := context.Background()

	root := newMinimalTask("roadmap")
	root.Level = ptr("milestone")
	if err := env.taskSvc.Create(ctx, root); err != nil {
		t.Fatalf("Create root: %v", err)
	}

	child := newMinimalTask("q1 story")
	child.Level = ptr("story")
	child.ParentID = &root.ID
	if err := env.taskSvc.Create(ctx, child); err != nil {
		t.Fatalf("Create child: %v", err)
	}
	got, err := env.taskSvc.GetByShortID(ctx, child.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if got.Level == nil || *got.Level != "story" {
		t.Fatalf("persisted level = %v, want 'story'", got.Level)
	}
}

func TestTaxonomy_Update_ReparentIncompatibleRank(t *testing.T) {
	env, _, _ := taxonomyTestEnv(t, basicTaxonomy)
	ctx := context.Background()

	rootA := newMinimalTask("roadmap A")
	rootA.Level = ptr("milestone")
	mustCreateTask(t, env.taskSvc, rootA)

	rootB := newMinimalTask("roadmap B")
	rootB.Level = ptr("milestone")
	mustCreateTask(t, env.taskSvc, rootB)

	// Re-parent a milestone under another milestone — ranks equal.
	pid := rootB.ID
	pp := &pid
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  rootA.ShortID,
		Version:  rootA.Version,
		ParentID: &pp,
	})
	assertTaxonomyReason(t, err, "parent_rank_not_lower")
}

func TestTaxonomy_Update_ReassignProjectToIncompatibleTaxonomy(t *testing.T) {
	env, projectRepo, _ := taxonomyTestEnv(t, [][]string{{"alpha"}, {"beta"}})
	ctx := context.Background()

	task := newMinimalTask("work")
	task.Level = ptr("alpha")
	mustCreateTask(t, env.taskSvc, task)

	// Seed a project whose own taxonomy does NOT include "alpha".
	overrideProject := seedProjectWithTaxonomy(t, projectRepo, "override", domain.Taxonomy{{"red"}, {"blue"}})

	newProjectID := overrideProject.ID
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:   task.ShortID,
		Version:   task.Version,
		ProjectID: &newProjectID,
	})
	assertTaxonomyReason(t, err, "unknown_level")
}

func TestTaxonomy_Update_LevelOnly_ReloadsParent(t *testing.T) {
	env, _, _ := taxonomyTestEnv(t, basicTaxonomy)
	ctx := context.Background()

	root := newMinimalTask("roadmap")
	root.Level = ptr("milestone")
	mustCreateTask(t, env.taskSvc, root)

	child := newMinimalTask("q1 task")
	child.Level = ptr("task")
	child.ParentID = &root.ID
	mustCreateTask(t, env.taskSvc, child)

	// Changing Level only — parent must be re-loaded by validateTaxonomy.
	newLevel := "story"
	lp := &newLevel
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Level:   &lp,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Level == nil || *updated.Level != "story" {
		t.Fatalf("level = %v, want 'story'", updated.Level)
	}
}

func TestTaxonomy_Update_ClearLevel_WithoutTaxonomy_Accepted(t *testing.T) {
	// No workspace taxonomy configured → validator short-circuits.
	env, _, _ := taxonomyTestEnv(t, nil)
	ctx := context.Background()

	task := newMinimalTask("plain")
	task.Level = ptr("legacy")
	mustCreateTask(t, env.taskSvc, task)

	var nilStr *string
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		Level:   &nilStr,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Level != nil {
		t.Fatalf("level = %v, want nil", updated.Level)
	}
}

func TestTaxonomy_Update_ChangingLevel_EmitsTaskModifiedWithLevelDiff(t *testing.T) {
	env, _, _ := taxonomyTestEnv(t, basicTaxonomy)
	ctx := WithActor(context.Background(), "german")

	root := newMinimalTask("roadmap")
	root.Level = ptr("milestone")
	mustCreateTask(t, env.taskSvc, root)

	child := newMinimalTask("child")
	child.Level = ptr("task")
	child.ParentID = &root.ID
	mustCreateTask(t, env.taskSvc, child)

	newLevel := "spike"
	lp := &newLevel
	if _, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Level:   &lp,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	events := listAllEvents(t, env.store)
	evt := firstEventOfType(t, events, domain.EventTaskModified)
	payload, ok := evt.Payload.(domain.TaskModifiedPayload)
	if !ok {
		t.Fatalf("payload: got %T, want TaskModifiedPayload", evt.Payload)
	}
	change, hasLevel := payload.Changes["level"]
	if !hasLevel {
		t.Fatalf("changes should include 'level', got keys=%v", keysOf(payload.Changes))
	}
	if change.From != "task" || change.To != "spike" {
		t.Fatalf("level change = %v → %v, want 'task' → 'spike'", change.From, change.To)
	}
}

// TestTaxonomy_LifecyclePaths_DoNotValidate verifies that Start, Claim,
// Release, and Complete do not re-run taxonomy validation. The task is
// seeded directly through the bundle so it violates the workspace
// taxonomy; each lifecycle call must still succeed.
func TestTaxonomy_LifecyclePaths_DoNotValidate(t *testing.T) {
	env, _, bundle := taxonomyTestEnv(t, basicTaxonomy)
	ctx := context.Background()
	registerTestPlayer(t, env, "agent-1")

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
		t.Fatalf("seed task: %v", err)
	}

	// Start — must not invoke validator.
	started, err := env.taskSvc.Start(ctx, task.ShortID, task.Version, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Claim — must not invoke validator.
	claimed, err := env.taskSvc.Claim(ctx, started.ShortID, "agent-1", started.Version)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	// Release — must not invoke validator.
	released, err := env.taskSvc.Release(ctx, claimed.ShortID, "agent-1", claimed.Version)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}

	// Complete — must not invoke validator.
	if _, err := env.taskSvc.Complete(ctx, released.ShortID, released.Version); err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

// TestTaxonomy_Delete_DoesNotValidate verifies Delete from pending does
// not re-run taxonomy validation even when the task violates the
// workspace taxonomy.
func TestTaxonomy_Delete_DoesNotValidate(t *testing.T) {
	env, _, bundle := taxonomyTestEnv(t, basicTaxonomy)
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
		t.Fatalf("seed task: %v", err)
	}

	if _, err := env.taskSvc.Delete(ctx, task.ShortID, task.Version); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestTaxonomy_Pop_DoesNotValidate(t *testing.T) {
	env, _, bundle := taxonomyTestEnv(t, basicTaxonomy)
	ctx := context.Background()
	registerTestPlayer(t, env, "agent-1")

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
		t.Fatalf("seed task: %v", err)
	}

	if _, err := env.taskSvc.Pop(ctx, "agent-1", nil); err != nil {
		t.Fatalf("Pop: %v", err)
	}
}

// seedProjectWithTaxonomy inserts a project bound to the builtin kanban
// workflow and with the given override taxonomy.
func seedProjectWithTaxonomy(t *testing.T, repo *sqlite.ProjectRepo, name string, tax domain.Taxonomy) *domain.Project {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	p := &domain.Project{
		ID:         uuid.New(),
		Name:       name,
		WorkflowID: uuid.Nil,
		Settings:   domain.ProjectSettings{Taxonomy: &tax},
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("seed project %q: %v", name, err)
	}
	return p
}
