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
	test       *testing.T
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

func newPortTestEnv(test *testing.T) *portTestEnv {
	test.Helper()
	bundle, projectRepo, wfRepo := newSeededBundle(test)
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
		test:        test,
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

func (env *portTestEnv) seedRichWorkspace(ctx context.Context) *seededWorkspace {
	env.test.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)

	playerID := "test-player"
	if _, err := env.playerSvc.Register(ctx, playerID, "human"); err != nil {
		env.test.Fatalf("seeding player: %v", err)
	}

	tagID := uuid.New()
	tag := &domain.Tag{ID: tagID, Name: "feature"}
	if err := env.bundle.Tags.Create(ctx, tag); err != nil {
		env.test.Fatalf("seeding tag: %v", err)
	}

	parent := &domain.Task{Title: "parent task"}
	if err := env.taskSvc.Create(ctx, parent); err != nil {
		env.test.Fatalf("seeding parent task: %v", err)
	}
	child := &domain.Task{Title: "child task", ParentID: &parent.ID}
	if err := env.taskSvc.Create(ctx, child); err != nil {
		env.test.Fatalf("seeding child task: %v", err)
	}

	if err := env.bundle.Tags.AssignToTask(ctx, child.ID, tagID); err != nil {
		env.test.Fatalf("seeding tag assignment: %v", err)
	}

	rel, err := env.relSvc.Add(ctx, parent.ShortID, child.ShortID, "blocks")

	if err != nil {
		env.test.Fatalf("seeding relation: %v", err)
	}

	annID := uuid.New()
	annotation := &domain.Annotation{
		ID: annID, TaskID: parent.ID, Body: "first annotation", CreatedAt: now,
	}
	if err := env.bundle.Annotations.Create(ctx, annotation); err != nil {
		env.test.Fatalf("seeding annotation: %v", err)
	}

	taskNote := &domain.Note{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  playerID,
		TaskID:    &parent.ID,
		Body:      "note attached to task",
	}
	if err := env.noteSvc.Create(ctx, taskNote); err != nil {
		env.test.Fatalf("seeding task note: %v", err)
	}
	projNote := &domain.Note{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  playerID,
		Body:      "project-level note",
	}
	if err := env.noteSvc.Create(ctx, projNote); err != nil {
		env.test.Fatalf("seeding project note: %v", err)
	}

	return &seededWorkspace{
		parentTask: parent,
		childTask:  child,
		relation:   rel,
		tag:        tag,
		annotation: annotation,
		playerID:   playerID,
		projectID:  domain.DefaultProjectUUID,
		taskNote:   taskNote,
		projNote:   projNote,
	}
}

func TestPortabilityService_RoundTripIntoEmptyWorkspace(test *testing.T) {
	envA := newPortTestEnv(test)
	ctx := WithActor(context.Background(), "test-player")
	envA.seedRichWorkspace(ctx)

	exportErr := error(nil)
	dump, exportErr := envA.port.Export(ctx)

	if exportErr != nil {
		test.Fatalf("Export A: %v", exportErr)
	}

	if dump.SchemaVersion != portability.SchemaVersion {
		test.Fatalf("schema version: got %d want %d", dump.SchemaVersion, portability.SchemaVersion)
	}
	if len(dump.Tasks) != 2 {
		test.Fatalf("dump tasks: got %d want 2", len(dump.Tasks))
	}

	envB := newPortTestEnv(test)
	report, importErr := envB.port.Import(ctx, dump, ImportOptions{Replace: true, Truncate: true})

	if importErr != nil {
		test.Fatalf("Import B: %v", importErr)
	}

	if report.Tasks != len(dump.Tasks) {
		test.Fatalf("report.Tasks: got %d want %d", report.Tasks, len(dump.Tasks))
	}

	dumpB, exportBErr := envB.port.Export(ctx)

	if exportBErr != nil {
		test.Fatalf("Export B: %v", exportBErr)
	}

	assertWorkspaceDeepEqualModuloImport(test, dump, dumpB)
}

