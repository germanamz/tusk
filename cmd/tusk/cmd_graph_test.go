package main

import (
	"context"
	"io"
	"net/http"
	"slices"
	"testing"
	"time"
)

func TestGraphAllowedHosts(test *testing.T) {
	cases := []struct {
		addr string
		want []string
	}{
		{"127.0.0.1:7373", nil},
		{"localhost:7373", nil},
		{"[::1]:7373", nil},
		{":7373", []string{"*"}},
		{"0.0.0.0:7373", []string{"*"}},
		{"192.168.1.5:7373", []string{"192.168.1.5"}},
	}

	for _, testCase := range cases {
		if got := graphAllowedHosts(testCase.addr); !slices.Equal(got, testCase.want) {
			test.Errorf("graphAllowedHosts(%q) = %v, want %v", testCase.addr, got, testCase.want)
		}
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:7373": true,
		"localhost:7373": true,
		"[::1]:7373":     true,
		"0.0.0.0:7373":   false,
		"192.168.1.5:80": false,
		":7373":          false, // all-interfaces
	}

	for addr, want := range cases {
		if got := isLoopbackAddr(addr); got != want {
			t.Errorf("isLoopbackAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestServeGraph_Integration(t *testing.T) {
	root := initWorkspace(t) // leaves cwd inside the workspace
	createNode(t, root, "notes/a.md", "note", "A", "")

	ctx, cancel := context.WithCancel(context.Background())

	addrCh := make(chan string, 1)
	rootCmd := newRootCmd()
	graphCmd, _, _ := rootCmd.Find([]string{"graph"})

	errCh := make(chan error, 1)
	go func() {
		errCh <- serveGraph(ctx, graphCmd, graphConfig{
			addr:  "127.0.0.1:0",
			ready: func(addr string) { addrCh <- addr },
		})
	}()

	addr := <-addrCh

	resp, err := http.Get("http://" + addr + "/api/graph")
	if err != nil {
		t.Fatalf("GET /api/graph: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	cancel()

	if serveErr := <-errCh; serveErr != nil {
		t.Fatalf("serveGraph returned: %v", serveErr)
	}
}

func TestConsoleLoop_QuitKeyCancels(test *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cancelled := false
	keys := make(chan rune, 1)
	keys <- 'q'

	done := make(chan struct{})
	go func() {
		consoleLoop(ctx, func() { cancelled = true }, io.Discard, keys, func() {}, func() {})
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
		consoleLoop(ctx, cancel, io.Discard, keys, func() {}, func() {})
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

	consoleLoop(ctx, cancel, io.Discard, keys, func() {}, func() { opened++ })

	if opened != 1 {
		test.Fatalf("openURL called %d times, want 1", opened)
	}
}
