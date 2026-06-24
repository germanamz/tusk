package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
)

// MaxEmbedAttempts caps how many retries DrainQueue accepts for a failing
// node. When attempts+1 would reach the cap, the row is dropped from the
// queue (EmbedQueueRepo.Drop) and a Warn `embed gave up` line is emitted.
// Fresh reindex runs re-enqueue every indexed node with attempts=0, so the
// cap is per-drain, not per-node-lifetime.
const MaxEmbedAttempts = 3

// defaultEmbedLeaseTTL is the defensive fallback DrainQueue applies when
// DrainConfig.TTL is zero or negative. Production callers resolve the TTL
// through internal/leaseconfig.Resolve and pass it in; tests that omit
// the field still get a sane window.
const defaultEmbedLeaseTTL = 60 * time.Second

// DrainConfig configures DrainQueue.
type DrainConfig struct {
	Root       string                // workspace root (required when Embedder is set)
	Nodes      *index.NodeRepo       // node repo for path lookups
	Queue      *index.EmbedQueueRepo // queue repo (required)
	Embeddings *index.EmbeddingRepo  // embeddings repo (required when Embedder is set)
	Embedder   Embedder              // when nil, DrainQueue is a no-op
	Chunker    ChunkingStrategy      // required when Embedder is set
	BatchSize  int                   // queue rows pulled per drain iteration; defaults to 50
	Workers    int                   // concurrent embed calls per node; defaults to 1 (serial)
	TTL        time.Duration         // lease window applied per Drain claim; defaults to 60s when <= 0
	Logger     *slog.Logger          // optional; nil silences output
}

// isSubUnit reports whether a node row represents a sub-unit (paragraph,
// list-item, etc.) of a parent file rather than a file-level row. The
// parent_id column is NULL on file-level rows and points at the parent
// file's id on sub-unit rows; see internal/index/node_repo.go.
func isSubUnit(row *index.NodeRow) bool {
	// Schema invariant: parent_id is either NULL (file row) or a non-empty
	// composite ID (sub-unit). A Valid && String=="" state is not produced
	// by the writer path, so Valid alone is the correct discriminator.
	return row != nil && row.ParentID.Valid
}

// embeddingsMatch reports whether the persisted rows already cover every new
// chunk: same count, same chunk_idx coverage, same content_hash, same model.
// Used by DrainQueue to skip re-embedding when content is unchanged.
func embeddingsMatch(existing []index.EmbeddingRow, newHashes []string, model string) bool {
	if len(existing) != len(newHashes) {
		return false
	}

	byIdx := make(map[int]index.EmbeddingRow, len(existing))

	for _, row := range existing {
		byIdx[row.ChunkIdx] = row
	}

	for chunkIdx, hash := range newHashes {
		row, ok := byIdx[chunkIdx]

		if !ok || row.ContentHash != hash || row.Model != model {
			return false
		}
	}

	return true
}

// retryOrDrop applies the single per-node retry policy after any non-retryable
// failure — a read/parse error, a content-hash map error, or an embed-call
// failure. It drops the node from the queue once it has exhausted
// MaxEmbedAttempts (logging "embed gave up"), otherwise re-enqueues it with the
// error attached (logging "embed re-enqueued"). Queue errors are surfaced to
// the log but otherwise ignored — the next drain pass re-converges. logger may
// be nil, which silences all output.
func retryOrDrop(queue *index.EmbedQueueRepo, logger *slog.Logger, nodeID, workerID string, attempts int, err error) {
	nextAttempts := attempts + 1

	if nextAttempts >= MaxEmbedAttempts {
		if dropErr := queue.Drop(nodeID, workerID); dropErr != nil && logger != nil {
			logger.Warn("embed drop failed",
				"node_id", nodeID,
				"err", dropErr.Error(),
			)
		}

		if logger != nil {
			logger.Warn("embed gave up",
				"node_id", nodeID,
				"attempts", nextAttempts,
				"err", err.Error(),
			)
		}

		return
	}

	if nackErr := queue.Nack(nodeID, workerID, err); nackErr != nil {
		if logger != nil {
			logger.Warn("embed re-enqueue failed",
				"node_id", nodeID,
				"err", nackErr.Error(),
			)
		}
	} else if logger != nil {
		logger.Warn("embed re-enqueued",
			"node_id", nodeID,
			"attempts", nextAttempts,
		)
	}
}

