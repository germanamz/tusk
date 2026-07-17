---
type: package
title: internal/webapp — unified local web app server
import-path: github.com/germanamz/tusk/internal/webapp
status: stable
---

# internal/webapp

Serves `tusk web`: one read-only, live-updating web app for the vault over a
single loopback HTTP server. It composes the two view providers —
`internal/graphview` and `internal/bookview` — as pure API providers behind
one mux: graphview's JSON + SSE routes mount under `/api/graph/*`, bookview's
under `/api/read/*`. Consolidating them here resolved the old `/api/node`
path collision (both views used to register that route on their own separate
servers). The single embedded Vite SPA (`web/`, built into
`internal/webapp/dist`) serves the graph view at `/` and the reading view at
`/read`; a top-bar light/dark/system theme toggle switches the shared shell.

webapp owns everything that used to be duplicated per view: the single
Host-header guard (from `internal/webui`), one unified Content-Security-Policy
covering both views, the one embedded frontend `dist/`, a `/healthz` liveness
check, and the SPA history fallback that serves `index.html` for deep links
like `/read` so a client-side route survives a hard refresh. The two views
keep only their own SSE hubs and change sources; webapp aggregates both hubs
for the CLI status line. Like each view, webapp receives an already-open
workspace handle through `Deps` and never opens the workspace itself.

## Public surface

- `New(deps Deps) *Server` — constructs the composed server; does not bind a
  port.
- `(*Server).Handler() http.Handler` — the whole app's HTTP handler: the
  shared mux (graphview + bookview routes, healthz, static frontend, SPA
  fallback) wrapped in the Host guard and the unified CSP.
- `(*Server).Run(ctx context.Context)` — runs both views' SSE
  change-detection loops.
- `(*Server).ClientCount() int` — connected SSE clients across both hubs, for
  the CLI status line.
- `DefaultAddr string` — the default loopback bind address (`127.0.0.1:7373`).
- `Deps` — bundles the two views' dependency sets plus the shared Host
  allowlist (which used to live on each view) and `Logger`.
- **Routes** (mounted on the shared mux):

  | Route | Behavior |
  |---|---|
  | `GET /healthz` | plain liveness check |
  | `/api/graph/*` | graphview's JSON + SSE routes (see `docs/packages/graphview.md`) |
  | `/api/read/*` | bookview's read routes (see `docs/packages/bookview.md`) |
  | `GET /` | the embedded unified SPA, graph view |
  | `GET /read` | the embedded unified SPA, reading view |
  | (deep links) | any non-API GET that isn't a real static asset falls through to `index.html` (SPA history fallback) |

`*Server` implements the `webViewServer` interface the `tusk web` command
drives (`Handler`, `Run`, `ClientCount`) — the same shape the old standalone
graph and book servers each implemented, now one implementation aggregating
both hubs.

## Unified CSP

One policy is the backstop behind client-side sanitization for both views
(the reading view renders untrusted vault content — arbitrary markdown,
including raw HTML — as DOM). A single server can't send two different
`Content-Security-Policy` headers, so merging the two views' policies into one
is why the CSP moved up here. It is the union of what each view needs:

- `script-src 'self'` — scripts load only from the same-origin bundle; never
  inline, never remote. This is the core control.
- `worker-src 'self' blob:` — the graph's semantic-layout worker (umap-js)
  runs in a Web Worker instantiated from a `blob:` URL, so worker sources must
  allow `blob:` on top of `'self'`.
- `style-src 'self' 'unsafe-inline'` — KaTeX and mermaid inject inline styles;
  inline style is unavoidable for them, but inline script never is.
- `img-src 'self' data:` — vault images load same-origin via
  `/api/read/asset`; `data:` covers inline and generated images. Node content
  can't silently phone home through a remote image.
- `base-uri 'none'`, `object-src 'none'` — no `<base>` hijack, no plugin
  embeds.

## Notes

- The `/api/node` collision is why composition lives in a real package rather
  than in thin `http.ServeMux` glue in the command. Both views historically
  registered `/api/node` on their own servers; mounting them under distinct
  `/api/graph` and `/api/read` bases (each view exposes its base as `APIBase`)
  disambiguates every route, not just that one.
- The SPA history fallback serves `index.html` for any GET that isn't an API
  route or a real static asset, so deep links (`/read`) and client-side
  navigation survive a hard refresh. API routes 404 as themselves rather than
  falling through to HTML.
- The deprecated `tusk graph` and `tusk book` aliases are thin wrappers: each
  prints a deprecation notice and launches `tusk web` pinned to its view
  (graph on `127.0.0.1:7373`, book on `127.0.0.1:7474`), so old scripts and
  muscle memory keep working.
- The embedded `dist/` is the one unified SPA, built by `make web` /
  `make frontend` from `web/` into `internal/webapp/dist` and `//go:embed`-ed
  here. The old per-view dists (`internal/graphview/dist`,
  `internal/bookview/dist`) are gone. Never edit the built `dist/` by hand;
  run `make web` to rebuild.

Backs `tusk web`, and the deprecated `tusk graph` / `tusk book` aliases.
