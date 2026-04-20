package service

import (
	"context"
	"testing"
)

func TestActorFromContext_Empty(t *testing.T) {
	if got := ActorFromContext(context.Background()); got != nil {
		t.Fatalf("expected nil, got %v", *got)
	}
}

func TestWithActor_EmptyString(t *testing.T) {
	base := context.Background()
	ctx := WithActor(base, "")
	if ctx != base {
		t.Fatalf("WithActor with empty playerID should return the same ctx")
	}
	if got := ActorFromContext(ctx); got != nil {
		t.Fatalf("expected nil, got %v", *got)
	}
}

func TestWithActor_RoundTrip(t *testing.T) {
	ctx := WithActor(context.Background(), "german")
	got := ActorFromContext(ctx)
	if got == nil {
		t.Fatal("expected non-nil *string")
	}
	if *got != "german" {
		t.Fatalf("expected \"german\", got %q", *got)
	}
}
