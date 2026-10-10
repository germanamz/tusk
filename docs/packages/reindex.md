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

## Skipped files

`Report.Skipped` counts every file acked without indexing, which mixes two cases. A file with no frontmatter or no `type` is simply not a node, and vaults rely on that for plain markdown. A file that fails for a fault is broken: frontmatter that does not decode, an edge value of the wrong shape (`ResolveEdges`), a sub-unit parse failure, or a reserved-id path (`#`, or a `reindex:` prefix). Only the faults are recorded, through `FileStateRepo.RecordSkip`, with the error text, and `tusk doctor` reports each as a `skipped-file` error. The record is cleared when the file indexes, vanishes before the worker reads it, parses as a non-node, or is tombstoned by the reaper. A file that indexed once and then broke keeps its last good node row; the record is what tells doctor so.

Recording skips is also why `edgeDerivationVersion` was bumped: a broken file is stamped live in `file_state` by the walk, so the mtime+size check never re-reads it, and the forced pass after an upgrade is what records files that were already broken.

## Notes

Ref resolution runs per file against the live index, so a file processed before its target has a node row records `ref_dangling` drift instead of an edge. The **ref-drift heal pass** makes this converge: after the sweep's drain, `Run` re-enqueues the file behind every ref-kind drift row and drains once more — by then every live file has a node row, so refs that dangled only for ordering reasons (fresh index) or because their target was created after the referencing file was last indexed resolve, write their edges, and clear their drift. Genuinely broken refs re-record drift and stay in the report. Async walks instead enqueue the drifted files for the background drainer, which heals after any tick that indexed something. Report ref counters reflect the post-heal end state of the pass.

**Sub-unit line ranges.** The worker numbers markdown units with `subunit.AssignLines` over the whole file, starting from `node.Node.BodyOffset`, under the manifest's `[workspace] line-numbering` scheme. The scheme the stored ranges were numbered with lives in meta under `line_numbering`. When the configured scheme differs, or the key is absent because the index predates line ranges, `Run` forces one full re-process pass and stamps the key once the pass succeeds. The sync diff rewrites only rows whose lines moved and never re-embeds, so the pass costs a re-parse. One mechanism covers both the upgrade fill and a later scheme change. A daemon that has not yet reloaded a changed `tusk.toml` renumbers with its old scheme until its manifest watcher reloads it; that also costs only a re-parse, and the next pass converges.

**Path refs.** The worker replaces each page's `path_refs` rows right after its edges, with `node.PathRefs` over the file as read (see [`internal/pathref`](pathref.md)). Meta records a fingerprint of the path-ref configuration under `path_refs`: the pathref rules version plus each paths edge type with its `from` list and whether it declares `to` (a paths-only type reads its frontmatter values as paths). When it differs, `Run` forces one full re-process pass, so turning `paths = true` on or off, editing `from`, adding or dropping `to`, or upgrading to a binary with new rules converges every page; nothing re-embeds. A vault with no paths edge type fingerprints as empty, so an index from before path refs existed needs no pass. The key is stamped after the pass succeeds.

**Date self-heal and lines.** Quoting an unquoted date rewrites the frontmatter, so the worker re-derives `BodyOffset` from the re-read bytes (the body is still a suffix of them). Without that, sub-unit and path-ref lines would count against the pre-heal file and could land a line early.

**Embedding settings changes.** After the walk, `Run` compares `embed.DocumentFingerprint` of the manifest's `[embeddings]` section with the value stored in meta (`embed.DocumentFingerprintKey`). On a mismatch it records the new fingerprint first, then re-queues every embeddable node (`EmbedQueueRepo.EnqueueAllEmbeddable`: file rows and non-section sub-unit leaves with a payload, revoking any lease a drainer holds on them), and reports the count as `Report.EmbedSettingsRequeued`. That order is what keeps concurrent drainers honest: a drainer built from the old settings sees the new fingerprint and stops (`embed.DrainConfig.Fingerprint`), and one already holding a row can't ack it once its lease is revoked. Enqueueing directly, rather than forcing a full re-process, works the same for the sync CLI pass and the MCP daemon's async walk, whose background drainer never forces.

A process whose loaded manifest no longer matches `tusk.toml` on disk (a daemon that wasn't reloaded after the file changed) logs a warning and does nothing, so it can't re-embed the vault back to its stale settings or overwrite the fingerprint another process recorded. An index with no stored fingerprint records it without re-embedding when every document setting is at its default and every stored vector uses the current vector key; otherwise it re-embeds. A side effect: a `model` change converges on the next reindex without `tusk reset`. `ApplyEmbeddingSettings` runs the same check without a walk (used by `tusk reload`). The check needs `Config.Manifest`, `Embedder`, `EmbedQueue`, and `EmbeddingRepo`, and is skipped when any is missing.
