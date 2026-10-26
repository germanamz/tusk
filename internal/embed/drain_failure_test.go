package embed_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
)

// funcEmbedder answers every call with fail(payload): a nil error embeds a
// constant vector. It records each payload. Goroutine-safe.
type funcEmbedder struct {
	fail func(payload []byte) error

	mu       sync.Mutex
	payloads []string
}

func (stub *funcEmbedder) Embed(_ context.Context, payload []byte) ([]float32, error) {
	stub.mu.Lock()
	stub.payloads = append(stub.payloads, string(payload))
	stub.mu.Unlock()

	if failErr := stub.fail(payload); failErr != nil {
		return nil, failErr
	}

	return []float32{0.1, 0.2, 0.3}, nil
}

func (stub *funcEmbedder) Model() string        { return "stub" }
func (stub *funcEmbedder) VectorKey() string    { return "stub" }
func (stub *funcEmbedder) Format() embed.Format { return embed.Format{} }
func (stub *funcEmbedder) Dim() int             { return 3 }

func (stub *funcEmbedder) probeCount() int {
	stub.mu.Lock()
	defer stub.mu.Unlock()

	probes := 0

	for _, payload := range stub.payloads {
		if payload == embed.ProbeInput {
			probes++
		}
	}

	return probes
}

// seedFileNodes writes and indexes one note per id, enqueued in order.
func seedFileNodes(test *testing.T, root string, nodeRepo *index.NodeRepo, queueRepo *index.EmbedQueueRepo, bodies map[string]string, order ...string) {
	test.Helper()

	for _, id := range order {
		createNodeFile(test, root, id+".md", bodies[id])

		if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: id, Type: "note", Path: id + ".md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
			test.Fatalf("upsert %s: %v", id, upsertErr)
		}

		if enqErr := queueRepo.Enqueue(id); enqErr != nil {
			test.Fatalf("enqueue %s: %v", id, enqErr)
		}
	}
}

// TestDrainQueue_RefusedInputFailsAlone pins #845 finding 1: when the model
// refuses one input and the backend answers a probe, only that node fails. The
// drain goes on to the next row, the refused node uses up its attempts and is
// parked with its error, and the node queued behind it embeds.
func TestDrainQueue_RefusedInputFailsAlone(test *testing.T) {
	for _, concurrency := range []int{1, 4} {
		test.Run(fmt.Sprintf("concurrency %d", concurrency), func(test *testing.T) {
			root := test.TempDir()
			store := openIndex(test, root)
			defer store.Close()

			nodeRepo := index.NewNodeRepo(store)
			queueRepo := index.NewEmbedQueueRepo(store)
			embeddingRepo := index.NewEmbeddingRepo(store)

			seedFileNodes(test, root, nodeRepo, queueRepo, map[string]string{"big": "OVERSIZE", "small": "fine"}, "big", "small")

			refusal := errors.New(`ollama: HTTP 400: {"error":"the input length exceeds the context length"}`)

			stub := &funcEmbedder{fail: func(payload []byte) error {
				if strings.Contains(string(payload), "OVERSIZE") {
					return refusal
				}

				return nil
			}}

			drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
				Root:             root,
				Nodes:            nodeRepo,
				Queue:            queueRepo,
				Embeddings:       embeddingRepo,
				Embedder:         stub,
				Chunker:          embed.WholeDocument{},
				EmbedConcurrency: concurrency,
			})

			if drainErr != nil {
				test.Fatalf("DrainQueue: %v (a refused input must not abort the drain)", drainErr)
			}

			if drained != 1 {
				test.Errorf("drained = %d, want 1 (small)", drained)
			}

			if rows, _ := embeddingRepo.GetByNodeID("small"); len(rows) != 1 {
				test.Errorf("small has %d vectors, want 1", len(rows))
			}

			if probes := stub.probeCount(); probes != embed.MaxEmbedAttempts {
				test.Errorf("probes = %d, want one per failed attempt (%d)", probes, embed.MaxEmbedAttempts)
			}

			parked, _ := queueRepo.ListParked()

			if len(parked) != 1 || parked[0].NodeID != "big" || parked[0].Attempts != embed.MaxEmbedAttempts || parked[0].LastError != refusal.Error() {
				test.Errorf("parked = %+v, want big at %d attempts with the refusal", parked, embed.MaxEmbedAttempts)
			}

			if depth, _ := queueRepo.Depth(); depth != 0 {
				test.Errorf("pending depth = %d, want 0", depth)
			}
		})
	}
}

// TestDrainQueue_ServerErrorOnOneInputFailsAlone: a 5xx that only one input
// triggers (the runner crashing on it) is that input's fault when the backend
// still answers the probe, so it fails alone instead of stalling the drain
// behind it forever.
func TestDrainQueue_ServerErrorOnOneInputFailsAlone(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	seedFileNodes(test, root, nodeRepo, queueRepo, map[string]string{"poison": "POISON", "fine": "fine"}, "poison", "fine")

	stub := &funcEmbedder{fail: func(payload []byte) error {
		if strings.Contains(string(payload), "POISON") {
			return &embed.TransportError{Err: errors.New("ollama: HTTP 500: llama runner process has terminated")}
		}

		return nil
	}}

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   stub,
		Chunker:    embed.WholeDocument{},
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	if drained != 1 {
		test.Errorf("drained = %d, want 1 (fine)", drained)
	}

	if parked, _ := queueRepo.CountParked(); parked != 1 {
		test.Errorf("parked = %d, want 1 (poison)", parked)
	}
}