// assertWorkspaceDeepEqualModuloImport reports a test failure when any
// per-entity slice fails reflect.DeepEqual after sorting by ID. It
// excludes (a) the workspace-level ExportedAt and TuskVersion header
// fields, which are scoped to the receiving workspace, and (b) the
// EventWorkspaceImported event added by the import. Every other field —
// IDs, timestamps, version on every task — must round-trip exactly.
func assertWorkspaceDeepEqualModuloImport(test *testing.T, want, got *portability.PortableWorkspace) {
	test.Helper()

	wfWant := append([]portability.PortableWorkflow(nil), want.Workflows...)
	wfGot := append([]portability.PortableWorkflow(nil), got.Workflows...)
	sort.Slice(wfWant, func(i, j int) bool { return wfWant[i].ID.String() < wfWant[j].ID.String() })
	sort.Slice(wfGot, func(i, j int) bool { return wfGot[i].ID.String() < wfGot[j].ID.String() })
	if !reflect.DeepEqual(wfWant, wfGot) {
		test.Errorf("workflows mismatch:\n want=%#v\n got=%#v", wfWant, wfGot)
	}

	prWant := append([]portability.PortableProject(nil), want.Projects...)
	prGot := append([]portability.PortableProject(nil), got.Projects...)
	sort.Slice(prWant, func(i, j int) bool { return prWant[i].ID.String() < prWant[j].ID.String() })
	sort.Slice(prGot, func(i, j int) bool { return prGot[i].ID.String() < prGot[j].ID.String() })
	if !reflect.DeepEqual(prWant, prGot) {
		test.Errorf("projects mismatch:\n want=%#v\n got=%#v", prWant, prGot)
	}

	plWant := append([]portability.PortablePlayer(nil), want.Players...)
	plGot := append([]portability.PortablePlayer(nil), got.Players...)
	sort.Slice(plWant, func(i, j int) bool { return plWant[i].ID < plWant[j].ID })
	sort.Slice(plGot, func(i, j int) bool { return plGot[i].ID < plGot[j].ID })
	if !reflect.DeepEqual(plWant, plGot) {
		test.Errorf("players mismatch:\n want=%#v\n got=%#v", plWant, plGot)
	}

	tgWant := append([]portability.PortableTag(nil), want.Tags...)
	tgGot := append([]portability.PortableTag(nil), got.Tags...)
	sort.Slice(tgWant, func(i, j int) bool { return tgWant[i].ID.String() < tgWant[j].ID.String() })
	sort.Slice(tgGot, func(i, j int) bool { return tgGot[i].ID.String() < tgGot[j].ID.String() })
	if !reflect.DeepEqual(tgWant, tgGot) {
		test.Errorf("tags mismatch:\n want=%#v\n got=%#v", tgWant, tgGot)
	}

	tkWant := append([]portability.PortableTask(nil), want.Tasks...)
	tkGot := append([]portability.PortableTask(nil), got.Tasks...)
	sort.Slice(tkWant, func(i, j int) bool { return tkWant[i].ID.String() < tkWant[j].ID.String() })
	sort.Slice(tkGot, func(i, j int) bool { return tkGot[i].ID.String() < tkGot[j].ID.String() })
	for index := range tkWant {
		sort.Strings(tkWant[index].Tags)
	}
	for index := range tkGot {
		sort.Strings(tkGot[index].Tags)
	}
	if !reflect.DeepEqual(tkWant, tkGot) {
		test.Errorf("tasks mismatch:\n want=%#v\n got=%#v", tkWant, tkGot)
	}

	rlWant := append([]portability.PortableRelation(nil), want.Relations...)
	rlGot := append([]portability.PortableRelation(nil), got.Relations...)
	sort.Slice(rlWant, func(i, j int) bool { return rlWant[i].ID.String() < rlWant[j].ID.String() })
	sort.Slice(rlGot, func(i, j int) bool { return rlGot[i].ID.String() < rlGot[j].ID.String() })
	if !reflect.DeepEqual(rlWant, rlGot) {
		test.Errorf("relations mismatch:\n want=%#v\n got=%#v", rlWant, rlGot)
	}

	anWant := append([]portability.PortableAnnotation(nil), want.Annotations...)
	anGot := append([]portability.PortableAnnotation(nil), got.Annotations...)
	sort.Slice(anWant, func(i, j int) bool { return anWant[i].ID.String() < anWant[j].ID.String() })
	sort.Slice(anGot, func(i, j int) bool { return anGot[i].ID.String() < anGot[j].ID.String() })
	if !reflect.DeepEqual(anWant, anGot) {
		test.Errorf("annotations mismatch:\n want=%#v\n got=%#v", anWant, anGot)
	}

	ntWant := append([]portability.PortableNote(nil), want.Notes...)
	ntGot := append([]portability.PortableNote(nil), got.Notes...)
	sort.Slice(ntWant, func(i, j int) bool { return ntWant[i].ID.String() < ntWant[j].ID.String() })
	sort.Slice(ntGot, func(i, j int) bool { return ntGot[i].ID.String() < ntGot[j].ID.String() })
	if !reflect.DeepEqual(ntWant, ntGot) {
		test.Errorf("notes mismatch:\n want=%#v\n got=%#v", ntWant, ntGot)
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
		test.Errorf("event count drift (excluding workspace_imported): want %d got %d", len(evWant), len(evGot))
	} else {
		for index := range evWant {
			if evWant[index].ID != evGot[index].ID || evWant[index].Type != evGot[index].Type || !evWant[index].CreatedAt.Equal(evGot[index].CreatedAt) {
				test.Errorf("event %d drift: want=%v got=%v", index, evWant[index], evGot[index])
			}
		}
	}
}

