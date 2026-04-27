package tui

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
	"github.com/google/uuid"
)

// updateGolden controls whether golden-file tests rewrite their fixtures
// instead of comparing. Triggered by `-update` flag or TUSK_UPDATE_GOLDEN=1.
// This is the first golden-file test in the project; the convention is
// documented here for future tests to follow.
var updateGolden = flag.Bool("update", false, "rewrite golden testdata files instead of comparing")

// kanbanWorkflowFixture returns a Workflow matching the built-in kanban
// workflow seeded by migration 003_workflows. Used by unit tests that need
// a workflow without spinning up the SQLite store.
func kanbanWorkflowFixture() *domain.Workflow {
	return &domain.Workflow{
		ID:   domain.KanbanWorkflowUUID,
		Name: "kanban",
		Statuses: map[string]domain.StatusConfig{
			"pending":   {Roles: []domain.StatusRole{domain.RoleInitial}},
			"active":    {Roles: []domain.StatusRole{domain.RoleStart, domain.RoleHighlight}},
			"completed": {Roles: []domain.StatusRole{domain.RoleTerminal, domain.RoleDone, domain.RoleDim}},
			"deleted":   {Roles: []domain.StatusRole{domain.RoleTerminal, domain.RoleDelete, domain.RoleDim}},
		},
	}
}

// testAppForMarkdown builds an App wired with a NoteService and PlayerService
// so runTree --format markdown can call ListAllForProject during input
// gathering. The base testApp helper passes nil for these, which would panic
// inside gatherMarkdownInputs.
func testAppForMarkdown(t *testing.T) (*App, *service.TaskService, *service.ProjectService) {
	t.Helper()
	store, projectRepo, workflowRepo := sqlitetest.NewStore(t)

	db := store.DB()
	bundle := &service.RepoBundle{
		Store:       store,
		WriteTx:     &storeWriteTx{store: store},
		Tasks:       sqlite.NewTaskRepo(db),
		Annotations: sqlite.NewAnnotationRepo(db),
		Notes:       sqlite.NewNoteRepo(db),
		Relations:   sqlite.NewRelationRepo(db),
		Tags:        sqlite.NewTagRepo(db),
		Players:     sqlite.NewPlayerRepo(db),
	}

	resolver := func(context.Context, uuid.UUID) (*service.RepoBundle, error) { return bundle, nil }
	projects := func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{domain.DefaultProjectUUID}, nil }

	workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)
	projectSvc := service.NewProjectService(projectRepo, bundle.Tasks, bundle.Store, service.ProjectDefaults{}, nil)
	taskSvc := service.NewTaskService(resolver, projects, projectRepo, projectSvc, workflowSvc, nil)
	tagSvc := service.NewTagService(resolver)
	relationSvc := service.NewRelationService(resolver, projects)
	playerSvc := service.NewPlayerService(bundle.Players)
	noteSvc := service.NewNoteService(bundle.Notes, bundle.Players, projectRepo, bundle.Tasks, 0)

	loadOpts := []config.Option{config.WithSearchPath(t.TempDir())}
	app := New(
		taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, playerSvc, noteSvc,
		nil,
		nil, nil, nil,
		VersionInfo{}, config.TUIConfig{}, config.MCPConfig{}, config.InlineConfig{}, loadOpts,
	)
	return app, taskSvc, projectSvc
}

// setProjectDescription updates the seeded default project so subsequent runs
// see a non-empty description.
func setProjectDescription(t *testing.T, projectSvc *service.ProjectService, name, desc string) {
	t.Helper()
	ctx := context.Background()
	p, err := projectSvc.GetByName(ctx, name)
	if err != nil {
		t.Fatalf("GetByName(%q): %v", name, err)
	}
	descPtr := &desc
	_, err = projectSvc.Modify(ctx, service.ModifyProjectInput{
		Name:            name,
		ExpectedVersion: p.Version,
		Description:     &descPtr,
	})
	if err != nil {
		t.Fatalf("Modify(%q) description: %v", name, err)
	}
}

