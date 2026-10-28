package filter_test

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// graphTraversalManifest declares the edge types the #850 fixture walks:
// depends-on with an inverse label, the default child→parent `parent`
// hierarchy, a parent→child `has-part` hierarchy, and a `capped` hierarchy
// that stops two levels out.
func graphTraversalManifest() manifest.Manifest {
	return manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{"service": {}, "note": {}},
		EdgeTypes: manifest.EdgeTypes{
			"depends-on": {From: []string{"service"}, To: []string{"service"}, Cardinality: manifest.CardinalityManyToMany, Inverse: "depended-on-by"},
			"describes":  {From: []string{"note"}, To: []string{"service"}, Cardinality: manifest.CardinalityManyToMany},
			"parent":     {From: []string{"service"}, To: []string{"service"}, Cardinality: manifest.CardinalityManyToOne, Hierarchy: "parent", HierarchyDefault: true},
			"has-part":   {From: []string{"service"}, To: []string{"service"}, Cardinality: manifest.CardinalityOneToMany, Hierarchy: "parts", ParentIsSource: true},
			"capped":     {From: []string{"service"}, To: []string{"service"}, Cardinality: manifest.CardinalityManyToOne, Hierarchy: "capped", MaxDepth: 2},
		},
	}
}

// seedGraphTraversal writes the #850 fixture:
//
//	depends-on  dep/a → dep/b → dep/c → dep/d,  dep/e → dep/c,  dep/e → parts/p11
//	            dep/x ⇄ dep/y (a cycle)
//	parent      tree/l1 → tree/l0, tree/l2 → tree/l1, … tree/l7 → tree/l6 (eight levels),
//	            tree/m → tree/l0
//	has-part    parts/p → parts/p1, parts/p → parts/p2, parts/p1 → parts/p11
//	capped      cap/c1 → cap/c0, cap/c2 → cap/c1, cap/c3 → cap/c2
//	describes   notes/n1 → dep/a
func seedGraphTraversal(test *testing.T, store *index.Index) {
	test.Helper()

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	edgesBySource := map[string][][2]string{
		"dep/a":     {{"depends-on", "dep/b"}},
		"dep/b":     {{"depends-on", "dep/c"}},
		"dep/c":     {{"depends-on", "dep/d"}},
		"dep/d":     nil,
		"dep/e":     {{"depends-on", "dep/c"}, {"depends-on", "parts/p11"}},
		"dep/x":     {{"depends-on", "dep/y"}},
		"dep/y":     {{"depends-on", "dep/x"}},
		"tree/l0":   nil,
		"tree/m":    {{"parent", "tree/l0"}},
		"parts/p":   {{"has-part", "parts/p1"}, {"has-part", "parts/p2"}},
		"parts/p1":  {{"has-part", "parts/p11"}},
		"parts/p2":  nil,
		"parts/p11": nil,
		"cap/c0":    nil,
		"cap/c1":    {{"capped", "cap/c0"}},
		"cap/c2":    {{"capped", "cap/c1"}},
		"cap/c3":    {{"capped", "cap/c2"}},
		"notes/n1":  {{"describes", "dep/a"}},
	}

	for level := 1; level <= 7; level++ {
		edgesBySource["tree/l"+string(rune('0'+level))] = [][2]string{{"parent", "tree/l" + string(rune('0'+level-1))}}
	}

	for id, links := range edgesBySource {
		nodeType := "service"

		if strings.HasPrefix(id, "notes/") {
			nodeType = "note"
		}

		if upsertErr := nodes.Upsert(index.NodeRow{ID: id, Type: nodeType, Path: id + ".md", Title: id, PropertiesJSON: `{}`}); upsertErr != nil {
			test.Fatalf("upsert %s: %v", id, upsertErr)
		}

		rows := make([]index.EdgeRow, 0, len(links))

		for _, link := range links {
			rows = append(rows, index.EdgeRow{Type: link[0], SourceID: id, TargetID: link[1], SourcePath: id + ".md", Kind: "direct"})
		}

		if upsertErr := edges.UpsertAll(id, id+".md", rows); upsertErr != nil {
			test.Fatalf("edges of %s: %v", id, upsertErr)
		}
	}
}

