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

func ptrFloat(v float64) *float64 { return &v }

// migrationsUpTo returns an fs.FS containing only migration files whose
// numeric prefix is <= maxVersion. Used to boot a Store at a known
// migration baseline so a test can apply a later migration manually.
func migrationsUpTo(t *testing.T, maxVersion int) fs.FS {
	t.Helper()
	entries, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	mfs := fstest.MapFS{}
	for _, name := range entries {
		var v int
		if _, err := fmt.Sscanf(name, "%d_", &v); err != nil {
			t.Fatalf("parse version from %s: %v", name, err)
		}
		if v > maxVersion {
			continue
		}
		data, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		mfs[name] = &fstest.MapFile{Data: data}
	}
	return mfs
}

// storeAtMigration boots a Store on a file DB with migrations applied up to
// maxVersion. Using a file DB (not :memory:) lets migrations that rely on
// cross-connection WAL behavior work identically to production.
func storeAtMigration(t *testing.T, maxVersion int) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "test.db"), migrationsUpTo(t, maxVersion))
	if err != nil {
		t.Fatalf("open store at migration %d: %v", maxVersion, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// execMigrationFile reads and executes a single migration file from the
// embedded migrations FS against the given DB.
func execMigrationFile(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	data, err := fs.ReadFile(migrations.FS, name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if _, err := db.Exec(string(data)); err != nil {
		t.Fatalf("exec %s: %v", name, err)
	}
}

// insertRawTask seeds a task using the pre-migration-011 column layout. We
// can't use TaskRepo.Create here because that writes the "order" column,
// which does not exist yet at migration 010.
func insertRawTask(t *testing.T, db *sql.DB, id uuid.UUID, parentID *uuid.UUID, createdAt time.Time) {
	t.Helper()
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
		t.Fatalf("insertRawTask: %v", err)
	}
}

// fetchOrder reads the "order" column for a given task ID.
func fetchOrder(t *testing.T, db *sql.DB, id uuid.UUID) (float64, bool) {
	t.Helper()
	var order sql.NullFloat64
	err := db.QueryRow(`SELECT "order" FROM tasks WHERE id = ?`, id.String()).Scan(&order)
	if err != nil {
		t.Fatalf("fetchOrder %s: %v", id, err)
	}
	return order.Float64, order.Valid
}

func TestMigration_011_TaskOrder_Backfill(t *testing.T) {
	s := storeAtMigration(t, 10)

	// Use deterministic UUIDs so the id-ASC tiebreak is predictable.
	idR1 := uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	idR2 := uuid.MustParse("00000000-0000-0000-0000-00000000000b")
	idC1 := uuid.MustParse("00000000-0000-0000-0000-00000000000c")
	idC2 := uuid.MustParse("00000000-0000-0000-0000-00000000000d")
	idC3 := uuid.MustParse("00000000-0000-0000-0000-00000000000e")

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(1 * time.Minute)
	t2 := t0.Add(10 * time.Minute)
	t3 := t0.Add(11 * time.Minute)

	// Root-level siblings.
	insertRawTask(t, s.DB(), idR1, nil, t0)
	insertRawTask(t, s.DB(), idR2, nil, t1)

	// Children of R1: C1 earliest, then C2 and C3 share created_at
	// so the tiebreak uses id ASC (C2 < C3 lexicographically).
	insertRawTask(t, s.DB(), idC1, &idR1, t2)
	insertRawTask(t, s.DB(), idC2, &idR1, t3)
	insertRawTask(t, s.DB(), idC3, &idR1, t3)

	execMigrationFile(t, s.DB(), "011_task_order.up.sql")

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
	for _, c := range cases {
		got, ok := fetchOrder(t, s.DB(), c.id)
		if !ok {
			t.Fatalf("task %s: order is NULL after backfill", c.id)
		}
		if got != c.want {
			t.Fatalf("task %s: got order %v, want %v", c.id, got, c.want)
		}
	}

	// The idx_tasks_parent_order index must exist after up.
	var idxName string
	err := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_tasks_parent_order'`,
	).Scan(&idxName)
	if err != nil {
		t.Fatalf("looking up index: %v", err)
	}
	if idxName != "idx_tasks_parent_order" {
		t.Fatalf("expected index idx_tasks_parent_order, got %q", idxName)
	}
}

func TestMigration_011_TaskOrder_Down(t *testing.T) {
	s := storeAtMigration(t, 10)
	execMigrationFile(t, s.DB(), "011_task_order.up.sql")

	// Confirm the column was added before we tear it down.
	before := columnNames(t, s.DB(), "tasks")
	if !contains(before, "order") {
		t.Fatalf("expected tasks.order to exist after up, got %v", before)
	}

	execMigrationFile(t, s.DB(), "011_task_order.down.sql")

	after := columnNames(t, s.DB(), "tasks")
	if contains(after, "order") {
		t.Fatalf("expected tasks.order to be removed after down, got %v", after)
	}

	var idxName string
	err := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_tasks_parent_order'`,
	).Scan(&idxName)
	if err != sql.ErrNoRows {
		t.Fatalf("expected index to be dropped, got err=%v name=%q", err, idxName)
	}
}

