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
// number of nodes successfully embedded. Failed rows are re-enqueued via
// MarkFailed. When DrainConfig.Embedder is nil, DrainQueue is a no-op.
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
				_ = config.Queue.Enqueue(queued.NodeID)
				_ = config.Queue.MarkFailed(queued.NodeID, readErr.Error())

				continue
			}

			parsed, parseErr := node.ParseFile(row.Path, content)

			if parseErr != nil {
				_ = config.Queue.Enqueue(queued.NodeID)
				_ = config.Queue.MarkFailed(queued.NodeID, parseErr.Error())

				continue
			}

			payload := BuildPayload(parsed)
			chunks := config.Chunker.Chunk(payload)

			if len(chunks) == 0 {
				continue
			}

			if config.Logger != nil {
				config.Logger.Debug("embed attempt",
					"node_id", queued.NodeID,
					"payload_bytes", len(payload),
					"chunks", len(chunks),
				)
			}

			embedStart := time.Now()

			vector, embedErr := config.Embedder.Embed(ctx, chunks[0])

			embedLatency := time.Since(embedStart)

			if embedErr != nil {
				if config.Logger != nil {
					config.Logger.Warn("embed call failed",
						"node_id", queued.NodeID,
						"payload_bytes", len(payload),
						"model", config.Embedder.Model(),
						"err", embedErr.Error(),
					)
				}

				_ = config.Queue.Enqueue(queued.NodeID)
				_ = config.Queue.MarkFailed(queued.NodeID, embedErr.Error())

				if config.Logger != nil {
					config.Logger.Warn("embed re-enqueued", "node_id", queued.NodeID)
				}

				batchFailed++

				continue
			}

			contentHash := sha256.Sum256(payload)

			if upsertErr := config.Embeddings.Upsert(index.EmbeddingRow{
				NodeID:      queued.NodeID,
				ChunkIdx:    0,
				Model:       config.Embedder.Model(),
				ContentHash: hex.EncodeToString(contentHash[:]),
				Vector:      vector,
				Dim:         config.Embedder.Dim(),
			}); upsertErr != nil {
				return drained, upsertErr
			}

			drained++

			if config.Logger != nil {
				config.Logger.Debug("embed attempt success",
					"node_id", queued.NodeID,
					"vector_dim", len(vector),
					"latency_ms", embedLatency.Milliseconds(),
				)
			}

			batchSucceeded++
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
