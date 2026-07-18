---
type: package
title: internal/bookview — read-only reading UI API provider
import-path: github.com/germanamz/tusk/internal/bookview
status: stable
---

# internal/bookview

Provides the read-only, live-updating reading UI as a set of read routes,
mounted by `internal/webapp` under `/api/read/*` (`const APIBase =
"/api/read"`). It is no longer a standalone server: `internal/webapp` owns
the loopback bind, the single Host-header guard, the unified CSP, the
embedded frontend, one healthz, and the SPA history fallback, and calls
`RegisterRoutes(mux, base)` to graft bookview's routes onto the shared mux.
bookview receives an open workspace handle via `Deps` and never opens the
workspace itself. Every route is a read — `POST /api/read/search` carries a
query body, not a mutation. Rendering markdown to HTML is the frontend's
job: the API returns the raw body (frontmatter stripped) and the browser —
the reading modules of the one unified Vite SPA, under `web/src/read/` —
renders math (KaTeX), diagrams (mermaid), and navigable wikilinks, and
sanitizes the result.

## Public surface

- `New(deps Deps) *Server` — constructs the API provider; does not bind a
  port.
- `(*Server).Run(ctx context.Context)` — starts the SSE change-detection
  loop.
- `(*Server).RegisterRoutes(mux *http.ServeMux, base string)` — grafts
  bookview's routes onto a caller-supplied mux under `base`
  (`internal/webapp` passes `APIBase`). Replaces the old `Handler()`;
  bookview no longer builds its own mux, Host guard, or CSP —
  `internal/webapp` owns all three.
- `(*Server).ClientCount() int` — connected SSE clients; `internal/webapp`
  aggregates it with graphview's for the CLI status line.
- `APIBase = "/api/read"` — the mount prefix `internal/webapp` grafts
  bookview under.
- `Deps` — bundles everything the provider needs: `Root`, `Nodes`
  (`NodeSource`), `Edges` (`EdgeSource`), `Search` (`Searcher`; nil makes
  `/api/read/search` report 503), `Related` (`RelatedSource`; nil makes the
  rail come back empty), `Meta` (`webui.MetaReader`; nil reports a
  constant zero change signal), `Logger`, `PollInterval`. The Host
  allowlist moved up to `internal/webapp` along with the guard.
- **Routes** (registered under `APIBase`; healthz, the embedded frontend,
  and the SPA history fallback are served by `internal/webapp`, not here):

  | Route | Behavior |
  |---|---|
  | `GET /api/read/index` | the Contents pane's node index — every file-level node |
  | `GET /api/read/node/{id...}` | one node's reading payload: metadata, stripped markdown, links, resolved wikilinks |
  | `GET /api/read/asset/{path...}` | one vault file served verbatim (images, etc.), guarded against traversal |
  | `POST /api/read/search` | structural / semantic / graph-expanded search over `query.Run` |
  | `GET /api/read/related/{id...}` | the embedder-free Related rail |
  | `GET /api/read/stream` | SSE change stream (`event: change`) |

- **Payload types:**
  - `IndexNode{ID, Type, Title, Path}` / `IndexResponse{Nodes}` — the
    `/api/read/index` payload. There is no `Parent` field: every row
    `ListFileNodes` returns has `parent_id` NULL by construction, so a
    `Parent` field would always be empty. The Contents pane derives its
    tree from `Path` instead (`specs/x.md` nests under `specs/`).
  - `NodeReadPayload{ID, Type, Title, Path, Properties, Markdown,
    Links{Out, In}, Wikilinks}` — the `/api/read/node/{id}` payload. `Markdown`
    is the raw body with frontmatter stripped
    (`render.StripFrontmatter`). `Wikilinks` maps every raw `[[target]]`
    found in the body to its resolution (`WikilinkTarget{ID, Title,
    Exists}`).
  - `LinkRef{ID, Title, Type, EdgeType}` — one entry in a node's reading
    rails (see below).
  - `SearchRequest` / `SearchResponse` / `Match` — the search payload.
    `Match`'s score-breakdown fields (`CosineScore`, `GraphScore`,
    `FinalScore`, `Distance`) populate only when the request set
    `Explain` and graph expansion actually ran.
  - `RelatedNode{ID, Title, Type, GraphScore, Distance}` /
    `RelatedResponse{Related}` — the Related rail payload.
