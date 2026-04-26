package service

import (
	"context"
	"errors"
	"reflect"
	"sort"
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

	dumpB, err := envB.port.Export(ctx)
	if err != nil {
		t.Fatalf("Export B: %v", err)
	}
	assertWorkspaceDeepEqualModuloImport(t, dump, dumpB)
}

// assertWorkspaceDeepEqualModuloImport reports a test failure when any
// per-entity slice fails reflect.DeepEqual after sorting by ID. It
// excludes (a) the workspace-level ExportedAt and TuskVersion header
// fields, which are scoped to the receiving workspace, and (b) the
// EventWorkspaceImported event added by the import. Every other field —
// IDs, timestamps, version on every task — must round-trip exactly.
func assertWorkspaceDeepEqualModuloImport(t *testing.T, want, got *portability.PortableWorkspace) {
	t.Helper()

	wfWant := append([]portability.PortableWorkflow(nil), want.Workflows...)
	wfGot := append([]portability.PortableWorkflow(nil), got.Workflows...)
	sort.Slice(wfWant, func(i, j int) bool { return wfWant[i].ID.String() < wfWant[j].ID.String() })
	sort.Slice(wfGot, func(i, j int) bool { return wfGot[i].ID.String() < wfGot[j].ID.String() })
	if !reflect.DeepEqual(wfWant, wfGot) {
		t.Errorf("workflows mismatch:\n want=%#v\n got=%#v", wfWant, wfGot)
	}

	prWant := append([]portability.PortableProject(nil), want.Projects...)
	prGot := append([]portability.PortableProject(nil), got.Projects...)
	sort.Slice(prWant, func(i, j int) bool { return prWant[i].ID.String() < prWant[j].ID.String() })
	sort.Slice(prGot, func(i, j int) bool { return prGot[i].ID.String() < prGot[j].ID.String() })
	if !reflect.DeepEqual(prWant, prGot) {
		t.Errorf("projects mismatch:\n want=%#v\n got=%#v", prWant, prGot)
	}

	plWant := append([]portability.PortablePlayer(nil), want.Players...)
	plGot := append([]portability.PortablePlayer(nil), got.Players...)
	sort.Slice(plWant, func(i, j int) bool { return plWant[i].ID < plWant[j].ID })
	sort.Slice(plGot, func(i, j int) bool { return plGot[i].ID < plGot[j].ID })
	if !reflect.DeepEqual(plWant, plGot) {
		t.Errorf("players mismatch:\n want=%#v\n got=%#v", plWant, plGot)
	}

	tgWant := append([]portability.PortableTag(nil), want.Tags...)
	tgGot := append([]portability.PortableTag(nil), got.Tags...)
	sort.Slice(tgWant, func(i, j int) bool { return tgWant[i].ID.String() < tgWant[j].ID.String() })
	sort.Slice(tgGot, func(i, j int) bool { return tgGot[i].ID.String() < tgGot[j].ID.String() })
	if !reflect.DeepEqual(tgWant, tgGot) {
		t.Errorf("tags mismatch:\n want=%#v\n got=%#v", tgWant, tgGot)
	}

	tkWant := append([]portability.PortableTask(nil), want.Tasks...)
	tkGot := append([]portability.PortableTask(nil), got.Tasks...)
	sort.Slice(tkWant, func(i, j int) bool { return tkWant[i].ID.String() < tkWant[j].ID.String() })
	sort.Slice(tkGot, func(i, j int) bool { return tkGot[i].ID.String() < tkGot[j].ID.String() })
	for i := range tkWant {
		sort.Strings(tkWant[i].Tags)
	}
	for i := range tkGot {
		sort.Strings(tkGot[i].Tags)
	}
	if !reflect.DeepEqual(tkWant, tkGot) {
		t.Errorf("tasks mismatch:\n want=%#v\n got=%#v", tkWant, tkGot)
	}

	rlWant := append([]portability.PortableRelation(nil), want.Relations...)
	rlGot := append([]portability.PortableRelation(nil), got.Relations...)
	sort.Slice(rlWant, func(i, j int) bool { return rlWant[i].ID.String() < rlWant[j].ID.String() })
	sort.Slice(rlGot, func(i, j int) bool { return rlGot[i].ID.String() < rlGot[j].ID.String() })
	if !reflect.DeepEqual(rlWant, rlGot) {
		t.Errorf("relations mismatch:\n want=%#v\n got=%#v", rlWant, rlGot)
	}

	anWant := append([]portability.PortableAnnotation(nil), want.Annotations...)
	anGot := append([]portability.PortableAnnotation(nil), got.Annotations...)
	sort.Slice(anWant, func(i, j int) bool { return anWant[i].ID.String() < anWant[j].ID.String() })
	sort.Slice(anGot, func(i, j int) bool { return anGot[i].ID.String() < anGot[j].ID.String() })
	if !reflect.DeepEqual(anWant, anGot) {
		t.Errorf("annotations mismatch:\n want=%#v\n got=%#v", anWant, anGot)
	}

	ntWant := append([]portability.PortableNote(nil), want.Notes...)
	ntGot := append([]portability.PortableNote(nil), got.Notes...)
	sort.Slice(ntWant, func(i, j int) bool { return ntWant[i].ID.String() < ntWant[j].ID.String() })
	sort.Slice(ntGot, func(i, j int) bool { return ntGot[i].ID.String() < ntGot[j].ID.String() })
	if !reflect.DeepEqual(ntWant, ntGot) {
		t.Errorf("notes mismatch:\n want=%#v\n got=%#v", ntWant, ntGot)
	}

	evWant := filterOutImportEvent(want.Events)
	evGot := filterOutImportEvent(got.Events)
	sort.Slice(evWant, func(i, j int) bool {
		return evWant[i].CreatedAt.Before(evWant[j].CreatedAt) || (evWant[i].CreatedAt.Equal(evWant[j].CreatedAt) && evWant[i].ID.String() < evWant[j].ID.String())
	})
	sort.Slice(evGot, func(i, j int) bool {
		return evGot[i].CreatedAt.Before(evGot[j].CreatedAt) || (evGot[i].CreatedAt.Equal(evGot[j].CreatedAt) && evGot[i].ID.String() < evGot[j].ID.String())
	})
	if len(evWant) != len(evGot) {
		t.Errorf("event count drift (excluding workspace_imported): want %d got %d", len(evWant), len(evGot))
	} else {
		for i := range evWant {
			if evWant[i].ID != evGot[i].ID || evWant[i].Type != evGot[i].Type || !evWant[i].CreatedAt.Equal(evGot[i].CreatedAt) {
				t.Errorf("event %d drift: want=%v got=%v", i, evWant[i], evGot[i])
			}
		}
	}
}

