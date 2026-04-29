package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestParseTaxonomyInline_AllSingle(test *testing.T) {
	got, parseErr := ParseTaxonomyInline("milestone:story:task")

	if parseErr != nil {
		test.Fatalf("parse: %v", parseErr)
	}

	want := domain.Taxonomy{{"milestone"}, {"story"}, {"task"}}
	if !reflect.DeepEqual(got, want) {
		test.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseTaxonomyInline_Mixed(test *testing.T) {
	got, parseErr := ParseTaxonomyInline("milestone:initiative:story:(task,spike)")

	if parseErr != nil {
		test.Fatalf("parse: %v", parseErr)
	}

	want := domain.Taxonomy{
		{"milestone"},
		{"initiative"},
		{"story"},
		{"task", "spike"},
	}
	if !reflect.DeepEqual(got, want) {
		test.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseTaxonomyInline_AllMulti(test *testing.T) {
	got, parseErr := ParseTaxonomyInline("(a,b):(c,d)")

	if parseErr != nil {
		test.Fatalf("parse: %v", parseErr)
	}

	want := domain.Taxonomy{{"a", "b"}, {"c", "d"}}
	if !reflect.DeepEqual(got, want) {
		test.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseTaxonomyInline_Empty(test *testing.T) {
	got, parseErr := ParseTaxonomyInline("")

	if parseErr != nil {
		test.Fatalf("parse: %v", parseErr)
	}

	if !got.IsEmpty() {
		test.Fatalf("expected empty taxonomy, got %+v", got)
	}
}

func TestParseTaxonomyInline_EmptyWhitespace(test *testing.T) {
	got, parseErr := ParseTaxonomyInline("   ")

	if parseErr != nil {
		test.Fatalf("parse: %v", parseErr)
	}

	if !got.IsEmpty() {
		test.Fatalf("expected empty taxonomy, got %+v", got)
	}
}

func TestParseTaxonomyInline_WhitespaceTolerance(test *testing.T) {
	got, parseErr := ParseTaxonomyInline(" milestone : (task , spike) ")

	if parseErr != nil {
		test.Fatalf("parse: %v", parseErr)
	}

	want := domain.Taxonomy{{"milestone"}, {"task", "spike"}}
	if !reflect.DeepEqual(got, want) {
		test.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseTaxonomyInline_Rejects(test *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"unmatched_open", "milestone:(task,spike"},
		{"unmatched_close", "milestone:task,spike)"},
		{"empty_group", "milestone:()"},
		{"duplicate_name", "milestone:milestone"},
		{"duplicate_in_group", "milestone:(task,task)"},
		{"invalid_character", "milestone:(task,sp!ke)"},
		{"empty_peer", "milestone:(task,)"},
		{"double_colon", "a::b"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			_, err := ParseTaxonomyInline(testCase.input)
			if err == nil {
				test.Fatalf("expected error for %q", testCase.input)
			}
		})
	}
}

func TestFormatTaxonomyInline_Empty(test *testing.T) {
	if got := FormatTaxonomyInline(domain.Taxonomy{}); got != "" {
		test.Fatalf("got %q, want empty", got)
	}
	if got := FormatTaxonomyInline(nil); got != "" {
		test.Fatalf("got %q, want empty", got)
	}
}

func TestFormatTaxonomyInline_Mixed(test *testing.T) {
	test.Run("single_then_multi", func(test *testing.T) {
		got := FormatTaxonomyInline(domain.Taxonomy{
			{"milestone"},
			{"initiative"},
			{"story"},
			{"task", "spike"},
		})
		want := "milestone:initiative:story:(task,spike)"
		if got != want {
			test.Fatalf("got %q, want %q", got, want)
		}
	})
}

func TestTaxonomyInline_RoundTrip(test *testing.T) {
	cases := []domain.Taxonomy{
		{{"milestone"}, {"story"}, {"task"}},
		{{"milestone"}, {"story"}, {"task", "spike"}},
		{{"a", "b"}, {"c", "d"}},
	}
	for _, taxonomy := range cases {
		test.Run(strings.Join(flatten(taxonomy), "_"), func(test *testing.T) {
			got, roundTripErr := ParseTaxonomyInline(FormatTaxonomyInline(taxonomy))

			if roundTripErr != nil {
				test.Fatalf("parse: %v", roundTripErr)
			}

			if !reflect.DeepEqual(got, taxonomy) {
				test.Fatalf("round-trip mismatch: got %+v, want %+v", got, taxonomy)
			}
		})
	}
}

func flatten(taxonomy domain.Taxonomy) []string {
	var out []string
	for _, peers := range taxonomy {
		out = append(out, peers...)
	}
	return out
}