- **Dependency interfaces:** `NodeSource`, `EdgeSource` (satisfied by
  `*index.NodeRepo` / `*index.EdgeRepo`); `Searcher`, `RelatedSource`
  (satisfied by the adapters below).
- `NewSearcher(db, workspaceManifest, embedder, embeddings, nodes, edges, root) Searcher`
  and `NewRelated(edges, workspaceManifest, nodes) RelatedSource` —
  concrete implementations wired when `tusk web` composes the reading API
  provider (`internal/webapp`).

## Links: sub-unit far ends roll up to their file

`webui.Neighbors` drops any neighbor whose far end is a sub-unit — the
file-level rule graphview has always used. Sub-units source real edges of
their own (a link authored in a note's `c#S1` section is a row `c#S1 →
a`), so under that rule a note's Backlinks rail can silently omit most of
its actual backlinks in a vault with sub-unit indexing on. A reading UI
can't afford that: "what links here" is a core reading affordance, and
under-reporting it reads as working.

bookview walks the index itself (`linksOf`) instead of calling
`webui.Neighbors`, and rolls each sub-unit far end up to the file it
belongs to — in both directions, de-duplicated. Three sections of `c` all
linking to `a` surface as one `c` entry in `a`'s inbound rail, not three.
Structural (`contains`) edges are excluded from the rails: without that
exclusion, a file's own containment edge into its sub-units would roll
back onto itself and every note would appear to link to itself.

## Related: an embedder-free, distance-ranked rail

`RelatedSource.Related` walks outward from the focus node via
`internal/graphexpand` — no embedder anywhere in the loop, so the rail
keeps working when the embedding provider is down. It seeds the walk at
the focus node with a cosine score of 1.0 (there's no query embedding to
seed with; this is a structural neighborhood, not a ranked search), then
blends with `graphexpand.Blender`.

Every walked neighbor's raw cosine is 0 and the seed's is fixed at 1.0, so
the wire `graph_score` comes back undecayed (`1.0`) for distance-1
neighbors and equal to the configured `weight` for distance-2 neighbors —
and the blended score that actually orders the walk decays multiplicatively
per hop (`weight` at distance 1, `weight²` at distance 2). Every neighbor
at the same hop-distance is scored identically, so the rail ranks by
distance and then falls back to alphabetical node id: it cannot
discriminate further within a distance band.

## Notes

- The asset guard (`resolveVaultAsset` in `asset.go`) is the one route
  that hands a request-supplied path to the filesystem — every other
  route resolves its path from the index walk, which is vault-relative by
  construction. It refuses `..` as authored, before `filepath.Clean` can
  fold it away; it refuses any dot-prefixed segment on both the request
  path and, separately, the resolved and symlink-evaluated path (a symlink
  can point at a dot-directory like `.tusk` or `.git` without the request
  path ever naming one); and it checks containment against the resolved
  vault root, so a `/vault` vs `/vault-secrets` prefix collision can't
  leak a sibling directory.
- The reading UI renders untrusted vault content — arbitrary markdown,
  including raw HTML — as the browser's own DOM, so a restrictive CSP is
  essential as the backstop behind client-side sanitization. bookview no
  longer sets it: the CSP is now the single unified policy in
  `internal/webapp`, covering both views (strict `script-src 'self'` —
  KaTeX and mermaid need `'unsafe-inline'` style, but never inline script;
  `img-src 'self' data:`, so vault images load same-origin via
  `/api/read/asset` and node content can't silently phone home through a
  remote image; `base-uri 'none'`, `object-src 'none'`). See
  `docs/packages/webapp.md` for the full directive list.
- bookview still builds on `internal/webui` for the SSE hub and the change
  source. The Host-header guard, the static handler, and the CSP no longer
  live here — `internal/webapp` owns them and shares them across both views
  (see `docs/packages/webapp.md`). The neighbor projection
  (`webui.Neighbors`) is graphview's, not bookview's: bookview
  re-implements its own incident-edge walk instead (see "Links" above). The
  routes and payloads above are bookview's own.

Backs the reading view of `tusk web` (`/read`), reachable through the
deprecated `tusk book` alias.
