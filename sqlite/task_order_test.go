package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/migrations"
	"github.com/google/uuid"
)

func ptrFloat(val float64) *float64 { return &val }

// migrationsUpTo returns an fs.FS containing only migration files whose
// numeric prefix is <= maxVersion. Used to boot a Store at a known
// migration baseline so a test can apply a later migration manually.
func migrationsUpTo(test *testing.T, maxVersion int) fs.FS {
	test.Helper()

	entries, err := fs.Glob(migrations.FS, "*.sql")

	if err != nil {
		test.Fatalf("glob migrations: %v", err)
	}

	mfs := fstest.MapFS{}
	for _, name := range entries {
		var version int

		if _, err := fmt.Sscanf(name, "%d_", &version); err != nil {
			test.Fatalf("parse version from %s: %v", name, err)
		}

		if version > maxVersion {
			continue
		}

		data, err := fs.ReadFile(migrations.FS, name)

		if err != nil {
			test.Fatalf("read %s: %v", name, err)
		}

		mfs[name] = &fstest.MapFile{Data: data}
	}
	return mfs
}

// storeAtMigration boots a Store on a file DB with migrations applied up to
// maxVersion. Using a file DB (not :memory:) lets migrations that rely on
// cross-connection WAL behavior work identically to production.
func storeAtMigration(test *testing.T, maxVersion int) *Store {
	test.Helper()

	dir := test.TempDir()

	store, err := New(filepath.Join(dir, "test.db"), migrationsUpTo(test, maxVersion))

	if err != nil {
		test.Fatalf("open store at migration %d: %v", maxVersion, err)
	}

	test.Cleanup(func() { _ = store.Close() })
	return store
}

// execMigrationFile reads and executes a single migration file from the
// embedded migrations FS against the given DB.
func execMigrationFile(test *testing.T, db *sql.DB, name string) {
	test.Helper()

	data, err := fs.ReadFile(migrations.FS, name)

	if err != nil {
		test.Fatalf("read %s: %v", name, err)
	}

	if _, err := db.Exec(string(data)); err != nil {
		test.Fatalf("exec %s: %v", name, err)
	}
}

