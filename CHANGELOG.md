# Changelog

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