// runFilter parses, validates and compiles input against loaded, runs it on
// store, and returns the sorted ids it matched.
func runFilter(test *testing.T, store *index.Index, loaded manifest.Manifest, input string) []string {
	test.Helper()

	expr, parseErrs := filter.NewParser(input).Parse()

	if len(parseErrs) > 0 {
		test.Fatalf("parse %q: %v", input, parseErrs)
	}

	if validateErrs := filter.Validate(expr, loaded); len(validateErrs) > 0 {
		test.Fatalf("validate %q: %v", input, validateErrs)
	}

	sqlText, params, compileErr := filter.Compile(expr, filter.CompileOptions{Now: time.Now()})

	if compileErr != nil {
		test.Fatalf("compile %q: %v", input, compileErr)
	}

	rows, queryErr := store.DB().Query(sqlText, params...)

	if queryErr != nil {
		test.Fatalf("query %q: %v\nSQL: %s", input, queryErr, sqlText)
	}

	defer rows.Close()

	ids := []string{}

	for rows.Next() {
		var id, nodeType, path, title, properties string
		var mtime, size int64
		var checksum, parentID any

		if scanErr := rows.Scan(&id, &nodeType, &path, &title, &properties, &mtime, &size, &checksum, &parentID); scanErr != nil {
			test.Fatalf("scan %q: %v", input, scanErr)
		}

		ids = append(ids, id)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		test.Fatalf("rows %q: %v", input, rowsErr)
	}

	sort.Strings(ids)

	return ids
}

