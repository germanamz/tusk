package main

import (
	"context"
	"testing"
	"time"
)

func TestConsoleLoop_QuitKeyCancels(test *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cancelled := false
	keys := make(chan rune, 1)
	keys <- 'q'

	done := make(chan struct{})
	go func() {
		consoleLoop(ctx, func() { cancelled = true }, keys, func() {}, func() {})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		test.Fatal("consoleLoop did not return on quit key (deadlock)")
	}

	if !cancelled {
		test.Fatal("consoleLoop did not call cancel on quit key")
	}
}

func TestConsoleLoop_CtxDoneReturns(test *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	keys := make(chan rune) // never sends

	done := make(chan struct{})
	go func() {
		consoleLoop(ctx, cancel, keys, func() {}, func() {})
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		test.Fatal("consoleLoop did not return on ctx cancel")
	}
}

func TestConsoleLoop_SpaceOpens(test *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opened := 0
	keys := make(chan rune, 2)
	keys <- ' '
	keys <- 'q'

	consoleLoop(ctx, cancel, keys, func() {}, func() { opened++ })

	if opened != 1 {
		test.Fatalf("openURL called %d times, want 1", opened)
	}
}