// TestDrainQueue_BackendFailureAbortsWithoutChargingAttempts: when the probe
// fails too, the backend can't serve anything (here the model is missing), so
// the drain stops without spending any node's attempts, as it does for an
// unreachable backend, and the error says why.
func TestDrainQueue_BackendFailureAbortsWithoutChargingAttempts(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	seedFileNodes(test, root, nodeRepo, queueRepo, map[string]string{"one": "one", "two": "two"}, "one", "two")

	missing := errors.New(`ollama: HTTP 404: {"error":"model \"nope\" not found, try pulling it first"}`)
	stub := &funcEmbedder{fail: func([]byte) error { return missing }}

	_, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   stub,
		Chunker:    embed.WholeDocument{},
		TTL:        time.Millisecond,
	})

	if !embed.IsTransportError(drainErr) || !strings.Contains(drainErr.Error(), "not found") {
		test.Fatalf("DrainQueue err = %v, want a TransportError naming the probe's failure", drainErr)
	}

	if parked, _ := queueRepo.CountParked(); parked != 0 {
		test.Errorf("parked = %d, want 0", parked)
	}

	time.Sleep(5 * time.Millisecond)

	reclaimed, reclaimErr := queueRepo.DrainEmbed("other-worker", 10, time.Minute)

	if reclaimErr != nil {
		test.Fatalf("DrainEmbed: %v", reclaimErr)
	}

	if len(reclaimed) != 2 {
		test.Fatalf("re-claimed %d rows, want both", len(reclaimed))
	}

	for _, row := range reclaimed {
		if row.Attempts != 0 {
			test.Errorf("%s attempts = %d, want 0 (a backend failure must not charge the node)", row.NodeID, row.Attempts)
		}
	}
}

// TestDrainQueue_SplitsSubUnitLongerThanChunkCap pins #845: a sub-unit whose
// payload is longer than the chunker's hard cap is split the way a file body
// is, one vector per chunk, while a sub-unit within the cap stays one vector
// of its whole payload.
func TestDrainQueue_SplitsSubUnitLongerThanChunkCap(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: "notes/parent", Type: "note", Path: "notes/parent.md", Title: "parent", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert parent: %v", upsertErr)
	}

	line := `  {"rate_plan_charge_id": "8a12abcdef0123456789", "price": 12.5},` + "\n"
	long := strings.Repeat(line, 40)   // ~2.7 KB, over the 1000-byte cap
	within := strings.Repeat(line, 10) // ~680 B, over the 400-byte target but within the cap
	parent := sql.NullString{String: "notes/parent", Valid: true}

	if upsertErr := nodeRepo.BulkUpsert([]index.NodeRow{
		{ID: "notes/parent#S1B1", Type: "code-block", Path: "notes/parent.md", PropertiesJSON: "{}", LastChecksum: "x", ParentID: parent, Ordinal: sql.NullInt64{Int64: 0, Valid: true}, EmbedPayload: sql.NullString{String: long, Valid: true}},
		{ID: "notes/parent#S1B2", Type: "code-block", Path: "notes/parent.md", PropertiesJSON: "{}", LastChecksum: "x", ParentID: parent, Ordinal: sql.NullInt64{Int64: 1, Valid: true}, EmbedPayload: sql.NullString{String: within, Valid: true}},
	}, "markdown"); upsertErr != nil {
		test.Fatalf("upsert sub-units: %v", upsertErr)
	}

	if enqErr := queueRepo.EnqueueMany([]string{"notes/parent#S1B1", "notes/parent#S1B2"}); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	stub := &funcEmbedder{fail: func([]byte) error { return nil }}

	if _, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   stub,
		Chunker:    embed.MarkdownRecursive{TargetBytes: 400, MaxBytes: 1000, OverlapBytes: 1},
	}); drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	longRows, _ := embeddingRepo.GetByNodeID("notes/parent#S1B1")

	if len(longRows) < 2 {
		test.Fatalf("over-cap sub-unit has %d vectors, want it split into several", len(longRows))
	}

	for _, row := range longRows {
		if len(row.Body) > 1000 {
			test.Errorf("chunk %d is %d bytes, over the 1000-byte cap", row.ChunkIdx, len(row.Body))
		}
	}

	withinRows, _ := embeddingRepo.GetByNodeID("notes/parent#S1B2")

	if len(withinRows) != 1 || withinRows[0].Body != within {
		test.Errorf("within-cap sub-unit = %d vectors, want 1 holding its whole payload", len(withinRows))
	}
}