// TestGraphTraversal_EndToEndAgainstIndex pins the rows #850's filters return
// over a real index: transitive hops (`->+`, `->{m,n}`) that stop on cycles,
// inverse labels, hierarchy shortcuts over parent→child edges, and walks that
// go deeper than the old five-level cap unless the edge sets max-depth.
func TestGraphTraversal_EndToEndAgainstIndex(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	seedGraphTraversal(test, store)

	loaded := graphTraversalManifest()

	cases := []struct {
		name  string
		input string
		want  []string
	}{
		// One or more hops, either way along the edge.
		{"plus", "depends-on->+ id=dep/d", []string{"dep/a", "dep/b", "dep/c", "dep/e"}},
		{"plus_incoming", "depends-on<-+ id=dep/a", []string{"dep/b", "dep/c", "dep/d"}},
		{"bare_plus", "id=dep/* depends-on->+", []string{"dep/a", "dep/b", "dep/c", "dep/e", "dep/x", "dep/y"}},

		// Bounded hop counts.
		{"one", "depends-on->{1} id=dep/d", []string{"dep/c"}},
		{"exact", "depends-on->{2} id=dep/d", []string{"dep/b", "dep/e"}},
		{"exact_three", "depends-on->{3} id=dep/d", []string{"dep/a"}},
		{"range", "depends-on->{1,2} id=dep/d", []string{"dep/b", "dep/c", "dep/e"}},
		{"open", "depends-on->{2,} id=dep/d", []string{"dep/a", "dep/b", "dep/e"}},
		{"incoming_range", "depends-on<-{2,3} id=dep/a", []string{"dep/c", "dep/d"}},

		// A cycle ends the walk instead of looping; a node on it reaches itself.
		{"cycle", "depends-on->+ id=dep/x", []string{"dep/x", "dep/y"}},
		{"cycle_open", "depends-on->{2,} id=dep/x", []string{"dep/x", "dep/y"}},
		{"cycle_exact", "depends-on->{3} id=dep/x", []string{"dep/y"}},

		// Composition: NOT, AND in either order, a group, an outer hop, and
		// CTE-backed terms on both sides (#704 bind order).
		{"not", "id=dep/* NOT depends-on->+ id=dep/d", []string{"dep/d", "dep/x", "dep/y"}},
		{"and_first", "depends-on->+ id=dep/d AND type=service", []string{"dep/a", "dep/b", "dep/c", "dep/e"}},
		{"and_last", "type=service AND depends-on->+ id=dep/d", []string{"dep/a", "dep/b", "dep/c", "dep/e"}},
		{"group", "depends-on->+ (id=dep/d OR id=dep/x)", []string{"dep/a", "dep/b", "dep/c", "dep/e", "dep/x", "dep/y"}},
		{"inside_hop", "describes-> depends-on->+ id=dep/d", []string{"notes/n1"}},
		{"inner_shortcut", "depends-on->+ tree:parts=parts/p", []string{"dep/e"}},
		{"beside_shortcut", "tree:parts=parts/p OR depends-on->{2} id=dep/d", []string{"dep/b", "dep/e", "parts/p1", "parts/p11", "parts/p2"}},
		{"two_walks", "depends-on->{2} id=dep/d AND depends-on<-+ id=dep/a", []string{"dep/b"}},

		// An inverse label walks its edge type the other way.
		{"inverse", "depended-on-by-> id=dep/b", []string{"dep/c"}},
		{"inverse_incoming", "depended-on-by<- id=dep/c", []string{"dep/b", "dep/e"}},
		{"inverse_plus", "depended-on-by->+ id=dep/a", []string{"dep/b", "dep/c", "dep/d"}},
		{"inverse_user_scope", ":depended-on-by-> id=dep/b", []string{"dep/c"}},

		// The child→parent default walks the whole tree, past five levels.
		{"tree_deep", "tree=tree/l0", []string{"tree/l1", "tree/l2", "tree/l3", "tree/l4", "tree/l5", "tree/l6", "tree/l7", "tree/m"}},
		{"parent_default", "parent=tree/l0", []string{"tree/l1", "tree/m"}},
		{"root_deep", "root=tree/l7", []string{"tree/l0", "tree/l1", "tree/l2", "tree/l3", "tree/l4", "tree/l5", "tree/l6", "tree/l7", "tree/m"}},

		// parent-is-source turns every shortcut around.
		{"tree_parts", "tree:parts=parts/p", []string{"parts/p1", "parts/p11", "parts/p2"}},
		{"parent_parts", "parent:parts=parts/p", []string{"parts/p1", "parts/p2"}},
		{"parent_parts_leaf", "parent:parts=parts/p11", []string{}},
		{"root_parts", "root:parts=parts/p11", []string{"parts/p", "parts/p1", "parts/p11", "parts/p2"}},
		{"root_parts_top", "root:parts=parts/p", []string{"parts/p", "parts/p1", "parts/p11", "parts/p2"}},

		// max-depth stops the walk that many levels out.
		{"tree_capped", "tree:capped=cap/c0", []string{"cap/c1", "cap/c2"}},
		{"root_capped", "root:capped=cap/c3", []string{"cap/c1", "cap/c2", "cap/c3"}},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			got := runFilter(subtest, store, loaded, testCase.input)

			if !reflect.DeepEqual(got, testCase.want) {
				subtest.Errorf("%s\n got  %v\n want %v", testCase.input, got, testCase.want)
			}
		})
	}
}

// TestGraphTraversal_ParseErrors pins the messages for malformed hop counts.
func TestGraphTraversal_ParseErrors(test *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"a->{0} x=1", "a hop count starts at 1"},
		{"a->{0,2} x=1", "a hop count starts at 1"},
		{"a->{1,0} x=1", "a hop count starts at 1"},
		{"a->{1,-2} x=1", "a hop count starts at 1"},
		{"a->{-1} x=1", "a hop count starts at 1"},
		{"a->{3,1} x=1", "hop count {3,1} has its minimum above its maximum"},
		{"a->{51} x=1", "a hop count goes up to 50"},
		{"a->{1,51} x=1", "a hop count goes up to 50"},
		{"a->{} x=1", "expected a hop count like {2}, {1,3} or {2,}"},
		{"a->{,3} x=1", "expected a hop count like {2}, {1,3} or {2,}"},
		{"a->{x} x=1", "expected a hop count like {2}, {1,3} or {2,}"},
		{"a->{1 x=1", "expected a hop count like {2}, {1,3} or {2,}"},
		{"a-> + x=1", "unexpected character '+'"},
	}

	for _, testCase := range cases {
		_, errs := filter.NewParser(testCase.input).Parse()

		if len(errs) == 0 {
			test.Errorf("%q: no error, want %q", testCase.input, testCase.want)

			continue
		}

		if errs[0].Message != testCase.want {
			test.Errorf("%q: error %q, want %q", testCase.input, errs[0].Message, testCase.want)
		}
	}
}

