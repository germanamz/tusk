package filter_test

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/filter"
)

func TestParseSort_Basic(test *testing.T) {
	cases := []struct {
		input string
		want  []filter.SortKey
	}{
		{"+priority", []filter.SortKey{{Property: "priority", Descending: false}}},
		{"-priority", []filter.SortKey{{Property: "priority", Descending: true}}},
		{"priority", []filter.SortKey{{Property: "priority", Descending: false}}},
		{"+priority,-due", []filter.SortKey{
			{Property: "priority", Descending: false},
			{Property: "due", Descending: true},
		}},
		{"+priority, -due, +modified", []filter.SortKey{
			{Property: "priority", Descending: false},
			{Property: "due", Descending: true},
			{Property: "modified", Descending: false},
		}},
	}

	for _, tc := range cases {
		got, err := filter.ParseSort(tc.input)

		if err != nil {
			test.Errorf("input %q: error %v", tc.input, err)

			continue
		}

		if !reflect.DeepEqual(got, tc.want) {
			test.Errorf("input %q: got %+v, want %+v", tc.input, got, tc.want)
		}
	}
}

func TestParseSort_EmptyReturnsEmptySlice(test *testing.T) {
	got, err := filter.ParseSort("")

	if err != nil {
		test.Errorf("error: %v", err)
	}

	if len(got) != 0 {
		test.Errorf("got %+v, want empty", got)
	}
}

func TestParseSort_RejectsEmptyKey(test *testing.T) {
	_, err := filter.ParseSort("+priority,,-due")

	if err == nil {
		test.Errorf("expected error for empty key")
	}
}

func TestParseSort_RejectsBareSign(test *testing.T) {
	_, err := filter.ParseSort("+,-due")

	if err == nil {
		test.Errorf("expected error for bare sign")
	}
}

func TestParseSort_RejectsUnsafePropertyNames(test *testing.T) {
	// Sort property names are interpolated into ORDER BY json_extract(...),
	// so anything outside the identifier charset must be rejected at the
	// boundary rather than reaching the SQL string.
	cases := []string{
		`priority') OR 1=1 --`,
		`foo'); DROP TABLE nodes;--`,
		`due DESC, (SELECT 1)`,
		`has space`,
		`with.dot`,
		`with/slash`,
		`with:colon`,
		`"quoted"`,
	}

	for _, input := range cases {
		if _, err := filter.ParseSort(input); err == nil {
			test.Errorf("input %q: expected error for unsafe property name", input)
		}
	}
}

func TestParseSort_AcceptsIdentifierCharset(test *testing.T) {
	// Hyphens, underscores, and digits are valid in property names.
	got, err := filter.ParseSort("+decided-at,-snake_case,field2")
	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	want := []filter.SortKey{
		{Property: "decided-at", Descending: false},
		{Property: "snake_case", Descending: true},
		{Property: "field2", Descending: false},
	}

	if !reflect.DeepEqual(got, want) {
		test.Errorf("got %+v, want %+v", got, want)
	}
}
