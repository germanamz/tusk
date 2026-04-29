package service

import (
	"context"
	"testing"
)

func TestActorFromContext_Empty(test *testing.T) {
	if got := ActorFromContext(context.Background()); got != nil {
		test.Fatalf("expected nil, got %v", *got)
	}
}

func TestWithActor_EmptyString(test *testing.T) {
	base := context.Background()
	ctx := WithActor(base, "")
	if ctx != base {
		test.Fatalf("WithActor with empty playerID should return the same ctx")
	}
	if got := ActorFromContext(ctx); got != nil {
		test.Fatalf("expected nil, got %v", *got)
	}
}

func TestWithActor_RoundTrip(test *testing.T) {
	ctx := WithActor(context.Background(), "german")
	got := ActorFromContext(ctx)
	if got == nil {
		test.Fatal("expected non-nil *string")
	}
	if *got != "german" {
		test.Fatalf("expected \"german\", got %q", *got)
	}
}
