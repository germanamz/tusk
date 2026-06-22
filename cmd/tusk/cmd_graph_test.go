package main

import (
	"context"
	"net/http"
	"testing"
)

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
