package bookview

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/epoch"
)

// fakeMeta is a read-only webui.MetaReader over a fixed key/value map. It holds
// no mutable state, so it is safe to share across the Hub's concurrent
// Payload() calls under -race.
type fakeMeta struct {
	values map[string]string
	err    error
}

func (meta fakeMeta) Get(key string) (string, error) {
	if meta.err != nil {
		return "", meta.err
	}

	return meta.values[key], nil
}

func TestHealthzAndStatic(test *testing.T) {
	srv := New(Deps{Root: test.TempDir()})

	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	healthResp, healthErr := http.Get(server.URL + "/healthz")

	if healthErr != nil {
		test.Fatalf("healthz: %v", healthErr)
	}

	defer healthResp.Body.Close()

	if healthResp.StatusCode != http.StatusOK {
		test.Fatalf("healthz code=%d want 200", healthResp.StatusCode)
	}

	healthBody, readErr := io.ReadAll(healthResp.Body)

	if readErr != nil {
		test.Fatalf("healthz body: %v", readErr)
	}

	if string(healthBody) != "ok" {
		test.Fatalf("healthz body=%q want %q", healthBody, "ok")
	}

	indexResp, indexErr := http.Get(server.URL + "/")

	if indexErr != nil {
		test.Fatalf("static index: %v", indexErr)
	}

	defer indexResp.Body.Close()

	if indexResp.StatusCode != http.StatusOK {
		test.Fatalf("static index code=%d want 200", indexResp.StatusCode)
	}

	if csp := indexResp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		test.Fatalf("missing/loose CSP: %q", csp)
	}
}

// TestChangePayload pins the SSE frame body the frontend's EventSource parses.
func TestChangePayload(test *testing.T) {
	root := test.TempDir()

	if _, bumpErr := epoch.Index.Bump(root); bumpErr != nil {
		test.Fatalf("bump epoch: %v", bumpErr)
	}

	srv := New(Deps{Root: root, Meta: fakeMeta{values: map[string]string{"reindex_gen": "7"}}})

	payload, payloadErr := srv.changePayload()

	if payloadErr != nil {
		test.Fatalf("changePayload: %v", payloadErr)
	}

	if string(payload) != `{"generation":7,"epoch":1}` {
		test.Fatalf("payload=%s want {\"generation\":7,\"epoch\":1}", payload)
	}
}

// TestChangePayloadWithoutMeta pins the nil-Meta path: webui.Hub calls Payload
// on every connect and dereferences ChangeSource without a nil check, so a Deps
// carrying no MetaReader must still produce a well-formed frame instead of
// panicking.
func TestChangePayloadWithoutMeta(test *testing.T) {
	srv := New(Deps{Root: test.TempDir()})

	payload, payloadErr := srv.changePayload()

	if payloadErr != nil {
		test.Fatalf("changePayload: %v", payloadErr)
	}

	if string(payload) != `{"generation":0,"epoch":0}` {
		test.Fatalf("payload=%s want a zero signal", payload)
	}
}

// TestChangePayloadPropagatesError keeps a failing meta read from broadcasting
// a bogus zero signal: the Hub skips the frame when Payload errors.
func TestChangePayloadPropagatesError(test *testing.T) {
	wantErr := errors.New("meta unavailable")
	srv := New(Deps{Root: test.TempDir(), Meta: fakeMeta{err: wantErr}})

	_, payloadErr := srv.changePayload()

	if !errors.Is(payloadErr, wantErr) {
		test.Fatalf("changePayload err=%v want %v", payloadErr, wantErr)
	}
}

// TestStreamEmitsChangeEvent pins the SSE event name "change" end-to-end
// through GET /api/stream. server.go wires webui.Hub with EventName: "change"
// (graph's hub broadcasts "graph"); nothing else in this package asserts that
// literal, and a Phase 5 TypeScript EventSource is coded against it verbatim —
// a copy-paste regression here would silently stop the frontend from ever
// updating.
//
// This also demonstrates TestChangePayloadWithoutMeta's stated premise for
// real: that test only calls changePayload() directly. webui.Hub calls
// Payload on every connecting client with no nil guard on the ChangeSource, so
// the actual risk is a real client hitting /api/stream against a Deps with no
// MetaReader. This test drives that exact path instead of the payload
// function in isolation.
func TestStreamEmitsChangeEvent(test *testing.T) {
	srv := New(Deps{Root: test.TempDir()})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)

	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	resp, getErr := http.Get(server.URL + "/api/stream")

	if getErr != nil {
		test.Fatalf("get /api/stream: %v", getErr)
	}

	defer resp.Body.Close()

	frame := readSSEFrame(test, bufio.NewReader(resp.Body))

	if !strings.Contains(frame, "event: change") {
		test.Fatalf("frame missing event name: %q", frame)
	}
}

// readSSEFrame reads one SSE frame (lines up to and including the blank line
// that terminates it) from reader. The read runs in a goroutine raced against
// a deadline so a frame that never arrives fails the test with a clear
// message instead of hanging the suite.
func readSSEFrame(test *testing.T, reader *bufio.Reader) string {
	test.Helper()

	type readResult struct {
		frame string
		err   error
	}

	resultCh := make(chan readResult, 1)

	go func() {
		var frame strings.Builder

		for {
			line, readErr := reader.ReadString('\n')
			frame.WriteString(line)

			if readErr != nil {
				resultCh <- readResult{frame: frame.String(), err: readErr}

				return
			}

			if line == "\n" {
				resultCh <- readResult{frame: frame.String()}

				return
			}
		}
	}()

	select {
	case result := <-resultCh:
		if result.err != nil {
			test.Fatalf("readSSEFrame: %v (partial frame: %q)", result.err, result.frame)
		}

		return result.frame
	case <-time.After(5 * time.Second):
		test.Fatal("readSSEFrame: timed out waiting for an SSE frame")

		return ""
	}
}

func TestWriteJSON(test *testing.T) {
	rec := httptest.NewRecorder()

	writeJSON(rec, IndexResponse{Nodes: []IndexNode{{ID: "a", Title: "A"}}})

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		test.Fatalf("content-type=%q want application/json", got)
	}

	var got IndexResponse

	if unmarshalErr := json.Unmarshal(rec.Body.Bytes(), &got); unmarshalErr != nil {
		test.Fatalf("unmarshal %q: %v", rec.Body.Bytes(), unmarshalErr)
	}

	if len(got.Nodes) != 1 || got.Nodes[0].ID != "a" {
		test.Fatalf("got %+v", got.Nodes)
	}
}
