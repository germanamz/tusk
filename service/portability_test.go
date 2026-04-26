package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/internal/portability"
	"github.com/germanamz/tusk/repository"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

// portTestEnv bundles every service the PortabilityService needs plus the
// raw repo handles tests use to seed and assert workspace state directly.
type portTestEnv struct {
	t          *testing.T
	bundle     *RepoBundle
	projectSvc *ProjectService
	taskSvc    *TaskService
	tagSvc     *TagService
	relSvc     *RelationService
	wfSvc      *WorkflowService
	playerSvc  *PlayerService
	noteSvc    *NoteService
	port       *PortabilityService

	projectRepo *sqlite.ProjectRepo
	wfRepo      *sqlite.WorkflowRepo
	eventRepo   repository.EventRepository
}

func newPortTestEnv(t *testing.T) *portTestEnv {
	t.Helper()
	bundle, projectRepo, wfRepo := newSeededBundle(t)
	resolver, projects := singleBundleResolver(bundle, domain.DefaultProjectUUID)

	wfSvc := NewWorkflowService(wfRepo, projectRepo)
	projectSvc := NewProjectService(projectRepo, bundle.Tasks, bundle.Store, ProjectDefaults{}, nil)
	taskSvc := NewTaskService(resolver, projects, projectRepo, projectSvc, wfSvc, nil)
	tagSvc := NewTagService(resolver)
	relSvc := NewRelationService(resolver, projects)
	playerSvc := NewPlayerService(bundle.Players)
	noteSvc := NewNoteService(bundle.Notes, bundle.Players, projectRepo, bundle.Tasks, 20)

	port := NewPortabilityService(
		bundle.WriteTx,
		taskSvc, projectSvc, wfSvc, relSvc, tagSvc, playerSvc, noteSvc,
		bundle, "test",
	)

	return &portTestEnv{
		t:           t,
		bundle:      bundle,
		projectSvc:  projectSvc,
		taskSvc:     taskSvc,
		tagSvc:      tagSvc,
		relSvc:      relSvc,
		wfSvc:       wfSvc,
		playerSvc:   playerSvc,
		noteSvc:     noteSvc,
		port:        port,
		projectRepo: projectRepo,
		wfRepo:      wfRepo,
		eventRepo:   sqlite.NewEventRepo(bundle.Store.DB(), 0, 0),
	}
}

// seedRichWorkspace populates the workspace with a representative slice of
// every entity kind so round-trip and apply tests have something to chew
// on. Returns the IDs of the parent task, child task, and tag — tests
// reuse these for follow-up assertions.
type seededWorkspace struct {
	parentTask *domain.Task
	childTask  *domain.Task
	relation   *domain.Relation
	tag        *domain.Tag
	annotation *domain.Annotation
	playerID   string
	projectID  uuid.UUID
	taskNote   *domain.Note
	projNote   *domain.Note
}

func (e *portTestEnv) seedRichWorkspace(ctx context.Context) *seededWorkspace {
	e.t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)

	playerID := "test-player"
	if _, err := e.playerSvc.Register(ctx, playerID, "human"); err != nil {
		e.t.Fatalf("seeding player: %v", err)
	}

	tagID := uuid.New()
	tag := &domain.Tag{ID: tagID, Name: "feature"}
	if err := e.bundle.Tags.Create(ctx, tag); err != nil {
		e.t.Fatalf("seeding tag: %v", err)
	}

	parent := &domain.Task{Title: "parent task"}
	if err := e.taskSvc.Create(ctx, parent); err != nil {
		e.t.Fatalf("seeding parent task: %v", err)
	}
	child := &domain.Task{Title: "child task", ParentID: &parent.ID}
	if err := e.taskSvc.Create(ctx, child); err != nil {
		e.t.Fatalf("seeding child task: %v", err)
	}

	if err := e.bundle.Tags.AssignToTask(ctx, child.ID, tagID); err != nil {
		e.t.Fatalf("seeding tag assignment: %v", err)
	}

	rel, err := e.relSvc.Add(ctx, parent.ShortID, child.ShortID, "blocks")
	if err != nil {
		e.t.Fatalf("seeding relation: %v", err)
	}

	annID := uuid.New()
	ann := &domain.Annotation{
		ID: annID, TaskID: parent.ID, Body: "first annotation", CreatedAt: now,
	}
	if err := e.bundle.Annotations.Create(ctx, ann); err != nil {
		e.t.Fatalf("seeding annotation: %v", err)
	}

	taskNote := &domain.Note{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  playerID,
		TaskID:    &parent.ID,
		Body:      "note attached to task",
	}
	if err := e.noteSvc.Create(ctx, taskNote); err != nil {
		e.t.Fatalf("seeding task note: %v", err)
	}
	projNote := &domain.Note{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  playerID,
		Body:      "project-level note",
	}
	if err := e.noteSvc.Create(ctx, projNote); err != nil {
		e.t.Fatalf("seeding project note: %v", err)
	}

	return &seededWorkspace{
		parentTask: parent,
		childTask:  child,
		relation:   rel,
		tag:        tag,
		annotation: ann,
		playerID:   playerID,
		projectID:  domain.DefaultProjectUUID,
		taskNote:   taskNote,
		projNote:   projNote,
	}
}

