package webui

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// HubOptions configures a Hub. EventName names the SSE event (graph broadcasts
// as "graph"; book as "change"). Payload produces the current frame body on
// demand — for the initial frame sent to each new client and for every
// broadcast. Changes reports the vault's change signal; Run polls it and
// broadcasts a fresh Payload() whenever it advances.
//
// Payload and Changes are REQUIRED: the Hub dereferences them without a nil
// check, so omitting either panics at use (ServeStream calls Payload on every
// connect; Run calls both). A caller whose change source may legitimately be
// absent should pass an adapter that reports a constant signal rather than nil.
//
// Payload MUST be safe to call from multiple goroutines: it is invoked
// concurrently from every ServeStream goroutine (once per connecting client)
// and from Run's poll loop, with no lock held by the Hub. A Payload closure
// over mutable state must do its own synchronization.
type HubOptions struct {
	EventName    string
	Payload      func() ([]byte, error) // required; must be goroutine-safe
	Changes      ChangeSource           // required
	PollInterval time.Duration          // defaults to 2s when <= 0
}

// Hub is a generic SSE broadcast hub: it fans a byte payload out to every
// registered client, dropping frames for clients too slow to keep up rather
// than blocking the broadcaster. Construct with NewHub, mount ServeStream on
// an SSE route, and run Run(ctx) to poll for changes and broadcast on
// advance.
type Hub struct {
	opts HubOptions

	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

// NewHub builds a Hub from opts. PollInterval defaults to 2s when <= 0.
func NewHub(opts HubOptions) *Hub {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second
	}

	return &Hub{opts: opts, clients: make(map[chan []byte]struct{})}
}

// ServeStream serves one SSE client: an initial Payload() frame, then every
// broadcast until the request context ends.
func (hub *Hub) ServeStream(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)

	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)

		return
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	channel := make(chan []byte, 8)
	hub.register(channel)
	defer hub.unregister(channel)

	if payload, err := hub.opts.Payload(); err == nil {
		hub.write(writer, flusher, payload)
	}

	for {
		select {
		case <-request.Context().Done():
			return
		case payload := <-channel:
			hub.write(writer, flusher, payload)
		}
	}
}

func (hub *Hub) write(writer http.ResponseWriter, flusher http.Flusher, payload []byte) {
	_, _ = writer.Write([]byte("event: " + hub.opts.EventName + "\ndata: "))
	_, _ = writer.Write(payload)
	_, _ = writer.Write([]byte("\n\n"))
	flusher.Flush()
}

func (hub *Hub) register(channel chan []byte) {
	hub.mu.Lock()
	hub.clients[channel] = struct{}{}
	hub.mu.Unlock()
}

func (hub *Hub) unregister(channel chan []byte) {
	hub.mu.Lock()
	delete(hub.clients, channel)
	hub.mu.Unlock()
}

// ClientCount reports the number of connected SSE clients (for status lines).
func (hub *Hub) ClientCount() int {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	return len(hub.clients)
}

// Broadcast fans payload out to every registered client. A client whose
// buffered channel is full is skipped — the frame is dropped rather than
// blocking the broadcaster on a slow reader.
func (hub *Hub) Broadcast(payload []byte) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	for channel := range hub.clients {
		select {
		case channel <- payload:
		default: // slow client; drop this frame rather than block the hub
		}
	}
}

// Run polls the change signal on PollInterval and broadcasts a fresh
// Payload() whenever the signal advances. It blocks until ctx is done.
func (hub *Hub) Run(ctx context.Context) {
	ticker := time.NewTicker(hub.opts.PollInterval)
	defer ticker.Stop()

	last, _ := hub.opts.Changes.Signal()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current, signalErr := hub.opts.Changes.Signal()

			if signalErr != nil || current == last {
				continue
			}

			last = current

			if payload, payloadErr := hub.opts.Payload(); payloadErr == nil {
				hub.Broadcast(payload)
			}
		}
	}
}
