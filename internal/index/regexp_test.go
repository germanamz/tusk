package index_test

import (
	"database/sql"
	"fmt"
	"sync"
	"testing"
)

func TestRegexpOperatorMatches(test *testing.T) {
	store := openTestIndex(test)

	cases := []struct {
		value   string
		pattern string
		want    bool
	}{
		{"docs/product/recording.md", `^docs/product/[^/]*$`, true},
		{"docs/product/sub/deep.md", `^docs/product/[^/]*$`, false},
		{"notes/a+b.md", `^notes/a\+b\.md$`, true},
		{"notes/aab.md", `^notes/a\+b\.md$`, false},
		{"notes/café.md", `^notes/caf[^/]\.md$`, true},
	}

	for _, testCase := range cases {
		var got bool

		queryErr := store.DB().QueryRow(`SELECT ? REGEXP ?`, testCase.value, testCase.pattern).Scan(&got)

		if queryErr != nil {
			test.Fatalf("%q REGEXP %q: %v", testCase.value, testCase.pattern, queryErr)
		}

		if got != testCase.want {
			test.Errorf("%q REGEXP %q = %v, want %v", testCase.value, testCase.pattern, got, testCase.want)
		}
	}
}

func TestRegexpOperatorNullValueIsNull(test *testing.T) {
	store := openTestIndex(test)

	var got sql.NullBool

	if queryErr := store.DB().QueryRow(`SELECT NULL REGEXP '^a$'`).Scan(&got); queryErr != nil {
		test.Fatalf("NULL REGEXP: %v", queryErr)
	}

	if got.Valid {
		test.Errorf("NULL REGEXP '^a$' = %v, want NULL", got.Bool)
	}
}

func TestRegexpOperatorRejectsInvalidPattern(test *testing.T) {
	store := openTestIndex(test)

	var got bool

	if queryErr := store.DB().QueryRow(`SELECT 'a' REGEXP '('`).Scan(&got); queryErr == nil {
		test.Errorf("'a' REGEXP '(' succeeded with %v, want an error", got)
	}
}

// TestRegexpOperatorConcurrentPatterns runs many distinct patterns from several
// goroutines at once, so the compiled-pattern cache is shared across pooled
// connections and churns past its bound. Run under -race.
func TestRegexpOperatorConcurrentPatterns(test *testing.T) {
	store := openTestIndex(test)

	const workers = 8

	const patternsPerWorker = 64

	var waitGroup sync.WaitGroup

	failures := make(chan string, workers*patternsPerWorker)

	for worker := range workers {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			for index := range patternsPerWorker {
				value := fmt.Sprintf("dir%d/file%d.md", worker, index)
				pattern := fmt.Sprintf(`^dir%d/file%d\.md$`, worker, index)

				var got bool

				if queryErr := store.DB().QueryRow(`SELECT ? REGEXP ?`, value, pattern).Scan(&got); queryErr != nil {
					failures <- fmt.Sprintf("%q REGEXP %q: %v", value, pattern, queryErr)

					continue
				}

				if !got {
					failures <- fmt.Sprintf("%q REGEXP %q = false, want true", value, pattern)
				}
			}
		}()
	}

	waitGroup.Wait()
	close(failures)

	for failure := range failures {
		test.Error(failure)
	}
}
