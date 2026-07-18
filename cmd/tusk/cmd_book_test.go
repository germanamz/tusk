package main

import (
	"context"
	"net/http"
	"testing"
)

func TestBookServesHealthz(test *testing.T) {
	root := initWorkspace(test) // leaves cwd inside the workspace
	createNode(test, root, "notes/a.md", "note", "A", "")

	ctx, cancel := context.WithCancel(context.Background())

	addrCh := make(chan string, 1)
	rootCmd := newRootCmd()
	bookCmd, _, _ := rootCmd.Find([]string{"book"})

	cfg := webWebUIConfig("127.0.0.1:0", false, "read")
	cfg.ready = func(addr string) { addrCh <- addr }

	errCh := make(chan error, 1)
	go func() {
		errCh <- serveWebUI(ctx, bookCmd, cfg)
	}()

	addr := <-addrCh

	healthzResp, healthzErr := http.Get("http://" + addr + "/healthz")
	if healthzErr != nil {
		test.Fatalf("GET /healthz: %v", healthzErr)
	}
	_ = healthzResp.Body.Close()

	if healthzResp.StatusCode != http.StatusOK {
		test.Fatalf("GET /healthz status = %d, want 200", healthzResp.StatusCode)
	}

	indexResp, indexErr := http.Get("http://" + addr + "/api/read/index")
	if indexErr != nil {
		test.Fatalf("GET /api/read/index: %v", indexErr)
	}
	_ = indexResp.Body.Close()

	if indexResp.StatusCode != http.StatusOK {
		test.Fatalf("GET /api/read/index status = %d, want 200", indexResp.StatusCode)
	}

	cancel()

	if serveErr := <-errCh; serveErr != nil {
		test.Fatalf("serveWebUI returned: %v", serveErr)
	}
}
