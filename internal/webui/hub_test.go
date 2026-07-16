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
