# Changelog

## [2.0.0](https://github.com/germanamz/tusk/compare/v1.7.1...v2.0.0) (2026-05-28)


### ⚠ BREAKING CHANGES

* **v1:** this commit retires every v0.x interface. v0 sources remain at the v0.14.0 tag and the v0-archive branch.
* **v0.11:** `--description` and `-d` are removed from `tusk task create` and `tusk task modify`. Use inline `description=...` instead.

### Features

* --verbose flag and structured logging for indexing pipeline ([#368](https://github.com/germanamz/tusk/issues/368)) ([283f9e2](https://github.com/germanamz/tusk/commit/283f9e2c9cc1d6701411cfbdd4c405d7926051c5))
* agent retrieval improvements (Phase 1) ([#413](https://github.com/germanamz/tusk/issues/413)) ([4449671](https://github.com/germanamz/tusk/commit/44496715f2ab2b8dceedb027b6afff8b55f9f46b))
* agent retrieval improvements (Phase 2) ([#415](https://github.com/germanamz/tusk/issues/415)) ([c739f6a](https://github.com/germanamz/tusk/commit/c739f6afb474e052a76657815623edafb133e913))
* agent retrieval improvements (Phase 3) ([#416](https://github.com/germanamz/tusk/issues/416)) ([c4e8073](https://github.com/germanamz/tusk/commit/c4e8073affe11741234936b64f6b1bb559d9b68b))
* **cli:** add man pages and markdown reference, generated from cobra help ([#393](https://github.com/germanamz/tusk/issues/393)) ([4b73f2a](https://github.com/germanamz/tusk/commit/4b73f2a5d0a96bdcee141b82f9617b12cfcfc633))
* **cli:** phase 1 — tusk task parent + CRUD subcommands ([22e7533](https://github.com/germanamz/tusk/commit/22e7533f4cd777a79130b2d69c56898a047e1d2a))
* **cli:** phase 2 — migrate lifecycle verbs to tusk task ([ec8aebc](https://github.com/germanamz/tusk/commit/ec8aebcba30b8046afca98e9f32e98023a66856a))
* **cli:** phase 3 — migrate claim/queue verbs to tusk task ([28259e1](https://github.com/germanamz/tusk/commit/28259e1d7d9c53fd04411977d8f90f5d2b844fa4))
* **cmd:** strip --config flag before Cobra parses ([4cbb6a0](https://github.com/germanamz/tusk/commit/4cbb6a086347912d57e28fb99839289182aaa3a9))
* **cmd:** thread --config and TUSK_CONFIG into config.Load ([2dacd6e](https://github.com/germanamz/tusk/commit/2dacd6e22c0a30ec089ce6216dccd3b709911c80))
* **config:** add CreateProject and DeleteProject helpers ([81ff268](https://github.com/germanamz/tusk/commit/81ff268e7de2d5cb667ca884e91b64d8c215fa94))
* **config:** add db_path field to ProjectConfig ([c54075c](https://github.com/germanamz/tusk/commit/c54075c5dd9a57b44ee43da0514dcf5475e81226))
* **config:** add json tags for snake_case MCP responses ([abd87da](https://github.com/germanamz/tusk/commit/abd87da71414f9387676766230637cce75c1d423))
* **config:** add ModifyProject with urgency absolute/delta semantics ([286b3b7](https://github.com/germanamz/tusk/commit/286b3b70a3487d4e607e0f7787ba9616d7d52d2b))
* **config:** add ResolveConfigFile abstraction ([7c97b62](https://github.com/germanamz/tusk/commit/7c97b62cbfd33dae466311bda953284faf884979))
* **config:** add ResolveWeightDelta helper ([c50c492](https://github.com/germanamz/tusk/commit/c50c4921652a7e4eefd613445dfa853f3fe80c3e))
* **config:** add Sources field and WithExplicitFile option ([9f9f486](https://github.com/germanamz/tusk/commit/9f9f486596a72495cf641041f0ba4cfe6c86d51c))
* **config:** phase 1 — DB-hydrated show/get, reject projects/workflows writes ([7681499](https://github.com/germanamz/tusk/commit/7681499b7fb92af319832703212dd203f6f33204))
* **config:** phase 2 — trim TOML schema, delete SyncConfigToDB ([4562dd3](https://github.com/germanamz/tusk/commit/4562dd37f8c25891df543425c4d39ebb4b29e234))
* **config:** phase 3 — harden config trim with tests + doc sweep ([0b0e073](https://github.com/germanamz/tusk/commit/0b0e073723ef7569c9c1a913cf3be32714205dd9))
* configurable wikilink-materialization edge via wikilinks flag ([#410](https://github.com/germanamz/tusk/issues/410)) ([f9f0da9](https://github.com/germanamz/tusk/commit/f9f0da9929d9534a6cfc936d668d5537d1fe84b7))
* **config:** walk-up tusk.toml discovery from CWD ([86a46bd](https://github.com/germanamz/tusk/commit/86a46bdfe19b576a483ab76f22b6aba00bd206fa))
* **devcontainer:** add sandboxed dev container with egress allowlist ([0ffc2f0](https://github.com/germanamz/tusk/commit/0ffc2f0479cf9917d1657818954e29213223fddf))
* **domain:** add ErrCrossStoreRelation sentinel ([a32f04b](https://github.com/germanamz/tusk/commit/a32f04b6a22995b1604aff99eb6a28ef49ac082b))
* **domain:** add id/version/timestamps to Project entity ([5a7de60](https://github.com/germanamz/tusk/commit/5a7de60ef3caecdabea75b83a152b855ea257128))
* **domain:** add id/version/timestamps to Workflow entity ([5e20c4d](https://github.com/germanamz/tusk/commit/5e20c4da94a938f8bf867bcf4ba444a4ffef06ce))
* **domain:** add Note entity for v0.12 trailing window notes ([29c0567](https://github.com/germanamz/tusk/commit/29c0567993cf33cd0449825d747b5f90ea670a1a))
* **edge:** explicit --ordinal flag on `tusk edge add` (bug [#4](https://github.com/germanamz/tusk/issues/4)) ([#400](https://github.com/germanamz/tusk/issues/400)) ([0203d74](https://github.com/germanamz/tusk/commit/0203d74c3db1fb4bd71b702f0ab2196583293821))
* **edge:** materialize all edges from frontmatter ([#409](https://github.com/germanamz/tusk/issues/409)) ([e2775b2](https://github.com/germanamz/tusk/commit/e2775b285609e210e8aa7d83ba76559b8d1c6d9e))
* **edges:** frontmatter-direct edges write kind='direct' ([#436](https://github.com/germanamz/tusk/issues/436)) ([37a11fa](https://github.com/germanamz/tusk/commit/37a11fab6b3f16a2c0edee60b8267e9ab9b86165))
* **edges:** ref-derived edges write kind='derived', source=NULL ([#434](https://github.com/germanamz/tusk/issues/434)) ([6ad796a](https://github.com/germanamz/tusk/commit/6ad796ade28838c50eb84c46c5fb8058aa974f5d))
* **embed:** cap embed-queue retries at MaxEmbedAttempts ([#369](https://github.com/germanamz/tusk/issues/369)) ([a6a2e6f](https://github.com/germanamz/tusk/commit/a6a2e6f71963ed8c70ba48336a208fa5cb71da31))
* **embed:** default chunker is MarkdownRecursive in production paths ([#376](https://github.com/germanamz/tusk/issues/376)) ([7f74ae4](https://github.com/germanamz/tusk/commit/7f74ae49f244c7c739aedb481ac916ed8eacd3c3))
* **embed:** markdown-structural recursive chunking strategy ([#372](https://github.com/germanamz/tusk/issues/372)) ([d43c528](https://github.com/germanamz/tusk/commit/d43c528526a52dfbec85e87f56686c18c7ac7b04))
* **embed:** parallel embed workers and configurable timeout (Spec B Phase 1) ([#381](https://github.com/germanamz/tusk/issues/381)) ([66f2e53](https://github.com/germanamz/tusk/commit/66f2e5394dfe75c869ac9e2d4405ee437d00b100))
* **embed:** per-chunk drain loop with delete-then-insert cleanup ([#373](https://github.com/germanamz/tusk/issues/373)) ([d54e27f](https://github.com/germanamz/tusk/commit/d54e27fcd16405364a4f4b2b5a1fa4eb50870fb9))
* **embed:** resolve worker pool size via env + manifest ([#475](https://github.com/germanamz/tusk/issues/475)) ([41893dc](https://github.com/germanamz/tusk/commit/41893dc8fb7a8e18e344a7cb187a1ba07296ca55))
* **filter:** accept qualified edge-type identifiers in filter grammar ([#451](https://github.com/germanamz/tusk/issues/451)) ([9dc95da](https://github.com/germanamz/tusk/commit/9dc95da11d137a012028cbc91cc19533192a15db))
* **filter:** aggregate semantic rank by max-per-node across chunks ([#374](https://github.com/germanamz/tusk/issues/374)) ([ff26843](https://github.com/germanamz/tusk/commit/ff26843798e4418bacbaa3d253b7376e3b5ef3d4))
* **filter:** compile node-type literals as scope-aware typerefs ([#449](https://github.com/germanamz/tusk/issues/449)) ([73e6fad](https://github.com/germanamz/tusk/commit/73e6fad26c71d49ba4803a768e1871777a7adee1))
* **filter:** traversal shortcuts respect per-edge hierarchy alias ([#407](https://github.com/germanamz/tusk/issues/407)) ([a4505ef](https://github.com/germanamz/tusk/commit/a4505ef72bec40bf2c24cadbf1a629c3fad07d2f))
* **index:** add file_state lease primitive and worker identity ([#461](https://github.com/germanamz/tusk/issues/461)) ([41180f0](https://github.com/germanamz/tusk/commit/41180f059825fc9c278dea97d736966a6c4d6626))
* **index:** add file_state table and FileStateRepo CRUD ([#459](https://github.com/germanamz/tusk/issues/459)) ([6418786](https://github.com/germanamz/tusk/commit/64187860ebfdce200f054f1023459998e3e24ca8))
* **index:** add lease and kind columns to embed_queue ([#460](https://github.com/germanamz/tusk/issues/460)) ([ce531d3](https://github.com/germanamz/tusk/commit/ce531d3755c71a465f5abf171043a9489ea3d010))
* **index:** add NeighborsByEdgeRefs accepting typeref scope-aware refs ([#447](https://github.com/germanamz/tusk/issues/447)) ([20099a9](https://github.com/germanamz/tusk/commit/20099a9dd0dd5849ae8f26a5d7d0417eaf79919f))
* **index:** add nullable kind/source columns to edges ([#433](https://github.com/germanamz/tusk/issues/433)) ([c729701](https://github.com/germanamz/tusk/commit/c729701734a6ce9068409cd9140bb8761bb3f9c4))
* **index:** add nullable kind/source columns to nodes ([#425](https://github.com/germanamz/tusk/issues/425)) ([87f3910](https://github.com/germanamz/tusk/commit/87f39106a44dada060cd536ad63bf0348d93b0f2))
* **index:** add SchemaVersion constant and meta persistence ([#418](https://github.com/germanamz/tusk/issues/418)) ([09bf297](https://github.com/germanamz/tusk/commit/09bf2974fcdd15770f32a684804daf99ecd55068))
* **index:** convert embed_queue Drain to lease claim + Ack/Nack ([#462](https://github.com/germanamz/tusk/issues/462)) ([8c06478](https://github.com/germanamz/tusk/commit/8c0647885158000e1627d22fde7cc1d7043cac68))
* **index:** file-row upserts set kind='file', source=NULL ([#426](https://github.com/germanamz/tusk/issues/426)) ([e2973e9](https://github.com/germanamz/tusk/commit/e2973e98497575f26f6917d087959b28fff7f5c9))
* **indexopen:** add OpenOrRebuild helper for schema-version rebuilds ([#421](https://github.com/germanamz/tusk/issues/421)) ([e5c5354](https://github.com/germanamz/tusk/commit/e5c5354788dfe1a06c13e0ea22f3237c2a08506f))
* **index:** phase-2 finishing — nodes reshape complete ([#431](https://github.com/germanamz/tusk/issues/431)) ([cfd775d](https://github.com/germanamz/tusk/commit/cfd775d24b7c419a3d98412389cf55ebd7c5846a))
* **index:** return SchemaVersionError on version mismatch ([#420](https://github.com/germanamz/tusk/issues/420)) ([938a7fd](https://github.com/germanamz/tusk/commit/938a7fd5a4ceed49941a86b45449d53d40b6e7f2))
* **index:** tighten edges DDL with NOT NULL kind, CHECK, UNIQUE+source ([#440](https://github.com/germanamz/tusk/issues/440)) ([a4822f5](https://github.com/germanamz/tusk/commit/a4822f5e4b58ffff11585985506bba49d3b0965a))
* **index:** tighten nodes DDL with NOT NULL kind, CHECK, composite index ([#430](https://github.com/germanamz/tusk/issues/430)) ([af805a5](https://github.com/germanamz/tusk/commit/af805a5083fdd09681f308763e90564a6928467f))
* **inmem:** hot-reload project repository ([566694b](https://github.com/germanamz/tusk/commit/566694b62c3377bc3ef65edc9e02c36193543795))
* **inmem:** hot-reload workflow repository ([f443d34](https://github.com/germanamz/tusk/commit/f443d341f3d6cd18a079901cf76334cf7c3e114c))
* **leaseconfig:** plumb lease TTL via env and manifest ([#463](https://github.com/germanamz/tusk/issues/463)) ([92e5977](https://github.com/germanamz/tusk/commit/92e5977ded2ba2f71f593e37280b40cbe7d47ac7))
* **manifest:** SubUnitConflict no longer fires for user-vs-pack collisions ([#443](https://github.com/germanamz/tusk/issues/443)) ([9d0ce89](https://github.com/germanamz/tusk/commit/9d0ce898c6fc9a83b7d9f2803888ebc9e8e53093))
* **mcp:** drop body parameter from tusk_node_modify ([#457](https://github.com/germanamz/tusk/issues/457)) ([1d04026](https://github.com/germanamz/tusk/commit/1d04026e4735bc478e8e40bb956b9d1dac46c5b7))
* **mcp:** implement tusk_config_set with storage.* guard ([387f1bb](https://github.com/germanamz/tusk/commit/387f1bb2de2b689f28ef29cc71de1668911af1c3))
* **mcp:** implement tusk_config_show handler ([87ae958](https://github.com/germanamz/tusk/commit/87ae9586fcef9ac7bcc8576377a23ff144baa7c9))
* **mcp:** implement tusk_project_create ([e50a829](https://github.com/germanamz/tusk/commit/e50a829cd98639a842daa4f1f490ff7cdaba842d))
* **mcp:** implement tusk_project_delete ([746eec0](https://github.com/germanamz/tusk/commit/746eec070d66981f9d433a87911a952a7075a945))
* **mcp:** implement tusk_project_modify ([25f85e6](https://github.com/germanamz/tusk/commit/25f85e6874af264b0574715df060dca84fe15b1a))
* **mcp:** implement tusk_workflow_create ([a1c3790](https://github.com/germanamz/tusk/commit/a1c3790c5a18f1649038d3fe828e02c2835e6d2f))
* **mcp:** implement tusk_workflow_delete ([a6e8376](https://github.com/germanamz/tusk/commit/a6e837658d37627506828491bef866fabcc6db29))
* **mcp:** implement tusk_workflow_modify ([5699f68](https://github.com/germanamz/tusk/commit/5699f687079239c5a89f62b21b9e2c7719c79278))
* **mcp:** parse qualified type references; remove NeighborsByEdgeTypes ([#452](https://github.com/germanamz/tusk/issues/452)) ([80946f1](https://github.com/germanamz/tusk/commit/80946f16c19497f8a528a348d4b052e26ab77a0b))
* **mcp:** plumb loadOpts and hot-reload helper into server ([005eecc](https://github.com/germanamz/tusk/commit/005eeccfc7f22a633309841450a17839f703d7e5))
* **mcp:** register project create/modify/delete tool shells ([ff13ff8](https://github.com/germanamz/tusk/commit/ff13ff8d56b2ae559f73edd73a4e1e4290473f2f))
* **mcp:** register tusk_config_show and tusk_config_set tool shells ([e80faff](https://github.com/germanamz/tusk/commit/e80faff2a6c1a0760b1f3eb91fd0a97303225b54))
* **mcp:** register workflow create/modify/delete tool shells ([1fd7c75](https://github.com/germanamz/tusk/commit/1fd7c75203e10abbaaec8b884fe17a1eeae26491))
* **mcp:** skip watcher and tusk watch CLI when workers=0 ([#476](https://github.com/germanamz/tusk/issues/476)) ([369462d](https://github.com/germanamz/tusk/commit/369462d8360b20413f5a877c0222a8be62f592f4))
* **mcp:** warn at startup when embed workers disabled ([#477](https://github.com/germanamz/tusk/issues/477)) ([8a1cb24](https://github.com/germanamz/tusk/commit/8a1cb24e486c20cec8e70018148079ced99651d2))
* **migrations:** add notes table with composite and partial indexes ([64573d2](https://github.com/germanamz/tusk/commit/64573d2a77a89c43d54e6d2accd2bc31ed460c47))
* **node:** add WriteWithLease helper for lease-protected writes ([#464](https://github.com/germanamz/tusk/issues/464)) ([3488752](https://github.com/germanamz/tusk/commit/3488752e5e4d67523cf55de9211b55cfd9c0cce4))
* **node:** convert node.Delete to WriteWithLease ([#467](https://github.com/germanamz/tusk/issues/467)) ([4bc6841](https://github.com/germanamz/tusk/commit/4bc68413dc61db64a022ba7c2dbbd29009c38057))
* **node:** convert node.Rename to lease-protected move ([#468](https://github.com/germanamz/tusk/issues/468)) ([9f4bb08](https://github.com/germanamz/tusk/commit/9f4bb08549d9678094d401f210b26b90ce46bcc0))
* **node:** convert Service.Create to WriteWithLease ([#465](https://github.com/germanamz/tusk/issues/465)) ([31ba4ac](https://github.com/germanamz/tusk/commit/31ba4ac34c1e56f4899839f99409a3f7e6ca67f2))
* **node:** convert Service.Modify to WriteWithLease ([#466](https://github.com/germanamz/tusk/issues/466)) ([92ab287](https://github.com/germanamz/tusk/commit/92ab287cb813d34500a5f1ad468cc8de2b4bc049))
* **query,doctor:** semantic snippets and doctor chunking diagnostics ([#380](https://github.com/germanamz/tusk/issues/380)) ([f53b29a](https://github.com/germanamz/tusk/commit/f53b29a2671f584921fed58223acf39110bb9646))
* **query:** pass chunk_idx through semantic query candidate build ([#375](https://github.com/germanamz/tusk/issues/375)) ([e4f8fc6](https://github.com/germanamz/tusk/commit/e4f8fc69b7fa7ac849fc644a5240c25c357373e9))
* **query:** tusk_query semantic agent ergonomics ([#384](https://github.com/germanamz/tusk/issues/384)) ([dacbf2a](https://github.com/germanamz/tusk/commit/dacbf2addc6c0a9ae9477e1ed338fdb90b52cb58))
* **reindex:** add reindex_gen counter and last_seen_gen tracking ([#471](https://github.com/germanamz/tusk/issues/471)) ([3522814](https://github.com/germanamz/tusk/commit/3522814a8499854b2344d56151b8588757740cb2))
* **reindex:** drain reindex jobs via parallel worker pool ([#474](https://github.com/germanamz/tusk/issues/474)) ([19a5c27](https://github.com/germanamz/tusk/commit/19a5c27d523d6cb8a6a5403c0c6fefa8a0534f69))
* **reindex:** enqueue per-file reindex jobs during walk (bridge) ([#473](https://github.com/germanamz/tusk/issues/473)) ([8ea3cad](https://github.com/germanamz/tusk/commit/8ea3cadc7738645e347ce048e43b74aace2d86bb))
* **reindex:** replace seenPaths reap with generation-based + lease-confirmed reap ([#472](https://github.com/germanamz/tusk/issues/472)) ([781128e](https://github.com/germanamz/tusk/commit/781128e20aef35e26edb09968e9470dfb6b1fea6))
* **repo:** add GetByID(uuid.UUID) to ProjectRepository interface ([49997cf](https://github.com/germanamz/tusk/commit/49997cff9c77c83c7aa4d7923b6b02e88ce49c9c))
* **repo:** add GetByID(uuid.UUID) to WorkflowRepository interface ([7e19b59](https://github.com/germanamz/tusk/commit/7e19b591e979f32c00f68f43e1818d7527644c2e))
* **repository:** add NoteRepository interface with window-aware List ([46d0db8](https://github.com/germanamz/tusk/commit/46d0db829b6451f74773c172d3ca9ace2b907240))
* **service:** add RepoBundle and resolver type aliases ([dbb7829](https://github.com/germanamz/tusk/commit/dbb7829ee78843d8a7abc51f9f2fa60b804db3c3))
* **service:** add WorkflowService.GetByID(uuid.UUID) ([eaa4909](https://github.com/germanamz/tusk/commit/eaa4909d926101267f9838062c3c9af040485d45))
* **service:** hot-reload urgency engine defaults ([102d96a](https://github.com/germanamz/tusk/commit/102d96a9fffe5cf6dd6a5fab2fcc4d1c20f4ad3c))
* **service:** phase 3 — ProjectService writes + caller migration ([295b3f2](https://github.com/germanamz/tusk/commit/295b3f212d245ad6a8ac96d5e49bc1347816a77f))
* **service:** phase 4 — WorkflowService writes + caller migration ([fc53f78](https://github.com/germanamz/tusk/commit/fc53f78abc7ffd36f0118ecd5abc09b0e52da41a))
* **service:** phase 5 — remove inmem package and MCP config mutex ([650823a](https://github.com/germanamz/tusk/commit/650823aca80f5d61d3175edfb1a52bd1e51a2539))
* **service:** route services through BundleResolver and fan out reads ([6cbe290](https://github.com/germanamz/tusk/commit/6cbe29024097f650164842c2b3eff8cb76697f3b))
* **sqlite:** add NoteRepo with Create, GetByID, Archive, and List ([0dc26b6](https://github.com/germanamz/tusk/commit/0dc26b6a65acd08a09dc038eae3fae83807dad9d))
* **sqlite:** add projects table and seed _default project ([260920f](https://github.com/germanamz/tusk/commit/260920fe6d8ef49d858d0c949a7eb4c951d069ee))
* **sqlite:** add StoreRegistry for per-project databases ([3c701d7](https://github.com/germanamz/tusk/commit/3c701d7aecaa8872523c756b7d2bbfba77002843))
* **sqlite:** add tasks.project_id FK and rebuild tasks table ([9f17e09](https://github.com/germanamz/tusk/commit/9f17e09bd4858de559e85ff9554c22bdc2a0ff63))
* **sqlite:** add workflows table and seed kanban workflow ([e826ef3](https://github.com/germanamz/tusk/commit/e826ef39682d637665655a6c7763b108dd961c6e))
* **sqlite:** implement ProjectRepo read operations ([17a5dff](https://github.com/germanamz/tusk/commit/17a5dff5c3b5507d128879009fcd5b79620afd1b))
* **sqlite:** implement ProjectRepo.Delete with optimistic locking ([193932c](https://github.com/germanamz/tusk/commit/193932cf6138f7bdc1a14dba4c2bfd73cba13fa3))
* **sqlite:** implement ProjectRepo.Update and CountByWorkflow ([7d5b0ee](https://github.com/germanamz/tusk/commit/7d5b0ee26afc24cd6d7f2f3f3c3adf0a2927406a))
* **sqlite:** implement WorkflowRepo read operations ([11961c6](https://github.com/germanamz/tusk/commit/11961c6f26d0fb892e16eff69ccff5ad80e9699e))
* **sqlite:** implement WorkflowRepo.Delete with optimistic locking ([58de9e4](https://github.com/germanamz/tusk/commit/58de9e48aea317f925b02fb0445e9dbd86d0a416))
* **sqlite:** implement WorkflowRepo.Update with optimistic locking ([29e8df6](https://github.com/germanamz/tusk/commit/29e8df6ee9c04a090733b1a913788d65cc6cea7f))
* **subunit:** contains edges write kind='structural', source='markdown' ([#437](https://github.com/germanamz/tusk/issues/437)) ([62a3189](https://github.com/germanamz/tusk/commit/62a3189d523cc61b450261a8cff5bedcb8e4cd3f))
* **subunit:** contains edges write kind='structural', source='markdown' ([#438](https://github.com/germanamz/tusk/issues/438)) ([14580cf](https://github.com/germanamz/tusk/commit/14580cfdf211ad5e84aeb0327f3f73c6772dc037))
* **subunit:** sync writes kind='subunit', source='markdown' ([#427](https://github.com/germanamz/tusk/issues/427)) ([990e11e](https://github.com/germanamz/tusk/commit/990e11eb73c78d547a4f35f5e1490c8ac632e710))
* **syntax:** strip registered prefix modifiers into Token.Modifier ([17d94c1](https://github.com/germanamz/tusk/commit/17d94c1bac63477cf69b79aa7f028b7427a3ea59))
* **task:** type Task.ProjectID as uuid.UUID and resolve names end-to-end ([84af8e2](https://github.com/germanamz/tusk/commit/84af8e24c2b179932c4a22b08e7714cdc772e768))
* **tui:** add project create/modify inline-syntax parsers ([a87e016](https://github.com/germanamz/tusk/commit/a87e016574a0241f6c184a75e6fa253e8852036f))
* **tui:** add tusk project create/modify/delete commands ([7f1a381](https://github.com/germanamz/tusk/commit/7f1a3816f04c11ac351221b4f365f7cc1afb3a26))
* **tui:** prepend active-file header to config show ([c0c5610](https://github.com/germanamz/tusk/commit/c0c561074a008dbc54bebb5e516383df7fa78edf))
* **tui:** workspace-aware config writes ([6e4b72c](https://github.com/germanamz/tusk/commit/6e4b72c8615a63a613bdd48d3f954743621f157b))
* **typepacks:** subdocument exposes Source() and source-scoped reservation semantics ([#442](https://github.com/germanamz/tusk/issues/442)) ([a8c19df](https://github.com/germanamz/tusk/commit/a8c19df4e06f83999f9dcd3e2a25bc225bdf2a30))
* **typeref:** parser for &lt;source&gt;:&lt;type&gt; canonical notation ([#446](https://github.com/germanamz/tusk/issues/446)) ([298ef45](https://github.com/germanamz/tusk/commit/298ef45becfbc9807add4d09ff2f34eda4d5999b))
* **v0.11:** add collectUDAs, validateKnownFields, and reservedTaskFields helpers ([0800103](https://github.com/germanamz/tusk/commit/0800103ba08e2c443f8c76061d302487dc0ad5c1))
* **v0.11:** add inline [@file](https://github.com/file) expander and [inline] config section ([5ea8e00](https://github.com/germanamz/tusk/commit/5ea8e0021f2ae2be3e5a93deae2d7d465972aef6))
* **v0.11:** add tusk completion subcommand ([560e79a](https://github.com/germanamz/tusk/commit/560e79a8c101f2c9bb3afff5db41e0747deb9b09))
* **v0.11:** phase 4 — task relations migration and MCP rename ([cafb3b9](https://github.com/germanamz/tusk/commit/cafb3b9ce749ea81ae7e8c121dd360c9b71e8bdd))
* **v0.11:** route task string fields through inline [@file](https://github.com/file) expander ([9cca9a8](https://github.com/germanamz/tusk/commit/9cca9a8caf78484b1e65efcdcf7e2928a2f1cf8f))
* **v0.11:** wire inline UDA syntax, remove --uda flag ([b35fc45](https://github.com/germanamz/tusk/commit/b35fc459be7e649dfc80cb833abfa5676619193b))
* **v0.12:** add MCP blocked_fields config surface ([f7413b7](https://github.com/germanamz/tusk/commit/f7413b70277ab8ed825ffb19926117acea9e4677))
* **v0.12:** add Note MCP tools ([7806374](https://github.com/germanamz/tusk/commit/7806374335e6ebc72bf03a61c2d8d187987fb6e0))
* **v0.12:** add NoteService with create/archive/list ([9ba996c](https://github.com/germanamz/tusk/commit/9ba996c36c38baa12206d2c77042213b51da1ec4))
* **v0.12:** add player modify with note-window-size override ([08de0cf](https://github.com/germanamz/tusk/commit/08de0cf7e5f201dc1eee58ff8dfd903aaef5c061))
* **v0.12:** add shared helpers for Note CLI ([f2b6c4f](https://github.com/germanamz/tusk/commit/f2b6c4f26a7526ee909e8752ca4935ac63d056cf))
* **v0.12:** add tusk note add and archive CLI commands ([0383dff](https://github.com/germanamz/tusk/commit/0383dff0b9caa40d7acf4f8acacf5a071619802a))
* **v0.12:** add tusk note list with trailing-window filters ([7eef951](https://github.com/germanamz/tusk/commit/7eef951d1c001fc889aac5e0db52b3d29990d462))
* **v0.12:** enforce MCP blocked_fields at runtime ([8e9b2ac](https://github.com/germanamz/tusk/commit/8e9b2ac8d8a280dbed0ab85bae7cfaff7cbf9af7))
* **v0.12:** resolve NoteService window through full chain ([a3d358b](https://github.com/germanamz/tusk/commit/a3d358b540fecef586c7ce2b19c1f2a56e1438db))
* **v0.12:** validate MCP blocked_fields at startup ([c35522a](https://github.com/germanamz/tusk/commit/c35522a4c70c3b9ae893e2b184a4f89a0d0be442))
* **v0.13:** add event log data layer ([a495f38](https://github.com/germanamz/tusk/commit/a495f3868daf639712cbb9fe5b59ba9fc8b50f4b))
* **v0.13:** add task level taxonomy foundation ([e257ec6](https://github.com/germanamz/tusk/commit/e257ec6c4ee08b15df451db6515043fddb245131))
* **v0.13:** cli surface + rendering for urgency overrides (phase 4) ([92d18f2](https://github.com/germanamz/tusk/commit/92d18f29564a2b836735269c06461c93c311b448))
* **v0.13:** cli, mcp, and filter surface for sibling order (phase 3) ([ec2fee6](https://github.com/germanamz/tusk/commit/ec2fee6647474c4c5d3e33a876cb9a4a90ec67b2))
* **v0.13:** cutover ROADMAP.md to tusk-generated source-of-truth ([203e9b2](https://github.com/germanamz/tusk/commit/203e9b28b94e751553cd3928577ca626530cbf6d))
* **v0.13:** data portability phase 1 foundations ([842bccb](https://github.com/germanamz/tusk/commit/842bccbd8e8b1a0385c41e9894ed55b05006b0af))
* **v0.13:** data portability phase 2 codec ([2698b14](https://github.com/germanamz/tusk/commit/2698b14ab3d664e0edbf77586c801387c799af7b))
* **v0.13:** data portability phase 3 service ([6c6233d](https://github.com/germanamz/tusk/commit/6c6233d85259c4906217a254c1befda49d3a1418))
* **v0.13:** data portability phase 4 CLI commands ([7111c32](https://github.com/germanamz/tusk/commit/7111c32f5fd985a91b2c4732b2191d5adfd062d4))
* **v0.13:** emit level on CLI JSON task output (phase 3 follow-up) ([4e492ad](https://github.com/germanamz/tusk/commit/4e492ad454aed07d3676181fbe064252cca87ac7))
* **v0.13:** emit relation add/remove events transactionally (phase 5) ([8a9e8b3](https://github.com/germanamz/tusk/commit/8a9e8b3a8ea343a84a8b25f36055c2301e572bd0))
* **v0.13:** emit task events from service mutations (phase 3) ([b9325fa](https://github.com/germanamz/tusk/commit/b9325fadb2a82e807c7273002435b3e36c43a46f))
* **v0.13:** enforce task level taxonomy and expose level on CLI/MCP (phase 3) ([494120b](https://github.com/germanamz/tusk/commit/494120b41391a80046cc611ba4e75434ae5cb054))
* **v0.13:** evalfilter honors tag predicates for descendant rollups ([563cbb3](https://github.com/germanamz/tusk/commit/563cbb3d0f357fd25d9b6000e40388c32234e4d1))
* **v0.13:** expose taxonomy on project modify, filter, and config (phase 4) ([a70fa5a](https://github.com/germanamz/tusk/commit/a70fa5ae90f33a8eeb92fb0ce7eeee7fe5f92221))
* **v0.13:** level-check, renderers, and E2E suite (phase 5) ([44245ba](https://github.com/germanamz/tusk/commit/44245bad83c99c32c93600912fa892ee28a086ef))
* **v0.13:** make the repo root a tusk workspace ([1288545](https://github.com/germanamz/tusk/commit/1288545272c5a8c451553cbf91543e4cfef6a42a))
* **v0.13:** markdown annotations and notes blocks (phase 5) ([2baab58](https://github.com/germanamz/tusk/commit/2baab58dae2c5f6890534693f542d2c123aac1ca))
* **v0.13:** markdown body renderer for task tree (phase 4) ([89ffb79](https://github.com/germanamz/tusk/commit/89ffb79058ef288ae3b32cc749b554fd9f76d210))
* **v0.13:** markdown tree dispatch + stub renderer (phase 3) ([f434043](https://github.com/germanamz/tusk/commit/f43404331ad7f98e9e543acf9624348b552d3a8e))
* **v0.13:** mcp surface for subtree urgency overrides (phase 5) ([dfc75d6](https://github.com/germanamz/tusk/commit/dfc75d67cbf6f8d7b59e60c927d754c08f8bb14b))
* **v0.13:** move, resequence, task_moved events (phase 2) ([4d55a85](https://github.com/germanamz/tusk/commit/4d55a859a8f6309f7719137601e42fc2f70764de))
* **v0.13:** plumb project description through CLI, MCP, codec, render (phase 2) ([9b674a6](https://github.com/germanamz/tusk/commit/9b674a694f2f8f04cc95fa3b6fdbcaede195f24d))
* **v0.13:** project description column, domain field, repo wiring (phase 1) ([4d0c5e4](https://github.com/germanamz/tusk/commit/4d0c5e427b98486835f3a08c915aea387257618b))
* **v0.13:** resolve subtree urgency override chain (phase 2) ([c969f80](https://github.com/germanamz/tusk/commit/c969f8009ed71532eb60fb2fd51e9ef33ce38316))
* **v0.13:** roadmap regeneration tooling and migration tracker (phase 6) ([50ce8be](https://github.com/germanamz/tusk/commit/50ce8be985a412af2b5bf14a16948caa03902f9d))
* **v0.13:** rollup domain types + service primitives (phase 1) ([f063ff4](https://github.com/germanamz/tusk/commit/f063ff478c7c09a1724c0f0404aee4f903e96f56))
* **v0.13:** single-event Start and Pop with tx-invariant coverage (phase 4) ([ab37038](https://github.com/germanamz/tusk/commit/ab37038845c6924449dfadd9b1b13c287815b2af))
* **v0.13:** surface task order in JSON renderer ([93dfee9](https://github.com/germanamz/tusk/commit/93dfee9a5bf97a858303d863d1360a90c3c7b026))
* **v0.13:** task order column, repo helpers, backfill migration (phase 1) ([8a841b8](https://github.com/germanamz/tusk/commit/8a841b8a9aa4db610fce77812af2f1be4ee3382d))
* **v0.13:** track repo-root tusk workspace as a v0.13 initiative ([8103310](https://github.com/germanamz/tusk/commit/810331070bfc8ad35fd137b72cb160cbdab01101))
* **v0.13:** tree --rollup flag with branch progress badges (phase 2) ([457ba58](https://github.com/germanamz/tusk/commit/457ba58ddd52f5d970bdd05e9def905ff3a86e50))
* **v0.13:** tusk task summary CLI with rollup blocks (phase 3) ([a9f42e9](https://github.com/germanamz/tusk/commit/a9f42e956a33f5e295cdad016492752309fce621))
* **v0.13:** tusk_task_summary MCP tool (phase 4) ([9b9fbaa](https://github.com/germanamz/tusk/commit/9b9fbaa07a177130a11b3d44c84784363248cca7))
* **v0.13:** urgency_overrides column, domain types, ancestor walk (phase 1) ([ba24f66](https://github.com/germanamz/tusk/commit/ba24f663aac811a62e68c1b8492775e7620cc3f3))
* **v0.13:** wire event-log shared plumbing (phase 2) ([6348d1e](https://github.com/germanamz/tusk/commit/6348d1ee19ab4b004b6baa7341648b9236d5f19b))
* **v0.13:** wire task level taxonomy through config and services (phase 2) ([28d6561](https://github.com/germanamz/tusk/commit/28d6561de0db49637135ab0730587f395860f62c))
* **v0.13:** wire urgency-overrides into TaskService.Update (phase 3) ([cd16dfa](https://github.com/germanamz/tusk/commit/cd16dfa246e00f9ded0f9f769fe4b8360b5e676f))
* **v0.14:** add blankline analyzer (rule 2) ([054841f](https://github.com/germanamz/tusk/commit/054841f67bb3d9ec55629443606a386712d45dd7))
* **v0.14:** add namederr analyzer (rule 3) ([4d5489c](https://github.com/germanamz/tusk/commit/4d5489c909dd97d8162b1a42aad512051de62eae))
* **v0.14:** add pathfilter helper and wire into analyzers (rule 2/3/4) ([b9ee942](https://github.com/germanamz/tusk/commit/b9ee9422f6db3007f2c641b533009f1e6cdb1314))
* **v0.14:** add testhandle analyzer (rule 4) ([d556ed0](https://github.com/germanamz/tusk/commit/d556ed010d14e3731d7681073419b8120774b611))
* **v0.14:** register blankline/namederr/testhandle in cmd/tusk-lint ([1a8bf65](https://github.com/germanamz/tusk/commit/1a8bf650472ff62775b91c452baeaa1aff25dde4))
* **v0.14:** scaffold cmd/tusk-lint multichecker shell ([b774a34](https://github.com/germanamz/tusk/commit/b774a3437f0edf3d5f876e6ce26f3c2be3b8961f))
* wire NoteRepo into RepoBundle, Tx, and Client ([ea53f46](https://github.com/germanamz/tusk/commit/ea53f463c35d128e20227fe78c02ad150c840916))
* **workspace:** commit repo-root tusk.toml and drop TUSK_DB plumbing ([71524ef](https://github.com/germanamz/tusk/commit/71524ef9f163fef7025163eb8ba8de950ed63ebf))


### Bug Fixes

* **ci:** drop package-name and set explicit release-please PR title pattern ([#389](https://github.com/germanamz/tusk/issues/389)) ([c3824ce](https://github.com/germanamz/tusk/commit/c3824cee97aec18b85714f55e74c5e15776ede31))
* **devcontainer:** allow R2 blob hosts in tinyproxy for ollama pulls ([#367](https://github.com/germanamz/tusk/issues/367)) ([b1dc6f3](https://github.com/germanamz/tusk/commit/b1dc6f35162f24fd5913a3cc61e6c338d8ff4783))
* **devcontainer:** install Claude Code into vscode-writable ~/.claude/local ([c7e2c04](https://github.com/germanamz/tusk/commit/c7e2c0429c4a20ce97c9659f73ff2780dddb5382))
* **devcontainer:** install Claude Code via native installer ([424aef8](https://github.com/germanamz/tusk/commit/424aef856b4f7b2a030d499b7f6539b81eefc37e))
* **devcontainer:** switch Ollama asset to .tar.zst and cache builds ([#366](https://github.com/germanamz/tusk/issues/366)) ([e509c0b](https://github.com/germanamz/tusk/commit/e509c0b6a11dd61c079db0a61cec1b70a91cc328))
* **docs:** expand tusk --help/man reference with config + examples ([#479](https://github.com/germanamz/tusk/issues/479)) ([61490f8](https://github.com/germanamz/tusk/commit/61490f8a3926dee440c7c3d21de9d0dd6c2a030a))
* **doctor:** treat behavior-reserved properties as declared ([#399](https://github.com/germanamz/tusk/issues/399)) ([650876c](https://github.com/germanamz/tusk/commit/650876c760765261aee6d39e694039a6acaa73fd))
* **domain:** preserve UnknownPayload bytes on JSON marshal ([cf33b0e](https://github.com/germanamz/tusk/commit/cf33b0e5b5f10ff6a604e292da4c681a3d8025eb))
* **embed:** skip drain re-embed when content hash matches ([#387](https://github.com/germanamz/tusk/issues/387)) ([0b6f11a](https://github.com/germanamz/tusk/commit/0b6f11a7f5a44b47d291b566e0ef2b12d0311346))
* **embed:** tighten MarkdownRecursive MaxBytes from 7200 to 4000 ([#377](https://github.com/germanamz/tusk/issues/377)) ([59cd732](https://github.com/germanamz/tusk/commit/59cd73243d832e3563d1e19f2f4e2959488e8e60))
* **filter:** accept `:` as alias for `=` and align help text with reality ([#397](https://github.com/germanamz/tusk/issues/397)) ([4eaea39](https://github.com/germanamz/tusk/commit/4eaea392c865732abe1d5339829bbea5ca41d8de))
* **index:** embeddings UNIQUE(node_id, chunk_idx) so hash-skip can fire ([#454](https://github.com/germanamz/tusk/issues/454)) ([0361ef4](https://github.com/germanamz/tusk/commit/0361ef4f58a7fa4e89cac6ff50875becf707eeda))
* **lock:** hold workspace lock for MCP and watch lifetimes ([#378](https://github.com/germanamz/tusk/issues/378)) ([2b8330e](https://github.com/germanamz/tusk/commit/2b8330e690e4298d49c678a9841e332c50d53c8f))
* **mcp:** serialize config mutations under server mutex ([0efcecf](https://github.com/germanamz/tusk/commit/0efcecfb97e162995e8066a3f6acee2dfb167f25))
* **mcp:** serialize workflow and project mutations under config mutex ([eec8ec6](https://github.com/germanamz/tusk/commit/eec8ec6a2d3b581f79796ae675c8c379ddda3898))
* **node:** inherit source extension when `tusk node move` target has none ([#401](https://github.com/germanamz/tusk/issues/401)) ([32433ff](https://github.com/germanamz/tusk/commit/32433ff5a160825a8b40e133734f324e976df45e))
* **node:** YAML-quote frontmatter `type` and `title` and ambiguous literals ([#402](https://github.com/germanamz/tusk/issues/402)) ([8e849d3](https://github.com/germanamz/tusk/commit/8e849d318a64ce5b064292d63c2e570de32449ba))
* **release:** inject version into binary via ldflags ([#391](https://github.com/germanamz/tusk/issues/391)) ([992e7e4](https://github.com/germanamz/tusk/commit/992e7e48b4ae361cea7bfa8057a9b415ee328513))
* **tui:** score urgency on tree subtree view ([d2bae4f](https://github.com/germanamz/tusk/commit/d2bae4f9fd23390fca468e66ca23ee7b67961b95))
* **v0.13:** accept project= inline filter on task tree, atomic make roadmap ([0b2b5b6](https://github.com/germanamz/tusk/commit/0b2b5b60f7113a699cb9530e41fa7793ac6980c1))
* **v0.13:** commit roadmap-state.db, drop the JSON snapshot indirection ([4926001](https://github.com/germanamz/tusk/commit/4926001789b4f79fca292bda015a046ae20c014f))
* **v0.13:** commit roadmap-state.json snapshot, hydrate CI from it ([8cb9f54](https://github.com/germanamz/tusk/commit/8cb9f54d020e4c38552da706e64e267df39ace28))
* **v0.14:** improve blankline analyzer fixture and diagnostic anchors ([b61dbcf](https://github.com/germanamz/tusk/commit/b61dbcfd0047c42702bd5198e7d0f88da662fe8d))
* **v0.14:** pathfilter must match external test packages ([99427a1](https://github.com/germanamz/tusk/commit/99427a11d48e49530f9151b335e51315e1c57cbf))


### Miscellaneous Chores

* **v1:** retire v0.x line, set up v1 integration branch ([#351](https://github.com/germanamz/tusk/issues/351)) ([e3d7c0f](https://github.com/germanamz/tusk/commit/e3d7c0f10da8b515187158d288dfda78233bd0a9))

## [1.7.1](https://github.com/germanamz/tusk/compare/v1.7.0...v1.7.1) (2026-05-28)


### Bug Fixes

* **docs:** expand tusk --help/man reference with config + examples ([#479](https://github.com/germanamz/tusk/issues/479)) ([61490f8](https://github.com/germanamz/tusk/commit/61490f8a3926dee440c7c3d21de9d0dd6c2a030a))

## [1.7.0](https://github.com/germanamz/tusk/compare/v1.6.0...v1.7.0) (2026-05-28)


### Features

* **embed:** resolve worker pool size via env + manifest ([#475](https://github.com/germanamz/tusk/issues/475)) ([41893dc](https://github.com/germanamz/tusk/commit/41893dc8fb7a8e18e344a7cb187a1ba07296ca55))
* **index:** add file_state lease primitive and worker identity ([#461](https://github.com/germanamz/tusk/issues/461)) ([41180f0](https://github.com/germanamz/tusk/commit/41180f059825fc9c278dea97d736966a6c4d6626))
* **index:** add file_state table and FileStateRepo CRUD ([#459](https://github.com/germanamz/tusk/issues/459)) ([6418786](https://github.com/germanamz/tusk/commit/64187860ebfdce200f054f1023459998e3e24ca8))
* **index:** add lease and kind columns to embed_queue ([#460](https://github.com/germanamz/tusk/issues/460)) ([ce531d3](https://github.com/germanamz/tusk/commit/ce531d3755c71a465f5abf171043a9489ea3d010))
* **index:** convert embed_queue Drain to lease claim + Ack/Nack ([#462](https://github.com/germanamz/tusk/issues/462)) ([8c06478](https://github.com/germanamz/tusk/commit/8c0647885158000e1627d22fde7cc1d7043cac68))
* **leaseconfig:** plumb lease TTL via env and manifest ([#463](https://github.com/germanamz/tusk/issues/463)) ([92e5977](https://github.com/germanamz/tusk/commit/92e5977ded2ba2f71f593e37280b40cbe7d47ac7))
* **mcp:** drop body parameter from tusk_node_modify ([#457](https://github.com/germanamz/tusk/issues/457)) ([1d04026](https://github.com/germanamz/tusk/commit/1d04026e4735bc478e8e40bb956b9d1dac46c5b7))
* **mcp:** skip watcher and tusk watch CLI when workers=0 ([#476](https://github.com/germanamz/tusk/issues/476)) ([369462d](https://github.com/germanamz/tusk/commit/369462d8360b20413f5a877c0222a8be62f592f4))
* **mcp:** warn at startup when embed workers disabled ([#477](https://github.com/germanamz/tusk/issues/477)) ([8a1cb24](https://github.com/germanamz/tusk/commit/8a1cb24e486c20cec8e70018148079ced99651d2))
* **node:** add WriteWithLease helper for lease-protected writes ([#464](https://github.com/germanamz/tusk/issues/464)) ([3488752](https://github.com/germanamz/tusk/commit/3488752e5e4d67523cf55de9211b55cfd9c0cce4))
* **node:** convert node.Delete to WriteWithLease ([#467](https://github.com/germanamz/tusk/issues/467)) ([4bc6841](https://github.com/germanamz/tusk/commit/4bc68413dc61db64a022ba7c2dbbd29009c38057))
* **node:** convert node.Rename to lease-protected move ([#468](https://github.com/germanamz/tusk/issues/468)) ([9f4bb08](https://github.com/germanamz/tusk/commit/9f4bb08549d9678094d401f210b26b90ce46bcc0))
* **node:** convert Service.Create to WriteWithLease ([#465](https://github.com/germanamz/tusk/issues/465)) ([31ba4ac](https://github.com/germanamz/tusk/commit/31ba4ac34c1e56f4899839f99409a3f7e6ca67f2))
* **node:** convert Service.Modify to WriteWithLease ([#466](https://github.com/germanamz/tusk/issues/466)) ([92ab287](https://github.com/germanamz/tusk/commit/92ab287cb813d34500a5f1ad468cc8de2b4bc049))
* **reindex:** add reindex_gen counter and last_seen_gen tracking ([#471](https://github.com/germanamz/tusk/issues/471)) ([3522814](https://github.com/germanamz/tusk/commit/3522814a8499854b2344d56151b8588757740cb2))
* **reindex:** drain reindex jobs via parallel worker pool ([#474](https://github.com/germanamz/tusk/issues/474)) ([19a5c27](https://github.com/germanamz/tusk/commit/19a5c27d523d6cb8a6a5403c0c6fefa8a0534f69))
* **reindex:** enqueue per-file reindex jobs during walk (bridge) ([#473](https://github.com/germanamz/tusk/issues/473)) ([8ea3cad](https://github.com/germanamz/tusk/commit/8ea3cadc7738645e347ce048e43b74aace2d86bb))
* **reindex:** replace seenPaths reap with generation-based + lease-confirmed reap ([#472](https://github.com/germanamz/tusk/issues/472)) ([781128e](https://github.com/germanamz/tusk/commit/781128e20aef35e26edb09968e9470dfb6b1fea6))

## [1.6.0](https://github.com/germanamz/tusk/compare/v1.5.0...v1.6.0) (2026-05-26)


### Features

* **edges:** frontmatter-direct edges write kind='direct' ([#436](https://github.com/germanamz/tusk/issues/436)) ([37a11fa](https://github.com/germanamz/tusk/commit/37a11fab6b3f16a2c0edee60b8267e9ab9b86165))
* **edges:** ref-derived edges write kind='derived', source=NULL ([#434](https://github.com/germanamz/tusk/issues/434)) ([6ad796a](https://github.com/germanamz/tusk/commit/6ad796ade28838c50eb84c46c5fb8058aa974f5d))
* **filter:** accept qualified edge-type identifiers in filter grammar ([#451](https://github.com/germanamz/tusk/issues/451)) ([9dc95da](https://github.com/germanamz/tusk/commit/9dc95da11d137a012028cbc91cc19533192a15db))
* **filter:** compile node-type literals as scope-aware typerefs ([#449](https://github.com/germanamz/tusk/issues/449)) ([73e6fad](https://github.com/germanamz/tusk/commit/73e6fad26c71d49ba4803a768e1871777a7adee1))
* **index:** add NeighborsByEdgeRefs accepting typeref scope-aware refs ([#447](https://github.com/germanamz/tusk/issues/447)) ([20099a9](https://github.com/germanamz/tusk/commit/20099a9dd0dd5849ae8f26a5d7d0417eaf79919f))
* **index:** add nullable kind/source columns to edges ([#433](https://github.com/germanamz/tusk/issues/433)) ([c729701](https://github.com/germanamz/tusk/commit/c729701734a6ce9068409cd9140bb8761bb3f9c4))
* **index:** add nullable kind/source columns to nodes ([#425](https://github.com/germanamz/tusk/issues/425)) ([87f3910](https://github.com/germanamz/tusk/commit/87f39106a44dada060cd536ad63bf0348d93b0f2))
* **index:** add SchemaVersion constant and meta persistence ([#418](https://github.com/germanamz/tusk/issues/418)) ([09bf297](https://github.com/germanamz/tusk/commit/09bf2974fcdd15770f32a684804daf99ecd55068))
* **index:** file-row upserts set kind='file', source=NULL ([#426](https://github.com/germanamz/tusk/issues/426)) ([e2973e9](https://github.com/germanamz/tusk/commit/e2973e98497575f26f6917d087959b28fff7f5c9))
* **indexopen:** add OpenOrRebuild helper for schema-version rebuilds ([#421](https://github.com/germanamz/tusk/issues/421)) ([e5c5354](https://github.com/germanamz/tusk/commit/e5c5354788dfe1a06c13e0ea22f3237c2a08506f))
* **index:** phase-2 finishing — nodes reshape complete ([#431](https://github.com/germanamz/tusk/issues/431)) ([cfd775d](https://github.com/germanamz/tusk/commit/cfd775d24b7c419a3d98412389cf55ebd7c5846a))
* **index:** return SchemaVersionError on version mismatch ([#420](https://github.com/germanamz/tusk/issues/420)) ([938a7fd](https://github.com/germanamz/tusk/commit/938a7fd5a4ceed49941a86b45449d53d40b6e7f2))
* **index:** tighten edges DDL with NOT NULL kind, CHECK, UNIQUE+source ([#440](https://github.com/germanamz/tusk/issues/440)) ([a4822f5](https://github.com/germanamz/tusk/commit/a4822f5e4b58ffff11585985506bba49d3b0965a))
* **index:** tighten nodes DDL with NOT NULL kind, CHECK, composite index ([#430](https://github.com/germanamz/tusk/issues/430)) ([af805a5](https://github.com/germanamz/tusk/commit/af805a5083fdd09681f308763e90564a6928467f))
* **manifest:** SubUnitConflict no longer fires for user-vs-pack collisions ([#443](https://github.com/germanamz/tusk/issues/443)) ([9d0ce89](https://github.com/germanamz/tusk/commit/9d0ce898c6fc9a83b7d9f2803888ebc9e8e53093))
* **mcp:** parse qualified type references; remove NeighborsByEdgeTypes ([#452](https://github.com/germanamz/tusk/issues/452)) ([80946f1](https://github.com/germanamz/tusk/commit/80946f16c19497f8a528a348d4b052e26ab77a0b))
* **subunit:** contains edges write kind='structural', source='markdown' ([#437](https://github.com/germanamz/tusk/issues/437)) ([62a3189](https://github.com/germanamz/tusk/commit/62a3189d523cc61b450261a8cff5bedcb8e4cd3f))
* **subunit:** contains edges write kind='structural', source='markdown' ([#438](https://github.com/germanamz/tusk/issues/438)) ([14580cf](https://github.com/germanamz/tusk/commit/14580cfdf211ad5e84aeb0327f3f73c6772dc037))
* **subunit:** sync writes kind='subunit', source='markdown' ([#427](https://github.com/germanamz/tusk/issues/427)) ([990e11e](https://github.com/germanamz/tusk/commit/990e11eb73c78d547a4f35f5e1490c8ac632e710))
* **typepacks:** subdocument exposes Source() and source-scoped reservation semantics ([#442](https://github.com/germanamz/tusk/issues/442)) ([a8c19df](https://github.com/germanamz/tusk/commit/a8c19df4e06f83999f9dcd3e2a25bc225bdf2a30))
* **typeref:** parser for &lt;source&gt;:&lt;type&gt; canonical notation ([#446](https://github.com/germanamz/tusk/issues/446)) ([298ef45](https://github.com/germanamz/tusk/commit/298ef45becfbc9807add4d09ff2f34eda4d5999b))


### Bug Fixes

* **index:** embeddings UNIQUE(node_id, chunk_idx) so hash-skip can fire ([#454](https://github.com/germanamz/tusk/issues/454)) ([0361ef4](https://github.com/germanamz/tusk/commit/0361ef4f58a7fa4e89cac6ff50875becf707eeda))

## [1.5.0](https://github.com/germanamz/tusk/compare/v1.4.0...v1.5.0) (2026-05-25)


### Features

* agent retrieval improvements (Phase 1) ([#413](https://github.com/germanamz/tusk/issues/413)) ([4449671](https://github.com/germanamz/tusk/commit/44496715f2ab2b8dceedb027b6afff8b55f9f46b))
* agent retrieval improvements (Phase 2) ([#415](https://github.com/germanamz/tusk/issues/415)) ([c739f6a](https://github.com/germanamz/tusk/commit/c739f6afb474e052a76657815623edafb133e913))
* agent retrieval improvements (Phase 3) ([#416](https://github.com/germanamz/tusk/issues/416)) ([c4e8073](https://github.com/germanamz/tusk/commit/c4e8073affe11741234936b64f6b1bb559d9b68b))

## [1.4.0](https://github.com/germanamz/tusk/compare/v1.3.0...v1.4.0) (2026-05-20)


### Features

* configurable wikilink-materialization edge via wikilinks flag ([#410](https://github.com/germanamz/tusk/issues/410)) ([f9f0da9](https://github.com/germanamz/tusk/commit/f9f0da9929d9534a6cfc936d668d5537d1fe84b7))

## [1.3.0](https://github.com/germanamz/tusk/compare/v1.2.0...v1.3.0) (2026-05-19)


### Features

* **edge:** materialize all edges from frontmatter ([#409](https://github.com/germanamz/tusk/issues/409)) ([e2775b2](https://github.com/germanamz/tusk/commit/e2775b285609e210e8aa7d83ba76559b8d1c6d9e))
* **filter:** traversal shortcuts respect per-edge hierarchy alias ([#407](https://github.com/germanamz/tusk/issues/407)) ([a4505ef](https://github.com/germanamz/tusk/commit/a4505ef72bec40bf2c24cadbf1a629c3fad07d2f))

## [1.2.0](https://github.com/germanamz/tusk/compare/v1.1.0...v1.2.0) (2026-05-17)


### Features

* **edge:** explicit --ordinal flag on `tusk edge add` (bug [#4](https://github.com/germanamz/tusk/issues/4)) ([#400](https://github.com/germanamz/tusk/issues/400)) ([0203d74](https://github.com/germanamz/tusk/commit/0203d74c3db1fb4bd71b702f0ab2196583293821))


### Bug Fixes

* **doctor:** treat behavior-reserved properties as declared ([#399](https://github.com/germanamz/tusk/issues/399)) ([650876c](https://github.com/germanamz/tusk/commit/650876c760765261aee6d39e694039a6acaa73fd))
* **filter:** accept `:` as alias for `=` and align help text with reality ([#397](https://github.com/germanamz/tusk/issues/397)) ([4eaea39](https://github.com/germanamz/tusk/commit/4eaea392c865732abe1d5339829bbea5ca41d8de))
* **node:** inherit source extension when `tusk node move` target has none ([#401](https://github.com/germanamz/tusk/issues/401)) ([32433ff](https://github.com/germanamz/tusk/commit/32433ff5a160825a8b40e133734f324e976df45e))
* **node:** YAML-quote frontmatter `type` and `title` and ambiguous literals ([#402](https://github.com/germanamz/tusk/issues/402)) ([8e849d3](https://github.com/germanamz/tusk/commit/8e849d318a64ce5b064292d63c2e570de32449ba))

## [1.1.0](https://github.com/germanamz/tusk/compare/v1.0.2...v1.1.0) (2026-05-17)


### Features

* **cli:** add man pages and markdown reference, generated from cobra help ([#393](https://github.com/germanamz/tusk/issues/393)) ([4b73f2a](https://github.com/germanamz/tusk/commit/4b73f2a5d0a96bdcee141b82f9617b12cfcfc633))

## [1.0.2](https://github.com/germanamz/tusk/compare/v1.0.1...v1.0.2) (2026-05-16)


### Bug Fixes

* **release:** inject version into binary via ldflags ([#391](https://github.com/germanamz/tusk/issues/391)) ([992e7e4](https://github.com/germanamz/tusk/commit/992e7e48b4ae361cea7bfa8057a9b415ee328513))

## [1.0.1](https://github.com/germanamz/tusk/compare/v1.0.0...v1.0.1) (2026-05-16)


### Bug Fixes

* **ci:** drop package-name and set explicit release-please PR title pattern ([#389](https://github.com/germanamz/tusk/issues/389)) ([c3824ce](https://github.com/germanamz/tusk/commit/c3824cee97aec18b85714f55e74c5e15776ede31))

## [1.0.0](https://github.com/germanamz/tusk/compare/v0.14.0...v1.0.0) (2026-05-16)


### ⚠ BREAKING CHANGES

* **v1:** this commit retires every v0.x interface. v0 sources remain at the v0.14.0 tag and the v0-archive branch.

### Features

* --verbose flag and structured logging for indexing pipeline ([#368](https://github.com/germanamz/tusk/issues/368)) ([283f9e2](https://github.com/germanamz/tusk/commit/283f9e2c9cc1d6701411cfbdd4c405d7926051c5))
* **embed:** cap embed-queue retries at MaxEmbedAttempts ([#369](https://github.com/germanamz/tusk/issues/369)) ([a6a2e6f](https://github.com/germanamz/tusk/commit/a6a2e6f71963ed8c70ba48336a208fa5cb71da31))
* **embed:** default chunker is MarkdownRecursive in production paths ([#376](https://github.com/germanamz/tusk/issues/376)) ([7f74ae4](https://github.com/germanamz/tusk/commit/7f74ae49f244c7c739aedb481ac916ed8eacd3c3))
* **embed:** markdown-structural recursive chunking strategy ([#372](https://github.com/germanamz/tusk/issues/372)) ([d43c528](https://github.com/germanamz/tusk/commit/d43c528526a52dfbec85e87f56686c18c7ac7b04))
* **embed:** parallel embed workers and configurable timeout (Spec B Phase 1) ([#381](https://github.com/germanamz/tusk/issues/381)) ([66f2e53](https://github.com/germanamz/tusk/commit/66f2e5394dfe75c869ac9e2d4405ee437d00b100))
* **embed:** per-chunk drain loop with delete-then-insert cleanup ([#373](https://github.com/germanamz/tusk/issues/373)) ([d54e27f](https://github.com/germanamz/tusk/commit/d54e27fcd16405364a4f4b2b5a1fa4eb50870fb9))
* **filter:** aggregate semantic rank by max-per-node across chunks ([#374](https://github.com/germanamz/tusk/issues/374)) ([ff26843](https://github.com/germanamz/tusk/commit/ff26843798e4418bacbaa3d253b7376e3b5ef3d4))
* **query,doctor:** semantic snippets and doctor chunking diagnostics ([#380](https://github.com/germanamz/tusk/issues/380)) ([f53b29a](https://github.com/germanamz/tusk/commit/f53b29a2671f584921fed58223acf39110bb9646))
* **query:** pass chunk_idx through semantic query candidate build ([#375](https://github.com/germanamz/tusk/issues/375)) ([e4f8fc6](https://github.com/germanamz/tusk/commit/e4f8fc69b7fa7ac849fc644a5240c25c357373e9))
* **query:** tusk_query semantic agent ergonomics ([#384](https://github.com/germanamz/tusk/issues/384)) ([dacbf2a](https://github.com/germanamz/tusk/commit/dacbf2addc6c0a9ae9477e1ed338fdb90b52cb58))


### Bug Fixes

* **devcontainer:** allow R2 blob hosts in tinyproxy for ollama pulls ([#367](https://github.com/germanamz/tusk/issues/367)) ([b1dc6f3](https://github.com/germanamz/tusk/commit/b1dc6f35162f24fd5913a3cc61e6c338d8ff4783))
* **devcontainer:** switch Ollama asset to .tar.zst and cache builds ([#366](https://github.com/germanamz/tusk/issues/366)) ([e509c0b](https://github.com/germanamz/tusk/commit/e509c0b6a11dd61c079db0a61cec1b70a91cc328))
* **embed:** skip drain re-embed when content hash matches ([#387](https://github.com/germanamz/tusk/issues/387)) ([0b6f11a](https://github.com/germanamz/tusk/commit/0b6f11a7f5a44b47d291b566e0ef2b12d0311346))
* **embed:** tighten MarkdownRecursive MaxBytes from 7200 to 4000 ([#377](https://github.com/germanamz/tusk/issues/377)) ([59cd732](https://github.com/germanamz/tusk/commit/59cd73243d832e3563d1e19f2f4e2959488e8e60))
* **lock:** hold workspace lock for MCP and watch lifetimes ([#378](https://github.com/germanamz/tusk/issues/378)) ([2b8330e](https://github.com/germanamz/tusk/commit/2b8330e690e4298d49c678a9841e332c50d53c8f))


### Miscellaneous Chores

* **v1:** retire v0.x line, set up v1 integration branch ([#351](https://github.com/germanamz/tusk/issues/351)) ([e3d7c0f](https://github.com/germanamz/tusk/commit/e3d7c0f10da8b515187158d288dfda78233bd0a9))
