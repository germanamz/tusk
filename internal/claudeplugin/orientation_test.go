package claudeplugin

import (
	"strings"
	"testing"
)

func TestOrientation_ReadyIndex(test *testing.T) {
	got := Orientation(Snapshot{
		Name:       "my-brain",
		NodeTypes:  map[string]int{"note": 5, "package": 29, "person": 1, "spec": 0},
		EdgeTypes:  []string{"relates-to", "depends-on", "references"},
		EdgeCount:  706,
		Aliases:    []string{"standup", "open-tickets"},
		Index:      IndexReady,
		EmbedQueue: 3,
	})

	want := `This project is a tusk vault ("my-brain"): its markdown files are indexed as a typed graph.
Use the tusk_* tools to query it before grepping files.
Node types: package 29 · note 5 · person 1
Edges: 706 · depends-on, references, relates-to
Aliases: open-tickets, standup (run with tusk_run)
Index: 3 embeddings pending
`

	if got != want {
		test.Errorf("Orientation =\n%s\nwant\n%s", got, want)
	}
}

func TestOrientation_MissingIndexListsDeclaredTypes(test *testing.T) {
	got := Orientation(Snapshot{
		Name:      "vault",
		NodeTypes: map[string]int{"note": 0, "decision": 0},
		EdgeTypes: []string{"references"},
		Index:     IndexMissing,
	})

	for _, want := range []string{
		"Node types: decision · note\n",
		"Edge types: references\n",
		"Index: not built yet; the tusk MCP server builds it on start\n",
	} {
		if !strings.Contains(got, want) {
			test.Errorf("Orientation missing %q:\n%s", want, got)
		}
	}

	if strings.Contains(got, "Aliases:") {
		test.Errorf("Orientation lists aliases when none are declared:\n%s", got)
	}
}

func TestOrientation_EdgeCases(test *testing.T) {
	cases := []struct {
		name string
		snap Snapshot
		want string
	}{
		{
			name: "no declared types",
			snap: Snapshot{Name: "vault", Index: IndexReady},
			want: "Node types: none declared in tusk.toml\n",
		},
		{
			name: "declared but none indexed",
			snap: Snapshot{Name: "vault", NodeTypes: map[string]int{"note": 0}, Index: IndexReady},
			want: "Node types: none indexed yet (declared: note)\n",
		},
		{
			name: "reindex queue",
			snap: Snapshot{Name: "vault", Index: IndexReady, ReindexQueue: 12},
			want: "Index: 12 files queued for reindex\n",
		},
		{
			name: "up to date",
			snap: Snapshot{Name: "vault", Index: IndexReady},
			want: "Index: up to date\n",
		},
		{
			name: "unreadable",
			snap: Snapshot{Name: "vault", Index: IndexUnreadable},
			want: "Index: needs a rebuild; the tusk MCP server rebuilds it on start\n",
		},
		{
			name: "edges without declared types",
			snap: Snapshot{Name: "vault", Index: IndexReady, EdgeCount: 4},
			want: "Edges: 4\n",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			got := Orientation(testCase.snap)

			if !strings.Contains(got, testCase.want) {
				test.Errorf("Orientation missing %q:\n%s", testCase.want, got)
			}
		})
	}
}