func TestPortabilityService_RoundTripIntoEmptyWorkspace(t *testing.T) {
	envA := newPortTestEnv(t)
	ctx := WithActor(context.Background(), "test-player")
	envA.seedRichWorkspace(ctx)

	dump, err := envA.port.Export(ctx)
	if err != nil {
		t.Fatalf("Export A: %v", err)
	}
	if dump.SchemaVersion != portability.SchemaVersion {
		t.Fatalf("schema version: got %d want %d", dump.SchemaVersion, portability.SchemaVersion)
	}
	if len(dump.Tasks) != 2 {
		t.Fatalf("dump tasks: got %d want 2", len(dump.Tasks))
	}

	envB := newPortTestEnv(t)
	report, err := envB.port.Import(ctx, dump, ImportOptions{Replace: true, Truncate: true})
	if err != nil {
		t.Fatalf("Import B: %v", err)
	}
	if report.Tasks != len(dump.Tasks) {
		t.Fatalf("report.Tasks: got %d want %d", report.Tasks, len(dump.Tasks))
	}

	// Re-export from B and confirm key entities round-trip with their
	// IDs and versions intact. We do not assert deep equality on the
	// raw structs because the import emits its own workspace_imported
	// event and its tuskVersion / ExportedAt header values are scoped
	// to the receiving workspace.
	dumpB, err := envB.port.Export(ctx)
	if err != nil {
		t.Fatalf("Export B: %v", err)
	}
	if got, want := len(dumpB.Tasks), len(dump.Tasks); got != want {
		t.Fatalf("re-export task count: got %d want %d", got, want)
	}
	if got, want := len(dumpB.Relations), len(dump.Relations); got != want {
		t.Fatalf("re-export relation count: got %d want %d", got, want)
	}
	if got, want := len(dumpB.Annotations), len(dump.Annotations); got != want {
		t.Fatalf("re-export annotation count: got %d want %d", got, want)
	}
	if got, want := len(dumpB.Notes), len(dump.Notes); got != want {
		t.Fatalf("re-export note count: got %d want %d", got, want)
	}

	bByID := make(map[uuid.UUID]portability.PortableTask, len(dumpB.Tasks))
	for _, t := range dumpB.Tasks {
		bByID[t.ID] = t
	}
	for _, original := range dump.Tasks {
		got, ok := bByID[original.ID]
		if !ok {
			t.Fatalf("task %s missing from re-export", original.ID)
		}
		if got.Version != original.Version {
			t.Errorf("task %s version: got %d want %d", original.ID, got.Version, original.Version)
		}
		if got.Title != original.Title {
			t.Errorf("task %s title: got %q want %q", original.ID, got.Title, original.Title)
		}
		if (got.ParentID == nil) != (original.ParentID == nil) {
			t.Errorf("task %s parent presence drifted", original.ID)
		} else if got.ParentID != nil && *got.ParentID != *original.ParentID {
			t.Errorf("task %s parent id: got %s want %s", original.ID, *got.ParentID, *original.ParentID)
		}
	}
}

