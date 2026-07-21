# Changelog

## [2.4.1](https://github.com/germanamz/tusk/compare/v2.4.0...v2.4.1) (2026-07-20)


### Bug Fixes

* **web:** give rendered markdown tables visible boundaries ([#733](https://github.com/germanamz/tusk/issues/733)) ([9ce4916](https://github.com/germanamz/tusk/commit/9ce4916986c2449dacf8f184fe1c5650f9b7c28d))

## [2.4.0](https://github.com/germanamz/tusk/compare/v2.3.0...v2.4.0) (2026-07-20)


### Features

* **cli:** add `tusk update` to self-update the installed binary ([#729](https://github.com/germanamz/tusk/issues/729)) ([f2a556a](https://github.com/germanamz/tusk/commit/f2a556ad03c5f18ed6dffdb845cba2d1c87a73e4))
* **web:** consolidate graph + book into a unified `tusk web` app with theming ([#726](https://github.com/germanamz/tusk/issues/726)) ([8acd7e0](https://github.com/germanamz/tusk/commit/8acd7e09470fef72be57790d9cd99870d27f998b))

## [2.3.0](https://github.com/germanamz/tusk/compare/v2.2.0...v2.3.0) (2026-07-17)


### Features

* **book:** add tusk book, a read-only reading web UI ([#718](https://github.com/germanamz/tusk/issues/718)) ([d39744b](https://github.com/germanamz/tusk/commit/d39744b344a634ff09349966c232dc11ef60261d))

## [2.2.0](https://github.com/germanamz/tusk/compare/v2.1.4...v2.2.0) (2026-07-14)


### Features

* **index:** index .mdx files as markdown-twin content nodes ([#716](https://github.com/germanamz/tusk/issues/716)) ([d112ce2](https://github.com/germanamz/tusk/commit/d112ce27e0c7e69a87a266bc18c4fa67f8097d74))

## [2.1.4](https://github.com/germanamz/tusk/compare/v2.1.3...v2.1.4) (2026-07-14)


### Bug Fixes

* **graph:** resolve status-footer stuck-progress, log clobbering, and rewalk loop ([#714](https://github.com/germanamz/tusk/issues/714)) ([364e678](https://github.com/germanamz/tusk/commit/364e678d7bf2e72447ed9b36609c5d8112782fb8))

## [2.1.3](https://github.com/germanamz/tusk/compare/v2.1.2...v2.1.3) (2026-07-12)


### Bug Fixes

* **filter:** bind recursive-CTE params ahead of WHERE params ([#707](https://github.com/germanamz/tusk/issues/707)) ([c8bf267](https://github.com/germanamz/tusk/commit/c8bf267b9480d640178d3640eff55c15d2c170f7)), closes [#704](https://github.com/germanamz/tusk/issues/704)
* **index:** recover a wiped index and rebuild schema atomically ([#709](https://github.com/germanamz/tusk/issues/709)) ([a81897e](https://github.com/germanamz/tusk/commit/a81897ef071c11bb153615c797bde8ba4ea3fb39))
* **node:** hydrate edges on node get --include edges ([#706](https://github.com/germanamz/tusk/issues/706)) ([#710](https://github.com/germanamz/tusk/issues/710)) ([d8196a3](https://github.com/germanamz/tusk/commit/d8196a3d5e5727f28075ee4d6b0a28257e077fc0))

## [2.1.2](https://github.com/germanamz/tusk/compare/v2.1.1...v2.1.2) (2026-07-12)


### Bug Fixes

* **doctor:** close five doctor blind spots and noise sources ([#685](https://github.com/germanamz/tusk/issues/685)) ([#697](https://github.com/germanamz/tusk/issues/697)) ([a03260d](https://github.com/germanamz/tusk/commit/a03260dd74fdbf41ffff77965987deaadd1612ef))
* **embed:** close four embedding-lifecycle gaps ([#684](https://github.com/germanamz/tusk/issues/684)) ([#696](https://github.com/germanamz/tusk/issues/696)) ([59f7195](https://github.com/germanamz/tusk/commit/59f7195eeabab9ff223a5988193037443969b86c))
* **index:** make file ids with special characters safe ([#683](https://github.com/germanamz/tusk/issues/683)) ([#695](https://github.com/germanamz/tusk/issues/695)) ([ec192c0](https://github.com/germanamz/tusk/commit/ec192c060d2e7819041afd1341825db89d73ab83))
* **node:** close node move/create destination-validation gaps ([#686](https://github.com/germanamz/tusk/issues/686)) ([#698](https://github.com/germanamz/tusk/issues/698)) ([5bcfe5c](https://github.com/germanamz/tusk/commit/5bcfe5cc69b9cf30ee0ec9c444068bdb6be52ab2))
* **node:** derive and rewrite aliased wikilinks [[id|alias]] ([#690](https://github.com/germanamz/tusk/issues/690)) ([#702](https://github.com/germanamz/tusk/issues/702)) ([f0fc034](https://github.com/germanamz/tusk/commit/f0fc034b23a47ff035905d4e44c767d570ec1afe))
* **node:** keep HTML id extension and rewrite HTML referrer hrefs on move ([#699](https://github.com/germanamz/tusk/issues/699)) ([d890489](https://github.com/germanamz/tusk/commit/d89048956ca13f59dc5672784c433b17ee427e5d))
* **node:** keep referrers' ref-property edges healthy across a move ([#680](https://github.com/germanamz/tusk/issues/680)) ([#691](https://github.com/germanamz/tusk/issues/691)) ([b541a4b](https://github.com/germanamz/tusk/commit/b541a4b778e5292d2d40e39bf72db9ee78ac4e88))
* **node:** resolve unquoted [[id]] frontmatter wikilinks ([#692](https://github.com/germanamz/tusk/issues/692)) ([#703](https://github.com/germanamz/tusk/issues/703)) ([3272b80](https://github.com/germanamz/tusk/commit/3272b8051189583cc350159a2fbe79ad877ce6ea))
* **query:** close graph-expansion residuals after [#674](https://github.com/germanamz/tusk/issues/674) ([#688](https://github.com/germanamz/tusk/issues/688)) ([#700](https://github.com/germanamz/tusk/issues/700)) ([6fa853e](https://github.com/germanamz/tusk/commit/6fa853e373ec9f46fb0909632ff3a62e753fa21b))
* **query:** run graph expansion at file level on the sub-unit semantic path ([#674](https://github.com/germanamz/tusk/issues/674)) ([8a06828](https://github.com/germanamz/tusk/commit/8a0682886426dc36e79c53661eecdb984e099021))
* **reindex:** retry recorded ref drift after every sweep so refs converge ([#677](https://github.com/germanamz/tusk/issues/677)) ([#679](https://github.com/germanamz/tusk/issues/679)) ([11f3a25](https://github.com/germanamz/tusk/commit/11f3a250eed79a4856da766a28601fc492b6e245))
* **reindex:** wake ref referrers on target removal and key drift per value ([#689](https://github.com/germanamz/tusk/issues/689)) ([#701](https://github.com/germanamz/tusk/issues/701)) ([3327bf6](https://github.com/germanamz/tusk/commit/3327bf61fb86ef01b10f648123647e0a9049bcea))
* **subunit:** close five sub-unit content pipeline gaps ([#682](https://github.com/germanamz/tusk/issues/682)) ([#694](https://github.com/germanamz/tusk/issues/694)) ([8f38d04](https://github.com/germanamz/tusk/commit/8f38d046773b132768be915b3c300d06e6f1da0e))
* **subunit:** re-derive section edges on re-parse and retarget sub-unit edges on move ([#678](https://github.com/germanamz/tusk/issues/678)) ([6227a4e](https://github.com/germanamz/tusk/commit/6227a4efe7d2c0b602a77042b80e66b51a9695bb))
* **watch:** keep the CLI watcher and MCP daemon on the reindex.Run path ([#693](https://github.com/germanamz/tusk/issues/693)) ([783b1bd](https://github.com/germanamz/tusk/commit/783b1bded1bedae2d58220216fea568793e8cad5)), closes [#681](https://github.com/germanamz/tusk/issues/681)

## [2.1.1](https://github.com/germanamz/tusk/compare/v2.1.0...v2.1.1) (2026-07-01)


### Bug Fixes

* **node:** preserve edges and frontmatter fidelity on re-render ([#670](https://github.com/germanamz/tusk/issues/670)) ([#671](https://github.com/germanamz/tusk/issues/671)) ([0f2dac7](https://github.com/germanamz/tusk/commit/0f2dac75a2a3f2cecbbe1099d7b8d13553ab045f))

## [2.1.0](https://github.com/germanamz/tusk/compare/v2.0.0...v2.1.0) (2026-07-01)


### Features

* **node:** full date handling — lenient parse, quoted write, reindex self-heal ([#663](https://github.com/germanamz/tusk/issues/663)) ([9c9906e](https://github.com/germanamz/tusk/commit/9c9906e20c133bdb478e6dbff0c72497e986205d))


### Bug Fixes

* **filter:** type-aware comparison for int/float/string and enum sort ([#668](https://github.com/germanamz/tusk/issues/668)) ([54474ca](https://github.com/germanamz/tusk/commit/54474ca6e9b6043b475880704318b0309c84c542))
* **filter:** type-aware ordering/range operators on date and enum properties ([#665](https://github.com/germanamz/tusk/issues/665)) ([093a9ff](https://github.com/germanamz/tusk/commit/093a9ff07a8f90869a30df93f6bcfe99207a3f38)), closes [#661](https://github.com/germanamz/tusk/issues/661)


### Code Refactoring

* **filter:** quote JSON-path keys uniformly in the compiler ([#667](https://github.com/germanamz/tusk/issues/667)) ([e74c6db](https://github.com/germanamz/tusk/commit/e74c6dba6000532e949f6c65708a947259df4b90)), closes [#666](https://github.com/germanamz/tusk/issues/666)

## [2.0.0](https://github.com/germanamz/tusk/compare/v1.18.0...v2.0.0) (2026-06-29)


### ⚠ BREAKING CHANGES

* **cli:** query --format json emits rows, not the [] sentinel ([#649](https://github.com/germanamz/tusk/issues/649))

### Features

* **doctor:** detect embedding model/dim drift ([#652](https://github.com/germanamz/tusk/issues/652)) ([40c4ae2](https://github.com/germanamz/tusk/commit/40c4ae2fb7e9949c60f9bf415a924b153d70daa8))
* **web:** SSE disconnect pill, recursive nav, and persist expanded sub-units ([#655](https://github.com/germanamz/tusk/issues/655)) ([79f8445](https://github.com/germanamz/tusk/commit/79f8445b78ab9392b79896b568157c91f8e8702e))


### Bug Fixes

* **behavior:** resolve hook instances in deterministic sorted order ([#626](https://github.com/germanamz/tusk/issues/626)) ([643c703](https://github.com/germanamz/tusk/commit/643c7038c0b5d7a9f4b1f5f60fa2ba5010badb41))
* **cli:** fail loudly when reload --reindex cannot reindex ([#624](https://github.com/germanamz/tusk/issues/624)) ([fd76608](https://github.com/germanamz/tusk/commit/fd766082f64a0e14a2ae596a20e9ff2c9acc86f5))
* **cli:** print the error before pack add exits with a code ([#623](https://github.com/germanamz/tusk/issues/623)) ([cf57441](https://github.com/germanamz/tusk/commit/cf57441b02c4c45d20736864ce9df7919cc539d5))
* **cli:** query --format json emits rows, not the [] sentinel ([#649](https://github.com/germanamz/tusk/issues/649)) ([6854c22](https://github.com/germanamz/tusk/commit/6854c220a847fc0c087938dc2ad6ef610d27c714))
* **embed:** abort drain on transport errors instead of dropping nodes ([#634](https://github.com/germanamz/tusk/issues/634)) ([165abd4](https://github.com/germanamz/tusk/commit/165abd428237af4abc195105efc3f91cca4e60e6))
* **filter:** walk hierarchy shortcuts in the correct edge direction ([#619](https://github.com/germanamz/tusk/issues/619)) ([f374eb0](https://github.com/germanamz/tusk/commit/f374eb005f2173c9275de2cbce93a129dbd72647))
* **graphview:** encode node ids and split the subunits route ([#654](https://github.com/germanamz/tusk/issues/654)) ([186aad8](https://github.com/germanamz/tusk/commit/186aad8a6bfe539a94ad11fded66929dbae0aed7))
* **graphview:** reject untrusted Host headers (DNS-rebinding guard) ([#621](https://github.com/germanamz/tusk/issues/621)) ([bbf673a](https://github.com/germanamz/tusk/commit/bbf673afeaee46e3510b7e1bcefb44bb2c28c0cf))
* **index:** chunk unbounded IN(...) clauses under the SQLite variable cap ([#629](https://github.com/germanamz/tusk/issues/629)) ([9e0e6af](https://github.com/germanamz/tusk/commit/9e0e6af53a21437809a301facce4bc25f36fad26))
* **manifest:** reject negative embeddings.workers regardless of provider ([#642](https://github.com/germanamz/tusk/issues/642)) ([a4c8b1c](https://github.com/germanamz/tusk/commit/a4c8b1c5653bfb5dd41a4067e7f565b5a78943b4))
* **mcp:** default the SSE transport to loopback, guard non-loopback binds ([#620](https://github.com/germanamz/tusk/issues/620)) ([d75cb20](https://github.com/germanamz/tusk/commit/d75cb20b31e3a980222e0fbdb6663a50e97b4a23))
* **mcp:** surface background daemon errors instead of running deaf ([#633](https://github.com/germanamz/tusk/issues/633)) ([44b8341](https://github.com/germanamz/tusk/commit/44b8341176855e5ee094a53adc7f7b888b426959))
* **node:** reject Create/Rename paths that escape the workspace root ([#622](https://github.com/germanamz/tusk/issues/622)) ([80c97e3](https://github.com/germanamz/tusk/commit/80c97e35de6042f7b93b23d0ab532a97cb5a02af))
* **node:** rewrite block-sequence edge targets on rename ([#618](https://github.com/germanamz/tusk/issues/618)) ([ddc7dd0](https://github.com/germanamz/tusk/commit/ddc7dd0de8348e82a286cf464764d2929c3cba89))
* **node:** support float properties end-to-end ([#641](https://github.com/germanamz/tusk/issues/641)) ([dfa498a](https://github.com/germanamz/tusk/commit/dfa498a4fc0dcd81c0b26dee592351a67bfab1a7))
* **query:** unify retrieval read defaults ([#648](https://github.com/germanamz/tusk/issues/648)) ([e690a30](https://github.com/germanamz/tusk/commit/e690a303444d316641c9b43fec7bc6539f855ef7))
* **watcher:** stop timer leak, serialize handlers, watch new dirs ([#635](https://github.com/germanamz/tusk/issues/635)) ([b7767a0](https://github.com/germanamz/tusk/commit/b7767a0bc727c9103ac842c927c2094221d153ad))
* **watch:** run workflow/property validation in tusk watch ([#636](https://github.com/germanamz/tusk/issues/636)) ([f9bbd0c](https://github.com/germanamz/tusk/commit/f9bbd0c76beff9410f324200edcdcb77878eb38d))


### Performance Improvements

* **embed:** embed nodes concurrently across a batch ([#640](https://github.com/germanamz/tusk/issues/640)) ([aeeef8d](https://github.com/germanamz/tusk/commit/aeeef8d80ebe9ce6cd84b56785ade33b4f7b39b6))
* **embed:** only GC orphan vectors after a pass drained work ([#627](https://github.com/germanamz/tusk/issues/627)) ([2c822c7](https://github.com/germanamz/tusk/commit/2c822c71397959ae276de8178b9093459762046c))
* **index:** add nodes_type_idx for bare type= filters ([#630](https://github.com/germanamz/tusk/issues/630)) ([0a0d31a](https://github.com/germanamz/tusk/commit/0a0d31a5e8c1ac16fc6a422342976f520f6c75f6))
* **index:** drop the never-read idx_file_state_lease index ([#638](https://github.com/germanamz/tusk/issues/638)) ([9530543](https://github.com/germanamz/tusk/commit/9530543c577805ef5d354c131f4504b255d1ca8e))
* **index:** open SQLite with synchronous=NORMAL ([#625](https://github.com/germanamz/tusk/issues/625)) ([f6e299c](https://github.com/germanamz/tusk/commit/f6e299c421c752cabd0a3a538d247b49584e72ad))
* **node:** hoist ExtractWikilinks out of the edge-type loop ([#628](https://github.com/germanamz/tusk/issues/628)) ([18d3f69](https://github.com/germanamz/tusk/commit/18d3f6910c9163beb4bdfafc8321828d2bc94ef5))
* **reindex:** skip unchanged files by mtime+size ([#637](https://github.com/germanamz/tusk/issues/637)) ([42601e3](https://github.com/germanamz/tusk/commit/42601e31284ad6478db8253d54d2c7e57d84f947))
* **subunit:** batch sub-unit edge writes and embed enqueues ([#639](https://github.com/germanamz/tusk/issues/639)) ([9f0ba65](https://github.com/germanamz/tusk/commit/9f0ba65bfeef31b753b66f63e6a00437d6ac3bed))


### Code Refactoring

* **embed:** centralize embedder construction in NewFromManifest ([#643](https://github.com/germanamz/tusk/issues/643)) ([51cb51a](https://github.com/germanamz/tusk/commit/51cb51a9cc5addac0870675f9b7f57220042d87a))
* **graphview:** classify semantic-unavailable by typed error ([#644](https://github.com/germanamz/tusk/issues/644)) ([36c3f91](https://github.com/germanamz/tusk/commit/36c3f9198aee731d9128ffe1a1a4da2cbea760fd))
* **node:** own the edge add/remove pipeline in node.Service ([#646](https://github.com/germanamz/tusk/issues/646)) ([23327f0](https://github.com/germanamz/tusk/commit/23327f07cfec4b11e7d4b463eeb9cb300037bf44))
* **subunit:** lift shared unit finalization out of the two parsers ([#645](https://github.com/germanamz/tusk/issues/645)) ([49c7eb0](https://github.com/germanamz/tusk/commit/49c7eb06dcdf0d56a37a7790cec347ae8bc4edb6))

## [1.18.0](https://github.com/germanamz/tusk/compare/v1.17.0...v1.18.0) (2026-06-28)


### Features

* **mcp:** add tusk_pack_add and stop telling agents to use the shell ([#612](https://github.com/germanamz/tusk/issues/612)) ([e626c04](https://github.com/germanamz/tusk/commit/e626c04abc46c2e7f08ba7ab773aaacb07d8241c))
* **mcp:** let tusk_node_modify replace the node body ([#610](https://github.com/germanamz/tusk/issues/610)) ([f941961](https://github.com/germanamz/tusk/commit/f9419612aee10cdd263d3dfd36be8a4986f945d0))
* **mcp:** surface embed-queue depth and set expectations on tusk_reindex ([#611](https://github.com/germanamz/tusk/issues/611)) ([45131c5](https://github.com/germanamz/tusk/commit/45131c56dec4a3a01138a4a1851c9a68374a9bfb))


### Bug Fixes

* **mcp:** hint at tusk_reload when an edge type is not declared ([#609](https://github.com/germanamz/tusk/issues/609)) ([0b91fc8](https://github.com/germanamz/tusk/commit/0b91fc86205426441861e332f2615bfb0f79d76a))

## [1.17.0](https://github.com/germanamz/tusk/compare/v1.16.0...v1.17.0) (2026-06-27)


### Features

* **graph:** add ancestor cluster-lens producer ([#590](https://github.com/germanamz/tusk/issues/590)) ([0a46ca1](https://github.com/germanamz/tusk/commit/0a46ca109fca058e6c61bdf40648f80e68d1c794))
* **graph:** add cluster hull overlays and document the cluster lens ([#593](https://github.com/germanamz/tusk/issues/593)) ([53c73da](https://github.com/germanamz/tusk/commit/53c73daa7d7da72ed6c31437b9ecfa415a51fc31))
* **graph:** add community-detection cluster-lens producer ([#592](https://github.com/germanamz/tusk/issues/592)) ([a663d7c](https://github.com/germanamz/tusk/commit/a663d7ceabd453372154d2a9e4c1486f29ea4928))
* **graph:** add deterministic community-detection engine ([#589](https://github.com/germanamz/tusk/issues/589)) ([6519c1d](https://github.com/germanamz/tusk/commit/6519c1d2e210381c6005623ec88bf654fb7cd1e5))
* **graph:** add opt-in cluster-force huddle layout ([#591](https://github.com/germanamz/tusk/issues/591)) ([2e3cabd](https://github.com/germanamz/tusk/commit/2e3cabd39eedbda372ad592dabe39a66040e75f2))
* **graph:** cluster-lens config and color nodes by group ([#588](https://github.com/germanamz/tusk/issues/588)) ([9c8188b](https://github.com/germanamz/tusk/commit/9c8188b29b62b2d2ca1f5bd057dd60489cc8c5c8))
* **graph:** fade cross-cluster and hub edges to declutter the graph ([#598](https://github.com/germanamz/tusk/issues/598)) ([dd33ffd](https://github.com/germanamz/tusk/commit/dd33ffd836d457d6f512be3609997c77552d4528))
* **graph:** fold type/kind filters into drawer and fix node search ([#597](https://github.com/germanamz/tusk/issues/597)) ([d3bc98a](https://github.com/germanamz/tusk/commit/d3bc98a7738bbdff86bd2277e93da839c1811452))
* **graph:** scrollable group legend-as-filter drawer ([#595](https://github.com/germanamz/tusk/issues/595)) ([65d8386](https://github.com/germanamz/tusk/commit/65d8386d2d22b3da4060b83d5ca5a5590ccb1fcc))
* **graph:** semantic layout mode for the graph view ([#600](https://github.com/germanamz/tusk/issues/600)) ([2a9d8bf](https://github.com/germanamz/tusk/commit/2a9d8bf174b9d859139d82509c360678ede6dfdf))


### Bug Fixes

* **graph:** address semantic-layout review follow-ups ([#602](https://github.com/germanamz/tusk/issues/602)) ([fa0fd8c](https://github.com/germanamz/tusk/commit/fa0fd8c7b79abdde9ea9e09f69ea56f551b20f17))
* **graph:** group filter hides non-matching group rows ([#603](https://github.com/germanamz/tusk/issues/603)) ([bceebec](https://github.com/germanamz/tusk/commit/bceebec1679c30cda3ff3dfd55a79eb3647e1b54))
* **graph:** render group hulls immediately on re-snapshot ([#594](https://github.com/germanamz/tusk/issues/594)) ([f82c927](https://github.com/germanamz/tusk/commit/f82c92755b0bfbd9ba046687fac5299b0a4ea4f3))
* **graph:** warm-start node positions across live re-snapshots ([#586](https://github.com/germanamz/tusk/issues/586)) ([08ac08f](https://github.com/germanamz/tusk/commit/08ac08f26ad9b564c455dd7bac440b11002608d5))
* **node:** extract HTML &lt;a href&gt; links as references edges ([#599](https://github.com/germanamz/tusk/issues/599)) ([5c08d0f](https://github.com/germanamz/tusk/commit/5c08d0fc8dd88043ba2fecfef5b1e72488c6c6e8)), closes [#596](https://github.com/germanamz/tusk/issues/596)

## [1.16.0](https://github.com/germanamz/tusk/compare/v1.15.0...v1.16.0) (2026-06-25)


### Features

* **graph:** populate degree on sub-unit drill-down nodes ([#585](https://github.com/germanamz/tusk/issues/585)) ([5c71bf7](https://github.com/germanamz/tusk/commit/5c71bf76753e0eec58e9c63ac135cdc239153127))
* **graph:** size & brighten nodes by total degree (in + out) ([#583](https://github.com/germanamz/tusk/issues/583)) ([5f831fc](https://github.com/germanamz/tusk/commit/5f831fc6b71a00fa032f1cb930d26f5e81d42c48))

## [1.15.0](https://github.com/germanamz/tusk/compare/v1.14.1...v1.15.0) (2026-06-24)


### Features

* **graph:** color nodes by type, size and brighten by in-degree ([#581](https://github.com/germanamz/tusk/issues/581)) ([adc7d22](https://github.com/germanamz/tusk/commit/adc7d22f60286587756823d3bb0d4c28e2f19e70))

## [1.14.1](https://github.com/germanamz/tusk/compare/v1.14.0...v1.14.1) (2026-06-24)


### Bug Fixes

* **filter:** reject unsafe --sort property names; drop dead code ([#573](https://github.com/germanamz/tusk/issues/573)) ([7eed2ae](https://github.com/germanamz/tusk/commit/7eed2ae4937c7b9ea88e8098a6eeab89cd809463))


### Performance Improvements

* eliminate N+1 queries and double-loads on read paths ([#579](https://github.com/germanamz/tusk/issues/579)) ([54cc519](https://github.com/germanamz/tusk/commit/54cc51926c2d78e0ce28eceb13403ffc19822de4))


### Code Refactoring

* decompose god functions and merge duplicated compilers ([#578](https://github.com/germanamz/tusk/issues/578)) ([6e10e76](https://github.com/germanamz/tusk/commit/6e10e76993908afcbf5688d38c56a4f50656e740))
* **index:** dedupe repo/compile boilerplate ([#575](https://github.com/germanamz/tusk/issues/575)) ([671d26b](https://github.com/germanamz/tusk/commit/671d26b041c0b1c7e1ceb3debc4da26b614df075))
* remove vestigial state and dead subsystems ([#574](https://github.com/germanamz/tusk/issues/574)) ([70bef30](https://github.com/germanamz/tusk/commit/70bef304cedbc900eadf61a88b06e59d09653669))
* single-source CLI/MCP wire shapes and runtime wiring ([#576](https://github.com/germanamz/tusk/issues/576)) ([15ee28f](https://github.com/germanamz/tusk/commit/15ee28f450a42a137432b8a8fb5f0c72be15da92))
* unify sub-unit address machinery and epoch packages ([#577](https://github.com/germanamz/tusk/issues/577)) ([d2310c6](https://github.com/germanamz/tusk/commit/d2310c646afd5cf8be2ededeb966a62a9b7243b5))

## [1.14.0](https://github.com/germanamz/tusk/compare/v1.13.1...v1.14.0) (2026-06-23)


### Features

* **graph:** focus, highlight selection, and alt-drag pan in 3D view ([#571](https://github.com/germanamz/tusk/issues/571)) ([45bf0f3](https://github.com/germanamz/tusk/commit/45bf0f30bab217d20260ca88c4469cddbe661f80))

## [1.13.1](https://github.com/germanamz/tusk/compare/v1.13.0...v1.13.1) (2026-06-23)


### Bug Fixes

* **graph:** size 3D viewport to its container and harden graph layout ([#569](https://github.com/germanamz/tusk/issues/569)) ([1837af4](https://github.com/germanamz/tusk/commit/1837af4c52f8682c5d2d8cc9954c3838e66aa17b))

## [1.13.0](https://github.com/germanamz/tusk/compare/v1.12.3...v1.13.0) (2026-06-23)


### Features

* **graph:** interactive 3D graph view (tusk graph) ([#567](https://github.com/germanamz/tusk/issues/567)) ([5702824](https://github.com/germanamz/tusk/commit/5702824d048fdc9b400c2f7af033f67ddfdd816b))

## [1.12.3](https://github.com/germanamz/tusk/compare/v1.12.2...v1.12.3) (2026-06-22)


### Bug Fixes

* **index:** chunk sub-unit GLOB predicates to avoid SQLite depth limit ([#565](https://github.com/germanamz/tusk/issues/565)) ([ada97a7](https://github.com/germanamz/tusk/commit/ada97a72110066f583a9db88aab43c7706c8f96e))

## [1.12.2](https://github.com/germanamz/tusk/compare/v1.12.1...v1.12.2) (2026-06-22)


### Bug Fixes

* **query:** rank full candidate pool for semantic sub-unit filters ([#562](https://github.com/germanamz/tusk/issues/562)) ([7685baf](https://github.com/germanamz/tusk/commit/7685baf9fc9731babdddc0f30ae0198bdafcedae))

## [1.12.1](https://github.com/germanamz/tusk/compare/v1.12.0...v1.12.1) (2026-06-21)


### Bug Fixes

* **docs:** tusk node get help omitted HTML nodes ([#558](https://github.com/germanamz/tusk/issues/558)) ([f585de3](https://github.com/germanamz/tusk/commit/f585de3ecbcccf743d34c8a3d515f99535a48ba0))

## [1.12.0](https://github.com/germanamz/tusk/compare/v1.11.0...v1.12.0) (2026-06-16)


### Features

* **htmlunit:** extract checkbox state from HTML list items ([#555](https://github.com/germanamz/tusk/issues/555)) ([d8a75fb](https://github.com/germanamz/tusk/commit/d8a75fb8fc309f49183875f2ae4fb5909696bf7e))

## [1.11.0](https://github.com/germanamz/tusk/compare/v1.10.0...v1.11.0) (2026-06-12)


### Features

* **cli:** add tusk node render + tusk_node_render, graduate HTML content AST (phase 6/6) ([#552](https://github.com/germanamz/tusk/issues/552)) ([4ce3300](https://github.com/germanamz/tusk/commit/4ce330035be2d7d5b5c94c2703a7996eac61d0b5))
* **htmlunit:** add HTML sub-unit parser and text normalizer (HTML AST phase 1/6) ([#546](https://github.com/germanamz/tusk/issues/546)) ([988dbba](https://github.com/germanamz/tusk/commit/988dbbaed28a0e2379f01194fce9bf46b340f994))
* **node:** add ParseHTMLFile + extract htmltext leaf package (HTML AST phase 3/6) ([#549](https://github.com/germanamz/tusk/issues/549)) ([7a7a9cc](https://github.com/germanamz/tusk/commit/7a7a9ccdea6029e1b7cea59d651a5287d0fed385))
* **reindex:** emit HTML sub-units via source-parameterized Sync (HTML AST phase 5/6) ([#551](https://github.com/germanamz/tusk/issues/551)) ([021ca24](https://github.com/germanamz/tusk/commit/021ca24ebcee8e2bcc49b87c7a4e00bb1942ef4a))
* **reindex:** index .html files as nodes — HTML AST pipeline activation (phase 4/6) ([#550](https://github.com/germanamz/tusk/issues/550)) ([cd81b6f](https://github.com/germanamz/tusk/commit/cd81b6f4199ce4f36b94235eaa17d467f46b26bc))
* **typepacks:** add html typepack constants package (HTML AST phase 2/6) ([#548](https://github.com/germanamz/tusk/issues/548)) ([21985f9](https://github.com/germanamz/tusk/commit/21985f9b129c8be7bddf3faf5d71f0cf5f55e164))


### Bug Fixes

* **node:** read HTML nodes via ParseContentFile; reject HTML create/modify ([#553](https://github.com/germanamz/tusk/issues/553)) ([499be0a](https://github.com/germanamz/tusk/commit/499be0aa2e85723d66f61a14c93214daeffccd5a))

## [1.10.0](https://github.com/germanamz/tusk/compare/v1.9.0...v1.10.0) (2026-06-09)


### Features

* **cli:** add tusk reload command for hot manifest reload ([#543](https://github.com/germanamz/tusk/issues/543)) ([b938795](https://github.com/germanamz/tusk/commit/b938795445b4b3beac906a417cd61b5cbeb6ab7c))
* **manifestepoch:** add .tusk/manifest-epoch sentinel ([#535](https://github.com/germanamz/tusk/issues/535)) ([bfcad37](https://github.com/germanamz/tusk/commit/bfcad37efd5a289c7240040865a5615831461899))
* **mcp:** add manifest diff for reload reporting ([#537](https://github.com/germanamz/tusk/issues/537)) ([887ebbb](https://github.com/germanamz/tusk/commit/887ebbb9c16773bc6524878091c3d8e8fa53f367))
* **mcp:** add manifest-epoch poll + fsnotify watchers ([#540](https://github.com/germanamz/tusk/issues/540)) ([4c28de5](https://github.com/germanamz/tusk/commit/4c28de53486341727440021dc7c79ac97cfcf050))
* **mcp:** add tusk_reload tool ([#542](https://github.com/germanamz/tusk/issues/542)) ([02e586d](https://github.com/germanamz/tusk/commit/02e586d9961e6a729f45d5449d1dd35117dab68c))
* **mcp:** run epoch watchers at workers=0 and gate reindex with reindexMu ([#541](https://github.com/germanamz/tusk/issues/541)) ([0023dbc](https://github.com/germanamz/tusk/commit/0023dbce7d047d014fac578f1cfd8294b5ee2ce7))
* **mcp:** sibling-side manifest-epoch convergence on Server ([#539](https://github.com/germanamz/tusk/issues/539)) ([d8d76fb](https://github.com/germanamz/tusk/commit/d8d76fbf5ac8901b934b6930c090f00ad6a2801c))


### Code Refactoring

* **mcp:** replace ReloadManifest with validating buildReloaded ([#538](https://github.com/germanamz/tusk/issues/538)) ([0d37803](https://github.com/germanamz/tusk/commit/0d378037e965e45590676f7355187fdc95473746))

## [1.9.0](https://github.com/germanamz/tusk/compare/v1.8.0...v1.9.0) (2026-06-08)


### Features

* **cli:** add tusk reset command ([#528](https://github.com/germanamz/tusk/issues/528)) ([721f60f](https://github.com/germanamz/tusk/commit/721f60fbfea8cce1a2dec729df203922f26d170f))
* **indexepoch:** add .tusk/epoch sentinel for index-reset detection ([#526](https://github.com/germanamz/tusk/issues/526)) ([5e4348b](https://github.com/germanamz/tusk/commit/5e4348b74d7c3baa392457a16bab7283c44fd020))
* **mcp:** add tusk_reset tool ([#530](https://github.com/germanamz/tusk/issues/530)) ([8142e0d](https://github.com/germanamz/tusk/commit/8142e0d2978432464b812176f99235bb699658b0))
* **mcp:** converge sibling daemons after a reset ([#531](https://github.com/germanamz/tusk/issues/531)) ([fd10a2b](https://github.com/germanamz/tusk/commit/fd10a2b495486d67da6c71dfa68e5c47deb89975))
* **mcp:** fsnotify fast-path for index-reset convergence ([#532](https://github.com/germanamz/tusk/issues/532)) ([06f3a8f](https://github.com/germanamz/tusk/commit/06f3a8f184587e2c19e26837e3a8d0a7a0426a7c))
* **reset:** add reset core (lock, delete, reopen, bump) ([#527](https://github.com/germanamz/tusk/issues/527)) ([750b3d9](https://github.com/germanamz/tusk/commit/750b3d92834760693a02d4a7ef965a944675be63))


### Bug Fixes

* **index:** drop orphaned WAL/SHM sidecars on index rebuild ([#524](https://github.com/germanamz/tusk/issues/524)) ([8d0360d](https://github.com/germanamz/tusk/commit/8d0360da5f2660cd15ef840757a37e34d6495107))
* **watcher:** honor ignore rules so .tusk/ writes don't loop reindex ([#534](https://github.com/germanamz/tusk/issues/534)) ([5210b48](https://github.com/germanamz/tusk/commit/5210b483a5f9bcbdcef964c8ffbfd48637dab6ff))


### Code Refactoring

* **mcp:** swappable runtime infrastructure (snapshot model) ([#529](https://github.com/germanamz/tusk/issues/529)) ([f53f5f9](https://github.com/germanamz/tusk/commit/f53f5f90e17499bc435b45edde31eea295b5bc4e))

## [1.8.0](https://github.com/germanamz/tusk/compare/v1.7.3...v1.8.0) (2026-06-03)


### Features

* **index:** content-addressed embeddings, no re-embed on restructure (phase 3/5) ([#517](https://github.com/germanamz/tusk/issues/517)) ([9d21272](https://github.com/germanamz/tusk/commit/9d2127265fe95f0e74f5a4fbf3f21a1b97fcca8c))
* **subunit:** compute structural sub-unit addresses in parser (phase 1/5) ([#514](https://github.com/germanamz/tusk/issues/514)) ([68622e8](https://github.com/germanamz/tusk/commit/68622e8f18fd6e46a0b13a14bc011e3e6478d2da))
* **subunit:** flip sub-unit identity to structural addresses (phase 2/5) ([#516](https://github.com/germanamz/tusk/issues/516)) ([42fa87f](https://github.com/germanamz/tusk/commit/42fa87fbfe34f1857750a06728e6cf8c9a28627f))


### Bug Fixes

* **doctor:** close [#513](https://github.com/germanamz/tusk/issues/513) — stop flagging never-embedded sections (phase 4/5) ([#518](https://github.com/germanamz/tusk/issues/518)) ([ac78aa4](https://github.com/germanamz/tusk/commit/ac78aa4bc92c6d6dd35843e0cb5d85b072cd7baa))

## [1.7.3](https://github.com/germanamz/tusk/compare/v1.7.2...v1.7.3) (2026-06-01)


### Bug Fixes

* **install:** deliver man pages so 'man tusk' works ([#511](https://github.com/germanamz/tusk/issues/511)) ([fb5d093](https://github.com/germanamz/tusk/commit/fb5d0937f81b86895f9d2b99c420b989d0355877))

## [1.7.2](https://github.com/germanamz/tusk/compare/v1.7.1...v1.7.2) (2026-05-31)


### Bug Fixes

* **workflow:** stop reindex flagging valid non-initial states as drift ([#507](https://github.com/germanamz/tusk/issues/507)) ([1f7a869](https://github.com/germanamz/tusk/commit/1f7a86982124de01dbb2c2a6d94b433b8c10d344)), closes [#497](https://github.com/germanamz/tusk/issues/497)


### Code Refactoring

* **aliasdispatch:** extract internal/argval and collapse coercion via argReader ([#503](https://github.com/germanamz/tusk/issues/503)) ([58c5a55](https://github.com/germanamz/tusk/commit/58c5a55ff73f11dd34351bcf429f0ef2513e9c0f))
* **behavior:** collapse the eight dispatch chains onto a generic entry ([#492](https://github.com/germanamz/tusk/issues/492)) ([c48c0f5](https://github.com/germanamz/tusk/commit/c48c0f56c187e186ebb8f07f3d5cbedb6af9c87c))
* **cli:** extract resolveWorkspace/openStore preamble and shared helpers ([#505](https://github.com/germanamz/tusk/issues/505)) ([80f3149](https://github.com/germanamz/tusk/commit/80f3149144d65417f0bb574fede66d6d7937c610))
* **doctor:** share edge comparator, legacy predicate, unify ref messages ([#502](https://github.com/germanamz/tusk/issues/502)) ([6237120](https://github.com/germanamz/tusk/commit/623712022c0b510e3e6ef78e6c0b9a6f31d92250))
* **filter:** dedupe binary-join SQL, the recursive CTE, and error appends ([#496](https://github.com/germanamz/tusk/issues/496)) ([75cf9cb](https://github.com/germanamz/tusk/commit/75cf9cb12533f64707d3acb1cbd07e4c23e5fe3a))
* **index:** dedupe edge column list and IN-clause placeholders ([#494](https://github.com/germanamz/tusk/issues/494)) ([e2b0d08](https://github.com/germanamz/tusk/commit/e2b0d088f7f8417bef459270ac98444bf2bb1e01))
* **manifest:** reuse isRefProperty and extract a graph-expansion decode helper ([#495](https://github.com/germanamz/tusk/issues/495)) ([3c237aa](https://github.com/germanamz/tusk/commit/3c237aac714a47917fe7f51cfa73033fa3c3f687))
* **mcp:** share node-write classify, alias-errors, required-strings, compaction ([#504](https://github.com/germanamz/tusk/issues/504)) ([cdc7eb6](https://github.com/germanamz/tusk/commit/cdc7eb6565206008ae3de8e241ffd5d16e554410))
* **node:** dedupe type-mismatch, edge write-back, and Create/Modify persist ([#498](https://github.com/germanamz/tusk/issues/498)) ([e840bde](https://github.com/germanamz/tusk/commit/e840bdef87d40716e276a65d0100ab7748b3bae8))
* **query:** dedupe graph-expansion blend, paging, and the compile preamble ([#499](https://github.com/germanamz/tusk/issues/499)) ([bf7d8cf](https://github.com/germanamz/tusk/commit/bf7d8cf5aa0d4f984c8dd932c94fe2a0695e5c2d))
* **reindex:** factor orphan-reap lease release and embed retry-or-drop ([#501](https://github.com/germanamz/tusk/issues/501)) ([6d238d8](https://github.com/germanamz/tusk/commit/6d238d8410c80729ec5f1c47a1abb997601282e9))
* **render:** extract maxWidth helper for the compact column passes ([#493](https://github.com/germanamz/tusk/issues/493)) ([bea4799](https://github.com/germanamz/tusk/commit/bea47995427762d9b020f343ec87ce6def3e19f0))
* **subunit:** add stringProperty twin and merge identical code-block cases ([#500](https://github.com/germanamz/tusk/issues/500)) ([594491e](https://github.com/germanamz/tusk/commit/594491ea3bb8dc5491d8ce4da09b5fab7f976772))

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
