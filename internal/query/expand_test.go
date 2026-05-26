package query

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestParseInclude(test *testing.T) {
	cases := []struct {
		name    string
		input   []string
		want    IncludeSet
		wantErr bool
	}{
		{name: "empty", input: nil, want: IncludeSet{}},
		{name: "single", input: []string{"body"}, want: IncludeSet{Body: true}},
		{name: "all", input: []string{"body", "edges", "properties"}, want: IncludeSet{Body: true, Edges: true, Properties: true}},
		{name: "dedup", input: []string{"body", "body", "edges"}, want: IncludeSet{Body: true, Edges: true}},
		{name: "blank-tokens-skipped", input: []string{"", "body"}, want: IncludeSet{Body: true}},
		{name: "unknown", input: []string{"banana"}, wantErr: true},
	}

	for _, tc := range cases {
		test.Run(tc.name, func(test *testing.T) {
			got, err := ParseInclude(tc.input)

			if tc.wantErr {
				if err == nil {
					test.Fatalf("expected error, got %+v", got)
				}

				if !strings.Contains(err.Error(), "valid: body, edges, properties, units") {
					test.Errorf("expected suggestion list in error, got %v", err)
				}

				return
			}

			if err != nil {
				test.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(got, tc.want) {
				test.Errorf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestIncludeFromFields(test *testing.T) {
	set := IncludeFromFields([]string{"id", "body", "edges", "ignored"})

	if !set.Body || !set.Edges || set.Properties {
		test.Errorf("got %+v", set)
	}
}

func TestMergeInclude(test *testing.T) {
	got := MergeInclude(IncludeSet{Body: true}, IncludeSet{Edges: true})
	want := IncludeSet{Body: true, Edges: true}

	if !reflect.DeepEqual(got, want) {
		test.Errorf("got %+v want %+v", got, want)
	}
}

func TestExpandListRows_BodyAndProperties(test *testing.T) {
	tmp := test.TempDir()
	relPath := "notes/hello.md"
	abs := filepath.Join(tmp, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		test.Fatal(mkErr)
	}

	if writeErr := os.WriteFile(abs, []byte("# hello\nworld\n"), 0o644); writeErr != nil {
		test.Fatal(writeErr)
	}

	rows := []ListRow{{
		ID:            "notes/hello",
		Type:          "note",
		Path:          relPath,
		Title:         "Hello",
		PropertiesRaw: `{"type":"note","title":"Hello","priority":3}`,
	}}

	set := IncludeSet{Body: true, Properties: true}

	if err := ExpandListRows(rows, set, tmp, nil); err != nil {
		test.Fatalf("expand: %v", err)
	}

	if rows[0].Body != "# hello\nworld\n" {
		test.Errorf("body: %q", rows[0].Body)
	}

	if rows[0].Properties["priority"] != float64(3) {
		test.Errorf("properties: %+v", rows[0].Properties)
	}
}

func TestExpandListRows_BodyMissingFileTolerated(test *testing.T) {
	tmp := test.TempDir()

	rows := []ListRow{{ID: "notes/missing", Path: "notes/missing.md"}}

	if err := ExpandListRows(rows, IncludeSet{Body: true}, tmp, nil); err != nil {
		test.Fatalf("expand should tolerate missing file: %v", err)
	}

	if rows[0].Body != "" {
		test.Errorf("expected empty body, got %q", rows[0].Body)
	}
}

func TestExpandListRows_EmptyPropertiesRaw(test *testing.T) {
	tmp := test.TempDir()
	rows := []ListRow{{ID: "n", Path: "n.md", PropertiesRaw: ""}}

	if err := ExpandListRows(rows, IncludeSet{Properties: true}, tmp, nil); err != nil {
		test.Fatalf("expand: %v", err)
	}

	if rows[0].Properties != nil {
		test.Errorf("expected nil properties, got %+v", rows[0].Properties)
	}
}

func TestExpandListRows_Edges(test *testing.T) {
	tmp := test.TempDir()
	store, openErr := index.Open(filepath.Join(tmp, "idx.db"))

	if openErr != nil {
		test.Fatal(openErr)
	}

	defer store.Close()

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	if err := nodes.Upsert(index.NodeRow{ID: "a", Type: "note", Path: "a.md", Title: "Alpha"}); err != nil {
		test.Fatal(err)
	}

	if err := nodes.Upsert(index.NodeRow{ID: "b", Type: "note", Path: "b.md", Title: "Beta"}); err != nil {
		test.Fatal(err)
	}

	if err := edges.UpsertAll("a", "a.md", []index.EdgeRow{{Type: "links", SourceID: "a", TargetID: "b", SourcePath: "a.md", Kind: "direct"}}); err != nil {
		test.Fatal(err)
	}

	rows := []ListRow{
		{ID: "a", Type: "note", Path: "a.md", Title: "Alpha"},
		{ID: "b", Type: "note", Path: "b.md", Title: "Beta"},
	}

	if err := ExpandListRows(rows, IncludeSet{Edges: true}, tmp, store.DB()); err != nil {
		test.Fatalf("expand edges: %v", err)
	}

	if len(rows[0].Edges) != 1 || rows[0].Edges[0].Direction != "out" || rows[0].Edges[0].TargetID != "b" || rows[0].Edges[0].TargetTitle != "Beta" {
		test.Errorf("row[a] edges: %+v", rows[0].Edges)
	}

	if len(rows[1].Edges) != 1 || rows[1].Edges[0].Direction != "in" || rows[1].Edges[0].TargetID != "a" || rows[1].Edges[0].TargetTitle != "Alpha" {
		test.Errorf("row[b] edges: %+v", rows[1].Edges)
	}
}
