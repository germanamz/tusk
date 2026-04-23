package service

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

// seedTaskDirect bypasses TaskService.Create (and its taxonomy validation) so
// level-check tests can place tasks that already violate the active taxonomy.
func seedTaskDirect(t *testing.T, bundle *RepoBundle, projectID uuid.UUID, shortID, title string, level *string, parentID *uuid.UUID, status string) *domain.Task {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.Task{
		ID:         uuid.New(),
		ShortID:    shortID,
		ProjectID:  projectID,
		Title:      title,
		Status:     status,
		Level:      level,
		ParentID:   parentID,
		Version:    1,
		CreatedAt:  now,
		ModifiedAt: now,
		UDA:        map[string]any{},
	}
	if err := bundle.Tasks.Create(context.Background(), task); err != nil {
		t.Fatalf("seed task %q: %v", shortID, err)
	}
	return task
}

func TestLevelCheck_NoTaxonomyReturnsNoViolations(t *testing.T) {
	env, _, bundle := taxonomyTestEnv(t, nil)
	ctx := context.Background()

	seedTaskDirect(t, bundle, domain.DefaultProjectUUID, "aaaaaaaa", "no taxonomy", nil, nil, "pending")

	vs, err := env.taskSvc.LevelCheck(ctx, nil)
	if err != nil {
		t.Fatalf("LevelCheck: %v", err)
	}
	if len(vs) != 0 {
		t.Fatalf("expected 0 violations, got %d", len(vs))
	}
}

func TestLevelCheck_ReportsAllReasons(t *testing.T) {
	env, _, bundle := taxonomyTestEnv(t, basicTaxonomy)
	ctx := context.Background()

	// missing — no level.
	miss := seedTaskDirect(t, bundle, domain.DefaultProjectUUID, "11111111", "no level", nil, nil, "pending")

	// unknown_level — "bogus" not in taxonomy.
	unk := seedTaskDirect(t, bundle, domain.DefaultProjectUUID, "22222222", "bogus level", ptr("bogus"), nil, "pending")

	// root_requires_top_rank — "story" at root.
	root := seedTaskDirect(t, bundle, domain.DefaultProjectUUID, "33333333", "orphan story", ptr("story"), nil, "pending")

	// parent_rank_not_lower — child "milestone" under parent "milestone".
	topParent := seedTaskDirect(t, bundle, domain.DefaultProjectUUID, "44444444", "top", ptr("milestone"), nil, "pending")
	parentID := topParent.ID
	bad := seedTaskDirect(t, bundle, domain.DefaultProjectUUID, "55555555", "bad child", ptr("milestone"), &parentID, "pending")

	// valid task — milestone root, not a violation.
	_ = seedTaskDirect(t, bundle, domain.DefaultProjectUUID, "66666666", "valid", ptr("milestone"), nil, "pending")

	vs, err := env.taskSvc.LevelCheck(ctx, nil)
	if err != nil {
		t.Fatalf("LevelCheck: %v", err)
	}
	if len(vs) != 4 {
		t.Fatalf("expected 4 violations, got %d", len(vs))
	}

	byShort := make(map[string]*domain.TaxonomyError, len(vs))
	for _, v := range vs {
		byShort[v.Task.ShortID] = v.Err
	}
	cases := []struct {
		shortID string
		reason  string
	}{
		{miss.ShortID, "missing"},
		{unk.ShortID, "unknown_level"},
		{root.ShortID, "root_requires_top_rank"},
		{bad.ShortID, "parent_rank_not_lower"},
	}
	for _, c := range cases {
		te, ok := byShort[c.shortID]
		if !ok {
			t.Fatalf("expected violation for %q, not found", c.shortID)
		}
		if te.Reason != c.reason {
			t.Errorf("%q: reason = %q, want %q", c.shortID, te.Reason, c.reason)
		}
	}
}

func TestLevelCheck_TerminalTasksIncluded(t *testing.T) {
	env, _, bundle := taxonomyTestEnv(t, basicTaxonomy)
	ctx := context.Background()

	seedTaskDirect(t, bundle, domain.DefaultProjectUUID, "deadbeef", "completed-violating", nil, nil, "completed")
	seedTaskDirect(t, bundle, domain.DefaultProjectUUID, "feedface", "deleted-violating", nil, nil, "deleted")

	vs, err := env.taskSvc.LevelCheck(ctx, nil)
	if err != nil {
		t.Fatalf("LevelCheck: %v", err)
	}
	if len(vs) != 2 {
		t.Fatalf("expected 2 violations (terminal tasks must be scanned), got %d", len(vs))
	}
}

func TestLevelCheck_ProjectWithEmptyTaxonomy_NotFlagged(t *testing.T) {
	// Workspace has no default taxonomy; override project carries its own.
	env, projectRepo, bundle := taxonomyTestEnv(t, nil)
	ctx := context.Background()

	override := seedProjectWithTaxonomy(t, projectRepo, "override", domain.Taxonomy{{"alpha"}, {"beta"}})

	// Default project has no taxonomy — this task with no level must NOT flag.
	seedTaskDirect(t, bundle, domain.DefaultProjectUUID, "77777777", "default-no-level", nil, nil, "pending")

	// Override project has a taxonomy — this task with no level MUST flag.
	seedTaskDirect(t, bundle, override.ID, "88888888", "override-no-level", nil, nil, "pending")

	vs, err := env.taskSvc.LevelCheck(ctx, nil)
	if err != nil {
		t.Fatalf("LevelCheck: %v", err)
	}
	if len(vs) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(vs))
	}
	if vs[0].Task.ShortID != "88888888" {
		t.Fatalf("violating task: got %q, want %q", vs[0].Task.ShortID, "88888888")
	}
	if vs[0].Source != TaxonomySourceProjectOverride {
		t.Fatalf("source: got %v, want TaxonomySourceProjectOverride", vs[0].Source)
	}
}

func TestLevelCheck_FilterNarrowsResults(t *testing.T) {
	env, _, bundle := taxonomyTestEnv(t, basicTaxonomy)
	ctx := context.Background()

	seedTaskDirect(t, bundle, domain.DefaultProjectUUID, "a0000000", "bad-pending", nil, nil, "pending")
	seedTaskDirect(t, bundle, domain.DefaultProjectUUID, "a0000001", "bad-completed", nil, nil, "completed")

	filter := &domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"pending"}}}
	vs, err := env.taskSvc.LevelCheck(ctx, filter)
	if err != nil {
		t.Fatalf("LevelCheck: %v", err)
	}
	if len(vs) != 1 {
		t.Fatalf("expected 1 violation under status=pending, got %d", len(vs))
	}
	if vs[0].Task.ShortID != "a0000000" {
		t.Fatalf("violating task: got %q, want %q", vs[0].Task.ShortID, "a0000000")
	}
}
