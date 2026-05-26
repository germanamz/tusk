package index_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestEdgesTableHasKindAndSourceColumns(test *testing.T) {
	test.Parallel()

	dbPath := filepath.Join(test.TempDir(), "tusk.db")
	store, openErr := index.Open(dbPath)
	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}
	defer store.Close()

	rows, queryErr := store.DB().Query(`PRAGMA table_info(edges)`)
	if queryErr != nil {
		test.Fatalf("table_info: %v", queryErr)
	}
	defer rows.Close()

	seen := map[string]struct{}{}
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notNull int
			dfltVal any
			pk      int
		)
		if scanErr := rows.Scan(&cid, &name, &ctype, &notNull, &dfltVal, &pk); scanErr != nil {
			test.Fatalf("scan: %v", scanErr)
		}
		seen[name] = struct{}{}
	}
	if iterErr := rows.Err(); iterErr != nil {
		test.Fatalf("iter: %v", iterErr)
	}

	for _, want := range []string{"kind", "source"} {
		if _, ok := seen[want]; !ok {
			test.Errorf("edges table missing %q column", want)
		}
	}
}
