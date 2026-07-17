// markdown-it-texmath ships no TypeScript types (no `types`/`typings` field in
// its package.json, no `.d.ts` in the published files, and no
// `@types/markdown-it-texmath` exists upstream). This is a local shim typing
// only the surface render.ts actually calls: `md.use(texmath, options)` with
// the `dollars` delimiter set and a KaTeX-shaped engine.
//
// Pinned exact (no caret) in package.json, so a version bump — the only thing
// that could invalidate this shim — is always an explicit, reviewed change;
// re-check texmath.js's option handling then.
declare module 'markdown-it-texmath' {
  import type MarkdownIt from 'markdown-it'

  export interface TexmathEngine {
    renderToString(expression: string, options?: Record<string, unknown>): string
  }

  export interface TexmathOptions {
    engine: TexmathEngine
    delimiters?: string | string[]
    outerSpace?: boolean
    katexOptions?: Record<string, unknown>
  }

  const texmath: MarkdownIt.PluginWithOptions<TexmathOptions>
  export default texmath
}
