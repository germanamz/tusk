package tui

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/filter"
)

func TestCollectUDAs(test *testing.T) {
	test.Run("empty FilterSet", func(test *testing.T) {
		fs := filter.FilterSet{}
		got, err := collectUDAs(&fs)

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		if got != nil {
			test.Fatalf("expected nil, got %v", got)
		}
	})

	test.Run("single uda field", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.env", Value: "prod"},
		}}
		got, err := collectUDAs(&fs)

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		if got["env"] != "prod" {
			test.Errorf("got %v, want prod", got["env"])
		}
		if len(got) != 1 {
			test.Errorf("got %d keys, want 1", len(got))
		}
	})

	test.Run("two uda fields", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.env", Value: "prod"},
			{Key: "uda.region", Value: "eu"},
		}}
		got, err := collectUDAs(&fs)

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		if got["env"] != "prod" || got["region"] != "eu" {
			test.Errorf("got %v", got)
		}
		if len(got) != 2 {
			test.Errorf("got %d keys, want 2", len(got))
		}
	})

	test.Run("duplicate last wins", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.env", Value: "a"},
			{Key: "uda.env", Value: "b"},
		}}
		got, err := collectUDAs(&fs)

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		if got["env"] != "b" {
			test.Errorf("got %v, want b", got["env"])
		}
		if len(got) != 1 {
			test.Errorf("got %d keys, want 1", len(got))
		}
	})

	test.Run("empty value", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.env", Value: ""},
		}}
		got, err := collectUDAs(&fs)

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		if got["env"] != "" {
			test.Errorf("got %v, want empty string", got["env"])
		}
	})

	test.Run("invalid tail digit-led", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.1env", Value: "x"},
		}}
		_, err := collectUDAs(&fs)
		if err == nil {
			test.Fatal("expected error, got nil")
		}
	})

	test.Run("empty tail", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.", Value: "x"},
		}}
		_, err := collectUDAs(&fs)
		if err == nil {
			test.Fatal("expected error, got nil")
		}
	})

	test.Run("dotted tail", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.a.b", Value: "x"},
		}}
		_, err := collectUDAs(&fs)
		if err == nil {
			test.Fatal("expected error, got nil")
		}
	})

	test.Run("modifier plus rejected", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.env", Value: "prod", Modifier: '+'},
		}}
		_, err := collectUDAs(&fs)
		if err == nil {
			test.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "modifier") {
			test.Errorf("error %q should contain 'modifier'", err)
		}
	})

	test.Run("modifier minus rejected", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.env", Value: "prod", Modifier: '-'},
		}}
		_, err := collectUDAs(&fs)
		if err == nil {
			test.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "modifier") {
			test.Errorf("error %q should contain 'modifier'", err)
		}
	})

	test.Run("mixed reserved and uda fields", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "title", Value: "t"},
			{Key: "uda.env", Value: "prod"},
			{Key: "priority", Value: "3"},
		}}
		got, err := collectUDAs(&fs)

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		if len(got) != 1 {
			test.Errorf("got %d keys, want 1", len(got))
		}
		if got["env"] != "prod" {
			test.Errorf("got %v, want prod", got["env"])
		}
	})
}

func TestValidateKnownFields(test *testing.T) {
	test.Run("empty FilterSet", func(test *testing.T) {
		fs := filter.FilterSet{}
		if err := validateKnownFields(&fs); err != nil {
			test.Fatalf("unexpected error: %v", err)
		}
	})

	test.Run("all reserved", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "title", Value: "t"},
			{Key: "project", Value: "p"},
			{Key: "priority", Value: "3"},
		}}
		if err := validateKnownFields(&fs); err != nil {
			test.Fatalf("unexpected error: %v", err)
		}
	})

	test.Run("all uda", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "uda.env", Value: "prod"},
			{Key: "uda.region", Value: "eu"},
		}}
		if err := validateKnownFields(&fs); err != nil {
			test.Fatalf("unexpected error: %v", err)
		}
	})

	test.Run("mixed reserved and uda", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "title", Value: "t"},
			{Key: "uda.env", Value: "prod"},
			{Key: "priority", Value: "3"},
		}}
		if err := validateKnownFields(&fs); err != nil {
			test.Fatalf("unexpected error: %v", err)
		}
	})

	test.Run("bare unknown with did-you-mean", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "env", Value: "prod"},
		}}
		err := validateKnownFields(&fs)
		if err == nil {
			test.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "unknown field") {
			test.Errorf("error %q should contain 'unknown field'", err)
		}
		if !strings.Contains(err.Error(), "did you mean uda.env?") {
			test.Errorf("error %q should contain 'did you mean uda.env?'", err)
		}
	})

	test.Run("bare unknown waiting", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "waiting", Value: "true"},
		}}
		err := validateKnownFields(&fs)
		if err == nil {
			test.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "did you mean uda.waiting?") {
			test.Errorf("error %q should contain 'did you mean uda.waiting?'", err)
		}
	})

	test.Run("dotted unknown no hint", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "foo.bar", Value: "1"},
		}}
		err := validateKnownFields(&fs)
		if err == nil {
			test.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "unknown field") {
			test.Errorf("error %q should contain 'unknown field'", err)
		}
		if strings.Contains(err.Error(), "did you mean") {
			test.Errorf("error %q should NOT contain 'did you mean'", err)
		}
	})

	test.Run("filter-only field tree rejected", func(test *testing.T) {
		fs := filter.FilterSet{Fields: []filter.FieldFilter{
			{Key: "tree", Value: "abc"},
		}}
		err := validateKnownFields(&fs)
		if err == nil {
			test.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "did you mean uda.tree?") {
			test.Errorf("error %q should contain 'did you mean uda.tree?'", err)
		}
	})
}