func filterOutImportEvent(events []portability.PortableEvent) []portability.PortableEvent {
	out := make([]portability.PortableEvent, 0, len(events))
	for _, event := range events {
		if event.Type == string(domain.EventWorkspaceImported) {
			continue
		}
		out = append(out, event)
	}
	return out
}

func TestPortabilityService_StrictModeCollisionLeavesWorkspaceUnchanged(test *testing.T) {
	env := newPortTestEnv(test)
	ctx := WithActor(context.Background(), "test-player")
	seed := env.seedRichWorkspace(ctx)

	dump, exportErr := env.port.Export(ctx)

	if exportErr != nil {
		test.Fatalf("Export: %v", exportErr)
	}

	for index := range dump.Tasks {
		if dump.Tasks[index].ID == seed.parentTask.ID {
			dump.Tasks[index].Title = "rewritten title"
		}
	}

	report, importErr := env.port.Import(ctx, dump, ImportOptions{})

	if importErr == nil {
		test.Fatal("expected ImportError in strict mode, got nil")
	}

	var importErrTyped *portability.ImportError
	if !errors.As(importErr, &importErrTyped) {
		test.Fatalf("expected *portability.ImportError, got %T: %v", importErr, importErr)
	}
	if len(importErrTyped.Issues) == 0 {
		test.Fatal("expected at least one issue in ImportError")
	}
	sawCollision := false
	for _, iss := range importErrTyped.Issues {
		if iss.Kind == "collision" {
			sawCollision = true
		}
	}
	if !sawCollision {
		test.Fatalf("expected at least one collision issue, got %#v", importErrTyped.Issues)
	}

	live, liveErr := env.bundle.Tasks.GetByID(ctx, seed.parentTask.ID)

	if liveErr != nil {
		test.Fatalf("loading live parent task: %v", liveErr)
	}

	if live.Title != seed.parentTask.Title {
		test.Errorf("strict-mode collision wrote workspace: title became %q", live.Title)
	}

	if report == nil {
		test.Fatal("expected non-nil report even on validation failure")
	}
	if report.Tasks != len(dump.Tasks) {
		test.Errorf("report.Tasks should reflect dump size on validation error: got %d want %d", report.Tasks, len(dump.Tasks))
	}
}

