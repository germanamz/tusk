package webui

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type stubChanges struct{ gen atomic.Int64 }

func (stub *stubChanges) Signal() (Signal, error) { return Signal{Generation: stub.gen.Load()}, nil }

func TestHubStreamsInitialAndBroadcast(test *testing.T) {
	changes := &stubChanges{}
	hub := NewHub(HubOptions{
		EventName:    "change",
		Payload:      func() ([]byte, error) { return []byte(`{"generation":` + itoa(changes.gen.Load()) + `}`), nil },
		Changes:      changes,
		PollInterval: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	server := httptest.NewServer(http.HandlerFunc(hub.ServeStream))
	defer server.Close()

	resp, getErr := http.Get(server.URL)

	if getErr != nil {
		test.Fatalf("get: %v", getErr)
	}

	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)

	if got := readSSEEvent(test, reader); !strings.Contains(got, "event: change") {
		test.Fatalf("initial frame missing event name: %q", got)
	}

	changes.gen.Store(1) // advance the signal -> hub broadcasts

	if got := readSSEEvent(test, reader); !strings.Contains(got, `"generation":1`) {
		test.Fatalf("broadcast frame wrong: %q", got)
	}
}

// TestHubBroadcastDropsFramesForSlowClient pins the drop-not-block contract:
// a client that never drains its channel must not wedge the broadcaster. The
// per-client buffer is 8, so the 9th and later frames have nowhere to go and
// must be dropped via the select's default arm. Without that arm the send
// blocks forever while holding hub.mu, freezing every other client and the
// poll loop — so this asserts Broadcast RETURNS, and separately that the slow
// client kept exactly the first 8 frames.
func TestHubBroadcastDropsFramesForSlowClient(test *testing.T) {
	hub := NewHub(HubOptions{
		EventName: "change",
		Payload:   func() ([]byte, error) { return []byte("{}"), nil },
		Changes:   &stubChanges{},
	})

	stalled := make(chan []byte, 8) // never drained: mirrors ServeStream's buffer
	hub.register(stalled)

	const frames = 20

	for i := 0; i < frames; i++ {
		broadcastWithin(test, hub, []byte(itoa(int64(i))))
	}

	if got := len(stalled); got != 8 {
		test.Fatalf("buffered frames = %d, want 8 (the buffer fills, then frames drop)", got)
	}

	for i := 0; i < 8; i++ {
		if got := string(<-stalled); got != itoa(int64(i)) {
			test.Fatalf("buffered frame %d = %q, want %q (the FIRST 8 frames are kept; later ones drop)", i, got, itoa(int64(i)))
		}
	}
}

// TestHubBroadcastSkipsOnlyTheSlowClient verifies the drop is per-client: a
// stalled client must not cost a healthy one its frame.
func TestHubBroadcastSkipsOnlyTheSlowClient(test *testing.T) {
	hub := NewHub(HubOptions{
		EventName: "change",
		Payload:   func() ([]byte, error) { return []byte("{}"), nil },
		Changes:   &stubChanges{},
	})

	stalled := make(chan []byte, 1)
	healthy := make(chan []byte, 8)

	hub.register(stalled)
	hub.register(healthy)

	stalled <- []byte("prefill") // stalled has no room for the broadcast below

	broadcastWithin(test, hub, []byte("live"))

	if len(healthy) != 1 {
		test.Fatalf("healthy client got %d frames, want 1: a full client must not cost a healthy one its frame", len(healthy))
	}

	if got := string(<-healthy); got != "live" {
		test.Fatalf("healthy client frame = %q, want %q", got, "live")
	}
}

// broadcastWithin calls hub.Broadcast and fails the test with a clear message
// if it has not returned within a generous deadline. A blocking send on a full
// client channel would otherwise deadlock the caller while holding hub.mu, and
// the suite would hang until go test's global timeout dumps every goroutine —
// a far worse signal than a named assertion.
func broadcastWithin(test *testing.T, hub *Hub, payload []byte) {
	test.Helper()

	done := make(chan struct{})

	go func() {
		defer close(done)

		hub.Broadcast(payload)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		test.Fatalf("Broadcast(%q) did not return within 5s: a client whose buffer is full must have its frame DROPPED (the select's default arm), never block the hub", payload)
	}
}

// itoa formats an int64 as a base-10 string; the test payload builds its own
// tiny JSON body without pulling in encoding/json for a single field.
func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

// readSSEEvent reads one SSE frame (lines up to and including the blank line
// that terminates it) from reader. It runs the read in a goroutine and races
// it against a generous deadline so a broadcast that never arrives fails the
// test with a clear message instead of hanging the suite forever.
func readSSEEvent(test *testing.T, reader *bufio.Reader) string {
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
			test.Fatalf("readSSEEvent: %v (partial frame: %q)", result.err, result.frame)
		}

		return result.frame
	case <-time.After(5 * time.Second):
		test.Fatal("readSSEEvent: timed out waiting for an SSE frame")

		return ""
	}
}