// DrainQueue pops every pending row from embed_queue and embeds it. Returns the
// number of nodes successfully embedded. Failed rows are re-enqueued (with an
// incremented attempts counter) until MaxEmbedAttempts is reached, at which
// point the row is dropped from the queue. When DrainConfig.Embedder is nil,
// DrainQueue is a no-op.
//
// ctx cancellation aborts before the next batch; in-flight batches finish.
func DrainQueue(ctx context.Context, config DrainConfig) (int, error) {
	if config.Embedder == nil {
		return 0, nil
	}

	if config.Queue == nil {
		return 0, fmt.Errorf("embed: drain: Queue is required")
	}

	if config.Embeddings == nil {
		return 0, fmt.Errorf("embed: drain: Embeddings is required")
	}

	if config.Nodes == nil {
		return 0, fmt.Errorf("embed: drain: Nodes is required")
	}

	if config.Chunker == nil {
		return 0, fmt.Errorf("embed: drain: Chunker is required")
	}

	limit := config.BatchSize

	if limit <= 0 {
		limit = 50
	}

	workerID := index.WorkerID()

	leaseTTL := config.TTL

	if leaseTTL <= 0 {
		leaseTTL = defaultEmbedLeaseTTL
	}

	var drained int

	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return drained, nil
		}

		batch, drainErr := config.Queue.DrainEmbed(workerID, limit, leaseTTL)

		if drainErr != nil {
			return drained, drainErr
		}

		if len(batch) == 0 {
			// Queue is drained: reclaim any vectors no node references any
			// more (orphaned by content edits or deletes). Safe here because
			// no drain work is in flight.
			if removed, gcErr := config.Embeddings.GCOrphanVectors(); gcErr != nil {
				if config.Logger != nil {
					config.Logger.Warn("embed gc failed", "err", gcErr.Error())
				}
			} else if removed > 0 && config.Logger != nil {
				config.Logger.Debug("embed gc orphan vectors", "removed", removed)
			}

			return drained, nil
		}

		var (
			batchSucceeded int
			batchFailed    int
		)

		for _, queued := range batch {
			outcome, nodeErr := embedNode(ctx, config, workerID, queued)

			if nodeErr != nil {
				return drained, nodeErr
			}

			switch outcome {
			case outcomeSucceeded:
				drained++
				batchSucceeded++
			case outcomeFailed:
				batchFailed++
			case outcomeSkipped:
				// Dropped without retry; neither a success nor a failure.
			}
		}

		if config.Logger != nil {
			config.Logger.Info("drain batch complete",
				"attempted", batchSucceeded+batchFailed,
				"succeeded", batchSucceeded,
				"failed", batchFailed,
			)
		}
	}
}

// nodeOutcome is the verdict embedNode returns for one queued row. A non-nil
// error from embedNode is fatal (aborts the whole drain) and is distinct from
// outcomeFailed, which is a per-node failure the drain recovers from.
type nodeOutcome int

const (
	// outcomeSucceeded: the node was embedded (or reused/unchanged) and acked.
	outcomeSucceeded nodeOutcome = iota
	// outcomeFailed: the node failed and was nacked or dropped via the retry
	// policy; the drain continues with the next row.
	outcomeFailed
	// outcomeSkipped: the node was dropped without retry (missing row, empty
	// sub-unit payload, or zero chunks) and counts as neither success nor
	// failure.
	outcomeSkipped
)

// embedNode runs one queued row through the read/parse/chunk/hash/skip/reuse/
// embed/upsert/ack pipeline and returns its outcome. A returned error is fatal:
// it propagates out of DrainQueue. Per-node failures are absorbed via the
// retry policy and reported as outcomeFailed.
func embedNode(ctx context.Context, config DrainConfig, workerID string, queued index.QueueRow) (nodeOutcome, error) {
	row, getErr := config.Nodes.Get(queued.NodeID)

	if getErr != nil {
		_ = config.Queue.Drop(queued.NodeID, workerID)

		return outcomeSkipped, nil
	}

	header, bodyChunks, payloadOutcome := buildChunkPayloads(config, workerID, queued, row)

	if payloadOutcome != outcomeSucceeded {
		return payloadOutcome, nil
	}

	if len(bodyChunks) == 0 {
		_ = config.Queue.Drop(queued.NodeID, workerID)

		return outcomeSkipped, nil
	}

	if config.Logger != nil {
		config.Logger.Debug("embed attempt",
			"node_id", queued.NodeID,
			"header_bytes", len(header),
			"chunks", len(bodyChunks),
		)
	}

	// Build the per-chunk payloads (header + chunk) and their content hashes.
	chunkPayloads := make([][]byte, len(bodyChunks))
	chunkHashes := make([]string, len(bodyChunks))

	for chunkIdx, bodyChunk := range bodyChunks {
		payload := make([]byte, 0, len(header)+len(bodyChunk))
		payload = append(payload, header...)
		payload = append(payload, bodyChunk...)
		hash := sha256.Sum256(payload)
		chunkPayloads[chunkIdx] = payload
		chunkHashes[chunkIdx] = hex.EncodeToString(hash[:])
	}

	// Short-circuit when every chunk's payload hash already matches an existing
	// row for this node under the same model. Reindex enqueues every seen node
	// every pass, so this skip keeps unchanged content from re-embedding on
	// every watcher tick.
	existingRows, existingErr := config.Embeddings.GetByNodeID(queued.NodeID)

	if existingErr == nil && embeddingsMatch(existingRows, chunkHashes, config.Embedder.Model()) {
		if config.Logger != nil {
			config.Logger.Debug("embed skip unchanged",
				"node_id", queued.NodeID,
				"chunks", len(bodyChunks),
			)
		}

		ackNode(config, workerID, queued.NodeID)

		return outcomeSucceeded, nil
	}

	if delErr := config.Embeddings.DeleteByNodeID(queued.NodeID); delErr != nil {
		if config.Logger != nil {
			config.Logger.Warn("embed delete-before-insert failed",
				"node_id", queued.NodeID,
				"err", delErr.Error(),
			)
		}

		_ = config.Queue.Drop(queued.NodeID, workerID)

		return outcomeFailed, nil
	}

	if reused := tryReuse(config, workerID, queued, chunkHashes); reused != outcomeSkipped {
		return reused, nil
	}

	return embedChunks(ctx, config, workerID, queued, header, chunkPayloads, chunkHashes)
}