// insertRawTask seeds a task using the pre-migration-011 column layout. We
// can't use TaskRepo.Create here because that writes the "order" column,
// which does not exist yet at migration 010.
func insertRawTask(test *testing.T, db *sql.DB, id uuid.UUID, parentID *uuid.UUID, createdAt time.Time) {
	test.Helper()
	var pid any
	if parentID != nil {
		pid = parentID.String()
	}
	ts := createdAt.UTC().Format(timeFormat)
	// The last 8 chars of the UUID string keep short_id unique across our
	// deterministic test IDs (which share a common prefix).
	idStr := id.String()
	shortID := idStr[len(idStr)-8:]

	_, err := db.Exec(`
		INSERT INTO tasks (
			id, short_id, parent_id, project_id,
			title, description, status, priority, version,
			due_at, wait_until, recurrence_rule, uda,
			created_at, modified_at, claimed_by, claimed_at, level
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id.String(), shortID, pid, domain.DefaultProjectUUID.String(),
		"seed", "", "pending", 0, 1,
		nil, nil, nil, "{}",
		ts, ts, nil, nil, nil,
	)

	if err != nil {
		test.Fatalf("insertRawTask: %v", err)
	}
}

// fetchOrder reads the "order" column for a given task ID.
func fetchOrder(test *testing.T, db *sql.DB, id uuid.UUID) (float64, bool) {
	test.Helper()

	var order sql.NullFloat64

	err := db.QueryRow(`SELECT "order" FROM tasks WHERE id = ?`, id.String()).Scan(&order)

	if err != nil {
		test.Fatalf("fetchOrder %s: %v", id, err)
	}

	return order.Float64, order.Valid
}

func TestMigration_011_TaskOrder_Backfill(test *testing.T) {
	store := storeAtMigration(test, 10)

	// Use deterministic UUIDs so the id-ASC tiebreak is predictable.
	idR1 := uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	idR2 := uuid.MustParse("00000000-0000-0000-0000-00000000000b")
	idC1 := uuid.MustParse("00000000-0000-0000-0000-00000000000c")
	idC2 := uuid.MustParse("00000000-0000-0000-0000-00000000000d")
	idC3 := uuid.MustParse("00000000-0000-0000-0000-00000000000e")

	time0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	time1 := time0.Add(1 * time.Minute)
	time2 := time0.Add(10 * time.Minute)
	time3 := time0.Add(11 * time.Minute)

	// Root-level siblings.
	insertRawTask(test, store.DB(), idR1, nil, time0)
	insertRawTask(test, store.DB(), idR2, nil, time1)

	// Children of R1: C1 earliest, then C2 and C3 share created_at
	// so the tiebreak uses id ASC (C2 < C3 lexicographically).
	insertRawTask(test, store.DB(), idC1, &idR1, time2)
	insertRawTask(test, store.DB(), idC2, &idR1, time3)
	insertRawTask(test, store.DB(), idC3, &idR1, time3)

	execMigrationFile(test, store.DB(), "011_task_order.up.sql")

	cases := []struct {
		id   uuid.UUID
		want float64
	}{
		{idR1, 1.0},
		{idR2, 2.0},
		{idC1, 1.0},
		{idC2, 2.0},
		{idC3, 3.0},
	}
	for _, testCase := range cases {
		got, ok := fetchOrder(test, store.DB(), testCase.id)
		if !ok {
			test.Fatalf("task %s: order is NULL after backfill", testCase.id)
		}
		if got != testCase.want {
			test.Fatalf("task %s: got order %v, want %v", testCase.id, got, testCase.want)
		}
	}

	// The idx_tasks_parent_order index must exist after up.
	var idxName string

	err := store.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_tasks_parent_order'`,
	).Scan(&idxName)

	if err != nil {
		test.Fatalf("looking up index: %v", err)
	}

	if idxName != "idx_tasks_parent_order" {
		test.Fatalf("expected index idx_tasks_parent_order, got %q", idxName)
	}
}

func TestMigration_011_TaskOrder_Down(test *testing.T) {
	store := storeAtMigration(test, 10)
	execMigrationFile(test, store.DB(), "011_task_order.up.sql")

	// Confirm the column was added before we tear it down.
	before := columnNames(test, store.DB(), "tasks")
	if !contains(before, "order") {
		test.Fatalf("expected tasks.order to exist after up, got %v", before)
	}

	execMigrationFile(test, store.DB(), "011_task_order.down.sql")

	after := columnNames(test, store.DB(), "tasks")
	if contains(after, "order") {
		test.Fatalf("expected tasks.order to be removed after down, got %v", after)
	}

	var idxName string

	err := store.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_tasks_parent_order'`,
	).Scan(&idxName)

	if err != sql.ErrNoRows {
		test.Fatalf("expected index to be dropped, got err=%v name=%q", err, idxName)
	}
}