func columnNames(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%q)`, table))
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
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
			t.Fatalf("scan table_info: %v", err)
		}
		cols = append(cols, name)
	}
	return cols
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestTaskRepo_OrderRoundTrip(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	task := newTestTask()
	task.Order = ptrFloat(3.5)
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Order == nil || *got.Order != 3.5 {
		t.Fatalf("expected Order=3.5, got %v", got.Order)
	}

	task.Order = ptrFloat(1.25)
	if err := repo.Update(ctx, task); err != nil {
		t.Fatalf("Update to 1.25: %v", err)
	}
	got, err = repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Order == nil || *got.Order != 1.25 {
		t.Fatalf("expected Order=1.25, got %v", got.Order)
	}

	task.Order = nil
	if err := repo.Update(ctx, task); err != nil {
		t.Fatalf("Update to nil: %v", err)
	}
	got, err = repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Order != nil {
		t.Fatalf("expected Order=nil, got %v", *got.Order)
	}
}

// seedSiblings inserts n children under parent with the given order values.
// A nil order value is inserted as SQL NULL. Returns the created task IDs
// in the same order as the input slice.
func seedSiblings(t *testing.T, repo *TaskRepo, parent *domain.Task, orders []*float64) []*domain.Task {
	t.Helper()
	ctx := context.Background()
	out := make([]*domain.Task, len(orders))
	for i, o := range orders {
		child := newTestTask()
		if parent != nil {
			child.ParentID = &parent.ID
		}
		child.Order = o
		// Nudge created_at so the secondary sort is deterministic when
		// order is NULL.
		child.CreatedAt = child.CreatedAt.Add(time.Duration(i) * time.Millisecond)
		child.ModifiedAt = child.CreatedAt
		if err := repo.Create(ctx, child); err != nil {
			t.Fatalf("Create child %d: %v", i, err)
		}
		out[i] = child
	}
	return out
}

func TestTaskRepo_NextOrder(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	parent := newTestTask()
	mustCreateTask(t, repo, parent)

	// Empty group -> 1.0.
	got, err := repo.NextOrder(ctx, &parent.ID)
	if err != nil {
		t.Fatalf("NextOrder empty: %v", err)
	}
	if got != 1.0 {
		t.Fatalf("empty NextOrder: got %v, want 1.0", got)
	}

	// Seed [1, 2, 3] under parent -> NextOrder = 4.0.
	seedSiblings(t, repo, parent, []*float64{ptrFloat(1), ptrFloat(2), ptrFloat(3)})
	got, err = repo.NextOrder(ctx, &parent.ID)
	if err != nil {
		t.Fatalf("NextOrder [1,2,3]: %v", err)
	}
	if got != 4.0 {
		t.Fatalf("NextOrder [1,2,3]: got %v, want 4.0", got)
	}

	// Fresh parent with single child [2.5] -> NextOrder = 3.5.
	p2 := newTestTask()
	mustCreateTask(t, repo, p2)
	seedSiblings(t, repo, p2, []*float64{ptrFloat(2.5)})
	got, err = repo.NextOrder(ctx, &p2.ID)
	if err != nil {
		t.Fatalf("NextOrder [2.5]: %v", err)
	}
	if got != 3.5 {
		t.Fatalf("NextOrder [2.5]: got %v, want 3.5", got)
	}

	// Root scope: count how many root tasks already exist, then seed new roots
	// with known order values and assert NextOrder against the max.
	// The pair of parent tasks we created above (parent, p2) are at root and
	// have NULL order, so only the explicitly-seeded root tasks contribute to
	// max("order") WHERE parent_id IS NULL.
	rootSeeds := seedSiblings(t, repo, nil, []*float64{ptrFloat(10), ptrFloat(20)})
	_ = rootSeeds
	got, err = repo.NextOrder(ctx, nil)
	if err != nil {
		t.Fatalf("NextOrder root: %v", err)
	}
	if got != 21.0 {
		t.Fatalf("NextOrder root: got %v, want 21.0", got)
	}
}

func TestTaskRepo_FirstOrder(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	parent := newTestTask()
	mustCreateTask(t, repo, parent)

	got, err := repo.FirstOrder(ctx, &parent.ID)
	if err != nil {
		t.Fatalf("FirstOrder empty: %v", err)
	}
	if got != 1.0 {
		t.Fatalf("FirstOrder empty: got %v, want 1.0", got)
	}

	seedSiblings(t, repo, parent, []*float64{ptrFloat(1), ptrFloat(2), ptrFloat(3)})
	got, err = repo.FirstOrder(ctx, &parent.ID)
	if err != nil {
		t.Fatalf("FirstOrder [1,2,3]: %v", err)
	}
	if got != 0.0 {
		t.Fatalf("FirstOrder [1,2,3]: got %v, want 0.0", got)
	}

	// Root scope: seed explicit root-level order values and assert.
	seedSiblings(t, repo, nil, []*float64{ptrFloat(10), ptrFloat(20)})
	got, err = repo.FirstOrder(ctx, nil)
	if err != nil {
		t.Fatalf("FirstOrder root: %v", err)
	}
	if got != 9.0 {
		t.Fatalf("FirstOrder root: got %v, want 9.0", got)
	}
}

func TestTaskRepo_NeighborOrders(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	parent := newTestTask()
	mustCreateTask(t, repo, parent)
	seedSiblings(t, repo, parent, []*float64{ptrFloat(1), ptrFloat(2), ptrFloat(3)})

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
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prev, next, err := repo.NeighborOrders(ctx, &parent.ID, c.pivot)
			if err != nil {
				t.Fatalf("NeighborOrders: %v", err)
			}
			if !floatPtrEqual(prev, c.wantPrev) {
				t.Fatalf("prev: got %v, want %v", fmtFloatPtr(prev), fmtFloatPtr(c.wantPrev))
			}
			if !floatPtrEqual(next, c.wantNext) {
				t.Fatalf("next: got %v, want %v", fmtFloatPtr(next), fmtFloatPtr(c.wantNext))
			}
		})
	}
}

func floatPtrEqual(a, b *float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func fmtFloatPtr(f *float64) string {
	if f == nil {
		return "nil"
	}
	return fmt.Sprintf("%v", *f)
}

func TestTaskRepo_GetChildren_SortOrder(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	parent := newTestTask()
	mustCreateTask(t, repo, parent)

	// Insert in scrambled order; expect sorted output (nil sorts last).
	children := seedSiblings(t, repo, parent, []*float64{
		ptrFloat(3), ptrFloat(1), nil, ptrFloat(2),
	})
	// children[0]=3, [1]=1, [2]=nil, [3]=2

	got, err := repo.GetChildren(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetChildren: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 children, got %d", len(got))
	}

	// Expected order by id: children[1] (order=1), children[3] (order=2),
	// children[0] (order=3), children[2] (order=nil).
	wantIDs := []uuid.UUID{children[1].ID, children[3].ID, children[0].ID, children[2].ID}
	for i, w := range wantIDs {
		if got[i].ID != w {
			t.Fatalf("position %d: got %s (order %v), want %s",
				i, got[i].ID, fmtFloatPtr(got[i].Order), w)
		}
	}
}