// buildChunkPayloads resolves a queued row into the embed header and body
// chunks. Sub-unit rows embed their pre-synthesized payload as a single chunk;
// file-level rows are read from disk, parsed, and chunked by config.Chunker. It
// returns the header, the body chunks, and an outcome: outcomeSucceeded means
// (header, chunks) are valid; any other outcome means the row was already
// dropped or retried and the caller should return that outcome.
func buildChunkPayloads(config DrainConfig, workerID string, queued index.QueueRow, row *index.NodeRow) ([]byte, [][]byte, nodeOutcome) {
	// Sub-unit rows carry their own pre-synthesized embed payload (set by the
	// sub-unit sync in Task 3) — including the `<column-header>: <cell-text>`
	// synthesis for table cells per spec §5.6. They are embedded as a single
	// vector with no file header context; the AST already chose the semantic
	// boundary so we must not chunk further.
	//
	// File-level rows fall through to the legacy read+parse+chunk path. That
	// path is now the back-compat default for workspaces that disable sub-unit
	// indexing.
	if isSubUnit(row) {
		payload := row.EmbedPayload.String

		if payload == "" {
			// Defensive: a sub-unit with an empty payload would send a
			// zero-byte prompt to the embedder. Drop it from the queue without
			// retry — re-running won't repopulate the column.
			if config.Logger != nil {
				config.Logger.Warn("embed skip empty sub-unit payload",
					"node_id", queued.NodeID,
				)
			}

			_ = config.Queue.Drop(queued.NodeID, workerID)

			return nil, nil, outcomeSkipped
		}

		return nil, ASTChunking{}.Chunk([]byte(payload)), outcomeSucceeded
	}

	content, readErr := os.ReadFile(filepath.Join(config.Root, row.Path))

	if readErr != nil {
		retryOrDrop(config.Queue, config.Logger, queued.NodeID, workerID, queued.Attempts, readErr)

		return nil, nil, outcomeFailed
	}

	parsed, parseErr := node.ParseContentFile(row.Path, content)

	if parseErr != nil {
		retryOrDrop(config.Queue, config.Logger, queued.NodeID, workerID, queued.Attempts, parseErr)

		return nil, nil, outcomeFailed
	}

	header := BuildHeader(parsed)

	return header, config.Chunker.Chunk(BuildBody(parsed)), outcomeSucceeded
}

// tryReuse attaches the node to already-stored vectors by content hash when
// every chunk's content already exists under the same model — the cross-node
// de-duplication path that avoids calling the embedder for unchanged content.
// Returns outcomeSucceeded (reused + acked), outcomeFailed (map error, retried/
// dropped), or outcomeSkipped (not all chunks reusable; caller must embed).
func tryReuse(config DrainConfig, workerID string, queued index.QueueRow, chunkHashes []string) nodeOutcome {
	allReusable := len(chunkHashes) > 0

	for _, hash := range chunkHashes {
		exists, existsErr := config.Embeddings.ExistsByContentHash(hash, config.Embedder.Model())

		if existsErr != nil || !exists {
			allReusable = false

			break
		}
	}

	if !allReusable {
		return outcomeSkipped
	}

	for chunkIdx, hash := range chunkHashes {
		if mapErr := config.Embeddings.MapNodeChunk(queued.NodeID, chunkIdx, hash, config.Embedder.Model()); mapErr != nil {
			retryOrDrop(config.Queue, config.Logger, queued.NodeID, workerID, queued.Attempts, mapErr)

			return outcomeFailed
		}
	}

	if config.Logger != nil {
		config.Logger.Debug("embed reuse by content hash",
			"node_id", queued.NodeID,
			"chunks", len(chunkHashes),
		)
	}

	ackNode(config, workerID, queued.NodeID)

	return outcomeSucceeded
}

