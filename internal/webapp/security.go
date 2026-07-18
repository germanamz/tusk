package webapp

import "net/http"

// withSecurityHeaders sets one Content-Security-Policy covering both views on
// every response. The reading view renders untrusted vault markdown (including
// raw HTML) into the browser's own DOM, so the policy is the backstop behind
// client-side sanitization; the graph view draws a WebGL canvas and spawns a
// Vite-bundled web worker for its semantic-layout projection.
//
// script-src stays strict 'self' (no 'unsafe-inline', no 'unsafe-eval'): the
// bundle is same-origin, so injected <script> in a node body cannot run, and
// neither three.js nor the layout worker needs eval. 'unsafe-inline' style is
// required — KaTeX and mermaid inject inline <style>/style attributes as they
// render, and the graph frontend ships an inline <style> block. worker-src
// 'self' blob: covers the bundled layout worker. img-src 'self' data: allows
// same-origin vault assets (/api/read/asset) and inline data URIs while
// blocking silent remote image loads that would leak what the user is reading.
// connect-src 'self' permits the two EventSource streams and the fetch API
// calls. base-uri 'none' stops an injected <base> from re-pointing relative
// fetches; object-src 'none' drops the legacy plugin vector. Fonts fall under
// default-src 'self', so KaTeX's woff2 files stay same-origin (the frontend
// build keeps assetsInlineLimit at 0 rather than inlining them as data: URIs,
// which default-src 'self' would block).
func withSecurityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; " +
		"script-src 'self'; worker-src 'self' blob:; connect-src 'self'; base-uri 'none'; object-src 'none'"

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", csp)
		writer.Header().Set("X-Content-Type-Options", "nosniff")

		next.ServeHTTP(writer, request)
	})
}
