package index_test

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestMetaRepo_GetMissingReturnsEmpty(test *testing.T) {
	store := openTempIndex(test)
	defer store.Close()

	repo := index.NewMetaRepo(store)

	value, getErr := repo.Get("missing")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if value != "" {
		test.Errorf("expected empty value, got %q", value)
	}
}

func TestMetaRepo_SetThenGet(test *testing.T) {
	store := openTempIndex(test)
	defer store.Close()

	repo := index.NewMetaRepo(store)

	if setErr := repo.Set("last_reindex_at", "1747000000"); setErr != nil {
		test.Fatalf("Set: %v", setErr)
	}

	value, getErr := repo.Get("last_reindex_at")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if value != "1747000000" {
		test.Errorf("value = %q", value)
	}
}

func TestMetaRepo_SetUpserts(test *testing.T) {
	store := openTempIndex(test)
	defer store.Close()

	repo := index.NewMetaRepo(store)

	if setErr := repo.Set("k", "v1"); setErr != nil {
		test.Fatalf("Set v1: %v", setErr)
	}

	if setErr := repo.Set("k", "v2"); setErr != nil {
		test.Fatalf("Set v2: %v", setErr)
	}

	value, _ := repo.Get("k")

	if value != "v2" {
		test.Errorf("expected v2, got %q", value)
	}
}

func TestMetaRepo_IncrOnMissingKeyReturnsDelta(test *testing.T) {
	store := openTempIndex(test)
	defer store.Close()

	repo := index.NewMetaRepo(store)

	value, incrErr := repo.Incr("reindex_gen", 1)

	if incrErr != nil {
		test.Fatalf("Incr: %v", incrErr)
	}

	if value != 1 {
		test.Errorf("first Incr returned %d, want 1", value)
	}

	stored, _ := repo.Get("reindex_gen")

	if stored != "1" {
		test.Errorf("stored value = %q, want %q", stored, "1")
	}
}

func TestMetaRepo_IncrAccumulatesAcrossCalls(test *testing.T) {
	store := openTempIndex(test)
	defer store.Close()

	repo := index.NewMetaRepo(store)

	for step := int64(1); step <= 5; step++ {
		value, incrErr := repo.Incr("counter", 1)

		if incrErr != nil {
			test.Fatalf("Incr #%d: %v", step, incrErr)
		}

		if value != step {
			test.Errorf("Incr #%d returned %d, want %d", step, value, step)
		}
	}
}

func TestMetaRepo_IncrConcurrentProducesDistinctValues(test *testing.T) {
	store := openTempIndex(test)
	defer store.Close()

	repo := index.NewMetaRepo(store)

	const goroutines = 20

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []int64
	)

	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()

			value, incrErr := repo.Incr("counter", 1)

			if incrErr != nil {
				test.Errorf("Incr: %v", incrErr)

				return
			}

			mu.Lock()
			results = append(results, value)
			mu.Unlock()
		}()
	}

	wg.Wait()

	if len(results) != goroutines {
		test.Fatalf("len(results) = %d, want %d", len(results), goroutines)
	}

	seen := map[int64]struct{}{}

	for _, value := range results {
		if _, dup := seen[value]; dup {
			test.Errorf("duplicate Incr result %d", value)
		}

		seen[value] = struct{}{}
	}

	final, _ := repo.Get("counter")

	if final != "20" {
		test.Errorf("final counter = %q, want %q", final, "20")
	}
}

func TestMetaRepo_IncrOnNonNumericValueReturnsError(test *testing.T) {
	store := openTempIndex(test)
	defer store.Close()

	repo := index.NewMetaRepo(store)

	if setErr := repo.Set("counter", "not-a-number"); setErr != nil {
		test.Fatalf("Set: %v", setErr)
	}

	if _, incrErr := repo.Incr("counter", 1); incrErr == nil {
		test.Fatalf("expected error from Incr on non-numeric value")
	}

	stored, _ := repo.Get("counter")

	if stored != "not-a-number" {
		test.Errorf("stored value mutated to %q after failed Incr", stored)
	}
}

func openTempIndex(test *testing.T) *index.Index {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return store
}