// embedChunks runs the worker pool: it embeds every chunk concurrently (capped
// at config.Workers), aborts the node on the first chunk error via the retry
// policy, and on full success upserts every vector and acks the row. A returned
// error is fatal (an upsert failure aborts the drain).
func embedChunks(ctx context.Context, config DrainConfig, workerID string, queued index.QueueRow, header []byte, chunkPayloads [][]byte, chunkHashes []string) (nodeOutcome, error) {
	workers := config.Workers

	if workers < 1 {
		workers = 1
	}

	workers = min(workers, len(chunkPayloads))

	type embedJob struct {
		chunkIdx int
		payload  []byte
	}

	type embedResult struct {
		chunkIdx     int
		vector       []float32
		body         []byte
		payloadBytes int
		latency      time.Duration
		err          error
	}

	nodeCtx, cancel := context.WithCancel(ctx)

	jobs := make(chan embedJob, len(chunkPayloads))
	results := make(chan embedResult, len(chunkPayloads))

	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for job := range jobs {
				if nodeCtx.Err() != nil {
					results <- embedResult{chunkIdx: job.chunkIdx, payloadBytes: len(job.payload), err: nodeCtx.Err()}

					continue
				}

				embedStart := time.Now()
				vec, err := config.Embedder.Embed(nodeCtx, job.payload)
				latency := time.Since(embedStart)
				// header is nil for sub-units (len(header)==0), so body == payload in that case.
				results <- embedResult{
					chunkIdx:     job.chunkIdx,
					vector:       vec,
					body:         job.payload[len(header):], // body is payload minus header
					payloadBytes: len(job.payload),
					latency:      latency,
					err:          err,
				}
			}
		}()
	}

	for chunkIdx, payload := range chunkPayloads {
		jobs <- embedJob{chunkIdx: chunkIdx, payload: payload}
	}

	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	collected := make([]embedResult, 0, len(chunkPayloads))

	// "first" here means first by completion order, not by submission order —
	// multiple chunks may be in-flight; we report one example failing chunk,
	// not necessarily the lowest chunk_idx.
	var (
		firstErr          error
		firstErrChunkIdx  int
		firstErrPayloadSz int
	)

	for res := range results {
		if res.err != nil && firstErr == nil {
			firstErr = res.err
			firstErrChunkIdx = res.chunkIdx
			firstErrPayloadSz = res.payloadBytes

			cancel()
		}

		collected = append(collected, res)
	}

	cancel()

	if firstErr != nil {
		if config.Logger != nil {
			config.Logger.Warn("embed call failed",
				"node_id", queued.NodeID,
				"chunk_idx", firstErrChunkIdx,
				"chunks_total", len(chunkPayloads),
				"payload_bytes", firstErrPayloadSz,
				"model", config.Embedder.Model(),
				"err", firstErr.Error(),
			)
		}

		retryOrDrop(config.Queue, config.Logger, queued.NodeID, workerID, queued.Attempts, firstErr)

		return outcomeFailed, nil
	}

	sort.Slice(collected, func(left, right int) bool {
		return collected[left].chunkIdx < collected[right].chunkIdx
	})

	for _, res := range collected {
		if upsertErr := config.Embeddings.Upsert(index.EmbeddingRow{
			NodeID:      queued.NodeID,
			ChunkIdx:    res.chunkIdx,
			Model:       config.Embedder.Model(),
			ContentHash: chunkHashes[res.chunkIdx],
			Vector:      res.vector,
			Dim:         config.Embedder.Dim(),
			Body:        string(res.body),
		}); upsertErr != nil {
			return outcomeFailed, upsertErr
		}

		if config.Logger != nil {
			config.Logger.Debug("embed attempt success",
				"node_id", queued.NodeID,
				"chunk_idx", res.chunkIdx,
				"chunks_total", len(chunkPayloads),
				"vector_dim", len(res.vector),
				"latency_ms", res.latency.Milliseconds(),
				"payload_bytes", res.payloadBytes,
			)
		}
	}

	ackNode(config, workerID, queued.NodeID)

	return outcomeSucceeded, nil
}

// ackNode acks a successfully-processed row, logging (but not surfacing) an ack
// failure — the lease expires on its own and the next pass re-converges.
func ackNode(config DrainConfig, workerID, nodeID string) {
	if ackErr := config.Queue.Ack(nodeID, workerID); ackErr != nil && config.Logger != nil {
		config.Logger.Warn("embed ack failed",
			"node_id", nodeID,
			"err", ackErr.Error(),
		)
	}
}
