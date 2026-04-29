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
func seedTaskDirect(test *testing.T, bundle *RepoBundle, projectID uuid.UUID, shortID, title string, level *string, parentID *uuid.UUID, status string) *domain.Task {
	test.Helper()
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
		test.Fatalf("seed task %q: %v", shortID, err)
	}

	return task
}

func TestLevelCheck_NoTaxonomyReturnsNoViolations(test *testing.T) {
	env, _, bundle := taxonomyTestEnv(test, nil)
	ctx := context.Background()

	seedTaskDirect(test, bundle, domain.DefaultProjectUUID, "aaaaaaaa", "no taxonomy", nil, nil, "pending")

	vs, err := env.taskSvc.LevelCheck(ctx, nil)

	if err != nil {
		test.Fatalf("LevelCheck: %v", err)
	}

	if len(vs) != 0 {
		test.Fatalf("expected 0 violations, got %d", len(vs))
	}
}

func TestLevelCheck_ReportsAllReasons(test *testing.T) {
	env, _, bundle := taxonomyTestEnv(test, basicTaxonomy)
	ctx := context.Background()

	// missing — no level.
	miss := seedTaskDirect(test, bundle, domain.DefaultProjectUUID, "11111111", "no level", nil, nil, "pending")

	// unknown_level — "bogus" not in taxonomy.
	unk := seedTaskDirect(test, bundle, domain.DefaultProjectUUID, "22222222", "bogus level", ptr("bogus"), nil, "pending")

	// root_requires_top_rank — "story" at root.
	root := seedTaskDirect(test, bundle, domain.DefaultProjectUUID, "33333333", "orphan story", ptr("story"), nil, "pending")

	// parent_rank_not_lower — child "milestone" under parent "milestone".
	topParent := seedTaskDirect(test, bundle, domain.DefaultProjectUUID, "44444444", "top", ptr("milestone"), nil, "pending")
	parentID := topParent.ID
	bad := seedTaskDirect(test, bundle, domain.DefaultProjectUUID, "55555555", "bad child", ptr("milestone"), &parentID, "pending")

	// valid task — milestone root, not a violation.
	_ = seedTaskDirect(test, bundle, domain.DefaultProjectUUID, "66666666", "valid", ptr("milestone"), nil, "pending")

	vs, err := env.taskSvc.LevelCheck(ctx, nil)

	if err != nil {
		test.Fatalf("LevelCheck: %v", err)
	}

	if len(vs) != 4 {
		test.Fatalf("expected 4 violations, got %d", len(vs))
	}

	byShort := make(map[string]*domain.TaxonomyError, len(vs))

	for _, violation := range vs {
		byShort[violation.Task.ShortID] = violation.Err
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

	for _, tc := range cases {
		te, ok := byShort[tc.shortID]

		if !ok {
			test.Fatalf("expected violation for %q, not found", tc.shortID)
		}

		if te.Reason != tc.reason {
			test.Errorf("%q: reason = %q, want %q", tc.shortID, te.Reason, tc.reason)
		}
	}
}

func TestLevelCheck_TerminalTasksIncluded(test *testing.T) {
	env, _, bundle := taxonomyTestEnv(test, basicTaxonomy)
	ctx := context.Background()

	seedTaskDirect(test, bundle, domain.DefaultProjectUUID, "deadbeef", "completed-violating", nil, nil, "completed")
	seedTaskDirect(test, bundle, domain.DefaultProjectUUID, "feedface", "deleted-violating", nil, nil, "deleted")

	vs, err := env.taskSvc.LevelCheck(ctx, nil)

	if err != nil {
		test.Fatalf("LevelCheck: %v", err)
	}

	if len(vs) != 2 {
		test.Fatalf("expected 2 violations (terminal tasks must be scanned), got %d", len(vs))
	}
}

func TestLevelCheck_ProjectWithEmptyTaxonomy_NotFlagged(test *testing.T) {
	// Workspace has no default taxonomy; override project carries its own.
	env, projectRepo, bundle := taxonomyTestEnv(test, nil)
	ctx := context.Background()

	override := seedProjectWithTaxonomy(test, projectRepo, "override", domain.Taxonomy{{"alpha"}, {"beta"}})

	// Default project has no taxonomy — this task with no level must NOT flag.
	seedTaskDirect(test, bundle, domain.DefaultProjectUUID, "77777777", "default-no-level", nil, nil, "pending")

	// Override project has a taxonomy — this task with no level MUST flag.
	seedTaskDirect(test, bundle, override.ID, "88888888", "override-no-level", nil, nil, "pending")

	vs, err := env.taskSvc.LevelCheck(ctx, nil)

	if err != nil {
		test.Fatalf("LevelCheck: %v", err)
	}

	if len(vs) != 1 {
		test.Fatalf("expected 1 violation, got %d", len(vs))
	}

	if vs[0].Task.ShortID != "88888888" {
		test.Fatalf("violating task: got %q, want %q", vs[0].Task.ShortID, "88888888")
	}

	if vs[0].Source != TaxonomySourceProjectOverride {
		test.Fatalf("source: got %v, want TaxonomySourceProjectOverride", vs[0].Source)
	}
}

func TestLevelCheck_FilterNarrowsResults(test *testing.T) {
	env, _, bundle := taxonomyTestEnv(test, basicTaxonomy)
	ctx := context.Background()

	seedTaskDirect(test, bundle, domain.DefaultProjectUUID, "a0000000", "bad-pending", nil, nil, "pending")
	seedTaskDirect(test, bundle, domain.DefaultProjectUUID, "a0000001", "bad-completed", nil, nil, "completed")

	filter := &domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: []string{"pending"}}}
	vs, err := env.taskSvc.LevelCheck(ctx, filter)

	if err != nil {
		test.Fatalf("LevelCheck: %v", err)
	}

	if len(vs) != 1 {
		test.Fatalf("expected 1 violation under status=pending, got %d", len(vs))
	}

	if vs[0].Task.ShortID != "a0000000" {
		test.Fatalf("violating task: got %q, want %q", vs[0].Task.ShortID, "a0000000")
	}
}
