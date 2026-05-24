package filter_test

import (
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// TestModifiedSince_EndToEndAgainstIndex parses, validates, compiles, and
// runs a modified-since filter against a real SQLite index. This is the
// only place in the filter package that touches the index; it guards
// against regressions where the nanosecond/second mismatch would silently
// match zero or all rows.
func TestModifiedSince_EndToEndAgainstIndex(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("index.Open: %v", openErr)
	}

	defer store.Close()

	repo := index.NewNodeRepo(store)
	now := time.Now()

	fixtures := []index.NodeRow{
		{
			ID:             "notes/fresh",
			Type:           "note",
			Path:           "notes/fresh.md",
			Title:          "Fresh",
			PropertiesJSON: "{}",
			LastMtime:      now.Add(-1 * time.Hour).UnixNano(),
			LastSize:       1,
			LastChecksum:   "a",
		},
		{
			ID:             "notes/middle",
			Type:           "note",
			Path:           "notes/middle.md",
			Title:          "Middle",
			PropertiesJSON: "{}",
			LastMtime:      now.Add(-3 * 24 * time.Hour).UnixNano(),
			LastSize:       1,
			LastChecksum:   "b",
		},
		{
			ID:             "notes/stale",
			Type:           "note",
			Path:           "notes/stale.md",
			Title:          "Stale",
			PropertiesJSON: "{}",
			LastMtime:      now.Add(-30 * 24 * time.Hour).UnixNano(),
			LastSize:       1,
			LastChecksum:   "c",
		},
	}

	for _, row := range fixtures {
		if err := repo.Upsert(row); err != nil {
			test.Fatalf("Upsert %s: %v", row.ID, err)
		}
	}

	cases := []struct {
		name    string
		input   string
		wantIDs []string
	}{
		{
			name:    "duration_1d_catches_fresh",
			input:   "modified-since:1d",
			wantIDs: []string{"notes/fresh"},
		},
		{
			name:    "duration_7d_catches_fresh_and_middle",
			input:   "modified-since:7d",
			wantIDs: []string{"notes/fresh", "notes/middle"},
		},
		{
			name:    "duration_60d_catches_all",
			input:   "modified-since:60d",
			wantIDs: []string{"notes/fresh", "notes/middle", "notes/stale"},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			expr, parseErrs := filter.NewParser(testCase.input).Parse()

			if len(parseErrs) != 0 {
				test.Fatalf("parse: %+v", parseErrs)
			}

			if validateErrs := filter.Validate(expr, manifest.Manifest{}); len(validateErrs) != 0 {
				test.Fatalf("validate: %+v", validateErrs)
			}

			sql, params, compileErr := filter.Compile(expr, filter.CompileOptions{})

			if compileErr != nil {
				test.Fatalf("compile: %v", compileErr)
			}

			rows, queryErr := store.DB().Query(sql, params...)

			if queryErr != nil {
				test.Fatalf("query: %v\nsql=%s\nparams=%v", queryErr, sql, params)
			}

			defer rows.Close()

			var gotIDs []string

			for rows.Next() {
				var (
					id, nodeType, path, title, propertiesJSON, lastChecksum string
					lastMtime, lastSize                                     int64
				)

				if scanErr := rows.Scan(&id, &nodeType, &path, &title, &propertiesJSON, &lastMtime, &lastSize, &lastChecksum); scanErr != nil {
					test.Fatalf("scan: %v", scanErr)
				}

				gotIDs = append(gotIDs, id)
			}

			sort.Strings(gotIDs)
			sort.Strings(testCase.wantIDs)

			if !equalStringSlices(gotIDs, testCase.wantIDs) {
				test.Errorf("ids = %v, want %v", gotIDs, testCase.wantIDs)
			}
		})
	}
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}

	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}

	return true
}
