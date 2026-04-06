package filter

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

// mockTaskLookup is a no-op implementation for tests that don't need task lookup.
type mockTaskLookup struct{}

func (m mockTaskLookup) GetByShortID(ctx context.Context, shortID string) (*domain.Task, error) {
	return &domain.Task{ID: uuid.New(), ShortID: shortID}, nil
}

func TestResolve_UDAField(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{})
	fs := &FilterSet{
		Fields: []FieldFilter{
			{Key: "uda.env", Value: "prod"},
		},
	}
	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.UDA == nil {
		t.Fatal("expected UDA filter to be set")
	}
	if tf.UDA["env"] != "prod" {
		t.Fatalf("expected env=prod, got %v", tf.UDA["env"])
	}
}

func TestResolve_UDAMultipleFields(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{})
	fs := &FilterSet{
		Fields: []FieldFilter{
			{Key: "uda.env", Value: "prod"},
			{Key: "uda.team", Value: "backend"},
		},
	}
	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(tf.UDA) != 2 {
		t.Fatalf("expected 2 UDA entries, got %d", len(tf.UDA))
	}
	if tf.UDA["env"] != "prod" || tf.UDA["team"] != "backend" {
		t.Fatalf("unexpected UDA: %v", tf.UDA)
	}
}

func TestResolve_UDAEmptyValue(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{})
	fs := &FilterSet{
		Fields: []FieldFilter{
			{Key: "uda.env", Value: ""},
		},
	}
	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.UDA == nil || tf.UDA["env"] != "" {
		t.Fatalf("expected UDA env with empty value, got %v", tf.UDA)
	}
}

func TestResolve_TitleContains(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{})
	fs := &FilterSet{
		Fields: []FieldFilter{
			{Key: "title", Value: "auth middleware"},
		},
	}
	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.TitleContains == nil || *tf.TitleContains != "auth middleware" {
		t.Fatalf("expected TitleContains=auth middleware, got %v", tf.TitleContains)
	}
}

func TestResolve_DescriptionContains(t *testing.T) {
	resolver := NewResolver(mockTaskLookup{})
	fs := &FilterSet{
		Fields: []FieldFilter{
			{Key: "description", Value: "implement feature"},
		},
	}
	tf, errs := resolver.Resolve(context.Background(), fs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if tf.DescriptionContains == nil || *tf.DescriptionContains != "implement feature" {
		t.Fatalf("expected DescriptionContains=implement feature, got %v", tf.DescriptionContains)
	}
}