func TestPortabilityService_ReplaceUpdatesInPlaceFaithful(test *testing.T) {
	for _, parented := range []bool{false, true} {
		name := "root"
		if parented {
			name = "parented"
		}
		test.Run(name, func(test *testing.T) {
			env := newPortTestEnv(test)
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
				taskErr := env.taskSvc.Create(ctx, task)

				if taskErr != nil {
					test.Fatalf("seed root task: %v", taskErr)
				}

				targetID = task.ID
				original = task
			}

			dump, exportErr := env.port.Export(ctx)

			if exportErr != nil {
				test.Fatalf("Export: %v", exportErr)
			}

			for index := range dump.Tasks {
				if dump.Tasks[index].ID == targetID {
					dump.Tasks[index].Title = "new"
					dump.Tasks[index].Version = 42
				}
			}

			report, importErr := env.port.Import(ctx, dump, ImportOptions{Replace: true})

			if importErr != nil {
				test.Fatalf("Import (replace): %v", importErr)
			}

			if report.Replaced == 0 {
				test.Errorf("expected at least one replacement, got %d", report.Replaced)
			}

			updated, updatedErr := env.bundle.Tasks.GetByID(ctx, targetID)

			if updatedErr != nil {
				test.Fatalf("loading replaced task: %v", updatedErr)
			}

			if updated.Title != "new" {
				test.Errorf("title not replaced: got %q want %q", updated.Title, "new")
			}
			if updated.Version != 42 {
				test.Errorf("version not preserved: got %d want 42", updated.Version)
			}
			if updated.ID != original.ID {
				test.Errorf("id drifted: got %s want %s", updated.ID, original.ID)
			}
		})
	}
}