func filterOutImportEvent(events []portability.PortableEvent) []portability.PortableEvent {
	out := make([]portability.PortableEvent, 0, len(events))
	for _, e := range events {
		if e.Type == string(domain.EventWorkspaceImported) {
			continue
		}
		out = append(out, e)
	}
	return out
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

// TestPortabilityService_ReplaceWithoutTruncatePreservesWorkflow asserts
// the documented limitation: under --replace without --truncate, the
// projects.workflow_id ON DELETE RESTRICT FK prevents a delete-then-
// create of any referenced workflow. The apply pass deliberately skips
// the workflow row in that case, so a dump that names the same kanban
// workflow with a tweaked transition list leaves the live workflow
// untouched. Faithful workflow replacement requires --truncate.
func TestPortabilityService_ReplaceWithoutTruncatePreservesWorkflow(t *testing.T) {
	env := newPortTestEnv(t)
	ctx := WithActor(context.Background(), "test-player")

	// Capture the current kanban workflow as the baseline for comparison.
	originalWf, err := env.wfSvc.GetByID(ctx, domain.KanbanWorkflowUUID)
	if err != nil {
		t.Fatalf("loading kanban workflow: %v", err)
	}

	dump, err := env.port.Export(ctx)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	// Mutate the dump's workflow so a successful replace would visibly
	// change the workspace.
	for i := range dump.Workflows {
		if dump.Workflows[i].ID == domain.KanbanWorkflowUUID {
			dump.Workflows[i].Transitions = append(
				dump.Workflows[i].Transitions,
				portability.PortableWorkflowTransition{FromStatus: "deleted", ToStatus: "pending"},
			)
		}
	}

	if _, err := env.port.Import(ctx, dump, ImportOptions{Replace: true}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	current, err := env.wfSvc.GetByID(ctx, domain.KanbanWorkflowUUID)
	if err != nil {
		t.Fatalf("re-loading kanban workflow: %v", err)
	}
	if len(current.Transitions) != len(originalWf.Transitions) {
		t.Errorf("workflow transitions changed without --truncate: was %d, now %d",
			len(originalWf.Transitions), len(current.Transitions))
	}
	if current.Version != originalWf.Version {
		t.Errorf("workflow version drifted without --truncate: was %d, now %d",
			originalWf.Version, current.Version)
	}
}

// TestPortabilityService_TruncateReplacesWorkflowFaithfully complements
// the preserve-if-exists test above: the same dump with --truncate set
// rewrites the workflow with the dump's payload because TruncateAll
// wipes the workflows table before applyWorkflows runs.
func TestPortabilityService_TruncateReplacesWorkflowFaithfully(t *testing.T) {
	env := newPortTestEnv(t)
	ctx := WithActor(context.Background(), "test-player")

	dump, err := env.port.Export(ctx)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	want := append([]portability.PortableWorkflowTransition(nil),
		portability.PortableWorkflowTransition{FromStatus: "deleted", ToStatus: "pending"})
	for i := range dump.Workflows {
		if dump.Workflows[i].ID == domain.KanbanWorkflowUUID {
			dump.Workflows[i].Transitions = append(dump.Workflows[i].Transitions, want...)
		}
	}

	if _, err := env.port.Import(ctx, dump, ImportOptions{Replace: true, Truncate: true}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	current, err := env.wfSvc.GetByID(ctx, domain.KanbanWorkflowUUID)
	if err != nil {
		t.Fatalf("loading workflow post-truncate: %v", err)
	}
	found := false
	for _, tr := range current.Transitions {
		if tr.FromStatus == "deleted" && tr.ToStatus == "pending" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected dump's deleted→pending transition to land under --truncate, got %v", current.Transitions)
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

func TestPortability_RoundTrip_ProjectDescription(t *testing.T) {
	envA := newPortTestEnv(t)
	ctx := WithActor(context.Background(), "test-player")

	const desc = "alpha project description\nwith two lines"
	wf, err := envA.wfSvc.GetByName(ctx, "kanban")
	if err != nil {
		t.Fatalf("resolving workflow: %v", err)
	}
	created, err := envA.projectSvc.Create(ctx, CreateProjectInput{
		Name:        "alpha",
		WorkflowID:  wf.ID,
		Description: desc,
	})
	if err != nil {
		t.Fatalf("seeding alpha: %v", err)
	}
	if created.Description != desc {
		t.Fatalf("seed Description = %q, want %q", created.Description, desc)
	}

	dump, err := envA.port.Export(ctx)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	var found bool
	for _, p := range dump.Projects {
		if p.Name == "alpha" {
			found = true
			if p.Description != desc {
				t.Fatalf("dump alpha Description = %q, want %q", p.Description, desc)
			}
		}
	}
	if !found {
		t.Fatalf("alpha project missing from dump: %+v", dump.Projects)
	}

	envB := newPortTestEnv(t)
	if _, err := envB.port.Import(ctx, dump, ImportOptions{Replace: true, Truncate: true}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	got, err := envB.projectSvc.GetByName(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetByName after import: %v", err)
	}
	if got.Description != desc {
		t.Fatalf("imported Description = %q, want %q", got.Description, desc)
	}
}