func TestPortabilityService_StrictModeCollisionLeavesWorkspaceUnchanged(t *testing.T) {
	env := newPortTestEnv(t)
	ctx := WithActor(context.Background(), "test-player")
	seed := env.seedRichWorkspace(ctx)

	dump, err := env.port.Export(ctx)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	for i := range dump.Tasks {
		if dump.Tasks[i].ID == seed.parentTask.ID {
			dump.Tasks[i].Title = "rewritten title"
		}
	}

	report, err := env.port.Import(ctx, dump, ImportOptions{})
	if err == nil {
		t.Fatal("expected ImportError in strict mode, got nil")
	}
	var importErr *portability.ImportError
	if !errors.As(err, &importErr) {
		t.Fatalf("expected *portability.ImportError, got %T: %v", err, err)
	}
	if len(importErr.Issues) == 0 {
		t.Fatal("expected at least one issue in ImportError")
	}
	sawCollision := false
	for _, iss := range importErr.Issues {
		if iss.Kind == "collision" {
			sawCollision = true
		}
	}
	if !sawCollision {
		t.Fatalf("expected at least one collision issue, got %#v", importErr.Issues)
	}

	live, err := env.bundle.Tasks.GetByID(ctx, seed.parentTask.ID)
	if err != nil {
		t.Fatalf("loading live parent task: %v", err)
	}
	if live.Title != seed.parentTask.Title {
		t.Errorf("strict-mode collision wrote workspace: title became %q", live.Title)
	}

	if report == nil {
		t.Fatal("expected non-nil report even on validation failure")
	}
	if report.Tasks != len(dump.Tasks) {
		t.Errorf("report.Tasks should reflect dump size on validation error: got %d want %d", report.Tasks, len(dump.Tasks))
	}
}

func TestPortabilityService_ReplaceUpdatesInPlaceFaithful(t *testing.T) {
	for _, parented := range []bool{false, true} {
		name := "root"
		if parented {
			name = "parented"
		}
		t.Run(name, func(t *testing.T) {
			env := newPortTestEnv(t)
			ctx := WithActor(context.Background(), "test-player")

			var (
				targetID uuid.UUID
				original *domain.Task
			)
			if parented {
				seed := env.seedRichWorkspace(ctx)
				targetID = seed.childTask.ID
				original = seed.childTask
			} else {
				task := &domain.Task{Title: "old"}
				if err := env.taskSvc.Create(ctx, task); err != nil {
					t.Fatalf("seed root task: %v", err)
				}
				targetID = task.ID
				original = task
			}

			dump, err := env.port.Export(ctx)
			if err != nil {
				t.Fatalf("Export: %v", err)
			}
			for i := range dump.Tasks {
				if dump.Tasks[i].ID == targetID {
					dump.Tasks[i].Title = "new"
					dump.Tasks[i].Version = 42
				}
			}

			report, err := env.port.Import(ctx, dump, ImportOptions{Replace: true})
			if err != nil {
				t.Fatalf("Import (replace): %v", err)
			}
			if report.Replaced == 0 {
				t.Errorf("expected at least one replacement, got %d", report.Replaced)
			}

			updated, err := env.bundle.Tasks.GetByID(ctx, targetID)
			if err != nil {
				t.Fatalf("loading replaced task: %v", err)
			}
			if updated.Title != "new" {
				t.Errorf("title not replaced: got %q want %q", updated.Title, "new")
			}
			if updated.Version != 42 {
				t.Errorf("version not preserved: got %d want 42", updated.Version)
			}
			if updated.ID != original.ID {
				t.Errorf("id drifted: got %s want %s", updated.ID, original.ID)
			}
		})
	}
}