func TestPortabilityService_TruncateWipesAndRehydrates(test *testing.T) {
	env := newPortTestEnv(test)
	ctx := WithActor(context.Background(), "test-player")
	env.seedRichWorkspace(ctx)

	// Build a fresh dump with one project + one task only.
	now := time.Now().UTC().Truncate(time.Millisecond)
	wfDump, wfDumpErr := env.port.Export(ctx)

	if wfDumpErr != nil {
		test.Fatalf("Export to capture workflows: %v", wfDumpErr)
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

	report, importErr := env.port.Import(ctx, one, ImportOptions{Replace: true, Truncate: true})

	if importErr != nil {
		test.Fatalf("Import (truncate): %v", importErr)
	}

	if !report.Truncated {
		test.Error("expected report.Truncated == true")
	}

	tasks, tasksErr := env.bundle.Tasks.List(ctx, &domain.TermFilter{})

	if tasksErr != nil {
		test.Fatalf("listing tasks: %v", tasksErr)
	}

	if len(tasks) != 1 {
		test.Fatalf("expected exactly one task post-truncate, got %d", len(tasks))
	}
	if tasks[0].Title != "lone survivor" {
		test.Errorf("post-truncate task title: got %q", tasks[0].Title)
	}
	projects, projectsErr := env.projectSvc.List(ctx)

	if projectsErr != nil {
		test.Fatalf("listing projects: %v", projectsErr)
	}

	if len(projects) != 1 {
		test.Fatalf("expected exactly one project post-truncate, got %d", len(projects))
	}
}

func TestPortabilityService_ValidationErrorsBatchCorrectly(test *testing.T) {
	env := newPortTestEnv(test)
	ctx := WithActor(context.Background(), "test-player")

	// Project with a taxonomy so we can raise a level-violation issue.
	taxStrict := domain.Taxonomy{{"epic"}, {"task"}}
	def, defErr := env.projectRepo.GetByID(ctx, domain.DefaultProjectUUID)

	if defErr != nil {
		test.Fatalf("loading default project: %v", defErr)
	}

	tax := taxStrict.Clone()
	def.Settings.Taxonomy = &tax
	if updateErr := env.projectRepo.Update(ctx, def); updateErr != nil {
		test.Fatalf("attaching taxonomy: %v", updateErr)
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

	_, importErr := env.port.Import(ctx, dump, ImportOptions{})

	if importErr == nil {
		test.Fatal("expected ImportError, got nil")
	}

	var importErrTyped *portability.ImportError
	if !errors.As(importErr, &importErrTyped) {
		test.Fatalf("expected *portability.ImportError, got %T: %v", importErr, importErr)
	}

	kinds := map[string]int{}
	for _, iss := range importErrTyped.Issues {
		kinds[iss.Kind]++
	}
	if kinds["fk"] == 0 {
		test.Errorf("expected at least one fk issue, got %v", kinds)
	}
	if kinds["taxonomy"] == 0 {
		test.Errorf("expected at least one taxonomy issue, got %v", kinds)
	}
	if kinds["cycle"] == 0 {
		test.Errorf("expected at least one cycle issue, got %v", kinds)
	}
}

func TestPortabilityService_DryRunDoesNotMutate(test *testing.T) {
	env := newPortTestEnv(test)
	ctx := WithActor(context.Background(), "test-player")
	env.seedRichWorkspace(ctx)

	dump, exportErr := env.port.Export(ctx)

	if exportErr != nil {
		test.Fatalf("Export: %v", exportErr)
	}

	preTasks, preTasksErr := env.bundle.Tasks.List(ctx, &domain.TermFilter{})

	if preTasksErr != nil {
		test.Fatalf("pre-count: %v", preTasksErr)
	}

	preEvents, preEventsErr := env.eventRepo.Count(ctx)

	if preEventsErr != nil {
		test.Fatalf("pre-event count: %v", preEventsErr)
	}

	report, importErr := env.port.Import(ctx, dump, ImportOptions{DryRun: true, Replace: true})

	if importErr != nil {
		test.Fatalf("dry-run import: %v", importErr)
	}

	if report.Tasks != len(dump.Tasks) {
		test.Errorf("report.Tasks: got %d want %d", report.Tasks, len(dump.Tasks))
	}
	if report.EventID != uuid.Nil {
		test.Errorf("dry-run should not record an event, got %s", report.EventID)
	}

	postTasks, postTasksErr := env.bundle.Tasks.List(ctx, &domain.TermFilter{})

	if postTasksErr != nil {
		test.Fatalf("post-count: %v", postTasksErr)
	}

	if len(postTasks) != len(preTasks) {
		test.Errorf("dry-run mutated tasks: pre=%d post=%d", len(preTasks), len(postTasks))
	}
	postEvents, postEventsErr := env.eventRepo.Count(ctx)

	if postEventsErr != nil {
		test.Fatalf("post-event count: %v", postEventsErr)
	}

	if postEvents != preEvents {
		test.Errorf("dry-run mutated events: pre=%d post=%d", preEvents, postEvents)
	}
}

func TestPortabilityService_WorkspaceImportedEventLandsOnce(test *testing.T) {
	envA := newPortTestEnv(test)
	ctx := WithActor(context.Background(), "test-player")
	envA.seedRichWorkspace(ctx)

	dump, exportErr := envA.port.Export(ctx)

	if exportErr != nil {
		test.Fatalf("Export: %v", exportErr)
	}

	envB := newPortTestEnv(test)
	report, importErr := envB.port.Import(ctx, dump, ImportOptions{Replace: true, Truncate: true})

	if importErr != nil {
		test.Fatalf("Import: %v", importErr)
	}

	wantType := domain.EventWorkspaceImported
	events, eventsErr := envB.eventRepo.List(ctx, repository.EventFilter{Type: &wantType})

	if eventsErr != nil {
		test.Fatalf("listing import events: %v", eventsErr)
	}

	if len(events) != 1 {
		test.Fatalf("expected exactly one workspace_imported event, got %d", len(events))
	}
	event := events[0]
	if event.ID != report.EventID {
		test.Errorf("event id: got %s want %s", event.ID, report.EventID)
	}
	if event.PlayerID == nil || *event.PlayerID != "test-player" {
		test.Errorf("event player id: got %v want test-player", event.PlayerID)
	}
	payload, ok := event.Payload.(domain.WorkspaceImportedPayload)
	if !ok {
		test.Fatalf("expected WorkspaceImportedPayload, got %T", event.Payload)
	}
	if payload.Counts["tasks"] != report.Tasks {
		test.Errorf("counts.tasks: got %d want %d", payload.Counts["tasks"], report.Tasks)
	}
}

// TestPortabilityService_ReplaceWithoutTruncatePreservesWorkflow asserts
// the documented limitation: under --replace without --truncate, the
// projects.workflow_id ON DELETE RESTRICT FK prevents a delete-then-
// create of any referenced workflow. The apply pass deliberately skips
// the workflow row in that case, so a dump that names the same kanban
// workflow with a tweaked transition list leaves the live workflow
// untouched. Faithful workflow replacement requires --truncate.
func TestPortabilityService_ReplaceWithoutTruncatePreservesWorkflow(test *testing.T) {
	env := newPortTestEnv(test)
	ctx := WithActor(context.Background(), "test-player")

	// Capture the current kanban workflow as the baseline for comparison.
	originalWf, originalWfErr := env.wfSvc.GetByID(ctx, domain.KanbanWorkflowUUID)

	if originalWfErr != nil {
		test.Fatalf("loading kanban workflow: %v", originalWfErr)
	}

	dump, exportErr := env.port.Export(ctx)

	if exportErr != nil {
		test.Fatalf("Export: %v", exportErr)
	}

	// Mutate the dump's workflow so a successful replace would visibly
	// change the workspace.
	for index := range dump.Workflows {
		if dump.Workflows[index].ID == domain.KanbanWorkflowUUID {
			dump.Workflows[index].Transitions = append(
				dump.Workflows[index].Transitions,
				portability.PortableWorkflowTransition{FromStatus: "deleted", ToStatus: "pending"},
			)
		}
	}

	if _, importErr := env.port.Import(ctx, dump, ImportOptions{Replace: true}); importErr != nil {
		test.Fatalf("Import: %v", importErr)
	}

	current, currentErr := env.wfSvc.GetByID(ctx, domain.KanbanWorkflowUUID)

	if currentErr != nil {
		test.Fatalf("re-loading kanban workflow: %v", currentErr)
	}

	if len(current.Transitions) != len(originalWf.Transitions) {
		test.Errorf("workflow transitions changed without --truncate: was %d, now %d",
			len(originalWf.Transitions), len(current.Transitions))
	}
	if current.Version != originalWf.Version {
		test.Errorf("workflow version drifted without --truncate: was %d, now %d",
			originalWf.Version, current.Version)
	}
}

// TestPortabilityService_TruncateReplacesWorkflowFaithfully complements
// the preserve-if-exists test above: the same dump with --truncate set
// rewrites the workflow with the dump's payload because TruncateAll
// wipes the workflows table before applyWorkflows runs.
func TestPortabilityService_TruncateReplacesWorkflowFaithfully(test *testing.T) {
	env := newPortTestEnv(test)
	ctx := WithActor(context.Background(), "test-player")

	dump, exportErr := env.port.Export(ctx)

	if exportErr != nil {
		test.Fatalf("Export: %v", exportErr)
	}

	want := append([]portability.PortableWorkflowTransition(nil),
		portability.PortableWorkflowTransition{FromStatus: "deleted", ToStatus: "pending"})
	for index := range dump.Workflows {
		if dump.Workflows[index].ID == domain.KanbanWorkflowUUID {
			dump.Workflows[index].Transitions = append(dump.Workflows[index].Transitions, want...)
		}
	}

	if _, importErr := env.port.Import(ctx, dump, ImportOptions{Replace: true, Truncate: true}); importErr != nil {
		test.Fatalf("Import: %v", importErr)
	}

	current, currentErr := env.wfSvc.GetByID(ctx, domain.KanbanWorkflowUUID)

	if currentErr != nil {
		test.Fatalf("loading workflow post-truncate: %v", currentErr)
	}

	found := false
	for _, tr := range current.Transitions {
		if tr.FromStatus == "deleted" && tr.ToStatus == "pending" {
			found = true
			break
		}
	}
	if !found {
		test.Errorf("expected dump's deleted→pending transition to land under --truncate, got %v", current.Transitions)
	}
}

func TestPortabilityService_TruncateWithoutReplaceRejected(test *testing.T) {
	env := newPortTestEnv(test)
	ctx := WithActor(context.Background(), "test-player")

	now := time.Now().UTC().Truncate(time.Millisecond)
	dump := &portability.PortableWorkspace{
		SchemaVersion: portability.SchemaVersion,
		TuskVersion:   "test",
		ExportedAt:    now,
	}

	_, importErr := env.port.Import(ctx, dump, ImportOptions{Truncate: true})

	if importErr == nil {
		test.Fatal("expected ImportError, got nil")
	}

	var importErrTyped *portability.ImportError
	if !errors.As(importErr, &importErrTyped) {
		test.Fatalf("expected *portability.ImportError, got %T: %v", importErr, importErr)
	}

	if len(importErrTyped.Issues) != 1 {
		test.Fatalf("expected one issue, got %d", len(importErrTyped.Issues))
	}
	if importErrTyped.Issues[0].Kind != "schema" {
		test.Errorf("issue kind: got %q want schema", importErrTyped.Issues[0].Kind)
	}
}

func TestPortability_RoundTrip_ProjectDescription(test *testing.T) {
	envA := newPortTestEnv(test)
	ctx := WithActor(context.Background(), "test-player")

	const desc = "alpha project description\nwith two lines"
	wf, wfErr := envA.wfSvc.GetByName(ctx, "kanban")

	if wfErr != nil {
		test.Fatalf("resolving workflow: %v", wfErr)
	}

	created, createdErr := envA.projectSvc.Create(ctx, CreateProjectInput{
		Name:        "alpha",
		WorkflowID:  wf.ID,
		Description: desc,
	})

	if createdErr != nil {
		test.Fatalf("seeding alpha: %v", createdErr)
	}

	if created.Description != desc {
		test.Fatalf("seed Description = %q, want %q", created.Description, desc)
	}

	dump, exportErr := envA.port.Export(ctx)

	if exportErr != nil {
		test.Fatalf("Export: %v", exportErr)
	}

	var found bool
	for _, proj := range dump.Projects {
		if proj.Name == "alpha" {
			found = true
			if proj.Description != desc {
				test.Fatalf("dump alpha Description = %q, want %q", proj.Description, desc)
			}
		}
	}
	if !found {
		test.Fatalf("alpha project missing from dump: %+v", dump.Projects)
	}

	envB := newPortTestEnv(test)
	if _, importErr := envB.port.Import(ctx, dump, ImportOptions{Replace: true, Truncate: true}); importErr != nil {
		test.Fatalf("Import: %v", importErr)
	}

	got, gotErr := envB.projectSvc.GetByName(ctx, "alpha")

	if gotErr != nil {
		test.Fatalf("GetByName after import: %v", gotErr)
	}

	if got.Description != desc {
		test.Fatalf("imported Description = %q, want %q", got.Description, desc)
	}
}