func columnNames(test *testing.T, db *sql.DB, table string) []string {
	test.Helper()

	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%q)`, table))

	if err != nil {
		test.Fatalf("PRAGMA table_info: %v", err)
	}

	defer rows.Close()
	var cols []string
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)

		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			test.Fatalf("scan table_info: %v", err)
		}

		cols = append(cols, name)
	}
	return cols
}

func contains(xs []string, want string) bool {
	for _, item := range xs {
		if item == want {
			return true
		}
	}
	return false
}

func TestTaskRepo_OrderRoundTrip(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()

	task := newTestTask()
	task.Order = ptrFloat(3.5)

	if err := repo.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, task.ID)

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.Order == nil || *got.Order != 3.5 {
		test.Fatalf("expected Order=3.5, got %v", got.Order)
	}

	task.Order = ptrFloat(1.25)

	if err := repo.Update(ctx, task); err != nil {
		test.Fatalf("Update to 1.25: %v", err)
	}

	got, err = repo.GetByID(ctx, task.ID)

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.Order == nil || *got.Order != 1.25 {
		test.Fatalf("expected Order=1.25, got %v", got.Order)
	}

	task.Order = nil

	if err := repo.Update(ctx, task); err != nil {
		test.Fatalf("Update to nil: %v", err)
	}

	got, err = repo.GetByID(ctx, task.ID)

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.Order != nil {
		test.Fatalf("expected Order=nil, got %v", *got.Order)
	}
}

// seedSiblings inserts n children under parent with the given order values.
// A nil order value is inserted as SQL NULL. Returns the created task IDs
// in the same order as the input slice.
func seedSiblings(test *testing.T, repo *TaskRepo, parent *domain.Task, orders []*float64) []*domain.Task {
	test.Helper()
	ctx := context.Background()
	out := make([]*domain.Task, len(orders))
	for index, orderVal := range orders {
		child := newTestTask()
		if parent != nil {
			child.ParentID = &parent.ID
		}
		child.Order = orderVal
		// Nudge created_at so the secondary sort is deterministic when
		// order is NULL.
		child.CreatedAt = child.CreatedAt.Add(time.Duration(index) * time.Millisecond)
		child.ModifiedAt = child.CreatedAt

		if err := repo.Create(ctx, child); err != nil {
			test.Fatalf("Create child %d: %v", index, err)
		}

		out[index] = child
	}
	return out
}

func TestTaskRepo_NextOrder(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()

	parent := newTestTask()
	mustCreateTask(test, repo, parent)

	// Empty group -> 1.0.
	got, err := repo.NextOrder(ctx, &parent.ID)

	if err != nil {
		test.Fatalf("NextOrder empty: %v", err)
	}

	if got != 1.0 {
		test.Fatalf("empty NextOrder: got %v, want 1.0", got)
	}

	// Seed [1, 2, 3] under parent -> NextOrder = 4.0.
	seedSiblings(test, repo, parent, []*float64{ptrFloat(1), ptrFloat(2), ptrFloat(3)})

	got, err = repo.NextOrder(ctx, &parent.ID)

	if err != nil {
		test.Fatalf("NextOrder [1,2,3]: %v", err)
	}

	if got != 4.0 {
		test.Fatalf("NextOrder [1,2,3]: got %v, want 4.0", got)
	}

	// Fresh parent with single child [2.5] -> NextOrder = 3.5.
	parent2 := newTestTask()
	mustCreateTask(test, repo, parent2)
	seedSiblings(test, repo, parent2, []*float64{ptrFloat(2.5)})

	got, err = repo.NextOrder(ctx, &parent2.ID)

	if err != nil {
		test.Fatalf("NextOrder [2.5]: %v", err)
	}

	if got != 3.5 {
		test.Fatalf("NextOrder [2.5]: got %v, want 3.5", got)
	}

	// Root scope: count how many root tasks already exist, then seed new roots
	// with known order values and assert NextOrder against the max.
	// The pair of parent tasks we created above (parent, parent2) are at root and
	// have NULL order, so only the explicitly-seeded root tasks contribute to
	// max("order") WHERE parent_id IS NULL.
	rootSeeds := seedSiblings(test, repo, nil, []*float64{ptrFloat(10), ptrFloat(20)})
	_ = rootSeeds

	got, err = repo.NextOrder(ctx, nil)

	if err != nil {
		test.Fatalf("NextOrder root: %v", err)
	}

	if got != 21.0 {
		test.Fatalf("NextOrder root: got %v, want 21.0", got)
	}
}

func TestTaskRepo_FirstOrder(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()

	parent := newTestTask()
	mustCreateTask(test, repo, parent)

	got, err := repo.FirstOrder(ctx, &parent.ID)

	if err != nil {
		test.Fatalf("FirstOrder empty: %v", err)
	}

	if got != 1.0 {
		test.Fatalf("FirstOrder empty: got %v, want 1.0", got)
	}

	seedSiblings(test, repo, parent, []*float64{ptrFloat(1), ptrFloat(2), ptrFloat(3)})

	got, err = repo.FirstOrder(ctx, &parent.ID)

	if err != nil {
		test.Fatalf("FirstOrder [1,2,3]: %v", err)
	}

	if got != 0.0 {
		test.Fatalf("FirstOrder [1,2,3]: got %v, want 0.0", got)
	}

	// Root scope: seed explicit root-level order values and assert.
	seedSiblings(test, repo, nil, []*float64{ptrFloat(10), ptrFloat(20)})

	got, err = repo.FirstOrder(ctx, nil)

	if err != nil {
		test.Fatalf("FirstOrder root: %v", err)
	}

	if got != 9.0 {
		test.Fatalf("FirstOrder root: got %v, want 9.0", got)
	}
}

func TestTaskRepo_NeighborOrders(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()

	parent := newTestTask()
	mustCreateTask(test, repo, parent)
	seedSiblings(test, repo, parent, []*float64{ptrFloat(1), ptrFloat(2), ptrFloat(3)})

	cases := []struct {
		name     string
		pivot    float64
		wantPrev *float64
		wantNext *float64
	}{
		{"between 2 and 3", 2.5, ptrFloat(2), ptrFloat(3)},
		{"below min", 0.5, nil, ptrFloat(1)},
		{"above max", 5, ptrFloat(3), nil},
		{"equal to 2", 2, ptrFloat(1), ptrFloat(3)},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			prev, next, err := repo.NeighborOrders(ctx, &parent.ID, testCase.pivot)

			if err != nil {
				test.Fatalf("NeighborOrders: %v", err)
			}

			if !floatPtrEqual(prev, testCase.wantPrev) {
				test.Fatalf("prev: got %v, want %v", fmtFloatPtr(prev), fmtFloatPtr(testCase.wantPrev))
			}
			if !floatPtrEqual(next, testCase.wantNext) {
				test.Fatalf("next: got %v, want %v", fmtFloatPtr(next), fmtFloatPtr(testCase.wantNext))
			}
		})
	}
}

func floatPtrEqual(first, second *float64) bool {
	if first == nil && second == nil {
		return true
	}
	if first == nil || second == nil {
		return false
	}
	return *first == *second
}

func fmtFloatPtr(floatPtr *float64) string {
	if floatPtr == nil {
		return "nil"
	}
	return fmt.Sprintf("%v", *floatPtr)
}

func TestTaskRepo_GetChildren_SortOrder(test *testing.T) {
	store := testStore(test)
	repo := NewTaskRepo(store.DB())
	ctx := context.Background()

	parent := newTestTask()
	mustCreateTask(test, repo, parent)

	// Insert in scrambled order; expect sorted output (nil sorts last).
	children := seedSiblings(test, repo, parent, []*float64{
		ptrFloat(3), ptrFloat(1), nil, ptrFloat(2),
	})
	// children[0]=3, [1]=1, [2]=nil, [3]=2

	got, err := repo.GetChildren(ctx, parent.ID)

	if err != nil {
		test.Fatalf("GetChildren: %v", err)
	}

	if len(got) != 4 {
		test.Fatalf("expected 4 children, got %d", len(got))
	}

	// Expected order by id: children[1] (order=1), children[3] (order=2),
	// children[0] (order=3), children[2] (order=nil).
	wantIDs := []uuid.UUID{children[1].ID, children[3].ID, children[0].ID, children[2].ID}
	for index, wantID := range wantIDs {
		if got[index].ID != wantID {
			test.Fatalf("position %d: got %s (order %v), want %s",
				index, got[index].ID, fmtFloatPtr(got[index].Order), wantID)
		}
	}
}
