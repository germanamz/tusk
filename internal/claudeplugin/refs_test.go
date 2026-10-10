package claudeplugin_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/claudeplugin"
)

func TestRefs_ListsPagesMostSpecificFirst(test *testing.T) {
	pages := []claudeplugin.RefPage{
		{ID: "technical/ledger", Title: "Ledger module", Named: "server/ledger/core/service.go", Where: "lines 40-41 (Core service)"},
		{ID: "technical/arch", Title: "Architecture", Named: "server/ledger", Where: "line 12 (Modules)"},
		{ID: "technical/map.html", Named: "server/ledger/core/service.go"},
	}

	want := `tusk: 3 pages name server/ledger/core/service.go. Check whether this edit changes what they say.
- technical/ledger "Ledger module": lines 40-41 (Core service)
- technical/arch "Architecture": line 12 (Modules), via server/ledger/
- technical/map.html
`

	if got := claudeplugin.Refs("server/ledger/core/service.go", pages, 0); got != want {
		test.Errorf("Refs =\n%s\nwant\n%s", got, want)
	}
}

func TestRefs_CapsTheList(test *testing.T) {
	pages := make([]claudeplugin.RefPage, 7)

	for position := range pages {
		pages[position] = claudeplugin.RefPage{ID: "p" + string(rune('a'+position)), Named: "go.mod"}
	}

	got := claudeplugin.Refs("go.mod", pages, 2)
	want := `tusk: 7 pages name go.mod. Check whether this edit changes what they say.
- pa
- pb
...and 5 more: tusk_query names-path=go.mod
`

	if got != want {
		test.Errorf("Refs =\n%s\nwant\n%s", got, want)
	}
}

func TestRefs_SingularAndEmpty(test *testing.T) {
	if got := claudeplugin.Refs("go.mod", nil, 0); got != "" {
		test.Errorf("Refs(no pages) = %q, want empty", got)
	}

	got := claudeplugin.Refs("go.mod", []claudeplugin.RefPage{{ID: "notes/build", Named: "go.mod", Where: "line 3"}}, 0)

	if want := "tusk: 1 page names go.mod. Check whether this edit changes what they say.\n- notes/build: line 3\n"; got != want {
		test.Errorf("Refs =\n%q\nwant\n%q", got, want)
	}
}