// TestGraphTraversal_ParsesHopCounts pins the AST each arrow suffix produces.
func TestGraphTraversal_ParsesHopCounts(test *testing.T) {
	cases := []struct {
		input            string
		direction        filter.Direction
		minHops, maxHops int
	}{
		{"a-> x=1", filter.DirectionOutgoing, 0, 0},
		{"a->+ x=1", filter.DirectionOutgoing, 1, 0},
		{"a<-+ x=1", filter.DirectionIncoming, 1, 0},
		{"a->{2} x=1", filter.DirectionOutgoing, 2, 2},
		{"a->{1,3} x=1", filter.DirectionOutgoing, 1, 3},
		{"a<-{ 2 , 4 } x=1", filter.DirectionIncoming, 2, 4},
		{"a->{2,} x=1", filter.DirectionOutgoing, 2, 0},
		{"a->{50}", filter.DirectionOutgoing, 50, 50},
	}

	for _, testCase := range cases {
		expr, errs := filter.NewParser(testCase.input).Parse()

		if len(errs) > 0 {
			test.Errorf("%q: %v", testCase.input, errs)

			continue
		}

		pred, ok := expr.(*filter.EdgePredicate)

		if !ok {
			test.Errorf("%q: got %T, want *EdgePredicate", testCase.input, expr)

			continue
		}

		if pred.Direction != testCase.direction || pred.MinHops != testCase.minHops || pred.MaxHops != testCase.maxHops {
			test.Errorf("%q: direction %v hops {%d,%d}, want %v {%d,%d}", testCase.input, pred.Direction, pred.MinHops, pred.MaxHops, testCase.direction, testCase.minHops, testCase.maxHops)
		}
	}
}

// TestGraphTraversal_ValidateResolvesInverse pins that Validate rewrites an
// inverse label to its edge type with the direction flipped, keeps a source
// qualifier, and suggests an inverse label for a near miss.
func TestGraphTraversal_ValidateResolvesInverse(test *testing.T) {
	loaded := graphTraversalManifest()

	cases := []struct {
		input     string
		edgeType  string
		direction filter.Direction
	}{
		{"depended-on-by-> x=1", "depends-on", filter.DirectionIncoming},
		{"depended-on-by<- x=1", "depends-on", filter.DirectionOutgoing},
		{":depended-on-by->+ x=1", ":depends-on", filter.DirectionIncoming},
		{"depends-on-> x=1", "depends-on", filter.DirectionOutgoing},
	}

	for _, testCase := range cases {
		expr, _ := filter.NewParser(testCase.input).Parse()

		if errs := filter.Validate(expr, loaded); len(errs) > 0 {
			test.Errorf("%q: %v", testCase.input, errs)

			continue
		}

		pred := expr.(*filter.EdgePredicate)

		if pred.EdgeType != testCase.edgeType || pred.Direction != testCase.direction {
			test.Errorf("%q: got %q %v, want %q %v", testCase.input, pred.EdgeType, pred.Direction, testCase.edgeType, testCase.direction)
		}

		// Validating again leaves the resolved predicate as it is.
		if errs := filter.Validate(expr, loaded); len(errs) > 0 || pred.EdgeType != testCase.edgeType || pred.Direction != testCase.direction {
			test.Errorf("%q: second Validate changed it to %q %v (%v)", testCase.input, pred.EdgeType, pred.Direction, errs)
		}
	}

	expr, _ := filter.NewParser("depended-on-bye-> x=1").Parse()
	errs := filter.Validate(expr, loaded)

	if len(errs) != 1 || errs[0].Hint != `did you mean "depended-on-by"?` {
		test.Errorf("near miss: errs = %v, want a hint naming depended-on-by", errs)
	}
}