func TestPortabilityService_TruncateWipesAndRehydrates(t *testing.T) {
	env := newPortTestEnv(t)
	ctx := WithActor(context.Background(), "test-player")
	env.seedRichWorkspace(ctx)

	// Build a fresh dump with one project + one task only.
	now := time.Now().UTC().Truncate(time.Millisecond)
	wfDump, err := env.port.Export(ctx)
	if err != nil {
		t.Fatalf("Export to capture workflows: %v", err)
	}

	one := &portability.PortableWorkspace{
		SchemaVersion: portability.SchemaVersion,
		TuskVersion:   "test",
		ExportedAt:    now,
		Workflows:     wfDump.Workflows,
		Projects:      wfDump.Projects[:1],
		Players:       []portability.PortablePlayer{},
		Tags:          []portability.PortableTag{},
		Tasks: []portability.PortableTask{{
			ID:         uuid.New(),
			ShortID:    "a1b2c3d4",
			ProjectID:  domain.DefaultProjectUUID,
			Title:      "lone survivor",
			Status:     "pending",
			Version:    1,
			UDA:        map[string]string{},
			Tags:       []string{},
			CreatedAt:  now,
			ModifiedAt: now,
		}},
		Relations:   []portability.PortableRelation{},
		Annotations: []portability.PortableAnnotation{},
		Notes:       []portability.PortableNote{},
		Events:      []portability.PortableEvent{},
	}

	report, err := env.port.Import(ctx, one, ImportOptions{Replace: true, Truncate: true})
	if err != nil {
		t.Fatalf("Import (truncate): %v", err)
	}
	if !report.Truncated {
		t.Error("expected report.Truncated == true")
	}

	tasks, err := env.bundle.Tasks.List(ctx, &domain.TermFilter{})
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected exactly one task post-truncate, got %d", len(tasks))
	}
	if tasks[0].Title != "lone survivor" {
		t.Errorf("post-truncate task title: got %q", tasks[0].Title)
	}
	projects, err := env.projectSvc.List(ctx)
	if err != nil {
		t.Fatalf("listing projects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected exactly one project post-truncate, got %d", len(projects))
	}
}

