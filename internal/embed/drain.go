package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
)

// MaxEmbedAttempts caps how many times DrainQueue retries a failing node
// within a single drain pass. After the cap is hit the node is dropped from
// the queue (Drain already deleted it; we just don't re-enqueue) and a
// Warn `embed gave up` line is emitted. Fresh reindex runs re-enqueue every
// indexed node with attempts=0, so the cap is per-drain, not per-node-lifetime.
const MaxEmbedAttempts = 3

// DrainConfig configures DrainQueue.
type DrainConfig struct {
	Root       string                // workspace root (required when Embedder is set)
	Nodes      *index.NodeRepo       // node repo for path lookups
	Queue      *index.EmbedQueueRepo // queue repo (required)
	Embeddings *index.EmbeddingRepo  // embeddings repo (required when Embedder is set)
	Embedder   Embedder              // when nil, DrainQueue is a no-op
	Chunker    ChunkingStrategy      // required when Embedder is set
	BatchSize  int                   // optional; defaults to 50
	Logger     *slog.Logger          // optional; nil silences output
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

	var drained int

	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return drained, nil
		}

		batch, drainErr := config.Queue.Drain(limit)

		if drainErr != nil {
			return drained, drainErr
		}

		if len(batch) == 0 {
			return drained, nil
		}

		var (
			batchSucceeded int
			batchFailed    int
		)

		for _, queued := range batch {
			row, getErr := config.Nodes.Get(queued.NodeID)

			if getErr != nil {
				continue
			}

			content, readErr := os.ReadFile(filepath.Join(config.Root, row.Path))

			if readErr != nil {
				nextAttempts := queued.Attempts + 1

				if nextAttempts < MaxEmbedAttempts {
					_ = config.Queue.ReEnqueue(queued.NodeID, nextAttempts, readErr.Error())
				}

				continue
			}

			parsed, parseErr := node.ParseFile(row.Path, content)

			if parseErr != nil {
				nextAttempts := queued.Attempts + 1

				if nextAttempts < MaxEmbedAttempts {
					_ = config.Queue.ReEnqueue(queued.NodeID, nextAttempts, parseErr.Error())
				}

				continue
			}

			header := BuildHeader(parsed)
			body := BuildBody(parsed)
			bodyChunks := config.Chunker.Chunk(body)

			if len(bodyChunks) == 0 {
				continue
			}

			if config.Logger != nil {
				config.Logger.Debug("embed attempt",
					"node_id", queued.NodeID,
					"header_bytes", len(header),
					"body_bytes", len(body),
					"chunks", len(bodyChunks),
				)
			}

			if delErr := config.Embeddings.DeleteByNodeID(queued.NodeID); delErr != nil {
				if config.Logger != nil {
					config.Logger.Warn("embed delete-before-insert failed",
						"node_id", queued.NodeID,
						"err", delErr.Error(),
					)
				}

				batchFailed++

				continue
			}

			nodeFailed := false

			for chunkIdx, bodyChunk := range bodyChunks {
				payload := make([]byte, 0, len(header)+len(bodyChunk))
				payload = append(payload, header...)
				payload = append(payload, bodyChunk...)

				embedStart := time.Now()

				vector, embedErr := config.Embedder.Embed(ctx, payload)

				embedLatency := time.Since(embedStart)

				if embedErr != nil {
					if config.Logger != nil {
						config.Logger.Warn("embed call failed",
							"node_id", queued.NodeID,
							"chunk_idx", chunkIdx,
							"chunks_total", len(bodyChunks),
							"payload_bytes", len(payload),
							"model", config.Embedder.Model(),
							"err", embedErr.Error(),
						)
					}

					nextAttempts := queued.Attempts + 1

					if nextAttempts >= MaxEmbedAttempts {
						if config.Logger != nil {
							config.Logger.Warn("embed gave up",
								"node_id", queued.NodeID,
								"attempts", nextAttempts,
								"err", embedErr.Error(),
							)
						}
					} else {
						if reEnqErr := config.Queue.ReEnqueue(queued.NodeID, nextAttempts, embedErr.Error()); reEnqErr != nil {
							if config.Logger != nil {
								config.Logger.Warn("embed re-enqueue failed",
									"node_id", queued.NodeID,
									"err", reEnqErr.Error(),
								)
							}
						} else if config.Logger != nil {
							config.Logger.Warn("embed re-enqueued",
								"node_id", queued.NodeID,
								"attempts", nextAttempts,
							)
						}
					}

					batchFailed++
					nodeFailed = true

					break
				}

				contentHash := sha256.Sum256(payload)

				if upsertErr := config.Embeddings.Upsert(index.EmbeddingRow{
					NodeID:      queued.NodeID,
					ChunkIdx:    chunkIdx,
					Model:       config.Embedder.Model(),
					ContentHash: hex.EncodeToString(contentHash[:]),
					Vector:      vector,
					Dim:         config.Embedder.Dim(),
				}); upsertErr != nil {
					return drained, upsertErr
				}

				if config.Logger != nil {
					config.Logger.Debug("embed attempt success",
						"node_id", queued.NodeID,
						"chunk_idx", chunkIdx,
						"chunks_total", len(bodyChunks),
						"vector_dim", len(vector),
						"latency_ms", embedLatency.Milliseconds(),
					)
				}
			}

			if !nodeFailed {
				drained++
				batchSucceeded++
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
