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
