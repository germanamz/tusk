package node_test

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/node"
)

// An unquoted frontmatter wikilink `key: [[id]]` parses in YAML as a nested
// flow sequence `[]any{[]any{"id"}}`, not a string. Left as-is, ResolveEdges
// rejects the shape and the whole file is dropped from the index (#692). Tusk
// has no nested-list property type, so the shape is never a legitimate value;
// ParseFile normalizes it back to the scalar string the human wrote, letting
// the wikilink resolve exactly like its quoted twin.
func TestParseFile_NormalizesUnquotedWikilinkSequence(test *testing.T) {
	cases := []struct {
		name string
		key  string
		line string
		want any
	}{
		{
			name: "bare unquoted wikilink",
			key:  "assignee",
			line: "assignee: [[people/jane]]",
			want: "[[people/jane]]",
		},
		{
			name: "aliased unquoted wikilink",
			key:  "assignee",
			line: "assignee: [[people/jane|Janey]]",
			want: "[[people/jane|Janey]]",
		},
		{
			name: "sub-unit deep link",
			key:  "assignee",
			line: "assignee: [[people/jane#S1]]",
			want: "[[people/jane#S1]]",
		},
		{
			name: "flat list is untouched",
			key:  "tags",
			line: "tags: [a, b]",
			want: []any{"a", "b"},
		},
		{
			name: "nested list of two is untouched",
			key:  "weird",
			line: "weird: [[a], [b]]",
			want: []any{[]any{"a"}, []any{"b"}},
		},
		{
			name: "plain scalar is untouched",
			key:  "note",
			line: "note: hello",
			want: "hello",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			content := []byte("---\ntype: ticket\n" + testCase.line + "\n---\n\nbody\n")

			parsed, parseErr := node.ParseFile("tickets/x.md", content)

			if parseErr != nil {
				subtest.Fatalf("ParseFile: %v", parseErr)
			}

			got := parsed.Properties[testCase.key]

			if !reflect.DeepEqual(got, testCase.want) {
				subtest.Errorf("Properties[%q] = %#v (%T), want %#v", testCase.key, got, got, testCase.want)
			}
		})
	}
}
