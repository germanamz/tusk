package graphview

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// handleStream serves one SSE client: an initial snapshot, then every broadcast
// until the request context ends.
func (srv *Server) handleStream(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)

		return
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	channel := make(chan []byte, 8)
	srv.register(channel)
	defer srv.unregister(channel)

	if payload, err := srv.snapshotBytes(); err == nil {
		writeSSE(writer, flusher, payload)
	}

	for {
		select {
		case <-request.Context().Done():
			return
		case payload := <-channel:
			writeSSE(writer, flusher, payload)
		}
	}
}

func writeSSE(writer http.ResponseWriter, flusher http.Flusher, payload []byte) {
	_, _ = writer.Write([]byte("event: graph\ndata: "))
	_, _ = writer.Write(payload)
	_, _ = writer.Write([]byte("\n\n"))
	flusher.Flush()
}

func (srv *Server) register(channel chan []byte) {
	srv.mu.Lock()
	srv.clients[channel] = struct{}{}
	srv.mu.Unlock()
}

func (srv *Server) unregister(channel chan []byte) {
	srv.mu.Lock()
	delete(srv.clients, channel)
	srv.mu.Unlock()
}

// ClientCount reports connected SSE clients (for the CLI status line). Exported
// here in Task 6 — defining the exported accessor in the same commit avoids the
// golangci-lint `unused` failure that an unexported, not-yet-called helper would
// trigger under the pre-commit hook (exported identifiers are never flagged).
func (srv *Server) ClientCount() int {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	return len(srv.clients)
}

func (srv *Server) snapshotBytes() ([]byte, error) {
	graph, err := srv.snapshot()
	if err != nil {
		return nil, err
	}

	return json.Marshal(graph)
}

func (srv *Server) broadcast(payload []byte) {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	for channel := range srv.clients {
		select {
		case channel <- payload:
		default: // slow client; drop this frame rather than block the hub
		}
	}
}

// runHub polls the change signal and broadcasts a fresh snapshot whenever the
// signal advances. Driven by Server.Run.
func (srv *Server) runHub(ctx context.Context) {
	ticker := time.NewTicker(srv.pollDur)
	defer ticker.Stop()

	last, _ := srv.signal()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current, sigErr := srv.signal()
			if sigErr != nil || current == last {
				continue
			}

			last = current

			if payload, err := srv.snapshotBytes(); err == nil {
				srv.broadcast(payload)
			}
		}
	}
}
