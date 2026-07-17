package graphview

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
)

func TestSSE_PushesOnSignalAdvance(t *testing.T) {
	changes := &fakeChanges{sig: Signal{Generation: 1}}
	nodes := &fakeNodes{files: []index.NodeRow{fileRow("notes/a", "note", "A", "")}}
	srv := New(Deps{Nodes: nodes, Edges: &fakeEdges{}, Changes: changes, PollInterval: 10 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)

	ts := httptest.NewServer(testHandler(srv))
	defer ts.Close()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/graph/stream", nil)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream GET: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)

	// Initial snapshot event on connect.
	if !waitForGraphEvent(t, reader) {
		t.Fatal("did not receive initial graph event")
	}

	// Advance the signal; expect a second push.
	changes.setSig(Signal{Generation: 2})

	if !waitForGraphEvent(t, reader) {
		t.Fatal("did not receive graph event after signal advance")
	}
}

// waitForGraphEvent reads lines until it sees "event: graph" or times out.
func waitForGraphEvent(t *testing.T, reader *bufio.Reader) bool {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			return false
		}

		if strings.HasPrefix(line, "event: graph") {
			return true
		}
	}

	return false
}
