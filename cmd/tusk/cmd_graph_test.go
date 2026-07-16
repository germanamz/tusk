package main

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/mcp"
)

func TestServeGraph_Integration(test *testing.T) {
	root := initWorkspace(test) // leaves cwd inside the workspace
	createNode(test, root, "notes/a.md", "note", "A", "")

	ctx, cancel := context.WithCancel(context.Background())

	addrCh := make(chan string, 1)
	rootCmd := newRootCmd()
	graphCmd, _, _ := rootCmd.Find([]string{"graph"})

	cfg := graphWebUIConfig("127.0.0.1:0", false)
	cfg.ready = func(addr string) { addrCh <- addr }

	errCh := make(chan error, 1)
	go func() {
		errCh <- serveWebUI(ctx, graphCmd, cfg)
	}()

	addr := <-addrCh

	resp, err := http.Get("http://" + addr + "/api/graph")
	if err != nil {
		test.Fatalf("GET /api/graph: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		test.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	cancel()

	if serveErr := <-errCh; serveErr != nil {
		test.Fatalf("serveWebUI returned: %v", serveErr)
	}
}

func TestFormatStatus(test *testing.T) {
	cases := []struct {
		name         string
		snap         mcp.WalkStatusSnapshot
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:         "walking shows an active indicator",
			snap:         mcp.WalkStatusSnapshot{Walking: true},
			wantContains: []string{"indexing…", "2 nodes", "3 edges", "1 clients"},
			wantAbsent:   []string{"synced", "index gen", "last walk"},
		},
		{
			name: "idle synced with a completed no-op walk",
			snap: mcp.WalkStatusSnapshot{
				EverWalked: true,
				Last:       mcp.WalkSummary{DurationMs: 2},
			},
			wantContains: []string{"synced", "last walk 2ms (0 changed)"},
			wantAbsent:   []string{"indexing", "index gen", "walk error"},
		},
		{
			name: "idle synced summarizes real changes",
			snap: mcp.WalkStatusSnapshot{
				EverWalked: true,
				Last:       mcp.WalkSummary{Indexed: 3, Removed: 1, DurationMs: 12},
			},
			wantContains: []string{"synced", "last walk 12ms (4 changed)"},
		},
		{
			name: "failed walk is not reported as synced",
			snap: mcp.WalkStatusSnapshot{
				EverWalked: true,
				Last:       mcp.WalkSummary{Err: "read error"},
			},
			wantContains: []string{"walk error"},
			wantAbsent:   []string{"synced", "last walk"},
		},
		{
			name:         "before the first walk it is not stuck",
			snap:         mcp.WalkStatusSnapshot{},
			wantContains: []string{"synced"},
			wantAbsent:   []string{"index gen", "last walk", "indexing"},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			got := formatStatus(testCase.snap, 2, 3, 1)

			for _, want := range testCase.wantContains {
				if !strings.Contains(got, want) {
					subtest.Errorf("formatStatus = %q, want it to contain %q", got, want)
				}
			}

			for _, absent := range testCase.wantAbsent {
				if strings.Contains(got, absent) {
					subtest.Errorf("formatStatus = %q, want it to NOT contain %q", got, absent)
				}
			}
		})
	}
}