func TestProjectDisplayName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"tusk-roadmap", "Tusk Roadmap"},
		{"_default", "Default"},
		{"a", "A"},
		{"multi-word_project", "Multi Word Project"},
	}
	for _, c := range cases {
		got := projectDisplayName(c.in)
		if got != c.want {
			t.Errorf("projectDisplayName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWriteBlockquote(t *testing.T) {
	t.Run("single line", func(t *testing.T) {
		var buf bytes.Buffer
		if err := writeBlockquote(&buf, "hello", ""); err != nil {
			t.Fatalf("writeBlockquote: %v", err)
		}
		want := "> hello\n\n"
		if buf.String() != want {
			t.Fatalf("got %q, want %q", buf.String(), want)
		}
	})

	t.Run("multi paragraph", func(t *testing.T) {
		var buf bytes.Buffer
		if err := writeBlockquote(&buf, "first\n\nsecond", ""); err != nil {
			t.Fatalf("writeBlockquote: %v", err)
		}
		want := "> first\n>\n> second\n\n"
		if buf.String() != want {
			t.Fatalf("got %q, want %q", buf.String(), want)
		}
	})

	t.Run("with indent", func(t *testing.T) {
		var buf bytes.Buffer
		if err := writeBlockquote(&buf, "line a\nline b", "  "); err != nil {
			t.Fatalf("writeBlockquote: %v", err)
		}
		want := "  > line a\n  > line b\n\n"
		if buf.String() != want {
			t.Fatalf("got %q, want %q", buf.String(), want)
		}
	})
}

func TestRunTree_MarkdownRejectsRollup(t *testing.T) {
	app, _, _ := testAppForMarkdown(t)

	app.root.SetArgs([]string{"task", "tree", "--format", "markdown", "--rollup"})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error for --format markdown --rollup, got nil")
	}
	if !strings.Contains(err.Error(), "--rollup is not supported with --format markdown") {
		t.Fatalf("error %q should mention rollup-not-supported", err.Error())
	}
}

func TestRunTree_MarkdownRejectsMultiProject(t *testing.T) {
	app, taskSvc, projectSvc := testAppForMarkdown(t)
	ctx := context.Background()

	defaultProj, err := projectSvc.GetByName(ctx, "default")
	if err != nil {
		t.Fatalf("GetByName(default): %v", err)
	}

	other, err := projectSvc.Create(ctx, service.CreateProjectInput{
		Name:       "second",
		WorkflowID: defaultProj.WorkflowID,
	})
	if err != nil {
		t.Fatalf("create second project: %v", err)
	}

	if err := taskSvc.Create(ctx, &domain.Task{Title: "in default"}); err != nil {
		t.Fatalf("create default task: %v", err)
	}
	if err := taskSvc.Create(ctx, &domain.Task{Title: "in second", ProjectID: other.ID}); err != nil {
		t.Fatalf("create second task: %v", err)
	}

	app.root.SetArgs([]string{"task", "tree", "--format", "markdown"})
	err = app.root.Execute()
	if err == nil {
		t.Fatal("expected error for multi-project markdown export, got nil")
	}
	if !strings.Contains(err.Error(), "requires a single project") {
		t.Fatalf("error %q should mention single-project requirement", err.Error())
	}
}

func TestRunTree_MarkdownEmptyWorkspace(t *testing.T) {
	app, _, _ := testAppForMarkdown(t)

	var stdout, stderr bytes.Buffer
	app.root.SetOut(&stdout)
	app.root.SetErr(&stderr)
	app.root.SetArgs([]string{"task", "tree", "--format", "markdown"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout.String() != "" {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
	if stderr.String() != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestFormatMarkdownTitleLine(t *testing.T) {
	wf := kanbanWorkflowFixture()
	level := "milestone"
	due := time.Date(2026, 4, 26, 15, 30, 0, 0, time.UTC)
	order := 1.25

	flt := func(f float64) *float64 { return &f }
	tm := func(t time.Time) *time.Time { return &t }

	cases := []struct {
		name        string
		task        *domain.Task
		tags        []*domain.Tag
		hasTaxonomy bool
		workflow    *domain.Workflow
		want        string
	}{
		{
			name:     "bare title pending omits status",
			task:     &domain.Task{Title: "Bare", Status: "pending"},
			workflow: wf,
			want:     "Bare",
		},
		{
			name:     "active emits status token",
			task:     &domain.Task{Title: "Work item", Status: "active"},
			workflow: wf,
			want:     "Work item status=active",
		},
		{
			name:     "completed (done role) omits status",
			task:     &domain.Task{Title: "Shipped", Status: "completed"},
			workflow: wf,
			want:     "Shipped",
		},
		{
			name:     "priority 3 emits priority token",
			task:     &domain.Task{Title: "Hot", Status: "pending", Priority: 3},
			workflow: wf,
			want:     "Hot priority=3",
		},
		{
			name:     "priority 0 omits priority token",
			task:     &domain.Task{Title: "Cold", Status: "pending", Priority: 0},
			workflow: wf,
			want:     "Cold",
		},
		{
			name:     "due renders YYYY-MM-DD only",
			task:     &domain.Task{Title: "Deadline", Status: "pending", DueAt: tm(due)},
			workflow: wf,
			want:     "Deadline due=2026-04-26",
		},
		{
			name:     "order renders short float",
			task:     &domain.Task{Title: "Sorted", Status: "pending", Order: flt(order)},
			workflow: wf,
			want:     "Sorted order=1.25",
		},
		{
			name:     "uda alphabetical",
			task:     &domain.Task{Title: "Custom", Status: "pending", UDA: map[string]any{"team": "backend", "env": "prod"}},
			workflow: wf,
			want:     "Custom uda.env=prod uda.team=backend",
		},
		{
			name:     "tags trailing alphabetical",
			task:     &domain.Task{Title: "Tagged", Status: "pending"},
			tags:     []*domain.Tag{{Name: "ship-blocker"}, {Name: "api"}},
			workflow: wf,
			want:     "Tagged +api +ship-blocker",
		},
		{
			name:        "level emitted with taxonomy on",
			task:        &domain.Task{Title: "Level", Status: "pending", Level: &level},
			hasTaxonomy: true,
			workflow:    wf,
			want:        "Level level=milestone",
		},
		{
			name:        "level absent with taxonomy off",
			task:        &domain.Task{Title: "Level", Status: "pending", Level: &level},
			hasTaxonomy: false,
			workflow:    wf,
			want:        "Level",
		},
		{
			name:     "nil workflow omits status regardless",
			task:     &domain.Task{Title: "Limbo", Status: "active"},
			workflow: nil,
			want:     "Limbo",
		},
		{
			name: "all tokens combined in documented order",
			task: &domain.Task{
				Title:    "Combined",
				Status:   "active",
				Level:    &level,
				Priority: 3,
				DueAt:    tm(due),
				Order:    flt(order),
				UDA:      map[string]any{"env": "prod", "team": "backend"},
			},
			tags:        []*domain.Tag{{Name: "ship-blocker"}, {Name: "api"}},
			hasTaxonomy: true,
			workflow:    wf,
			want:        "Combined status=active level=milestone priority=3 due=2026-04-26 order=1.25 uda.env=prod uda.team=backend +api +ship-blocker",
		},
		{
			name:     "uda value with whitespace gets quoted",
			task:     &domain.Task{Title: "Q", Status: "pending", UDA: map[string]any{"k": "two words"}},
			workflow: wf,
			want:     `Q uda.k="two words"`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := formatMarkdownTitleLine(c.task, c.tags, c.hasTaxonomy, c.workflow)
			if got != c.want {
				t.Errorf("formatMarkdownTitleLine\n  got:  %q\n  want: %q", got, c.want)
			}
		})
	}
}

func TestQuoteUDAValue(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"with space", `"with space"`},
		{"+leading-plus", `"+leading-plus"`},
		{"-leading-minus", `"-leading-minus"`},
		{"@leading-at", `"@leading-at"`},
		{"", `""`},
		{`contains "quote"`, `"contains \"quote\""`},
		{`back\slash`, `back\slash`},
		{`back\slash and space`, `"back\\slash and space"`},
	}
	for _, c := range cases {
		got := quoteUDAValue(c.in)
		if got != c.want {
			t.Errorf("quoteUDAValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// renderTreeMarkdownFromFixture builds a Renderer with the given inputs and
// returns the rendered markdown. Used by golden-file and string-assertion
// tests that don't need an end-to-end CLI execution.
func renderTreeMarkdownFromFixture(t *testing.T, in *markdownInputs, nodes []*treeNode, hasTaxonomy bool) string {
	t.Helper()
	var buf bytes.Buffer
	r := NewRenderer(&buf, "markdown", false, nil)
	r.setMarkdownInputs(in)
	if hasTaxonomy {
		r.SetTaxonomyResolver(func(uuid.UUID) bool { return true })
	}
	if err := r.renderTreeMarkdown(nodes); err != nil {
		t.Fatalf("renderTreeMarkdown: %v", err)
	}
	return buf.String()
}

func TestRenderTreeMarkdown_GoldenBasic(t *testing.T) {
	wf := kanbanWorkflowFixture()
	proj := &domain.Project{
		ID:          uuid.New(),
		Name:        "tusk-roadmap",
		Description: "The product backlog for tusk.",
		WorkflowID:  wf.ID,
	}

	str := func(s string) *string { return &s }
	flt := func(f float64) *float64 { return &f }
	tm := func(t time.Time) *time.Time { return &t }
	due := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)

	mile := &domain.Task{
		ID:          uuid.New(),
		ShortID:     "m0000001",
		ProjectID:   proj.ID,
		Title:       "v1 launch",
		Description: "Ship the first public release.",
		Status:      "active",
		Priority:    3,
		Level:       str("milestone"),
	}
	init := &domain.Task{
		ID:          uuid.New(),
		ShortID:     "i0000001",
		ProjectID:   proj.ID,
		ParentID:    &mile.ID,
		Title:       "Onboarding flow",
		Description: "Make the first-run experience friendly.",
		Status:      "pending",
		Level:       str("initiative"),
		DueAt:       tm(due),
	}
	storyA := &domain.Task{
		ID:          uuid.New(),
		ShortID:     "sa000001",
		ProjectID:   proj.ID,
		ParentID:    &init.ID,
		Title:       "Welcome screen",
		Description: "Greet the user.\n\nShow next steps.",
		Status:      "completed",
		Level:       str("story"),
	}
	storyB := &domain.Task{
		ID:        uuid.New(),
		ShortID:   "sb000001",
		ProjectID: proj.ID,
		ParentID:  &init.ID,
		Title:     "Sample data import",
		Status:    "active",
		Level:     str("story"),
		Order:     flt(1.5),
		UDA:       map[string]any{"team": "growth"},
	}
	taskA := &domain.Task{
		ID:        uuid.New(),
		ShortID:   "ta000001",
		ProjectID: proj.ID,
		ParentID:  &storyB.ID,
		Title:     "Wire CSV parser",
		Status:    "pending",
		Level:     str("task"),
	}
	taskB := &domain.Task{
		ID:        uuid.New(),
		ShortID:   "tb000001",
		ProjectID: proj.ID,
		ParentID:  &storyB.ID,
		Title:     "Add fixture pack",
		Status:    "completed",
		Level:     str("task"),
	}

	tasks := []*domain.Task{mile, init, storyA, storyB, taskA, taskB}
	tags := map[uuid.UUID][]*domain.Tag{
		storyB.ID: {{Name: "ship-blocker"}, {Name: "api"}},
	}

	annTime1 := time.Date(2026, 4, 15, 8, 0, 0, 0, time.UTC)
	annTime2 := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	noteTime1 := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	noteTime2 := time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC)
	projNoteTime := time.Date(2026, 4, 14, 7, 0, 0, 0, time.UTC)

	annsByTask := map[uuid.UUID][]*domain.Annotation{
		mile.ID: {
			{TaskID: mile.ID, CreatedAt: annTime1, Body: "Initial scope ratified"},
			{TaskID: mile.ID, CreatedAt: annTime2, Body: "Customer interviews booked"},
		},
		taskA.ID: {
			{TaskID: taskA.ID, CreatedAt: annTime1, Body: "Schema field finalized"},
		},
	}
	notesByTask := map[uuid.UUID][]*domain.Note{
		uuid.Nil: {
			{
				ProjectID: proj.ID,
				PlayerID:  "german",
				CreatedAt: projNoteTime,
				Body:      "caching strategy notes",
				Metadata:  map[string]any{"topic": "perf"},
			},
		},
		mile.ID: {
			{
				ProjectID: proj.ID,
				PlayerID:  "german",
				TaskID:    &mile.ID,
				CreatedAt: noteTime1,
				Body:      "weekly checkpoint",
			},
			{
				ProjectID: proj.ID,
				PlayerID:  "german",
				TaskID:    &mile.ID,
				CreatedAt: noteTime2,
				Body:      "second checkpoint",
				Metadata:  map[string]any{"area": "backend"},
			},
		},
		taskA.ID: {
			{
				ProjectID: proj.ID,
				PlayerID:  "german",
				TaskID:    &taskA.ID,
				CreatedAt: noteTime1,
				Body:      "retry needed",
			},
		},
	}

	in := &markdownInputs{
		project:     proj,
		tagsByTask:  tags,
		annsByTask:  annsByTask,
		notesByTask: notesByTask,
		workflowFor: func(*domain.Task) *domain.Workflow { return wf },
	}
	nodes := buildTree(tasks, nil)

	got := renderTreeMarkdownFromFixture(t, in, nodes, true)

	goldenPath := filepath.Join("testdata", "tree_markdown_basic.golden.md")
	if *updateGolden || os.Getenv("TUSK_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (re-run with -update or TUSK_UPDATE_GOLDEN=1 to create it)", goldenPath, err)
	}
	if got != string(want) {
		t.Fatalf("markdown output diverges from golden\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

func TestRenderTreeMarkdown_NotesAndAnnotations(t *testing.T) {
	wf := kanbanWorkflowFixture()
	pid := uuid.New()
	mile := &domain.Task{
		ID:        uuid.New(),
		ShortID:   "m0000001",
		ProjectID: pid,
		Title:     "v1 launch",
		Status:    "active",
	}
	leafID := uuid.New()
	leaf := &domain.Task{
		ID:        leafID,
		ShortID:   "l0000001",
		ProjectID: pid,
		ParentID:  &mile.ID,
		Title:     "Leaf",
		Status:    "pending",
	}
	annTime := time.Date(2026, 4, 15, 8, 0, 0, 0, time.UTC)
	noteTime := time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC)
	in := &markdownInputs{
		project:    &domain.Project{ID: pid, Name: "x", Description: "P desc."},
		tagsByTask: map[uuid.UUID][]*domain.Tag{},
		annsByTask: map[uuid.UUID][]*domain.Annotation{
			mile.ID: {{TaskID: mile.ID, CreatedAt: annTime, Body: "Initial scope ratified"}},
			leafID:  {{TaskID: leafID, CreatedAt: annTime, Body: "Schema field finalized"}},
		},
		notesByTask: map[uuid.UUID][]*domain.Note{
			mile.ID: {{ProjectID: pid, PlayerID: "german", TaskID: &mile.ID, CreatedAt: noteTime, Body: "checkpoint"}},
			leafID:  {{ProjectID: pid, PlayerID: "german", TaskID: &leafID, CreatedAt: noteTime, Body: "retry needed"}},
		},
		workflowFor: func(*domain.Task) *domain.Workflow { return wf },
	}
	// buildTree alone gives a depth-0 milestone. Force the leaf one extra
	// level deeper than buildTree would by hanging a story between them so
	// the leaf renders as a depth >= 2 bullet (the indented variant).
	nodes := []*treeNode{
		{
			Task: mile,
			Children: []*treeNode{
				{
					Task: &domain.Task{ID: uuid.New(), ProjectID: pid, ParentID: &mile.ID, Title: "story", Status: "pending"},
					Children: []*treeNode{
						{Task: leaf},
					},
				},
			},
		},
	}

	got := renderTreeMarkdownFromFixture(t, in, nodes, false)

	// Heading-level annotations on the milestone.
	if !strings.Contains(got, "## v1 launch status=active\n") {
		t.Fatalf("expected milestone heading, got:\n%s", got)
	}
	if !strings.Contains(got, "**Annotations:**\n- 2026-04-15: Initial scope ratified\n") {
		t.Fatalf("expected milestone heading-level annotations, got:\n%s", got)
	}
	if !strings.Contains(got, "**Notes:**\n- 2026-04-16 (german): checkpoint\n") {
		t.Fatalf("expected milestone heading-level notes, got:\n%s", got)
	}
	// Bullet-level annotations and notes on the leaf at depth 2 (2-space
	// sub-indent under the bullet itself, which sits at column 0).
	if !strings.Contains(got, "- [ ] Leaf\n  - **Annotations:**\n    - 2026-04-15: Schema field finalized\n") {
		t.Fatalf("expected leaf bullet-level annotations indented, got:\n%s", got)
	}
	if !strings.Contains(got, "  - **Notes:**\n    - 2026-04-16 (german): retry needed\n") {
		t.Fatalf("expected leaf bullet-level notes indented, got:\n%s", got)
	}
}

func TestRenderTreeMarkdown_ProjectLevelNotes(t *testing.T) {
	wf := kanbanWorkflowFixture()
	pid := uuid.New()
	root := &domain.Task{
		ID:        uuid.New(),
		ShortID:   "r0000001",
		ProjectID: pid,
		Title:     "Root",
		Status:    "pending",
	}
	noteTime := time.Date(2026, 4, 14, 7, 0, 0, 0, time.UTC)
	in := &markdownInputs{
		project:    &domain.Project{ID: pid, Name: "x", Description: "Project description."},
		tagsByTask: map[uuid.UUID][]*domain.Tag{},
		annsByTask: map[uuid.UUID][]*domain.Annotation{},
		notesByTask: map[uuid.UUID][]*domain.Note{
			uuid.Nil: {{ProjectID: pid, PlayerID: "german", CreatedAt: noteTime, Body: "project-scope memo"}},
		},
		workflowFor: func(*domain.Task) *domain.Workflow { return wf },
	}
	nodes := buildTree([]*domain.Task{root}, nil)
	got := renderTreeMarkdownFromFixture(t, in, nodes, false)

	want := "# X\n" +
		"> Project description.\n" +
		"\n" +
		"**Notes:**\n" +
		"- 2026-04-14 (german): project-scope memo\n" +
		"\n" +
		"## Root\n"
	if got != want {
		t.Fatalf("project-level notes\n  got:  %q\n  want: %q", got, want)
	}
}

func TestRenderMarkdownNode_Bullet_WithDescription(t *testing.T) {
	wf := kanbanWorkflowFixture()
	pid := uuid.New()
	storyID := uuid.New()
	story := &domain.Task{
		ID:        storyID,
		ShortID:   "s0000001",
		ProjectID: pid,
		Title:     "Story",
		Status:    "pending",
	}
	leaf := &domain.Task{
		ID:          uuid.New(),
		ShortID:     "t0000001",
		ProjectID:   pid,
		ParentID:    &storyID,
		Title:       "Leaf",
		Description: "Para one.\n\nPara two.",
		Status:      "active",
	}
	in := &markdownInputs{
		project:     &domain.Project{ID: pid, Name: "x"},
		tagsByTask:  map[uuid.UUID][]*domain.Tag{},
		annsByTask:  map[uuid.UUID][]*domain.Annotation{},
		notesByTask: map[uuid.UUID][]*domain.Note{},
		workflowFor: func(*domain.Task) *domain.Workflow { return wf },
	}
	storyNode := &treeNode{
		Task:     story,
		Children: []*treeNode{{Task: leaf}},
	}

	var buf bytes.Buffer
	r := NewRenderer(&buf, "markdown", false, nil)
	r.setMarkdownInputs(in)
	// Render the story at depth 2 directly so we exercise the bullet path
	// with its blockquote indent.
	if err := r.renderMarkdownNode(storyNode, 2); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	// Story is depth 2 → bullet with no indent. Leaf is depth 3 → 2-space indent.
	if !strings.Contains(out, "- [ ] Story\n") {
		t.Fatalf("expected story bullet without indent, got:\n%s", out)
	}
	if !strings.Contains(out, "  - [ ] Leaf status=active\n") {
		t.Fatalf("expected indented leaf bullet with status=active, got:\n%s", out)
	}
	if !strings.Contains(out, "  > Para one.\n  >\n  > Para two.\n") {
		t.Fatalf("expected indented multi-paragraph blockquote for leaf, got:\n%s", out)
	}
}

func TestRenderTreeMarkdown_TaxonomyDisabled(t *testing.T) {
	wf := kanbanWorkflowFixture()
	pid := uuid.New()
	level := "milestone"
	root := &domain.Task{
		ID:        uuid.New(),
		ShortID:   "r0000001",
		ProjectID: pid,
		Title:     "Root",
		Status:    "pending",
		Level:     &level,
	}
	in := &markdownInputs{
		project:     &domain.Project{ID: pid, Name: "x"},
		tagsByTask:  map[uuid.UUID][]*domain.Tag{},
		annsByTask:  map[uuid.UUID][]*domain.Annotation{},
		notesByTask: map[uuid.UUID][]*domain.Note{},
		workflowFor: func(*domain.Task) *domain.Workflow { return wf },
	}
	nodes := buildTree([]*domain.Task{root}, nil)
	out := renderTreeMarkdownFromFixture(t, in, nodes, false)
	if strings.Contains(out, "level=") {
		t.Fatalf("expected no level= token when taxonomy disabled, got:\n%s", out)
	}
}

func TestRenderTreeMarkdown_EmptyDescription(t *testing.T) {
	wf := kanbanWorkflowFixture()
	pid := uuid.New()
	root := &domain.Task{
		ID:        uuid.New(),
		ShortID:   "r0000001",
		ProjectID: pid,
		Title:     "Root",
		Status:    "pending",
	}
	in := &markdownInputs{
		project:     &domain.Project{ID: pid, Name: "x"},
		tagsByTask:  map[uuid.UUID][]*domain.Tag{},
		annsByTask:  map[uuid.UUID][]*domain.Annotation{},
		notesByTask: map[uuid.UUID][]*domain.Note{},
		workflowFor: func(*domain.Task) *domain.Workflow { return wf },
	}
	nodes := buildTree([]*domain.Task{root}, nil)
	out := renderTreeMarkdownFromFixture(t, in, nodes, false)
	// No description → header line, blank line, then the task heading.
	want := "# X\n\n## Root\n"
	if out != want {
		t.Fatalf("unexpected output for empty-description project\n  got:  %q\n  want: %q", out, want)
	}
}

func TestRenderAnnotationsBlock_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := renderAnnotationsBlock(&buf, nil, ""); err != nil {
		t.Fatalf("renderAnnotationsBlock: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected empty output, got %q", buf.String())
	}
}

func TestRenderAnnotationsBlock_Heading(t *testing.T) {
	t1 := time.Date(2026, 4, 15, 8, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 18, 9, 30, 0, 0, time.UTC)
	t3 := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	anns := []*domain.Annotation{
		{CreatedAt: t1, Body: "Blocked by upstream API changes"},
		{CreatedAt: t2, Body: "Investigating root cause"},
		{CreatedAt: t3, Body: "Resolved upstream"},
	}
	var buf bytes.Buffer
	if err := renderAnnotationsBlock(&buf, anns, ""); err != nil {
		t.Fatalf("renderAnnotationsBlock: %v", err)
	}
	want := "**Annotations:**\n" +
		"- 2026-04-15: Blocked by upstream API changes\n" +
		"- 2026-04-18: Investigating root cause\n" +
		"- 2026-04-20: Resolved upstream\n\n"
	if buf.String() != want {
		t.Fatalf("heading annotations\n  got:  %q\n  want: %q", buf.String(), want)
	}
}

func TestRenderAnnotationsBlock_Bullet(t *testing.T) {
	t1 := time.Date(2026, 4, 15, 8, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 18, 9, 30, 0, 0, time.UTC)
	t3 := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	anns := []*domain.Annotation{
		{CreatedAt: t1, Body: "Blocked by upstream API changes"},
		{CreatedAt: t2, Body: "Investigating root cause"},
		{CreatedAt: t3, Body: "Resolved upstream"},
	}
	var buf bytes.Buffer
	if err := renderAnnotationsBlock(&buf, anns, "  "); err != nil {
		t.Fatalf("renderAnnotationsBlock: %v", err)
	}
	want := "  - **Annotations:**\n" +
		"    - 2026-04-15: Blocked by upstream API changes\n" +
		"    - 2026-04-18: Investigating root cause\n" +
		"    - 2026-04-20: Resolved upstream\n"
	if buf.String() != want {
		t.Fatalf("bullet annotations\n  got:  %q\n  want: %q", buf.String(), want)
	}
}

func TestRenderNotesBlock_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := renderNotesBlock(&buf, nil, ""); err != nil {
		t.Fatalf("renderNotesBlock: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected empty output, got %q", buf.String())
	}
}

func TestRenderNotesBlock_HeadingMinimal(t *testing.T) {
	created := time.Date(2026, 4, 15, 8, 0, 0, 0, time.UTC)
	notes := []*domain.Note{
		{CreatedAt: created, PlayerID: "german", Body: "scratch pad note"},
	}
	var buf bytes.Buffer
	if err := renderNotesBlock(&buf, notes, ""); err != nil {
		t.Fatalf("renderNotesBlock: %v", err)
	}
	want := "**Notes:**\n" +
		"- 2026-04-15 (german): scratch pad note\n\n"
	if buf.String() != want {
		t.Fatalf("heading minimal\n  got:  %q\n  want: %q", buf.String(), want)
	}
}

func TestRenderNotesBlock_HeadingWithMetadata(t *testing.T) {
	created := time.Date(2026, 4, 15, 8, 0, 0, 0, time.UTC)
	notes := []*domain.Note{
		{
			CreatedAt: created,
			PlayerID:  "german",
			Body:      "with two metadata keys",
			Metadata:  map[string]any{"topic": "auth", "area": "backend"},
		},
	}
	var buf bytes.Buffer
	if err := renderNotesBlock(&buf, notes, ""); err != nil {
		t.Fatalf("renderNotesBlock: %v", err)
	}
	want := "**Notes:**\n" +
		"- 2026-04-15 (german, area=backend, topic=auth): with two metadata keys\n\n"
	if buf.String() != want {
		t.Fatalf("heading with metadata\n  got:  %q\n  want: %q", buf.String(), want)
	}
}

func TestRenderNotesBlock_Multiline(t *testing.T) {
	created := time.Date(2026, 4, 15, 8, 0, 0, 0, time.UTC)
	notes := []*domain.Note{
		{CreatedAt: created, PlayerID: "german", Body: "line one\nline two\nline three"},
	}
	var buf bytes.Buffer
	if err := renderNotesBlock(&buf, notes, ""); err != nil {
		t.Fatalf("renderNotesBlock: %v", err)
	}
	want := "**Notes:**\n" +
		"- 2026-04-15 (german): line one\n" +
		"  line two\n" +
		"  line three\n\n"
	if buf.String() != want {
		t.Fatalf("multiline note\n  got:  %q\n  want: %q", buf.String(), want)
	}
}

func TestRenderNotesBlock_Bullet(t *testing.T) {
	created := time.Date(2026, 4, 15, 8, 0, 0, 0, time.UTC)
	notes := []*domain.Note{
		{CreatedAt: created, PlayerID: "german", Body: "leaf note"},
	}
	var buf bytes.Buffer
	if err := renderNotesBlock(&buf, notes, "  "); err != nil {
		t.Fatalf("renderNotesBlock: %v", err)
	}
	want := "  - **Notes:**\n" +
		"    - 2026-04-15 (german): leaf note\n"
	if buf.String() != want {
		t.Fatalf("bullet note\n  got:  %q\n  want: %q", buf.String(), want)
	}
}

func TestRenderNotesBlock_QuotedMetadata(t *testing.T) {
	created := time.Date(2026, 4, 15, 8, 0, 0, 0, time.UTC)
	notes := []*domain.Note{
		{
			CreatedAt: created,
			PlayerID:  "german",
			Body:      "quoted meta value",
			Metadata:  map[string]any{"topic": "two words"},
		},
	}
	var buf bytes.Buffer
	if err := renderNotesBlock(&buf, notes, ""); err != nil {
		t.Fatalf("renderNotesBlock: %v", err)
	}
	want := "**Notes:**\n" +
		`- 2026-04-15 (german, topic="two words"): quoted meta value` + "\n\n"
	if buf.String() != want {
		t.Fatalf("quoted metadata\n  got:  %q\n  want: %q", buf.String(), want)
	}
}

func TestRunTree_MarkdownDeleteRoleTaskExcluded(t *testing.T) {
	app, taskSvc, projectSvc := testAppForMarkdown(t)
	ctx := context.Background()

	setProjectDescription(t, projectSvc, "default", "An overview.")
	if err := taskSvc.Create(ctx, &domain.Task{Title: "Keep me"}); err != nil {
		t.Fatalf("create alive task: %v", err)
	}
	gone := &domain.Task{Title: "Drop me"}
	if err := taskSvc.Create(ctx, gone); err != nil {
		t.Fatalf("create soon-deleted task: %v", err)
	}
	// Transition through `active` because kanban does not allow pending → deleted directly.
	stActive := "active"
	if _, err := taskSvc.Update(ctx, domain.TaskUpdate{ShortID: gone.ShortID, Version: gone.Version, Status: &stActive}); err != nil {
		t.Fatalf("activate task: %v", err)
	}
	// Refetch to pick up the new version.
	refreshed, err := taskSvc.GetByShortID(ctx, gone.ShortID)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	stDeleted := "deleted"
	if _, err := taskSvc.Update(ctx, domain.TaskUpdate{ShortID: refreshed.ShortID, Version: refreshed.Version, Status: &stDeleted}); err != nil {
		t.Fatalf("delete task: %v", err)
	}

	var stdout bytes.Buffer
	app.root.SetOut(&stdout)
	app.root.SetArgs([]string{"task", "tree", "--format", "markdown"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Keep me") {
		t.Fatalf("expected alive task to be rendered, got:\n%s", out)
	}
	if strings.Contains(out, "Drop me") {
		t.Fatalf("expected delete-role task to be excluded, got:\n%s", out)
	}
}
