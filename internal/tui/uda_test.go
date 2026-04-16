package tui

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/filter"
)

func TestParseUDAFlags_Valid(t *testing.T) {
	cases := []struct {
		name   string
		input  []string
		expect map[string]any
	}{
		{"single", []string{"env=prod"}, map[string]any{"env": "prod"}},
		{"multiple", []string{"env=prod", "team=backend"}, map[string]any{"env": "prod", "team": "backend"}},
		{"empty value clears", []string{"env="}, map[string]any{"env": ""}},
		{"value with equals", []string{"desc=a=b"}, map[string]any{"desc": "a=b"}},
		{"duplicate last wins", []string{"env=dev", "env=prod"}, map[string]any{"env": "prod"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseUDAFlags(tc.input)
			if err != nil {
				t.Fatalf("parseUDAFlags(%v) error: %v", tc.input, err)
			}
			for k, want := range tc.expect {
				if got[k] != want {
					t.Errorf("key %q: got %v, want %v", k, got[k], want)
				}
			}
			if len(got) != len(tc.expect) {
				t.Errorf("got %d keys, want %d", len(got), len(tc.expect))
			}
		})
	}
}

func TestParseUDAFlags_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		input []string
	}{
		{"no equals", []string{"invalid"}},
		{"empty key", []string{"=value"}},
		{"invalid key chars", []string{"my.key=value"}},
		{"key starts with digit", []string{"1key=value"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseUDAFlags(tc.input)
			if err == nil {
				t.Fatalf("parseUDAFlags(%v) = nil error, want error", tc.input)
			}
		})
	}
}

func TestParseUDAFlags_Empty(t *testing.T) {
	got, err := parseUDAFlags(nil)
	if err != nil {
		t.Fatalf("parseUDAFlags(nil) error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestCollectUDAs(t *testing.T) {
	t.Run("empty FilterSet", func(t *testing.T) {
		fs := filter.FilterSet{}
		got, err := collectUDAs(&fs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})

	t.Run("single uda field", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.env", Value: "prod"},
		}}
		got, err := collectUDAs(&fs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["env"] != "prod" {
			t.Errorf("got %v, want prod", got["env"])
		}
		if len(got) != 1 {
			t.Errorf("got %d keys, want 1", len(got))
		}
	})

	t.Run("two uda fields", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.env", Value: "prod"},
			{Key: "uda.region", Value: "eu"},
		}}
		got, err := collectUDAs(&fs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["env"] != "prod" || got["region"] != "eu" {
			t.Errorf("got %v", got)
		}
		if len(got) != 2 {
			t.Errorf("got %d keys, want 2", len(got))
		}
	})

	t.Run("duplicate last wins", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.env", Value: "a"},
			{Key: "uda.env", Value: "b"},
		}}
		got, err := collectUDAs(&fs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["env"] != "b" {
			t.Errorf("got %v, want b", got["env"])
		}
		if len(got) != 1 {
			t.Errorf("got %d keys, want 1", len(got))
		}
	})

	t.Run("empty value", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.env", Value: ""},
		}}
		got, err := collectUDAs(&fs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["env"] != "" {
			t.Errorf("got %v, want empty string", got["env"])
		}
	})

	t.Run("invalid tail digit-led", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.1env", Value: "x"},
		}}
		_, err := collectUDAs(&fs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("empty tail", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.", Value: "x"},
		}}
		_, err := collectUDAs(&fs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("dotted tail", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.a.b", Value: "x"},
		}}
		_, err := collectUDAs(&fs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("modifier plus rejected", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.env", Value: "prod", Modifier: '+'},
		}}
		_, err := collectUDAs(&fs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "modifier") {
			t.Errorf("error %q should contain 'modifier'", err)
		}
	})

	t.Run("modifier minus rejected", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.env", Value: "prod", Modifier: '-'},
		}}
		_, err := collectUDAs(&fs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "modifier") {
			t.Errorf("error %q should contain 'modifier'", err)
		}
	})

	t.Run("mixed reserved and uda fields", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "title", Value: "t"},
			{Key: "uda.env", Value: "prod"},
			{Key: "priority", Value: "3"},
		}}
		got, err := collectUDAs(&fs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("got %d keys, want 1", len(got))
		}
		if got["env"] != "prod" {
			t.Errorf("got %v, want prod", got["env"])
		}
	})
}

func TestValidateKnownFields(t *testing.T) {
	t.Run("empty FilterSet", func(t *testing.T) {
		fs := filter.FilterSet{}
		if err := validateKnownFields(&fs); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("all reserved", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "title", Value: "t"},
			{Key: "project", Value: "p"},
			{Key: "priority", Value: "3"},
		}}
		if err := validateKnownFields(&fs); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("all uda", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.env", Value: "prod"},
			{Key: "uda.region", Value: "eu"},
		}}
		if err := validateKnownFields(&fs); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("mixed reserved and uda", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "title", Value: "t"},
			{Key: "uda.env", Value: "prod"},
			{Key: "priority", Value: "3"},
		}}
		if err := validateKnownFields(&fs); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("bare unknown with did-you-mean", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "env", Value: "prod"},
		}}
		err := validateKnownFields(&fs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "unknown field") {
			t.Errorf("error %q should contain 'unknown field'", err)
		}
		if !strings.Contains(err.Error(), "did you mean uda.env?") {
			t.Errorf("error %q should contain 'did you mean uda.env?'", err)
		}
	})

	t.Run("bare unknown waiting", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "waiting", Value: "true"},
		}}
		err := validateKnownFields(&fs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "did you mean uda.waiting?") {
			t.Errorf("error %q should contain 'did you mean uda.waiting?'", err)
		}
	})

	t.Run("dotted unknown no hint", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "foo.bar", Value: "1"},
		}}
		err := validateKnownFields(&fs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "unknown field") {
			t.Errorf("error %q should contain 'unknown field'", err)
		}
		if strings.Contains(err.Error(), "did you mean") {
			t.Errorf("error %q should NOT contain 'did you mean'", err)
		}
	})

	t.Run("filter-only field tree rejected", func(t *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "tree", Value: "abc"},
		}}
		err := validateKnownFields(&fs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "did you mean uda.tree?") {
			t.Errorf("error %q should contain 'did you mean uda.tree?'", err)
		}
	})
}
