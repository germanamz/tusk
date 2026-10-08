---
type: package
title: internal/reindex — full reindex pipeline
import-path: github.com/germanamz/tusk/internal/reindex
status: stable
---

# internal/reindex

Walks the workspace tree, parses every markdown file, validates against the manifest, runs behavior hooks, resolves refs, and upserts nodes + edges + drift rows in one pass. Powers `tusk reindex` and the `tusk_reindex` MCP tool.

## Public surface

- `Run(ctx, Config) (*Report, error)` — single entry point.
- `Config` — repos, manifest, behaviors, optional drainer hookup.
- `Report` — `Indexed`, `Removed`, `Skipped`, `WorkflowViolations`, `PropertyViolations`, `RefDangling`, `RefAmbiguous`, `RefTypeMismatch`, `RefCycle`, `RefHealed`.
- `HealRefDrift(ctx, WorkerConfig) (HealReport, error)` — re-resolves recorded ref drift after a sweep; the MCP drainer calls it after productive ticks.

## Notes

Ref resolution runs per file against the live index, so a file processed before its target has a node row records `ref_dangling` drift instead of an edge. The **ref-drift heal pass** makes this converge: after the sweep's drain, `Run` re-enqueues the file behind every ref-kind drift row and drains once more — by then every live file has a node row, so refs that dangled only for ordering reasons (fresh index) or because their target was created after the referencing file was last indexed resolve, write their edges, and clear their drift. Genuinely broken refs re-record drift and stay in the report. Async walks instead enqueue the drifted files for the background drainer, which heals after any tick that indexed something. Report ref counters reflect the post-heal end state of the pass.

**Embedding settings changes.** After the walk, `Run` compares `embed.DocumentFingerprint` of the manifest's `[embeddings]` section with the value stored in meta (`embed.DocumentFingerprintKey`). On a mismatch it records the new fingerprint first, then re-queues every embeddable node (`EmbedQueueRepo.EnqueueAllEmbeddable`: file rows and non-section sub-unit leaves with a payload, revoking any lease a drainer holds on them), and reports the count as `Report.EmbedSettingsRequeued`. That order is what keeps concurrent drainers honest: a drainer built from the old settings sees the new fingerprint and stops (`embed.DrainConfig.Fingerprint`), and one already holding a row can't ack it once its lease is revoked. Enqueueing directly, rather than forcing a full re-process, works the same for the sync CLI pass and the MCP daemon's async walk, whose background drainer never forces.

A process whose loaded manifest no longer matches `tusk.toml` on disk (a daemon that wasn't reloaded after the file changed) logs a warning and does nothing, so it can't re-embed the vault back to its stale settings or overwrite the fingerprint another process recorded. An index with no stored fingerprint records it without re-embedding when every document setting is at its default and every stored vector uses the current vector key; otherwise it re-embeds. A side effect: a `model` change converges on the next reindex without `tusk reset`. `ApplyEmbeddingSettings` runs the same check without a walk (used by `tusk reload`). The check needs `Config.Manifest`, `Embedder`, `EmbedQueue`, and `EmbeddingRepo`, and is skipped when any is missing.
