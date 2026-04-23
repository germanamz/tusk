package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestParseTaxonomyInline_AllSingle(t *testing.T) {
	got, err := ParseTaxonomyInline("milestone:story:task")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := domain.Taxonomy{{"milestone"}, {"story"}, {"task"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseTaxonomyInline_Mixed(t *testing.T) {
	got, err := ParseTaxonomyInline("milestone:initiative:story:(task,spike)")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := domain.Taxonomy{
		{"milestone"},
		{"initiative"},
		{"story"},
		{"task", "spike"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseTaxonomyInline_AllMulti(t *testing.T) {
	got, err := ParseTaxonomyInline("(a,b):(c,d)")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := domain.Taxonomy{{"a", "b"}, {"c", "d"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseTaxonomyInline_Empty(t *testing.T) {
	got, err := ParseTaxonomyInline("")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !got.IsEmpty() {
		t.Fatalf("expected empty taxonomy, got %+v", got)
	}
}

func TestParseTaxonomyInline_EmptyWhitespace(t *testing.T) {
	got, err := ParseTaxonomyInline("   ")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !got.IsEmpty() {
		t.Fatalf("expected empty taxonomy, got %+v", got)
	}
}

func TestParseTaxonomyInline_WhitespaceTolerance(t *testing.T) {
	got, err := ParseTaxonomyInline(" milestone : (task , spike) ")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := domain.Taxonomy{{"milestone"}, {"task", "spike"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseTaxonomyInline_Rejects(t *testing.T) {
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
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseTaxonomyInline(tc.input)
			if err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
		})
	}
}

func TestFormatTaxonomyInline_Empty(t *testing.T) {
	if got := FormatTaxonomyInline(domain.Taxonomy{}); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
	if got := FormatTaxonomyInline(nil); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestFormatTaxonomyInline_Mixed(t *testing.T) {
	t.Run("single_then_multi", func(t *testing.T) {
		got := FormatTaxonomyInline(domain.Taxonomy{
			{"milestone"},
			{"initiative"},
			{"story"},
			{"task", "spike"},
		})
		want := "milestone:initiative:story:(task,spike)"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

func TestTaxonomyInline_RoundTrip(t *testing.T) {
	cases := []domain.Taxonomy{
		{{"milestone"}, {"story"}, {"task"}},
		{{"milestone"}, {"story"}, {"task", "spike"}},
		{{"a", "b"}, {"c", "d"}},
	}
	for _, tax := range cases {
		t.Run(strings.Join(flatten(tax), "_"), func(t *testing.T) {
			got, err := ParseTaxonomyInline(FormatTaxonomyInline(tax))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !reflect.DeepEqual(got, tax) {
				t.Fatalf("round-trip mismatch: got %+v, want %+v", got, tax)
			}
		})
	}
}

func flatten(t domain.Taxonomy) []string {
	var out []string
	for _, peers := range t {
		out = append(out, peers...)
	}
	return out
}