func TestPortabilityService_ValidationErrorsBatchCorrectly(t *testing.T) {
	env := newPortTestEnv(t)
	ctx := WithActor(context.Background(), "test-player")

	// Project with a taxonomy so we can raise a level-violation issue.
	taxStrict := domain.Taxonomy{{"epic"}, {"task"}}
	def, err := env.projectRepo.GetByID(ctx, domain.DefaultProjectUUID)
	if err != nil {
		t.Fatalf("loading default project: %v", err)
	}
	tax := taxStrict.Clone()
	def.Settings.Taxonomy = &tax
	if err := env.projectRepo.Update(ctx, def); err != nil {
		t.Fatalf("attaching taxonomy: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	missingParent := uuid.New()
	taskA := uuid.New()
	taskB := uuid.New()
	taskC := uuid.New()

	level := "task"
	dump := &portability.PortableWorkspace{
		SchemaVersion: portability.SchemaVersion,
		TuskVersion:   "test",
		ExportedAt:    now,
		Workflows:     []portability.PortableWorkflow{},
		Projects:      []portability.PortableProject{},
		Players:       []portability.PortablePlayer{},
		Tags:          []portability.PortableTag{},
		Tasks: []portability.PortableTask{
			{ID: taskA, ShortID: "aaaaaaaa", ProjectID: domain.DefaultProjectUUID, Title: "missing parent", ParentID: &missingParent, Status: "pending", Version: 1, UDA: map[string]string{}, Tags: []string{}, CreatedAt: now, ModifiedAt: now},
			{ID: taskB, ShortID: "bbbbbbbb", ProjectID: domain.DefaultProjectUUID, Title: "wrong level root", Level: &level, Status: "pending", Version: 1, UDA: map[string]string{}, Tags: []string{}, CreatedAt: now, ModifiedAt: now},
			{ID: taskC, ShortID: "cccccccc", ProjectID: domain.DefaultProjectUUID, Title: "cycle node", Status: "pending", Version: 1, UDA: map[string]string{}, Tags: []string{}, CreatedAt: now, ModifiedAt: now},
		},
		Relations: []portability.PortableRelation{
			{ID: uuid.New(), SourceID: taskB, TargetID: taskC, RelationType: "blocks", CreatedAt: now},
			{ID: uuid.New(), SourceID: taskC, TargetID: taskB, RelationType: "blocks", CreatedAt: now},
		},
		Annotations: []portability.PortableAnnotation{},
		Notes:       []portability.PortableNote{},
		Events:      []portability.PortableEvent{},
	}

	_, err = env.port.Import(ctx, dump, ImportOptions{})
	if err == nil {
		t.Fatal("expected ImportError, got nil")
	}
	var importErr *portability.ImportError
	if !errors.As(err, &importErr) {
		t.Fatalf("expected *portability.ImportError, got %T: %v", err, err)
	}

	kinds := map[string]int{}
	for _, iss := range importErr.Issues {
		kinds[iss.Kind]++
	}
	if kinds["fk"] == 0 {
		t.Errorf("expected at least one fk issue, got %v", kinds)
	}
	if kinds["taxonomy"] == 0 {
		t.Errorf("expected at least one taxonomy issue, got %v", kinds)
	}
	if kinds["cycle"] == 0 {
		t.Errorf("expected at least one cycle issue, got %v", kinds)
	}
}

func TestPortabilityService_DryRunDoesNotMutate(t *testing.T) {
	env := newPortTestEnv(t)
	ctx := WithActor(context.Background(), "test-player")
	env.seedRichWorkspace(ctx)

	dump, err := env.port.Export(ctx)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	preTasks, err := env.bundle.Tasks.List(ctx, &domain.TermFilter{})
	if err != nil {
		t.Fatalf("pre-count: %v", err)
	}
	preEvents, err := env.eventRepo.Count(ctx)
	if err != nil {
		t.Fatalf("pre-event count: %v", err)
	}

	report, err := env.port.Import(ctx, dump, ImportOptions{DryRun: true, Replace: true})
	if err != nil {
		t.Fatalf("dry-run import: %v", err)
	}
	if report.Tasks != len(dump.Tasks) {
		t.Errorf("report.Tasks: got %d want %d", report.Tasks, len(dump.Tasks))
	}
	if report.EventID != uuid.Nil {
		t.Errorf("dry-run should not record an event, got %s", report.EventID)
	}

	postTasks, err := env.bundle.Tasks.List(ctx, &domain.TermFilter{})
	if err != nil {
		t.Fatalf("post-count: %v", err)
	}
	if len(postTasks) != len(preTasks) {
		t.Errorf("dry-run mutated tasks: pre=%d post=%d", len(preTasks), len(postTasks))
	}
	postEvents, err := env.eventRepo.Count(ctx)
	if err != nil {
		t.Fatalf("post-event count: %v", err)
	}
	if postEvents != preEvents {
		t.Errorf("dry-run mutated events: pre=%d post=%d", preEvents, postEvents)
	}
}

func TestPortabilityService_WorkspaceImportedEventLandsOnce(t *testing.T) {
	envA := newPortTestEnv(t)
	ctx := WithActor(context.Background(), "test-player")
	envA.seedRichWorkspace(ctx)

	dump, err := envA.port.Export(ctx)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	envB := newPortTestEnv(t)
	report, err := envB.port.Import(ctx, dump, ImportOptions{Replace: true, Truncate: true})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	wantType := domain.EventWorkspaceImported
	events, err := envB.eventRepo.List(ctx, repository.EventFilter{Type: &wantType})
	if err != nil {
		t.Fatalf("listing import events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly one workspace_imported event, got %d", len(events))
	}
	evt := events[0]
	if evt.ID != report.EventID {
		t.Errorf("event id: got %s want %s", evt.ID, report.EventID)
	}
	if evt.PlayerID == nil || *evt.PlayerID != "test-player" {
		t.Errorf("event player id: got %v want test-player", evt.PlayerID)
	}
	payload, ok := evt.Payload.(domain.WorkspaceImportedPayload)
	if !ok {
		t.Fatalf("expected WorkspaceImportedPayload, got %T", evt.Payload)
	}
	if payload.Counts["tasks"] != report.Tasks {
		t.Errorf("counts.tasks: got %d want %d", payload.Counts["tasks"], report.Tasks)
	}
}

func TestPortabilityService_TruncateWithoutReplaceRejected(t *testing.T) {
	env := newPortTestEnv(t)
	ctx := WithActor(context.Background(), "test-player")

	now := time.Now().UTC().Truncate(time.Millisecond)
	dump := &portability.PortableWorkspace{
		SchemaVersion: portability.SchemaVersion,
		TuskVersion:   "test",
		ExportedAt:    now,
	}

	_, err := env.port.Import(ctx, dump, ImportOptions{Truncate: true})
	if err == nil {
		t.Fatal("expected ImportError, got nil")
	}
	var importErr *portability.ImportError
	if !errors.As(err, &importErr) {
		t.Fatalf("expected *portability.ImportError, got %T: %v", err, err)
	}
	if len(importErr.Issues) != 1 {
		t.Fatalf("expected one issue, got %d", len(importErr.Issues))
	}
	if importErr.Issues[0].Kind != "schema" {
		t.Errorf("issue kind: got %q want schema", importErr.Issues[0].Kind)
	}
}
