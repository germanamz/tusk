package reindex

import (
	"fmt"
	"path/filepath"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/workspace"
)

// ApplyEmbeddingSettings runs the [embeddings] settings check on its own,
// without a walk: when the document settings differ from the recorded ones it
// records the new fingerprint and re-queues every embeddable node, returning
// how many. `tusk reload` without --reindex calls it, because a daemon that
// converges on the reload swaps in the new settings but never reindexes, so
// nothing else would record them. Needs Root, Manifest, Embedder, EmbedQueue,
// EmbeddingRepo, and Meta; a no-op without an embedder. See
// reembedOnSettingsChange for the full contract.
func ApplyEmbeddingSettings(config Config) (int, error) {
	return reembedOnSettingsChange(config)
}

// reembedOnSettingsChange re-embeds the whole index when the [embeddings]
// document settings (model, num-ctx, document-prefix, document-header, chunk
// sizes) differ from the ones recorded under embed.DocumentFingerprintKey.
// The incremental walk skips unchanged files and sub-unit sync re-enqueues
// only changed leaves, so without this a settings change would re-embed
// nothing. Returns how many nodes it re-queued.
//
// It enqueues every embeddable node directly instead of forcing a full
// re-process: that re-embeds without re-reading or re-deriving anything, and
// it works the same for the sync CLI pass and the MCP daemon's async walk,
// whose background drainer never forces. The drain then re-embeds each node
// whose text hash or vector key changed and skip-acks the rest.
//
// Order matters for concurrent drainers. The new fingerprint is recorded
// first, so a drainer still built from the old settings sees it and stops
// (embed.DrainConfig.Fingerprint); then EnqueueAllEmbeddable re-queues every
// node and revokes any lease such a drainer already holds, so its ack can't
// remove the row. Recording before the pass finishes is safe because the
// queued work is durable.
//
// A process whose loaded manifest no longer matches tusk.toml on disk (a
// daemon that wasn't reloaded after the file was edited) does nothing but
// warn: acting on its stale settings would re-embed the vault back to them
// and overwrite the fingerprint another process recorded from the file.
//
// When no fingerprint is recorded yet (an index written before it existed),
// the pass records it without re-embedding if every document setting is at
// its default and every stored vector already uses the current vector key;
// otherwise it re-embeds, which covers a user who upgrades and sets a prefix
// in the same edit, and a model change that was never converged.
//
// No-op when no embedder is configured.
func reembedOnSettingsChange(config Config) (int, error) {
	if config.Embedder == nil || config.Manifest == nil || config.Manifest.Embeddings.Provider == "" ||
		config.EmbedQueue == nil || config.EmbeddingRepo == nil {
		return 0, nil
	}

	section := config.Manifest.Embeddings
	current := embed.DocumentFingerprint(section)

	stored, getErr := config.Meta.Get(embed.DocumentFingerprintKey)

	if getErr != nil {
		return 0, fmt.Errorf("reindex: read %s: %w", embed.DocumentFingerprintKey, getErr)
	}

	if stored == current {
		return 0, nil
	}

	if onDisk, diskErr := diskDocumentFingerprint(config.Root); diskErr != nil || onDisk != current {
		if config.Logger != nil {
			config.Logger.Warn("reindex: this process's [embeddings] settings differ from tusk.toml; skipping the re-embed check until it reloads (tusk_reload, or tusk reload from the CLI)",
				"err", diskErr,
			)
		}

		return 0, nil
	}

	if stored == "" {
		converged, convergedErr := storedVectorsUseKey(config, embed.VectorKeyFor(section))

		if convergedErr != nil {
			return 0, convergedErr
		}

		if converged && current == embed.DocumentFingerprint(manifest.EmbeddingsSection{Model: section.Model}) {
			return 0, recordFingerprint(config, current)
		}
	}

	if recordErr := recordFingerprint(config, current); recordErr != nil {
		return 0, recordErr
	}

	queued, enqueueErr := config.EmbedQueue.EnqueueAllEmbeddable()

	if enqueueErr != nil {
		// Put the previous fingerprint back so the next pass retries the
		// re-embed instead of treating the settings as already applied.
		_ = config.Meta.Set(embed.DocumentFingerprintKey, stored)

		return 0, fmt.Errorf("reindex: re-embed after embeddings settings change: %w", enqueueErr)
	}

	if config.Logger != nil {
		config.Logger.Info("reindex: embeddings settings changed; re-embedding every node",
			"model", section.Model,
			"queued", queued,
			"stored_fingerprint", stored,
			"current_fingerprint", current,
		)
	}

	return queued, nil
}

func recordFingerprint(config Config, fingerprint string) error {
	if setErr := config.Meta.Set(embed.DocumentFingerprintKey, fingerprint); setErr != nil {
		return fmt.Errorf("reindex: record %s: %w", embed.DocumentFingerprintKey, setErr)
	}

	return nil
}

// diskDocumentFingerprint is the DocumentFingerprint of the [embeddings]
// section in the workspace's tusk.toml as it is on disk now.
func diskDocumentFingerprint(root string) (string, error) {
	loaded, loadErr := manifest.Load(filepath.Join(root, workspace.ManifestFilename))

	if loadErr != nil {
		return "", loadErr
	}

	return embed.DocumentFingerprint(loaded.Embeddings), nil
}

// storedVectorsUseKey reports whether every stored vector is keyed under
// vectorKey (vacuously true for an index with no vectors).
func storedVectorsUseKey(config Config, vectorKey string) (bool, error) {
	pairs, distinctErr := config.EmbeddingRepo.DistinctModelDims()

	if distinctErr != nil {
		return false, fmt.Errorf("reindex: distinct embedding models: %w", distinctErr)
	}

	for _, pair := range pairs {
		if pair.Model != vectorKey {
			return false, nil
		}
	}

	return true, nil
}

// drainFingerprint is the DocumentFingerprint this process's embed drain runs
// under, for embed.DrainConfig.Fingerprint; "" (guard off) without a
// configured [embeddings] section.
func drainFingerprint(config Config) string {
	if config.Manifest == nil || config.Manifest.Embeddings.Provider == "" {
		return ""
	}

	return embed.DocumentFingerprint(config.Manifest.Embeddings)
}
